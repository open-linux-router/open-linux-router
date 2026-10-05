package ingress

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type iconTransport func(*http.Request) (*http.Response, error)

func (f iconTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestServiceIconPrefersLargestManifestImage(t *testing.T) {
	paths := []string{}
	client := &http.Client{Transport: iconTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "app.home.example.com" {
			t.Fatalf("unexpected host %s", r.URL.Host)
		}
		paths = append(paths, r.URL.Path)
		content := map[string]string{
			"/":                     `<html><head><link rel="icon" href="/favicon.ico"><link rel="manifest" href="/app/site.webmanifest"></head></html>`,
			"/app/site.webmanifest": `{"icons":[{"src":"small.png","sizes":"48x48"},{"src":"large.png","sizes":"512x512"}]}`,
			"/app/large.png":        "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 512),
		}[r.URL.Path]
		status := http.StatusOK
		if content == "" {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(content)), Header: make(http.Header)}, nil
	})}
	data, kind, err := serviceIcon(context.Background(), client, "https://app.home.example.com")
	if err != nil || kind != "image/png" || len(data) == 0 {
		t.Fatalf("image = %q, %q, %v; paths = %v", data, kind, err, paths)
	}
	if got := strings.Join(paths, ","); got != "/,/app/site.webmanifest,/app/large.png" {
		t.Fatalf("paths = %s", got)
	}
}

func TestServiceIconDoesNotFollowCrossOriginResourcesOrRedirects(t *testing.T) {
	paths := []string{}
	client := &http.Client{Transport: iconTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.String())
		if r.URL.Host != "app.home.example.com" {
			t.Fatalf("cross-origin fetch: %s", r.URL)
		}
		body := `<html><head><link rel="manifest" href="https://other.example/manifest"><link rel="icon" href="https://other.example/icon"><link rel="icon" href="/redirect"></head></html>`
		status := http.StatusOK
		header := make(http.Header)
		if r.URL.Path == "/redirect" {
			status = http.StatusFound
			header.Set("Location", "https://other.example/icon")
		}
		if r.URL.Path == "/favicon.ico" {
			body = "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 512)
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: header, Request: r}, nil
	})}
	_, _, err := serviceIcon(context.Background(), client, "https://app.home.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 3 {
		t.Fatalf("paths = %v", paths)
	}
}

func TestIconRouteOnlyServesEnabledPublishedNames(t *testing.T) {
	h, _ := testHTTP(t)
	for _, name := range []string{"grafana", "other"} {
		w := do(t, h, "GET", "/services/"+name+"/icon", "")
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: %d", name, w.Code)
		}
	}
}

func TestIconURLRejectsOtherOrigins(t *testing.T) {
	base := mustURL(t, "https://app.home.example.com/")
	for _, ref := range []string{"https://elsewhere/icon", "//elsewhere/icon", "http://app.home.example.com/icon", "data:image/png,abc"} {
		if got := iconURL(base, base.String(), ref); got != "" {
			t.Errorf("%s resolved to %s", ref, got)
		}
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestServiceIconFallsBackWhenManifestImageIsUnavailable(t *testing.T) {
	client := &http.Client{Transport: iconTransport(func(r *http.Request) (*http.Response, error) {
		body := ""
		status := http.StatusOK
		switch r.URL.Path {
		case "/":
			body = `<html><head><link rel="manifest" href="/manifest.json"><link rel="apple-touch-icon" sizes="180x180" href="/touch.png"></head></html>`
		case "/manifest.json":
			body = `{"icons":[{"src":"https://other.example/icon.png","sizes":"1024x1024"},{"src":"/missing.png","sizes":"512x512"}]}`
		case "/touch.png":
			body = "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 512)
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	_, kind, err := serviceIcon(context.Background(), client, "https://app.home.example.com")
	if err != nil || kind != "image/png" {
		t.Fatalf("fallback: %q, %v", kind, err)
	}
}

func TestIconGetRejectsOversizedResponse(t *testing.T) {
	client := &http.Client{Transport: iconTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(strings.Repeat("x", 11))), Header: make(http.Header)}, nil
	})}
	if _, _, err := iconGet(context.Background(), client, "https://app.home.example.com/icon", 10); err == nil {
		t.Fatal("oversized icon accepted")
	}
}

func TestIconRouteServesPublishedService(t *testing.T) {
	h, applier := testHTTP(t)
	publish(t, h)
	routes := HTTP{Applier: applier, IconClient: &http.Client{Transport: iconTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "grafana.home.example.com" {
			t.Fatalf("unexpected origin %s", r.URL)
		}
		body := ""
		status := http.StatusOK
		if r.URL.Path == "/" {
			body = `<html><head><link rel="icon" href="/icon.png"></head></html>`
		} else if r.URL.Path == "/icon.png" {
			body = "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 512)
		} else {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}}.Handler()
	w := do(t, routes, http.MethodGet, "/services/grafana/icon", "")
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("icon: %d %s", w.Code, w.Body)
	}
	if got := w.Header().Get("Cache-Control"); got != "private, max-age=86400" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := w.Header().Get("Vary"); got != "Authorization" {
		t.Errorf("Vary = %q", got)
	}
}

func TestServiceIconFollowsSameOriginRedirect(t *testing.T) {
	paths := []string{}
	client := &http.Client{Transport: iconTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		status, body := http.StatusOK, ""
		header := make(http.Header)
		switch r.URL.Path {
		case "/":
			status = http.StatusFound
			header.Set("Location", "/web/")
		case "/web/":
			body = `<html><head><link rel="manifest" href="manifest.json"></head></html>`
		case "/web/manifest.json":
			body = `{"icons":[{"src":"app.png","sizes":"512x512"}]}`
		case "/web/app.png":
			body = "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 512)
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: header, Request: r}, nil
	})}
	_, kind, err := serviceIcon(context.Background(), client, "https://app.home.example.com")
	if err != nil || kind != "image/png" {
		t.Fatalf("redirect icon: %s, %v (%v)", kind, err, paths)
	}
	if got := strings.Join(paths, ","); got != "/,/web/,/web/manifest.json,/web/app.png" {
		t.Fatalf("paths = %s", got)
	}
}

func TestServiceIconUsesConfiguredPath(t *testing.T) {
	paths := []string{}
	client := &http.Client{Transport: iconTransport(func(r *http.Request) (*http.Response, error) {
		paths = append(paths, r.URL.Path)
		body := ""
		status := http.StatusOK
		switch r.URL.Path {
		case "/ui":
			body = `<html><head><link rel="icon" href="/ui/icon.png"></head></html>`
		case "/ui/icon.png":
			body = "\x89PNG\r\n\x1a\n" + strings.Repeat("x", 512)
		default:
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
	})}
	_, kind, err := serviceIcon(context.Background(), client, "https://clash.home.example.com/ui")
	if err != nil || kind != "image/png" {
		t.Fatalf("configured path icon: %s, %v (%v)", kind, err, paths)
	}
	if paths[0] != "/ui" {
		t.Fatalf("first request = %s", paths[0])
	}
}
