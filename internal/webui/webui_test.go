package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testSPA() http.Handler {
	return &spa{files: fstest.MapFS{
		"index.html":              {Data: []byte("<!doctype html><title>olr</title>")},
		"favicon.svg":             {Data: []byte("<svg/>")},
		"assets/index-abc123.js":  {Data: []byte("console.log(1)")},
		"assets/index-abc123.css": {Data: []byte("body{}")},
	}}
}

func get(h http.Handler, path string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path, nil))
	return w
}

// A client-side route has to load the app shell; the router takes it from there.
func TestClientSideRoutesServeTheAppShell(t *testing.T) {
	h := testSPA()

	for _, path := range []string{"/", "/dhcp", "/dhcp/pools", "/anything/deep"} {
		w := get(h, path)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, w.Code)
			continue
		}
		if !strings.Contains(w.Body.String(), "<!doctype html>") {
			t.Errorf("GET %s did not serve index.html", path)
		}
	}
}

// The important half of the fallback rule. Serving index.html for a missing
// .js turns "I forgot to rebuild" into an inscrutable syntax error in the
// browser console, which is a far worse afternoon than a 404.
func TestMissingAssetsAre404NotTheAppShell(t *testing.T) {
	h := testSPA()

	for _, path := range []string{
		"/assets/index-gone.js",
		"/assets/nothing.css",
		"/missing.svg",
		"/some/file.json",
	} {
		w := get(h, path)
		if w.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404 (body %q)", path, w.Code, w.Body.String())
		}
	}
}

func TestRealAssetsAreServed(t *testing.T) {
	h := testSPA()

	w := get(h, "/assets/index-abc123.js")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if w.Body.String() != "console.log(1)" {
		t.Errorf("body = %q", w.Body.String())
	}
}

// Fingerprinted files are immutable, index.html is not — and getting that
// backwards means a browser holds a stale index naming bundles that an upgrade
// has already deleted.
func TestCacheHeaders(t *testing.T) {
	h := testSPA()

	if got := get(h, "/assets/index-abc123.js").Header().Get("Cache-Control"); !strings.Contains(got, "immutable") {
		t.Errorf("fingerprinted asset Cache-Control = %q, want immutable", got)
	}
	for _, path := range []string{"/", "/dhcp"} {
		if got := get(h, path).Header().Get("Cache-Control"); got != "no-cache" {
			t.Errorf("GET %s Cache-Control = %q, want no-cache", path, got)
		}
	}
}

func TestPathTraversalIsRefused(t *testing.T) {
	h := testSPA()
	// Cleaned to /etc/passwd, which has no extension-free basename... it does
	// have one, so this must not escape the embedded FS regardless.
	for _, path := range []string{"/../../etc/passwd", "/assets/../../etc/passwd"} {
		w := get(h, path)
		if w.Code == http.StatusOK && strings.Contains(w.Body.String(), "root:") {
			t.Errorf("GET %s escaped the embedded filesystem", path)
		}
	}
}

func TestNonGetMethodsAreRefused(t *testing.T) {
	h := testSPA()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/dhcp", nil))

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", w.Code)
	}
	if w.Header().Get("Allow") == "" {
		t.Error("a 405 should say which methods are allowed")
	}
}

// A clone with no Node produces a binary with no UI. That has to explain
// itself, because a 404 looks identical to a broken route.
func TestAMissingUIExplainsItself(t *testing.T) {
	h := notBuilt("this olrd was built without the web UI")
	w := get(h, "/")

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "make web") {
		t.Error("the page does not say how to build the UI")
	}
	if !strings.Contains(w.Body.String(), "/api") {
		t.Error("the page should say the API is unaffected")
	}
}

// The committed placeholder keeps //go:embed working on a fresh clone; if this
// fails, `go build` is about to break for anyone without a built SPA.
func TestEmbeddedAssetDirectoryExists(t *testing.T) {
	if _, err := assets.ReadDir("assets"); err != nil {
		t.Fatalf("the embedded assets directory is unreadable: %v", err)
	}
}
