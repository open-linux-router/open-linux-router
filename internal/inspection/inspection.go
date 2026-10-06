// Package inspection owns a single short-lived, per-device transparent proxy.
// No capture is persisted; the only durable secret is mitmproxy's CA in confdir.
package inspection

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

//go:embed addon.py
var addon embed.FS

const ModuleName = "inspection"
const table = "olr_inspection"
const duration = 15 * time.Minute
const port = "18081"
const maxEvents = 200

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
	Active        bool      `json:"active"`
	MAC           string    `json:"mac,omitempty"`
	IP            string    `json:"ip,omitempty"`
	Expires       time.Time `json:"expires,omitempty"`
	Events        []Event   `json:"events"`
	CAPresent     bool      `json:"ca_present"`
	Redirected    *uint64   `json:"redirected_packets,omitempty"`
	InputSeen     *uint64   `json:"input_packets,omitempty"`
	InputDropped  *uint64   `json:"input_dropped_packets,omitempty"`
	ProxyAccepted uint64    `json:"proxy_accepted"`
}

type Service struct {
	Enabled bool
	Dir     string
	mu      sync.Mutex
	session *session
}

type session struct {
	mac      string
	ip       string
	expires  time.Time
	cancel   func()
	done     chan struct{}
	events   []Event
	accepted uint64
	retrying bool
}

func (s *Service) Routes() []core.Route {
	return []core.Route{
		{Method: "GET", Path: "/status", Tool: "status", Summary: "Show the temporary device request inspection session.", Handler: s.status},
		{Method: "POST", Path: "/session", Summary: "Start a 15-minute inspection for an explicitly selected IPv4 address.", Mutating: true, Handler: s.start},
		{Method: "DELETE", Path: "/session", Summary: "Stop inspection, remove interception and clear captured requests.", Mutating: true, Handler: s.stop},
		{Method: "GET", Path: "/ca", Tool: "show ca", Summary: "Download the public debugging CA certificate, never its private key.", Handler: s.ca},
	}
}

func (s *Service) snapshot() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := Status{Events: []Event{}}
	if q := s.session; q != nil {
		out.Active, out.MAC, out.IP, out.Expires = true, q.mac, q.ip, q.expires
		out.Events = append(out.Events, q.events...)
		out.ProxyAccepted = q.accepted
		if n, err := inspectionCount("redirected"); err == nil {
			out.Redirected = &n
		}
		if n, err := inspectionCount("input_seen"); err == nil {
			out.InputSeen = &n
		}
		if n, err := inspectionCount("input_dropped"); err == nil {
			out.InputDropped = &n
		}
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

// PublicCA serves only the public certificate, so a phone can scan a URL
// without receiving the router's administrative API token.
func (s *Service) PublicCA(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(s.Dir, "mitmproxy-ca-cert.pem"))
	if err != nil {
		http.Error(w, "start an inspection session to generate the CA", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/x-x509-ca-cert")
	w.Header().Set("Content-Disposition", `attachment; filename="olr-debugging-ca.crt"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (s *Service) start(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MAC string `json:"mac"`
		IP  string `json:"ip"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024)).Decode(&body); err != nil {
		core.WriteError(w, http.StatusBadRequest, "expected a device MAC and IPv4 address")
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
	ip, err := netip.ParseAddr(strings.TrimSpace(body.IP))
	if err != nil || !ip.Is4() || ip.IsUnspecified() || ip.IsMulticast() || ip.String() == "255.255.255.255" {
		core.WriteError(w, http.StatusBadRequest, "expected a unicast IPv4 address to inspect")
		return
	}
	target := ip.String()
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
	cmd := exec.Command("mitmdump", "--mode", "transparent", "--listen-host", "0.0.0.0", "--listen-port", port,
		"--set", "confdir="+s.Dir, "--set", "termlog_verbosity=error", "-s", script)
	// mitmdump may fork; its launcher is not the lifetime of the listener.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	stdout, output, err := os.Pipe()
	if err != nil {
		core.WriteError(w, 500, err.Error())
		return
	}
	cmd.Stdout = output
	cmd.Stderr = nil
	if conn, dialErr := net.DialTimeout("tcp", "127.0.0.1:"+port, 100*time.Millisecond); dialErr == nil {
		conn.Close()
		_ = stdout.Close()
		_ = output.Close()
		core.WriteError(w, http.StatusServiceUnavailable, "inspection port is already in use; stop the stale proxy before starting")
		return
	}
	if err = cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = output.Close()
		core.WriteError(w, 500, err.Error())
		return
	}
	_ = output.Close()
	cancel := func() { _ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	// The launcher can exit while its listener child still owns stdout.
	go func() { _ = cmd.Wait() }()
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
		_ = stdout.Close()
		core.WriteError(w, http.StatusServiceUnavailable, "mitmproxy did not become ready; no traffic was redirected")
		return
	}
	// A fresh table is owned wholly by this module. Cleanup of a previous
	// daemon's table also runs at startup; never modify someone else's table.
	if err = install(target); err != nil {
		if cleanupErr := cleanup(); cleanupErr != nil {
			// A partially installed rule might remain. Preserve a session
			// owner so cleanup is retried and no new session can start.
			q := &session{mac: normalized, ip: target, expires: time.Now(), cancel: cancel, done: make(chan struct{})}
			s.session = q
			q.retrying = true
			go s.retryCleanup(q)
			_ = stdout.Close()
			core.WriteError(w, 500, "inspection cleanup failed; retrying: "+cleanupErr.Error())
			return
		}
		cancel()
		_ = stdout.Close()
		core.WriteError(w, 500, "could not install interception: "+err.Error())
		return
	}
	q := &session{mac: normalized, ip: target, expires: time.Now().Add(duration), cancel: cancel, done: make(chan struct{})}
	s.session = q
	go s.consume(q, stdout)
	go s.watch(q)
	core.WriteJSON(w, http.StatusOK, s.snapshotLocked())
}

func (s *Service) snapshotLocked() Status {
	q := s.session
	return Status{Active: true, MAC: q.mac, IP: q.ip, Expires: q.expires, Events: []Event{}, CAPresent: true}
}

func (s *Service) consume(q *session, stdout interface{ Read([]byte) (int, error) }) {
	defer close(q.done)
	defer func() {
		if file, ok := stdout.(*os.File); ok {
			_ = file.Close()
		}
	}()
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var event Event
		if json.Unmarshal(scanner.Bytes(), &event) != nil || (event.Kind != "request" && event.Kind != "failed" && event.Kind != "accepted") {
			continue
		}
		s.mu.Lock()
		if s.session == q {
			if event.Kind == "accepted" {
				q.accepted++
			} else {
				q.events = append(q.events, event)
			}
			if len(q.events) > maxEvents {
				q.events = q.events[len(q.events)-maxEvents:]
			}
		}
		s.mu.Unlock()
	}
}

