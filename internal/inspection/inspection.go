// Package inspection owns a single short-lived, per-device transparent proxy.
// No capture is persisted; the only durable secret is mitmproxy's CA in confdir.
package inspection

import (
	"bufio"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/devices"
)

//go:embed addon.py
var addon embed.FS

const ModuleName = "inspection"
const table = "olr_inspection"
const duration = 15 * time.Minute
const port = "18081"
const maxEvents = 200

// Devices is read again during a session. A stale address must never authorize
// another device; inability to establish ownership ends the capture.
type Devices interface {
	List(context.Context) ([]devices.Resolved, []devices.Problem, error)
}

type Event struct {
	Kind            string            `json:"kind"`
	At              float64           `json:"at"`
	Method          string            `json:"method,omitempty"`
	URL             string            `json:"url,omitempty"`
	Status          int               `json:"status,omitempty"`
	Target          string            `json:"target,omitempty"`
	Reason          string            `json:"reason,omitempty"`
	RequestHeaders  map[string]string `json:"request_headers,omitempty"`
	ResponseHeaders map[string]string `json:"response_headers,omitempty"`
}

type Status struct {
	Active    bool      `json:"active"`
	MAC       string    `json:"mac,omitempty"`
	Expires   time.Time `json:"expires,omitempty"`
	Events    []Event   `json:"events"`
	CAPresent bool      `json:"ca_present"`
}

type Service struct {
	Devices Devices
	Enabled bool
	Dir     string
	mu      sync.Mutex
	session *session
}

type session struct {
	mac      string
	ip       string
	expires  time.Time
	cancel   context.CancelFunc
	cmd      *exec.Cmd
	events   []Event
	retrying bool
}

func (s *Service) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Path: "/status", Tool: "status", Summary: "Show the temporary device request inspection session.", Handler: s.status},
		{Method: "POST", Path: "/session", Summary: "Start a 15-minute inspection for one device MAC.", Mutating: true, Handler: s.start},
		{Method: "DELETE", Path: "/session", Summary: "Stop inspection, remove interception and clear captured requests.", Mutating: true, Handler: s.stop},
		{Method: "GET", Path: "/ca", Tool: "show ca", Summary: "Download the public debugging CA certificate, never its private key.", Handler: s.ca},
	}
}

func (s *Service) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{Events: []Event{}}
	if q := s.session; q != nil {
		out.Active, out.MAC, out.Expires = true, q.mac, q.expires
		out.Events = append(out.Events, q.events...)
	}
	_, err := os.Stat(filepath.Join(s.Dir, "mitmproxy-ca-cert.pem"))
	out.CAPresent = err == nil
	return out
}

func (s *Service) status(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, http.StatusOK, s.snapshot())
}

