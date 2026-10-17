package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLatencySelectsOnceAndKeepsFailedTarget(t *testing.T) {
	m := NewLatencyMonitor()
	m.dnsProbe = nil
	var mu sync.Mutex
	calls := map[string]int{}
	values := map[string]float64{m.targets[0].URL: 100, m.targets[1].URL: 20, m.targets[2].URL: 80, m.targets[3].URL: 90}
	m.probe = func(_ context.Context, url string) (float64, error) {
		mu.Lock()
		defer mu.Unlock()
		calls[url]++
		if values[url] < 0 {
			return 0, errors.New("offline")
		}
		return values[url], nil
	}
	m.sample(context.Background())
	s := m.Snapshot()
	if s.Target != "Baidu" || *s.Milliseconds != 20 {
		t.Fatalf("selection: %+v", s)
	}
	for _, n := range calls {
		if n != 1 {
			t.Fatal("selection must probe each candidate once")
		}
	}
	values[m.targets[0].URL] = 1
	values[m.targets[1].URL] = -1
	m.sample(context.Background())
	s = m.Snapshot()
	if s.Target != "Baidu" || s.Milliseconds != nil || s.State != "unreachable" {
		t.Fatalf("failure changed target: %+v", s)
	}
	if calls[m.targets[0].URL] != 1 || calls[m.targets[1].URL] != 2 {
		t.Fatal("fixed target must be the only probe")
	}
	if err := m.SetTarget(""); err != nil {
		t.Fatal(err)
	}
	m.sample(context.Background())
	if m.Snapshot().Target != "Google" {
		t.Fatal("explicit reselection did not choose new fastest")
	}
}

func TestLatencyAllFailedSelectionRetries(t *testing.T) {
	m := NewLatencyMonitor()
	m.dnsProbe = nil
	m.probe = func(context.Context, string) (float64, error) { return 0, errors.New("offline") }
	m.sample(context.Background())
	if s := m.Snapshot(); s.State != "unreachable" || s.Milliseconds != nil || s.TargetURL != "" {
		t.Fatalf("%+v", s)
	}
	m.probe = func(context.Context, string) (float64, error) { return 2, nil }
	m.sample(context.Background())
	if m.Snapshot().State != "ok" {
		t.Fatal("initial selection never recovered")
	}
}

func TestLatencySnapshotIsolationAndCancellation(t *testing.T) {
	m := NewLatencyMonitor()
	m.dnsProbe = nil
	m.probe = func(context.Context, string) (float64, error) { return 12, nil }
	m.sample(context.Background())
	s := m.Snapshot()
	s.Sites[0].Name = "changed"
	if m.Snapshot().Sites[0].Name == "changed" {
		t.Fatal("snapshot aliases internal state")
	}
	before := m.Snapshot()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.sample(ctx)
	if !reflect.DeepEqual(before, m.Snapshot()) {
		t.Fatal("cancelled round published")
	}
}

func TestLatencyEditDiscardsInFlightSelection(t *testing.T) {
	m := NewLatencyMonitor()
	m.dnsProbe = nil
	started, release := make(chan struct{}, 4), make(chan struct{})
	m.probe = func(context.Context, string) (float64, error) { started <- struct{}{}; <-release; return 12, nil }
	done := make(chan struct{})
	go func() { defer close(done); m.sample(context.Background()) }()
	<-started
	if err := m.SetTarget(m.targets[1].URL); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	if s := m.Snapshot(); s.Target != "Baidu" || s.State != "measuring" {
		t.Fatalf("old selection overwrote edit: %+v", s)
	}
}

func TestLatencyProbeExcludesSetupAndDoesNotDownload(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodHead {
			t.Errorf("method = %s", r.Method)
		}
		time.Sleep(30 * time.Millisecond)
		w.Header().Set("Content-Length", "100000000")
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := server.Client()
	transport := client.Transport.(*http.Transport).Clone()
	dial := transport.DialContext
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		time.Sleep(150 * time.Millisecond)
		return dial(ctx, network, addr)
	}
	client.Transport = transport
	defer transport.CloseIdleConnections()
	start := time.Now()
	value, err := probePageWithClient(context.Background(), server.URL, client)
	if err != nil || value < 25 || value >= float64(time.Since(start)/time.Millisecond)-100 {
		t.Fatalf("latency = %.1f ms, elapsed = %v, err = %v", value, time.Since(start), err)
	}
}

