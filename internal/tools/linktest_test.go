package tools

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// linkTestRoute calls the handler the daemon mounts at /link-test/.
func linkTestRoute(t *testing.T, h *HTTP, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	h.LinkTestHandler().ServeHTTP(w, httptest.NewRequest(method, path, body))
	return w
}

// linkTestAuthorized mints a session and returns the cookie it grants.
func linkTestAuthorized(t *testing.T, h *HTTP) *http.Cookie {
	t.Helper()
	w := httptest.NewRecorder()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /link-test/session", h.linkTestSession)
	mux.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/link-test/session", nil))
	if w.Code != http.StatusNoContent {
		t.Fatalf("session status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("session set %d cookies, want 1", len(cookies))
	}
	return cookies[0]
}

func linkTestRequest(t *testing.T, h *HTTP, cookie *http.Cookie, method, path string, body io.Reader) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, body)
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.LinkTestHandler().ServeHTTP(w, r)
	return w
}

// The page is not public: a client that never asked this router's API for a
// session gets an explanation, not a bandwidth source.
func TestLinkTestRefusesWithoutASession(t *testing.T) {
	h := &HTTP{}
	for _, path := range []string{"/link-test/", "/link-test/download", "/link-test/upload", "/link-test/ping", "/link-test/speedtest.js"} {
		method := http.MethodGet
		if path == "/link-test/upload" {
			method = http.MethodPost
		}
		w := linkTestRoute(t, h, method, path, strings.NewReader("data"))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s %s status = %d, want 401", method, path, w.Code)
		}
	}
}

func TestLinkTestSessionScopesItsCookie(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	if cookie.Path != "/link-test/" || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}
	if cookie.Name != linkTestCookie {
		t.Fatalf("cookie name = %q, want %q", cookie.Name, linkTestCookie)
	}
	// A session cookie, so closing the browser ends the grant. The store's own
	// expiry is the ceiling, not the browser's promise.
	if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		t.Fatalf("cookie outlives the browser: MaxAge %d, Expires %v", cookie.MaxAge, cookie.Expires)
	}
	if !h.linkTestAllowed(&http.Request{Header: http.Header{"Cookie": {cookie.Name + "=" + cookie.Value}}}) {
		t.Fatal("freshly minted cookie is not accepted")
	}
}

func TestLinkTestSessionRejectsForeignOrigin(t *testing.T) {
	h := &HTTP{}
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/link-test/session", nil)
	r.Header.Set("Origin", "https://evil.example")
	h.linkTestSession(w, r)
	if w.Code != http.StatusForbidden || len(w.Result().Cookies()) != 0 {
		t.Fatalf("foreign origin: status %d, cookies %v", w.Code, w.Result().Cookies())
	}
}

func TestLinkTestExpiredSessionIsRefused(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	h.linkMu.Lock()
	h.linkViewers = []linkTestViewer{{value: cookie.Value, expires: time.Now().Add(-time.Second)}}
	h.linkMu.Unlock()
	if w := linkTestRequest(t, h, cookie, http.MethodGet, "/link-test/", nil); w.Code != http.StatusUnauthorized {
		t.Fatalf("expired session status = %d, want 401", w.Code)
	}
}

func TestLinkTestServesTheVendoredPage(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	w := linkTestRequest(t, h, cookie, http.MethodGet, "/link-test/", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("page status = %d, want 200", w.Code)
	}
	// The page is served for a direct visit, but nothing frames it any more:
	// the tool page drives the engine in its own document, so the module leaves
	// BrowserGuard's global frame policy alone.
	if got := w.Header().Get("X-Frame-Options"); got != "" {
		t.Fatalf("X-Frame-Options = %q; the link test should not relax the global frame policy", got)
	}
	if !strings.Contains(w.Body.String(), "LibreSpeed") && !strings.Contains(w.Body.String(), "speedtest.js") {
		t.Fatalf("page does not look like the vendored LibreSpeed page: %.120q", w.Body.String())
	}
	if w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("page Cache-Control = %q, want no-cache", w.Header().Get("Cache-Control"))
	}
}

func TestLinkTestServesItsAssets(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	for path, want := range map[string]string{
		"/link-test/speedtest.js":        "LibreSpeed",
		"/link-test/speedtest_worker.js": "testState",
		"/link-test/javascript/index.js": "server-list.json",
		"/link-test/styling/index.css":   "background",
	} {
		w := linkTestRequest(t, h, cookie, http.MethodGet, path, nil)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s status = %d, want 200", path, w.Code)
			continue
		}
		if !bytes.Contains(w.Body.Bytes(), []byte(want)) {
			t.Errorf("GET %s does not contain %q", path, want)
		}
	}
}

// The fallback rule matters on a surface whose real files are the app: a
// missing asset must not turn into the page, and a path that climbs out of the
// asset root must not reach the source tree.
func TestLinkTestMissingAndEscapingPathsAre404(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	for _, path := range []string{"/link-test/nothing.js", "/link-test/../go.mod", "/link-test/..%2fgo.mod", "/link-test/styling/"} {
		if w := linkTestRequest(t, h, cookie, http.MethodGet, path, nil); w.Code != http.StatusNotFound {
			t.Errorf("GET %s status = %d, want 404", path, w.Code)
		}
	}
}

