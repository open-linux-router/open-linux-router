package gateway

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
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

func TestHTTPSProbe(t *testing.T) {
	for _, status := range []int{204, 302, 403, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodHead {
					t.Errorf("method = %s", r.Method)
				}
				if status == 302 {
					w.Header().Set("Location", "/loop")
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			_, err := probeHTTPS(ctx, server.URL)
			if (err != nil) != (status >= 400) {
				t.Fatalf("status %d: %v", status, err)
			}
		})
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