func TestLatencyProbeNeedsAuthenticatedRemoteResponse(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := probePageWithClient(ctx, server.URL, server.Client()); err == nil {
		t.Fatal("connection without remote response counted as latency")
	}
	if _, err := probePage(context.Background(), server.URL); err == nil {
		t.Fatal("untrusted TLS certificate accepted")
	}
}

func TestLatencyDNSProbeFailure(t *testing.T) {
	m := NewLatencyMonitor()
	m.probe = func(context.Context, string) (float64, error) { return 20, nil }
	m.dnsProbe = func(context.Context) (float64, error) { return 0, errors.New("DNS down") }
	m.sample(context.Background())
	if m.Snapshot().DNSMilliseconds != nil {
		t.Fatal("failed lookup must not claim zero latency")
	}
	m.dnsProbe = func(context.Context) (float64, error) { return 12, nil }
	m.sample(context.Background())
	if got := m.Snapshot().DNSMilliseconds; got == nil || *got != 12 {
		t.Fatalf("DNS latency = %v", got)
	}
}

func TestCustomLatencyPersistenceAndProbes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sites.json")
	m, err := NewCustomLatencyMonitor(path)
	if err != nil {
		t.Fatal(err)
	}
	sites := []CustomLatencySite{{Name: "YouTube", URL: "https://www.youtube.com/"}, {Name: "WeChat", URL: "https://weixin.qq.com/"}}
	if err := m.Replace(sites); err != nil {
		t.Fatal(err)
	}
	m, err = NewCustomLatencyMonitor(path)
	if err != nil || !reflect.DeepEqual(m.Config(), sites) {
		t.Fatalf("reload: %v %v", m.Config(), err)
	}
	m.probe = func(_ context.Context, url string) (float64, error) {
		if url == sites[0].URL {
			return 42, nil
		}
		return 0, errors.New("offline")
	}
	m.sample(context.Background())
	got := m.Snapshot()
	if got[0].Milliseconds == nil || *got[0].Milliseconds != 42 || got[1].Milliseconds != nil || got[1].CheckedAt.IsZero() {
		t.Fatalf("measurements: %+v", got)
	}
	if err := m.Replace(sites[:1]); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot()) != 1 || m.Snapshot()[0].Milliseconds == nil || *m.Snapshot()[0].Milliseconds != 42 {
		t.Fatal("removing another site must preserve this result")
	}
}

func TestCustomLatencyValidationPreservesSettings(t *testing.T) {
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	valid := []CustomLatencySite{{Name: "Video", URL: "https://example.com/"}}
	if err := m.Replace(valid); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []CustomLatencySite{
		{Name: "Video", URL: "http://example.com"},
		{Name: "Video", URL: "https://user:pass@example.com"},
		{Name: "Video", URL: "https://example.com:8443"},
		{Name: "Video", URL: "https://example.com/#fragment"},
	} {
		if err := m.Replace([]CustomLatencySite{bad}); err == nil {
			t.Fatalf("accepted %+v", bad)
		}
		if !reflect.DeepEqual(m.Config(), valid) {
			t.Fatal("rejected edit changed settings")
		}
	}
	if err := m.Replace([]CustomLatencySite{valid[0], {Name: "video", URL: "https://other.com"}}); err == nil {
		t.Fatal("duplicate names accepted")
	}
}

