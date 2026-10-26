package brandlogo

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Every entry in the index has to name a real slice of the bundle, and the
// index is generated, so this is the test that catches a half-written pair of
// files — the one mistake that would otherwise surface as a broken tile in a
// picker and nothing else.
// requireCatalogue skips when nothing was generated. A clone with no Node
// installed still has to pass `make check` (CI runs the two jobs separately),
// so the generated half of these tests is conditional; the daemon warns at
// startup when it is missing.
func requireCatalogue(t *testing.T) {
	t.Helper()
	if !Built() {
		t.Skip("brand-logo catalogue not generated; run `make brand-logos` or `make web`")
	}
}

func TestEveryMarkIsServed(t *testing.T) {
	requireCatalogue(t)
	c, err := load()
	if err != nil {
		t.Fatal(err)
	}
	if len(c.marks) < 5000 {
		t.Fatalf("catalogue has %d marks, want the whole library", len(c.marks))
	}
	for _, m := range c.marks {
		svg, err := SVG(m.slug)
		if err != nil {
			t.Fatalf("%s: %v", m.slug, err)
		}
		if !bytes.HasPrefix(bytes.TrimSpace(svg), []byte("<")) {
			t.Fatalf("%s does not start with a markup element: %q", m.slug, svg[:40])
		}
	}
}

func TestSearchIsASubstringOfTheSlug(t *testing.T) {
	requireCatalogue(t)
	if got := Search("grafana"); len(got) == 0 || got[0] != "grafana" {
		t.Errorf("Search(grafana) = %v, want grafana first", got)
	}
	// Half-remembered brands are the common case, so "link" has to find the
	// hyphenated one rather than only a prefix match would.
	if got := Search("link"); !contains(got, "tp-link") {
		t.Errorf("Search(link) = %v, want it to include tp-link", got)
	}
	if got := Search("GRAFANA"); len(got) == 0 {
		t.Error("Search is case sensitive; a picker types lowercase but should not have to")
	}
	if got := Search("  "); got != nil {
		t.Errorf("Search(blank) = %v, want nothing", got)
	}
	if got := Search("no-such-brand-at-all"); len(got) != 0 {
		t.Errorf("Search(unknown) = %v, want nothing", got)
	}
	if got := Search("a"); len(got) != Limit {
		t.Errorf("Search(a) returned %d slugs, want the limit of %d", len(got), Limit)
	}
}

func TestMarkIsServedGzippedToABrowser(t *testing.T) {
	requireCatalogue(t)
	request := httptest.NewRequest(http.MethodGet, Prefix+"/caddy", nil)
	request.Header.Set("Accept-Encoding", "gzip")
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", got)
	}
	if got := response.Header().Get("Content-Type"); got != "image/svg+xml" {
		t.Fatalf("Content-Type = %q", got)
	}
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		t.Fatalf("body is not a gzip stream: %v", err)
	}
	svg, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(svg), "<svg") {
		t.Fatalf("decompressed body does not look like a mark: %q", svg[:40])
	}
}

func TestMarkIsServedPlainOtherwise(t *testing.T) {
	requireCatalogue(t)
	// curl without --compressed is the case this protects: it must get an SVG,
	// not a stream of bytes it will print as garbage.
	request := httptest.NewRequest(http.MethodGet, Prefix+"/caddy", nil)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	if !strings.HasPrefix(response.Body.String(), "<svg") {
		t.Fatalf("body = %q, want the mark itself", response.Body.String()[:40])
	}
}

func TestMarkRevalidates(t *testing.T) {
	requireCatalogue(t)
	first := httptest.NewRecorder()
	Handler().ServeHTTP(first, httptest.NewRequest(http.MethodGet, Prefix+"/caddy", nil))
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	request := httptest.NewRequest(http.MethodGet, Prefix+"/caddy", nil)
	request.Header.Set("If-None-Match", etag)
	second := httptest.NewRecorder()
	Handler().ServeHTTP(second, request)
	if second.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304", second.Code)
	}
}

func TestUnknownMarkIs404(t *testing.T) {
	requireCatalogue(t)
	for _, path := range []string{
		Prefix + "/no-such-brand-at-all",
		Prefix + "/Caddy", // slugs are lowercase
		Prefix + "/../etc/passwd",
		Prefix + "/caddy/extra",
		Prefix + "/-caddy",
	} {
		response := httptest.NewRecorder()
		Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, response.Code)
		}
	}
}

func TestSearchEndpoint(t *testing.T) {
	requireCatalogue(t)
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, Prefix+"?q=grap", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	if got := response.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	var body struct{ Slugs []string }
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !contains(body.Slugs, "graphql") {
		t.Errorf("slugs = %v, want graphql", body.Slugs)
	}

	// An empty query is an empty list, not a null and not the whole catalogue:
	// the picker asks only once something has been typed.
	response = httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, Prefix, nil))
	if got := strings.TrimSpace(response.Body.String()); got != `{"slugs":[]}` {
		t.Errorf("empty query = %s", got)
	}
}

func TestUnsupportedMethod(t *testing.T) {
	response := httptest.NewRecorder()
	Handler().ServeHTTP(response, httptest.NewRequest(http.MethodPost, Prefix+"/caddy", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", response.Code)
	}
}

func contains(haystack []string, needle string) bool {
	for _, value := range haystack {
		if value == needle {
			return true
		}
	}
	return false
}