func (s *Service) ca(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "mitmproxy-ca-cert.pem"))
	if err != nil {
		core.WriteError(w, http.StatusNotFound, "start a session to generate the debugging CA")
		return
	}
	// RouteTable/MCP share JSON responses; the PEM is public, the key is not.
	core.WriteJSON(w, http.StatusOK, map[string]string{"pem": string(data)})
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MAC string `json:"mac"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		core.WriteError(w, http.StatusBadRequest, "expected a device MAC")
		return
	}
	if !s.Enabled {
		core.WriteError(w, http.StatusServiceUnavailable, "inspection is available only on the live Linux router")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != nil {
		core.WriteError(w, http.StatusConflict, "a device is already being inspected")
		return
	}
	normalized, macErr := core.NormalizeMAC(body.MAC)
	if macErr != nil {
		core.WriteError(w, http.StatusBadRequest, macErr.Error())
		return
	}
	ip, err := s.deviceIP(r.Context(), normalized)
	if err != nil {
		core.WriteError(w, http.StatusConflict, err.Error())
		return
	}
	if _, err = exec.LookPath("mitmdump"); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "install mitmproxy (mitmdump) on the router first")
		return
	}
	if _, err = exec.LookPath("nft"); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "nft is required for transparent inspection")
		return
	}
	if err = os.MkdirAll(s.Dir, 0o700); err != nil {
		core.WriteError(w, 500, err.Error())
		return
	}
	if err = os.Chmod(s.Dir, 0o700); err != nil {
		core.WriteError(w, 500, err.Error())
		return
	}
	script := filepath.Join(s.Dir, "olr-addon.py")
	source, _ := addon.ReadFile("addon.py")
	if err = os.WriteFile(script, source, 0o600); err != nil {
		core.WriteError(w, 500, err.Error())
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, "mitmdump", "--mode", "transparent", "--listen-host", "0.0.0.0", "--listen-port", port,
		"--set", "confdir="+s.Dir, "--set", "termlog_verbosity=error", "-s", script)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		core.WriteError(w, 500, err.Error())
		return
	}
	cmd.Stderr = nil
	if err = cmd.Start(); err != nil {
		cancel()
		core.WriteError(w, 500, err.Error())
		return
	}
	ready := false
	for i := 0; i < 40; i++ {
		conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:"+port, 100*time.Millisecond)
		if dialErr == nil {
			conn.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !ready {
		cancel()
		_ = cmd.Wait()
		core.WriteError(w, http.StatusServiceUnavailable, "mitmproxy did not become ready; no traffic was redirected")
		return
	}
	// A fresh table is owned wholly by this module. Cleanup of a previous
	// daemon's table also runs at startup; never modify someone else's table.
	// Recheck after the process starts: an address may change during startup.
	current, checkErr := s.deviceIP(r.Context(), normalized)
	if checkErr != nil || current != ip {
		cancel()
		_ = cmd.Wait()
		core.WriteError(w, http.StatusConflict, "device identity changed before interception; nothing was redirected")
		return
	}
	if err = install(ip); err != nil {
		cancel()
		_ = cmd.Wait()
		if cleanupErr := cleanup(); cleanupErr != nil {
			// A partially installed rule might remain. Preserve a session
			// owner so cleanup is retried and no new session can start.
			q := &session{mac: normalized, ip: ip, expires: time.Now(), cancel: cancel, cmd: cmd}
			s.session = q
			q.retrying = true
			go s.retryCleanup(q)
			core.WriteError(w, 500, "inspection cleanup failed; retrying: "+cleanupErr.Error())
			return
		}
		core.WriteError(w, 500, "could not install interception: "+err.Error())
		return
	}
	q := &session{mac: normalized, ip: ip, expires: time.Now().Add(duration), cancel: cancel, cmd: cmd}
	s.session = q
	go s.consume(q, stdout)
	go s.watch(q)
	core.WriteJSON(w, http.StatusOK, s.snapshotLocked())
}

func (s *Service) snapshotLocked() Status {
	q := s.session
	return Status{Active: true, MAC: q.mac, Expires: q.expires, Events: []Event{}, CAPresent: true}
}

func (s *Service) deviceIP(ctx context.Context, mac string) (string, error) {
	list, problems, err := s.Devices.List(ctx)
	if err != nil || len(problems) != 0 {
		return "", errors.New("device identity could not be verified")
	}
	var selected *devices.Resolved
	owners := map[string]int{}
	for i := range list {
		if list[i].Presence == nil {
			continue
		}
		for _, raw := range list[i].Presence.IPs {
			owners[strings.ToLower(raw)]++
		}
		if strings.EqualFold(list[i].MAC, mac) {
			selected = &list[i]
		}
	}
	if selected == nil || !selected.Online() {
		return "", errors.New("device must be online with a currently observed address")
	}
	// DHCP leases can remain active after a client has left and its address
	// has been reassigned. Require a live neighbour observation as well.
	hasNeighbour := false
	for _, source := range selected.Presence.Sources {
		if source == devices.SourceARP {
			hasNeighbour = true
		}
	}
	if !hasNeighbour {
		return "", errors.New("device needs a current neighbour-table observation")
	}
	if len(selected.Presence.IPs) != 1 {
		return "", errors.New("inspection currently requires exactly one observed IPv4 address; IPv6 or multiple addresses are not yet supported")
	}
	ip, err := netip.ParseAddr(selected.Presence.IPs[0])
	if err != nil || !ip.Is4() || !ip.IsPrivate() || owners[strings.ToLower(ip.String())] != 1 {
		return "", errors.New("device needs one unique private IPv4 address")
	}
	return ip.String(), nil
}

func (s *Service) consume(q *session, stdout interface{ Read([]byte) (int, error) }) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var event Event
		if json.Unmarshal(scanner.Bytes(), &event) != nil || (event.Kind != "request" && event.Kind != "failed") {
			continue
		}
		s.mu.Lock()
		if s.session == q {
			q.events = append(q.events, event)
			if len(q.events) > maxEvents {
				q.events = q.events[len(q.events)-maxEvents:]
			}
		}
		s.mu.Unlock()
	}
}

func (s *Service) watch(q *session) {
	done := make(chan struct{})
	go func() { _ = q.cmd.Wait(); close(done) }()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			s.end(q)
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			ip, err := s.deviceIP(ctx, q.mac)
			cancel()
			if err != nil || ip != q.ip || time.Now().After(q.expires) {
				s.end(q)
				return
			}
		}
	}
}

func (s *Service) end(q *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != q {
		return
	}
	if err := cleanup(); err != nil {
		// Keep the session visible and retry; do not claim the rule is gone.
		if !q.retrying {
			q.retrying = true
			go s.retryCleanup(q)
		}
		return
	}
	q.cancel()
	s.session = nil
}

func (s *Service) retryCleanup(q *session) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for range ticker.C {
		s.mu.Lock()
		active := s.session == q
		s.mu.Unlock()
		if !active {
			return
		}
		s.end(q)
	}
}

func (s *Service) stop(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	q := s.session
	s.mu.Unlock()
	if q != nil {
		s.end(q)
	}
	if s.snapshot().Active {
		core.WriteError(w, http.StatusInternalServerError, "interception cleanup failed; retrying automatically")
		return
	}
	core.WriteJSON(w, http.StatusOK, s.snapshot())
}

// Close is called on daemon shutdown; the next startup also removes any table
// left by an unclean shutdown before accepting requests.
func (s *Service) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session == nil {
		return nil
	}
	if err := cleanup(); err != nil {
		return err
	}
	s.session.cancel()
	s.session = nil
	return nil
}
func Recover() error {
	if _, err := exec.LookPath("nft"); err != nil {
		return nil
	}
	return cleanup()
}

func nft(args ...string) error {
	out, err := exec.Command("nft", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("nft %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
func cleanup() error {
	out, err := exec.Command("nft", "delete", "table", "inet", table).CombinedOutput()
	if err == nil || strings.Contains(string(out), "No such file or directory") {
		return nil
	}
	return fmt.Errorf("removing inspection table: %w: %s", err, strings.TrimSpace(string(out)))
}
func install(ip string) error {
	// Restrict to TCP 80/443 from one source, before NAT. Exclude local and
	// private destinations so router administration and LAN services survive.
	if err := nft("add", "table", "inet", table); err != nil {
		return err
	}
	if err := nft("add", "chain", "inet", table, "prerouting", "{ type nat hook prerouting priority dstnat - 1; policy accept; }"); err != nil {
		return err
	}
	if err := nft("add", "chain", "inet", table, "input", "{ type filter hook input priority -1; policy accept; }"); err != nil {
		return err
	}
	// A transparent listener must bind all local addresses. Do not expose it
	// as a direct LAN proxy: only redirected connections may enter.
	if err := nft("add", "rule", "inet", table, "input", "iifname", "!=", "lo", "tcp", "dport", port, "ct", "status", "!=", "dnat", "drop"); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"fib", "daddr", "type", "local", "return"},
		{"ip", "daddr", "10.0.0.0/8", "return"},
		{"ip", "daddr", "172.16.0.0/12", "return"},
		{"ip", "daddr", "192.168.0.0/16", "return"},
		{"ip", "saddr", ip, "tcp", "dport", "{", "80,", "443", "}", "redirect", "to", ":" + port},
	} {
		if err := nft(append([]string{"add", "rule", "inet", table, "prerouting"}, args...)...); err != nil {
			return err
		}
	}
	return nil
}