func (s *Service) watch(q *session) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-q.done:
			s.end(q)
			return
		case <-ticker.C:
			if time.Now().After(q.expires) {
				s.end(q)
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
	// Restrict to TCP 80/443 from the chosen address, before NAT. Exclude local
	// and private destinations so router administration and LAN services survive.
	if err := nft("add", "table", "inet", table); err != nil {
		return err
	}
	for _, name := range []string{"redirected", "input_seen", "input_dropped"} {
		if err := nft("add", "counter", "inet", table, name); err != nil {
			return err
		}
	}
	if err := nft("add", "chain", "inet", table, "prerouting", "{ type nat hook prerouting priority dstnat - 1; policy accept; }"); err != nil {
		return err
	}
	if err := nft("add", "chain", "inet", table, "input", "{ type filter hook input priority -1; policy accept; }"); err != nil {
		return err
	}
	// Count packets delivered to the local input hook separately from the
	// redirect match, before deciding whether they may enter the listener.
	if err := nft("add", "rule", "inet", table, "input", "iifname", "!=", "lo", "tcp", "dport", port, "counter", "name", "input_seen"); err != nil {
		return err
	}
	// Redirect sets the original destination port in conntrack, but does not
	// necessarily set the DNAT status bit. Permit only the selected source's
	// connections that originally targeted HTTP(S), not direct LAN proxy use.
	if err := nft("add", "rule", "inet", table, "input", "ip", "saddr", ip, "tcp", "dport", port, "ct", "original", "proto-dst", "{", "80,", "443", "}", "accept"); err != nil {
		return err
	}
	if err := nft("add", "rule", "inet", table, "input", "iifname", "!=", "lo", "tcp", "dport", port, "counter", "name", "input_dropped", "drop"); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"fib", "daddr", "type", "local", "return"},
		{"ip", "daddr", "10.0.0.0/8", "return"},
		{"ip", "daddr", "172.16.0.0/12", "return"},
		{"ip", "daddr", "192.168.0.0/16", "return"},
		// Fake-IP DNS answers belong to the device's proxy exit. A local
		// mitmdump connection would lose that assignment and time out or leak.
		{"ip", "daddr", "198.18.0.0/15", "return"},
	} {
		if err := nft(append([]string{"add", "rule", "inet", table, "prerouting"}, args...)...); err != nil {
			return err
		}
	}
	if err := nft("add", "rule", "inet", table, "prerouting", "ip", "saddr", ip, "tcp", "dport", "{", "80,", "443", "}", "counter", "name", "redirected", "redirect", "to", ":"+port); err != nil {
		return err
	}
	return nil
}

func inspectionCount(name string) (uint64, error) {
	out, err := exec.Command("nft", "-j", "list", "counter", "inet", table, name).Output()
	if err != nil {
		return 0, err
	}
	var result struct {
		Nftables []struct {
			Counter *struct {
				Packets uint64 `json:"packets"`
			} `json:"counter"`
		} `json:"nftables"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return 0, err
	}
	for _, item := range result.Nftables {
		if item.Counter != nil {
			return item.Counter.Packets, nil
		}
	}
	return 0, fmt.Errorf("missing inspection counter")
}