func TestCustomLatencyUsesSelectedExit(t *testing.T) {
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	m.ValidateExit = func(name string) error {
		if name != "Proxy" {
			return errors.New("unknown exit")
		}
		return nil
	}
	m.Resolve = func(name string) (uint32, error) {
		if name != "Proxy" {
			t.Fatalf("resolved %q", name)
		}
		return 0x30000, nil
	}
	m.probe = func(_ context.Context, url string) (float64, error) {
		if url != "https://direct.example/" {
			t.Errorf("default route used for %s", url)
		}
		return 10, nil
	}
	m.probeThrough = func(_ context.Context, url string, mark uint32) (float64, error) {
		if url != "https://proxy.example/" || mark != 0x30000 {
			t.Errorf("wrong route: %s %#x", url, mark)
		}
		return 20, nil
	}
	sites := []CustomLatencySite{{Name: "Default", URL: "https://direct.example/"}, {Name: "Proxy site", URL: "https://proxy.example/", Exit: "Proxy"}}
	if err := m.Replace(sites); err != nil {
		t.Fatal(err)
	}
	m.sample(context.Background())
	got := m.Snapshot()
	if *got[0].Milliseconds != 10 || *got[1].Milliseconds != 20 || got[1].Exit != "Proxy" {
		t.Fatalf("results: %+v", got)
	}
	if err := m.Replace([]CustomLatencySite{{Name: "No exit", URL: "https://example.com", Exit: "Unknown"}}); err == nil {
		t.Fatal("unknown exit accepted")
	}
	m.Resolve = func(string) (uint32, error) { return 0, errors.New("exit disabled") }
	m.sample(context.Background())
	if m.Snapshot()[1].Milliseconds != nil {
		t.Fatal("unavailable exit must not fall back to default")
	}
	reloaded, err := NewCustomLatencyMonitor(filepath.Join(filepath.Dir(m.path), "sites.json"))
	if err != nil || !reflect.DeepEqual(reloaded.Config(), sites) {
		t.Fatalf("stored routes: %v, %v", reloaded.Config(), err)
	}
}

func TestPageProbeLimitsAndStatus(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.WriteHeader(status)
				w.Write([]byte("<html>ok</html>"))
			}))
			defer server.Close()
			_, err := probePageWithClient(context.Background(), server.URL, server.Client())
			if err != nil {
				t.Fatalf("status %d: %v", status, err)
			}
		})
	}
}

func TestPageProbeCountsCrossHostRedirectResponse(t *testing.T) {
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("cross-host request reached destination") }))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer server.Close()
	if _, err := probePageWithClient(context.Background(), server.URL, server.Client()); err != nil {
		t.Fatalf("redirect response was not measured: %v", err)
	}
}

func TestPageProbeCountsAnyCompleteResponse(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusNotFound, 444, http.StatusInternalServerError} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				w.Write([]byte(`{"message":"hello"}`))
			}))
			defer server.Close()
			if _, err := probePageWithClient(context.Background(), server.URL, server.Client()); err != nil {
				t.Fatalf("HTTP %d was not measured: %v", status, err)
			}
		})
	}
}

func TestCustomLatencyIconValidation(t *testing.T) {
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Replace([]CustomLatencySite{{Name: "Test", URL: "https://example.com/", Icon: "thesvg:wechat"}}); err != nil {
		t.Fatal(err)
	}
	for _, icon := range []string{"thesvg:../wechat", "data:image/svg+xml;base64,PHN2Zz4=", "https://evil.example/icon.svg"} {
		if err := m.Replace([]CustomLatencySite{{Name: "Test", URL: "https://example.com/", Icon: icon}}); err == nil {
			t.Fatalf("accepted %s", icon)
		}
	}
}

func TestSafeTheSVG(t *testing.T) {
	for _, input := range []string{`<svg xmlns="http://www.w3.org/2000/svg"><path fill="#07C160" d="M0 0"/></svg>`, `<svg><script>alert(1)</script></svg>`, `<svg><image href="https://example.com/a"/></svg>`, `<svg onload="alert(1)"/>`} {
		safe := safeTheSVG([]byte(input))
		if safe != strings.Contains(input, "<path") {
			t.Fatalf("unexpected SVG safety for %s", input)
		}
	}
}

func TestCustomLatencyOrderAndUploadedIconPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sites.json")
	m, err := NewCustomLatencyMonitor(path)
	if err != nil {
		t.Fatal(err)
	}
	// A minimal valid PNG detected by net/http's content sniffer.
	image := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ"
	sites := []CustomLatencySite{{Name: "Second", URL: "https://second.example/", Icon: image}, {Name: "First", URL: "https://first.example/", Icon: "thesvg:wechat"}}
	if err := m.Replace(sites); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewCustomLatencyMonitor(path)
	if err != nil || !reflect.DeepEqual(reloaded.Config(), sites) {
		t.Fatalf("reload: %+v, %v", reloaded.Config(), err)
	}
	got := reloaded.Snapshot()
	if got[0].Name != "Second" || got[0].Icon != image || got[1].Name != "First" {
		t.Fatalf("snapshot order/icons: %+v", got)
	}
}