func TestLinkTestDownloadSendsRequestedChunks(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	w := linkTestRequest(t, h, cookie, http.MethodGet, "/link-test/download?ckSize=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := w.Body.Len(); got != 2*linkTestMiB {
		t.Fatalf("body = %d bytes, want %d", got, 2*linkTestMiB)
	}
	// Repeated bytes would let a compressing step report a speed the wire never
	// carried, so every chunk must be the same random block and the block must
	// not be a constant.
	first := w.Body.Bytes()[:linkTestMiB]
	if !bytes.Equal(first, w.Body.Bytes()[linkTestMiB:]) {
		t.Fatal("download chunks are not the same block")
	}
	if bytes.Equal(first, make([]byte, linkTestMiB)) {
		t.Fatal("download block is all zeros")
	}
}

func TestLinkTestDownloadClampsChunkCount(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	for _, tc := range []struct {
		query string
		want  int
	}{
		{"", 4 * linkTestMiB},
		{"?ckSize=0", 4 * linkTestMiB},
		{"?ckSize=abc", 4 * linkTestMiB},
		{"?ckSize=1", linkTestMiB},
	} {
		w := linkTestRequest(t, h, cookie, http.MethodGet, "/link-test/download"+tc.query, nil)
		if w.Code != http.StatusOK || w.Body.Len() != tc.want {
			t.Errorf("download%s: status %d, %d bytes, want %d", tc.query, w.Code, w.Body.Len(), tc.want)
		}
	}
	// The cap is checked through Content-Length: asking for it would mean
	// transferring a gigabyte to answer a question about a number.
	w := linkTestRequest(t, h, cookie, http.MethodHead, "/link-test/download?ckSize=99999", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("head status = %d", w.Code)
	}
	if got, want := w.Header().Get("Content-Length"), "1073741824"; got != want {
		t.Fatalf("Content-Length = %q, want %q (the %d chunk cap)", got, want, linkTestMaxChunks)
	}
}

func TestLinkTestUploadDiscardsItsBody(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	body := bytes.Repeat([]byte{0x5a}, linkTestMiB)
	w := linkTestRequest(t, h, cookie, http.MethodPost, "/link-test/upload", bytes.NewReader(body))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (%s)", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("upload answered with %d bytes, want none", w.Body.Len())
	}
}

func TestLinkTestPingIsAnEmptyReply(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	w := linkTestRequest(t, h, cookie, http.MethodGet, "/link-test/ping", nil)
	if w.Code != http.StatusOK || w.Body.Len() != 0 {
		t.Fatalf("ping: status %d, %d bytes", w.Code, w.Body.Len())
	}
	if got := w.Header().Get("Content-Length"); got != "0" {
		t.Fatalf("ping Content-Length = %q, want 0", got)
	}
}

func TestLinkTestClientIPNamesTheDeviceTheRouterSees(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	r := httptest.NewRequest(http.MethodGet, "/link-test/ip", nil)
	r.RemoteAddr = "192.168.10.44:51234"
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.LinkTestHandler().ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body struct {
		Processed string `json:"processedString"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("ip body: %v (%s)", err, w.Body.String())
	}
	if body.Processed != "192.168.10.44" {
		t.Fatalf("processedString = %q, want the device address", body.Processed)
	}
}

// The page's own config must point back at this router, relatively, so that a
// box behind a reverse proxy under a subpath still tests itself.
func TestLinkTestServerListPointsAtItself(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	w := linkTestRequest(t, h, cookie, http.MethodGet, "/link-test/server-list.json", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var servers []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &servers); err != nil {
		t.Fatalf("server list: %v (%s)", err, w.Body.String())
	}
	if len(servers) != 1 {
		t.Fatalf("server list has %d entries, want exactly this router", len(servers))
	}
	for _, field := range []string{"server", "dlURL", "ulURL", "pingURL", "getIpURL"} {
		if value, _ := servers[0][field].(string); value == "" {
			t.Fatalf("server list entry has no %s", field)
		}
	}
	if servers[0]["server"] != "./" {
		t.Fatalf("server = %v, want a relative URL", servers[0]["server"])
	}
}

// Telemetry would put a privacy dialog on the page that promises storage this
// build does not do, so it has to stay off.
func TestLinkTestSettingsDisableTelemetry(t *testing.T) {
	h := &HTTP{}
	cookie := linkTestAuthorized(t, h)
	w := linkTestRequest(t, h, cookie, http.MethodGet, "/link-test/settings.json", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var settings map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &settings); err != nil {
		t.Fatalf("settings: %v (%s)", err, w.Body.String())
	}
	if settings["telemetry_level"] != "off" {
		t.Fatalf("telemetry_level = %v, want off", settings["telemetry_level"])
	}
}

func TestLinkTestRoutesAreDeclared(t *testing.T) {
	h := &HTTP{}
	found := false
	for _, route := range h.Routes() {
		if route.Path == "/link-test/session" {
			found = route.Method == http.MethodPost && route.Handler != nil && route.Summary != ""
		}
	}
	if !found {
		t.Fatal("POST /link-test/session is not declared in the tools route table")
	}
}
