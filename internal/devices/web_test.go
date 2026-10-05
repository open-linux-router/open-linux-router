package devices

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type webRoundTrip func(*http.Request) (*http.Response, error)

func (f webRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func webDevice(mac, address string) Resolved {
	return Resolved{MAC: mac, Presence: &Presence{Active: true, IPs: []string{address}}}
}

func TestWebDiscoveryCachesAndPersistsResults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "web.json")
	hits := map[string]int{}
	discovery := NewWebDiscovery(path)
	discovery.Client = &http.Client{Transport: webRoundTrip(func(r *http.Request) (*http.Response, error) {
		hits[r.URL.Host]++
		status, body := http.StatusNotFound, ""
		if r.URL.Host == "172.16.1.238" {
			if r.URL.Path == "" || r.URL.Path == "/" {
				status, body = http.StatusOK, `<html><head><link rel="icon" href="/icon.png"></head></html>`
			}
			if r.URL.Path == "/icon.png" {
				status, body = http.StatusOK, "\x89PNG\r\n\x1a\n"+strings.Repeat("x", 512)
			}
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	list := func(context.Context) ([]Resolved, error) {
		return []Resolved{webDevice("a", "172.16.1.238"), webDevice("b", "172.16.1.239"), webDevice("public", "8.8.8.8")}, nil
	}
	discovery.scan(context.Background(), list)
	sites := discovery.Sites()
	if len(sites) != 1 || sites[0].URL != "http://172.16.1.238" {
		t.Fatalf("sites = %+v", sites)
	}
	if icon, kind := discovery.Icon("a"); len(icon) == 0 || kind != "image/png" {
		t.Fatalf("icon = %d %s", len(icon), kind)
	}
	if hits["8.8.8.8"] != 0 {
		t.Fatal("probed a public address")
	}
	before := hits["172.16.1.238"] + hits["172.16.1.239"]
	discovery.scan(context.Background(), list)
	if got := hits["172.16.1.238"] + hits["172.16.1.239"]; got != before {
		t.Fatalf("reprobed cached results: %d -> %d", before, got)
	}
	reopened := NewWebDiscovery(path)
	if len(reopened.Sites()) != 1 {
		t.Fatalf("lost result after restart: %+v", reopened.Sites())
	}
	if icon, _ := reopened.Icon("a"); len(icon) == 0 {
		t.Fatal("lost icon after restart")
	}
	reopened.Client = discovery.Client
	reopened.scan(context.Background(), func(context.Context) ([]Resolved, error) { return []Resolved{webDevice("a", "172.16.1.240")}, nil })
	if len(reopened.Sites()) != 0 {
		t.Fatal("kept stale page after IP changed")
	}
}

func TestWebAddressRejectsOfflineAndPublic(t *testing.T) {
	for _, device := range []Resolved{webDevice("a", "8.8.8.8"), webDevice("b", "fe80::1"), {MAC: "c"}} {
		if got := webAddress(device); got != "" {
			t.Errorf("webAddress = %s", got)
		}
	}
	device := webDevice("d", "192.168.1.1")
	device.Presence.Active = false
	if got := webAddress(device); got != "" {
		t.Errorf("offline = %s", got)
	}
}

func TestWebProbeDoesNotFollowRedirect(t *testing.T) {
	client := &http.Client{Transport: webRoundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "192.168.1.2" {
			t.Fatalf("followed redirect to %s", r.URL.Host)
		}
		header := make(http.Header)
		header.Set("Location", "https://elsewhere.example/")
		return &http.Response{StatusCode: 301, Header: header, Body: io.NopCloser(strings.NewReader(""))}, nil
	})}
	if !probeWeb(context.Background(), client, "http://192.168.1.2") {
		t.Fatal("redirecting management page was not detected")
	}
}

func TestWebDiscoveryDoesNotAttributeSharedIP(t *testing.T) {
	d := NewWebDiscovery("")
	d.Client = &http.Client{Transport: webRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("shared address must not be probed")
		return nil, nil
	})}
	d.scan(context.Background(), func(context.Context) ([]Resolved, error) {
		return []Resolved{webDevice("a", "192.168.1.10"), webDevice("b", "192.168.1.10")}, nil
	})
	if len(d.Sites()) != 0 {
		t.Fatalf("shared address attributed: %+v", d.Sites())
	}
}

func TestWebDiscoveryLimitsScanBatch(t *testing.T) {
	d := NewWebDiscovery("")
	calls := 0
	d.Client = &http.Client{Transport: webRoundTrip(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
	})}
	d.scan(context.Background(), func(context.Context) ([]Resolved, error) {
		out := make([]Resolved, webScanBatch+1)
		for i := range out {
			out[i] = webDevice(fmt.Sprintf("mac-%d", i), fmt.Sprintf("192.168.1.%d", i+2))
		}
		return out, nil
	})
	if calls != 2*webScanBatch {
		t.Fatalf("requests = %d, want %d", calls, 2*webScanBatch)
	}
}
