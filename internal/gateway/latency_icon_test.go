package gateway

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type latencyIconTransport func(*http.Request) (*http.Response, error)

func (f latencyIconTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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