func TestUploadedLatencyIconEndpoint(t *testing.T) {
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	icon := "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJ"
	if err := m.Replace([]CustomLatencySite{{Name: "Test", URL: "https://example.com", Icon: icon}}); err != nil {
		t.Fatal(err)
	}
	data, kind, err := m.Icon(context.Background(), "Test")
	if err != nil || kind != "image/png" || len(data) == 0 {
		t.Fatalf("icon: %s, %v", kind, err)
	}
}

func TestCustomLatencyEditsKeepUnchangedResults(t *testing.T) {
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := []CustomLatencySite{{Name: "A", URL: "https://a.example/"}, {Name: "B", URL: "https://b.example/"}}
	if err := m.Replace(original); err != nil {
		t.Fatal(err)
	}
	m.probe = func(_ context.Context, url string) (float64, error) {
		if url == original[0].URL {
			return 20, nil
		}
		return 40, nil
	}
	m.sample(context.Background())
	before := m.Snapshot()
	reordered := []CustomLatencySite{{Name: "Renamed B", URL: original[1].URL, Icon: "thesvg:wechat"}, original[0]}
	if err := m.Replace(reordered); err != nil {
		t.Fatal(err)
	}
	got := m.Snapshot()
	if got[0].Name != "Renamed B" || got[0].Milliseconds == nil || *got[0].Milliseconds != 40 || got[0].CheckedAt != before[1].CheckedAt || got[0].Icon != "thesvg:wechat" {
		t.Fatalf("reorder lost B: %+v", got[0])
	}
	if got[1].Milliseconds == nil || *got[1].Milliseconds != 20 {
		t.Fatalf("reorder lost A: %+v", got[1])
	}
	reordered[0].URL = "https://new.example/"
	if err := m.Replace(reordered); err != nil {
		t.Fatal(err)
	}
	got = m.Snapshot()
	if got[0].Milliseconds != nil || !got[0].CheckedAt.IsZero() || got[1].Milliseconds == nil {
		t.Fatalf("changed target retained old sample: %+v", got)
	}
}

func TestCustomLatencyReconcilesRoundAfterReorder(t *testing.T) {
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	a := CustomLatencySite{Name: "A", URL: "https://a.example/"}
	b := CustomLatencySite{Name: "B", URL: "https://b.example/"}
	if err := m.Replace([]CustomLatencySite{a, b}); err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	m.probe = func(_ context.Context, url string) (float64, error) {
		once.Do(func() { close(started) })
		<-release
		if url == a.URL {
			return 10, nil
		}
		return 20, nil
	}
	done := make(chan struct{})
	go func() { m.sample(context.Background()); close(done) }()
	<-started
	if err := m.Replace([]CustomLatencySite{b, a}); err != nil {
		t.Fatal(err)
	}
	close(release)
	<-done
	got := m.Snapshot()
	if got[0].Name != "B" || got[0].Milliseconds == nil || *got[0].Milliseconds != 20 || got[1].Name != "A" || got[1].Milliseconds == nil || *got[1].Milliseconds != 10 {
		t.Fatalf("results attached to wrong rows: %+v", got)
	}
}

func TestCustomLatencyKeepsResultWhileProbingThenPublishesFailure(t *testing.T) {
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Replace([]CustomLatencySite{{Name: "A", URL: "https://a.example/"}}); err != nil {
		t.Fatal(err)
	}
	m.probe = func(context.Context, string) (float64, error) { return 25, nil }
	m.sample(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	m.probe = func(context.Context, string) (float64, error) {
		close(started)
		<-release
		return 0, errors.New("offline")
	}
	done := make(chan struct{})
	go func() { m.sample(context.Background()); close(done) }()
	<-started
	if got := m.Snapshot()[0]; got.Milliseconds == nil || *got.Milliseconds != 25 {
		t.Fatalf("cleared while probing: %+v", got)
	}
	close(release)
	<-done
	if got := m.Snapshot()[0]; got.Milliseconds != nil || got.Error != "Connection failed" {
		t.Fatalf("failure not published: %+v", got)
	}
}
