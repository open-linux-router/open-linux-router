package gateway

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/ingress"
)

type latencyIconTransport func(*http.Request) (*http.Response, error)

func (f latencyIconTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLatencyTheSVGIconResolvesBeforeFaviconClient(t *testing.T) {
	for _, tc := range []struct {
		name    string
		icon    string
		builtin bool
	}{
		{name: "catalog", icon: "thesvg:wechat", builtin: true},
		{name: "custom", icon: "thesvg:wechat"},
		{name: "missing mark", icon: "thesvg:no-such-brand-at-all"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			site := CustomLatencySite{Name: "WeChat", URL: "https://example.com/", Exit: "Unavailable", Icon: tc.icon}
			m := &CustomLatencyMonitor{}
			if tc.builtin {
				catalog, ok := commonLatencySite(site.Name)
				if !ok {
					t.Fatal("missing catalog site")
				}
				m.common = []CommonLatencySite{catalog}
				m.commonExits = map[string]string{site.Name: site.Exit}
			} else {
				m.sites = []CustomLatencySite{site}
			}
			m.iconClient = func(*CustomLatencySite) (*http.Client, error) {
				t.Error("local icon created a favicon client")
				return nil, errors.New("favicon client unavailable")
			}
			m.Resolve = func(string) (uint32, error) {
				t.Error("local icon resolved a network exit")
				return 0, errors.New("exit unavailable")
			}
			// Go-only checkouts have no artwork bundle. They must return the
			// local resolver's error without attempting favicon discovery.
			wantData, wantKind, wantErr := ingress.CustomIcon(tc.icon)
			for range 2 {
				data, kind, err := m.Icon(context.Background(), site.Name)
				if (err == nil) != (wantErr == nil) || (err != nil && err.Error() != wantErr.Error()) {
					t.Fatalf("icon error = %v, want local resolver error %v", err, wantErr)
				}
				if !bytes.Equal(data, wantData) || kind != wantKind {
					t.Fatalf("icon = %q (%d bytes), want %q (%d bytes)", kind, len(data), wantKind, len(wantData))
				}
			}
			if wantErr == nil {
				if cached, ok := m.icons[site.Name]; !ok || !bytes.Equal(cached.data, wantData) {
					t.Fatal("local icon was not cached")
				}
			}
			req := httptest.NewRequest(http.MethodGet, "/latency/icon/WeChat", nil)
			req.SetPathValue("name", site.Name)
			response := httptest.NewRecorder()
			HTTP{Latency: &LatencyMonitor{Custom: m}}.getLatencyIcon(response, req)
			if wantErr != nil {
				if response.Code != http.StatusNotFound {
					t.Fatalf("missing local artwork: status = %d", response.Code)
				}
			} else if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "image/svg+xml" || !bytes.Equal(response.Body.Bytes(), wantData) {
				t.Fatalf("local artwork response = %d %s", response.Code, response.Header().Get("Content-Type"))
			}
		})
	}
}

func TestCustomLatencyIconDiscoversAndCaches(t *testing.T) {
	m, err := NewCustomLatencyMonitor(filepath.Join(t.TempDir(), "sites.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Replace([]CustomLatencySite{{Name: "Test", URL: "https://example.com/app"}}); err != nil {
		t.Fatal(err)
	}
	requests := 0
	m.iconClient = func(*CustomLatencySite) (*http.Client, error) {
		return &http.Client{Transport: latencyIconTransport(func(r *http.Request) (*http.Response, error) {
			requests++
			if r.URL.Host != "example.com" && r.URL.Host != "other.example" {
				t.Errorf("unexpected host %s", r.URL.Host)
			}
			body := `<html><link rel="icon" href="/brand.png">`
			if r.URL.Path == "/brand.png" {
				body = "\x89PNG\r\n\x1a\nicon"
			}
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
		})}, nil
	}
	data, kind, err := m.Icon(context.Background(), "Test")
	if err != nil || kind != "image/png" || len(data) == 0 {
		t.Fatalf("icon = %s %v", kind, err)
	}
	first := requests
	if _, _, err := m.Icon(context.Background(), "Test"); err != nil || requests != first {
		t.Fatalf("cache: %d -> %d, %v", first, requests, err)
	}
	if err := m.Replace([]CustomLatencySite{{Name: "Test", URL: "https://other.example/"}}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := m.Icon(context.Background(), "Test"); err != nil || requests == first {
		t.Fatalf("edit did not invalidate cache: %v", err)
	}
	if _, _, err := m.Icon(context.Background(), "missing"); err == nil {
		t.Fatal("unknown site accepted")
	}
}
