package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestLatencySelectionAndRecovery(t *testing.T) {
	m := NewLatencyMonitor()
	m.dnsProbe = func(context.Context) (float64, error) { return 8, nil }
	values := map[string]float64{m.targets[0].URL: 100, m.targets[1].URL: 20, m.targets[2].URL: 80, m.targets[3].URL: 90}
	m.probe = func(_ context.Context, url string) (float64, error) {
		v := values[url]
		if v < 0 {
			return 0, errors.New("blocked")
		}
		return v, nil
	}
	for range 4 {
		m.sample(context.Background())
	}
	s := m.Snapshot()
	if len(m.selected) != 1 || s.Target != "Baidu" || *s.Milliseconds != 20 {
		t.Fatalf("selection: %+v / %v", s, m.selected)
	}
	values[m.targets[1].URL] = -1
	m.sample(context.Background())
	if len(m.selected) != 0 || m.Snapshot().Milliseconds != nil || m.Snapshot().State != "unreachable" {
		t.Fatal("failed preferred site must clear stale measurement and restart discovery")
	}
	m.sample(context.Background())
	if m.Snapshot().Target != "Yandex" {
		t.Fatal("discovery must recover using another site")
	}
	for range 3 {
		m.sample(context.Background())
	}
	if len(m.selected) != 2 {
		t.Fatalf("close alternatives should both be retained: %v", m.selected)
	}
	values[m.targets[0].URL] = 5
	m.discoveryStarted = time.Now().Add(-latencyRediscoveryInterval)
	m.sample(context.Background())
	if m.Snapshot().Target != "Google" || len(m.selected) != 0 {
		t.Fatal("periodic discovery must revisit excluded sites")
	}
}

func TestLatencyRejectsLuckyAndFailedSites(t *testing.T) {
	m := NewLatencyMonitor()
	m.dnsProbe = func(context.Context) (float64, error) { return 8, nil }
	round := 0
	m.probe = func(_ context.Context, url string) (float64, error) {
		if url == m.targets[0].URL {
			if round == 0 {
				return 1, nil
			}
			return 0, errors.New("blocked")
		}
		return 50, nil
	}
	for round = 0; round < 4; round++ {
		m.sample(context.Background())
	}
	for _, i := range m.selected {
		if i == 0 {
			t.Fatal("one lucky success must not win")
		}
	}
	m.probe = func(context.Context, string) (float64, error) { return 0, errors.New("offline") }
	m.sample(context.Background())
	m.sample(context.Background())
	if m.Snapshot().State != "unreachable" || m.Snapshot().Milliseconds != nil {
		t.Fatal("all failures must not report zero or an old latency")
	}
}

func TestLatencySnapshotIsolationAndCancellation(t *testing.T) {
	m := NewLatencyMonitor()
	m.dnsProbe = func(context.Context) (float64, error) { return 8, nil }
	m.probe = func(context.Context, string) (float64, error) { return 12, nil }
	m.sample(context.Background())
	s := m.Snapshot()
	s.Sites[0].Name = "changed"
	if m.Snapshot().Sites[0].Name == "changed" {
		t.Fatal("snapshot aliases internal state")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m.sample(ctx)
	if m.rounds != 1 {
		t.Fatal("cancelled rounds must not publish")
	}
}

func TestPageProbeDownloadsDocumentAndFollowsSameOrigin(t *testing.T) {
	methods := []string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		methods = append(methods, r.Method+" "+r.URL.Path)
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/page", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>"))
		time.Sleep(40 * time.Millisecond)
		w.Write([]byte("done</html>"))
	}))
	defer server.Close()
	value, err := probePageWithClient(context.Background(), server.URL, server.Client())
	if err != nil || value < 40 {
		t.Fatalf("page = %.1f ms, %v", value, err)
	}
	if !reflect.DeepEqual(methods, []string{"GET /", "GET /page"}) {
		t.Fatalf("requests = %v", methods)
	}
}

func TestLatencyProbeCadence(t *testing.T) {
	m := NewLatencyMonitor()
	m.dnsProbe = func(context.Context) (float64, error) { return 8, nil }
	m.probe = func(context.Context, string) (float64, error) { return 10, nil }
	m.sample(context.Background())
	if m.nextInterval() != 15*time.Second {
		t.Fatal("initial discovery should finish promptly")
	}
	for range 3 {
		m.sample(context.Background())
	}
	if m.nextInterval() != time.Minute {
		t.Fatal("stable targets should be checked every minute")
	}
	m.probe = func(context.Context, string) (float64, error) { return 0, errors.New("offline") }
	m.sample(context.Background())
	for range 4 {
		m.sample(context.Background())
	}
	if m.nextInterval() != time.Minute {
		t.Fatal("failed discovery must back off")
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
	if len(m.Snapshot()) != 1 || m.Snapshot()[0].Milliseconds != nil {
		t.Fatal("edit must clear stale results")
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
			if (err != nil) != (status != http.StatusOK) {
				t.Fatalf("status %d: %v", status, err)
			}
		})
	}
}

func TestPageProbeRejectsCrossHostRedirect(t *testing.T) {
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("cross-host request reached destination") }))
	defer other.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, http.StatusFound) }))
	defer server.Close()
	_, err := probePageWithClient(context.Background(), server.URL, server.Client())
	if err == nil || !strings.Contains(err.Error(), "HTTP 302") {
		t.Fatalf("cross-host redirect: %v", err)
	}
}

func TestPageProbeRejectsLargeAndNonHTML(t *testing.T) {
	for _, nonHTML := range []bool{false, true} {
		server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if nonHTML {
				w.Header().Set("Content-Type", "image/png")
				w.Write([]byte("png"))
				return
			}
			w.Header().Set("Content-Type", "text/html")
			w.Write([]byte(strings.Repeat("x", pageProbeLimit+1)))
		}))
		_, err := probePageWithClient(context.Background(), server.URL, server.Client())
		server.Close()
		if err == nil {
			t.Fatalf("accepted invalid page (nonHTML=%v)", nonHTML)
		}
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
