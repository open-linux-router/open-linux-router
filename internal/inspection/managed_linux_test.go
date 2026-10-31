//go:build linux

package inspection

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// This opt-in smoke uses its own service, CA, loopback ports and a no-op
// ExecStopPost. It never installs or deletes any nftables rule.
func TestManagedSystemdContinuity(t *testing.T) {
	if os.Getenv("OLR_INSPECTION_SYSTEMD_SMOKE") != "1" {
		t.Skip("requires an explicitly enabled root/systemd smoke environment")
	}
	if os.Geteuid() != 0 {
		t.Fatal("isolated systemd smoke requires root")
	}
	for _, address := range []string{"127.0.0.1:28081", "127.0.0.1:28082"} {
		if conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond); err == nil {
			conn.Close()
			t.Fatalf("isolated smoke port is in use: %s", address)
		}
	}
	const name = "olr-inspection-persistence-smoke.service"
	const unitPath = "/run/systemd/system/" + name
	if _, err := os.Stat(unitPath); !os.IsNotExist(err) {
		t.Fatal("smoke unit already exists; refusing to replace it")
	}
	dir, err := os.MkdirTemp("/var/tmp", "olr-inspection-systemd-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Mkdir(filepath.Join(dir, "tmp"), 0700); err != nil {
		t.Fatal(err)
	}
	source, _ := addon.ReadFile("addon.py")
	script := filepath.Join(dir, "addon.py")
	if err := os.WriteFile(script, source, 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := exec.LookPath("mitmweb")
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"--listen-host", "127.0.0.1", "--listen-port", "28081", "--web-host", "127.0.0.1", "--web-port", "28082", "--no-web-open-browser", "--set", "confdir=" + dir, "--set", "web_password=isolated-smoke-token", "--set", "olr_device=02:00:00:00:00:01", "-s", script}
	// /usr/bin/true prevents even the smoke unit's stop hook touching nftables.
	if err := os.WriteFile(unitPath, managedUnit(binary, "/usr/bin/true", dir, args, 0), 0600); err != nil {
		t.Fatal(err)
	}
	u, _ := core.NewUnit(name)
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	t.Cleanup(func() {
		cleanupCtx, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		_ = u.Stop(cleanupCtx)
		_ = os.Remove(unitPath)
		_ = core.DaemonReload(cleanupCtx)
	})
	if err := core.DaemonReload(ctx); err != nil {
		t.Fatal(err)
	}
	if err := u.Start(ctx); err != nil {
		t.Fatal(err)
	}
	ready := func() {
		t.Helper()
		client := &http.Client{Timeout: time.Second}
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:28082/", nil)
			req.Header.Set("Authorization", "Bearer isolated-smoke-token")
			if resp, err := client.Do(req); err == nil {
				resp.Body.Close()
				if resp.StatusCode == 200 {
					return
				}
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatal("isolated proxy did not become ready")
	}
	ready()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "retained-content")
	}))
	defer target.Close()
	proxyURL, _ := url.Parse("http://127.0.0.1:28081")
	transport := &http.Transport{Proxy: http.ProxyURL(proxyURL)}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	capture := func() {
		t.Helper()
		resp, err := client.Get(target.URL)
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil || string(data) != "retained-content" {
			t.Fatalf("capture response %q %v", data, err)
		}
	}
	capture()
	before, err := u.Status(ctx)
	if err != nil || !before.Active {
		t.Fatalf("proxy inactive: %+v %v", before, err)
	}
	mainPID := func() int {
		t.Helper()
		out, err := exec.CommandContext(ctx, "systemctl", "show", name, "--property=MainPID", "--value").Output()
		if err != nil {
			t.Fatal(err)
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
		if err != nil || pid <= 0 {
			t.Fatalf("missing isolated service PID: %q %v", out, err)
		}
		return pid
	}
	beforePID := mainPID()
	// Reproduce control-plane shutdown through the actual Service.Close path.
	q := &session{managed: true, detach: make(chan struct{})}
	s := &Service{Managed: true, ProxyUnit: u, session: q}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	after, err := u.Status(ctx)
	if err != nil || !after.Active || mainPID() != beforePID {
		t.Fatalf("control-plane detach restarted proxy: before=%+v after=%+v err=%v", before, after, err)
	}
	capture()
	files, err := filepath.Glob(filepath.Join(dir, "history", "02:00:00:00:00:01", "*.meta"))
	if err != nil || len(files) != 2 {
		t.Fatalf("capture did not continue after detach: entries=%d err=%v", len(files), err)
	}
	if err := u.Restart(ctx); err != nil {
		t.Fatal(err)
	}
	ready()
	capture()
	files, err = filepath.Glob(filepath.Join(dir, "history", "02:00:00:00:00:01", "*.meta"))
	if err != nil || len(files) != 3 {
		t.Fatalf("history did not survive proxy restart: entries=%d err=%v", len(files), err)
	}
	t.Logf("same proxy PID %d survived control-plane detach; three captures retained across proxy restart", beforePID)
}
