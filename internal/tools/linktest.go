package tools

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/base64"
	"io"
	"io/fs"
	"net"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The device-to-router speed test, measured in the device's own browser.
//
// The page is LibreSpeed's (vendored under librespeed/, LGPLv3, unmodified); what
// it needs from a server is four dumb endpoints — a byte source, a byte sink, an
// empty reply for latency, and one line naming the client — which are replaced
// here by Go so that nothing on the router has to run PHP.
//
// This surface is deliberately not part of the API. Two reasons, and the first
// is not negotiable: the upload test posts 20 MB of raw bytes, and BrowserGuard
// requires application/json on every /api body — correctly, because JSON is what
// every other route takes. The second is that these endpoints are not a control
// surface: there is nothing here a CLI or an agent would want to call, and
// publishing a bandwidth generator as an MCP tool would be a way to spend a
// router's uplink by asking nicely.
//
// What keeps that unauthenticated-by-API path honest is a short-lived viewer
// cookie minted by POST /api/tools/link-test/session, which sits inside the API
// and is therefore behind the same password or token as everything else. That
// is the pattern internal/inspection already uses to embed mitmweb: the browser
// never invents a second credential, it asks the authenticated surface for a
// scoped one. A client that has not opened this router's UI cannot move bytes.

// LinkTestPrefix is where the embedded page and its endpoints are mounted.
const LinkTestPrefix = "/link-test"

const (
	linkTestCookie = "olr-link-test-viewer"
	// linkTestSessionLifetime caps how long a minted viewer is honoured. The
	// cookie itself carries no expiry, so closing the browser usually ends the
	// grant sooner than this.
	linkTestSessionLifetime = 2 * time.Hour
	linkTestMaxViewers      = 16

	linkTestPage = "index.html"

	// linkTestMiB is both the size of one chunk on the wire and one block in
	// memory: the block is generated once and repeated, which is what keeps
	// the router's CPU out of the measurement.
	linkTestMiB = 1 << 20
	// linkTestMaxChunks matches upstream garbage.php. A larger request would
	// not be a speed test, it would be a request to hold a gigabyte.
	linkTestMaxChunks = 1024
)

//go:embed all:librespeed/static
var linkTestAssets embed.FS

var linkTestFS = func() fs.FS {
	sub, err := fs.Sub(linkTestAssets, "librespeed/static")
	if err != nil {
		panic("tools: embedded link test assets are unreadable: " + err.Error())
	}
	return sub
}()

// linkTestBlock is the chunk every download repeats. Randomness is not a
// security property here — it exists so that no step between the router and the
// browser can compress the payload and report a speed the wire never carried.
// One block is enough, and generating it once keeps the measurement honest: a
// test that spends the router's CPU hashing new bytes would measure the router.
var linkTestBlock = func() []byte {
	block := make([]byte, linkTestMiB)
	if _, err := rand.Read(block); err != nil {
		// A failure here would mean /dev/urandom is broken; a periodic pattern
		// still cannot be compressed, which is all this block is for.
		for i := range block {
			block[i] = byte(i*167 + 13)
		}
	}
	return block
}()

type linkTestViewer struct {
	value   string
	expires time.Time
}

// linkTestSession mints one viewer cookie for this browser.
//
// Scoped to the link test prefix and HttpOnly: the SPA never reads it, and it
// buys nothing outside this surface. The mux already refuses cross-site fetches
// and the cookie is SameSite=Strict, so a page elsewhere cannot ask for one.
func (h *HTTP) linkTestSession(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		core.WriteError(w, http.StatusForbidden, "invalid viewer origin")
		return
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		core.WriteError(w, http.StatusServiceUnavailable, "could not generate a link test session")
		return
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	until := time.Now().Add(linkTestSessionLifetime)

	h.linkMu.Lock()
	kept := h.linkViewers[:0]
	for _, viewer := range h.linkViewers {
		if time.Now().Before(viewer.expires) {
			kept = append(kept, viewer)
		}
	}
	if len(kept) >= linkTestMaxViewers {
		kept = kept[len(kept)-linkTestMaxViewers+1:]
	}
	h.linkViewers = append(kept, linkTestViewer{value: token, expires: until})
	h.linkMu.Unlock()

	// A session cookie on purpose: no Expires and no Max-Age, so closing the
	// browser ends the grant rather than leaving one behind on the device. The
	// store still forgets it after linkTestSessionLifetime, which is what makes
	// the two-hour figure here a ceiling rather than a promise.
	http.SetCookie(w, &http.Cookie{
		Name: linkTestCookie, Value: token, Path: LinkTestPrefix + "/",
		HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil,
	})
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

// linkTestAllowed reports whether this request carries a live viewer cookie.
func (h *HTTP) linkTestAllowed(r *http.Request) bool {
	cookie, err := r.Cookie(linkTestCookie)
	if err != nil {
		return false
	}
	got := []byte(cookie.Value)
	h.linkMu.Lock()
	defer h.linkMu.Unlock()
	for _, viewer := range h.linkViewers {
		want := []byte(viewer.value)
		if len(got) == len(want) && subtle.ConstantTimeCompare(got, want) == 1 && time.Now().Before(viewer.expires) {
			return true
		}
	}
	return false
}

// LinkTestHandler serves the embedded page, its assets and the four endpoints
// the LibreSpeed worker expects. The daemon mounts it outside /api; the prefix
// stays in the request path because the page's own URLs are absolute.
func (h *HTTP) LinkTestHandler() http.Handler {
	return http.HandlerFunc(h.serveLinkTest)
}

func (h *HTTP) serveLinkTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodPost {
		w.Header().Set("Allow", "GET, HEAD, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// BrowserGuard denies framing for the whole box, and it is right to: no
	// other surface may be framed. This one is framed by the tool page that owns
	// it and by nothing else.
	w.Header().Set("X-Frame-Options", "SAMEORIGIN")
	w.Header().Set("Content-Security-Policy", "frame-ancestors 'self'")
	if !h.linkTestAllowed(r) {
		h.linkTestLocked(w, r)
		return
	}
	switch path.Clean(r.URL.Path) {
	case LinkTestPrefix + "/download":
		h.linkTestDownload(w, r)
	case LinkTestPrefix + "/upload":
		h.linkTestUpload(w, r)
	case LinkTestPrefix + "/ping":
		h.linkTestPing(w, r)
	case LinkTestPrefix + "/ip":
		h.linkTestClientIP(w, r)
	case LinkTestPrefix + "/settings.json":
		h.linkTestSettings(w, r)
	case LinkTestPrefix + "/server-list.json":
		h.linkTestServerList(w, r)
	default:
		h.linkTestFile(w, r)
	}
}

// linkTestSettings overrides the worker's defaults. Telemetry stays off: nothing
// here records a result, and a privacy dialog promising otherwise would be a lie
// on the router's own page.
func (h *HTTP) linkTestSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	core.WriteJSON(w, http.StatusOK, map[string]any{
		"telemetry_level": "off",
		"time_dl_max":     12,
		"time_ul_max":     12,
		"time_auto":       true,
	})
}

// linkTestServerList names this router as the only test point.
//
// Upstream expects a list of public speed test servers with absolute URLs. Here
// there is exactly one, reached through relative URLs so that a reverse proxy
// serving this box under a subpath still points at itself.
func (h *HTTP) linkTestServerList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	core.WriteJSON(w, http.StatusOK, []map[string]any{{
		"name": "Open Linux Router", "server": "./", "id": 1,
		"dlURL": "download", "ulURL": "upload", "pingURL": "ping", "getIpURL": "ip",
		"sponsorName": "", "sponsorURL": "",
	}})
}

// linkTestDownload answers garbage.php: N chunks of one MiB, flushed as they go.
func (h *HTTP) linkTestDownload(w http.ResponseWriter, r *http.Request) {
	chunks := linkTestChunkCount(r.URL.Query().Get("ckSize"))
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=random.dat")
	w.Header().Set("Content-Transfer-Encoding", "binary")
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Length", strconv.Itoa(chunks*linkTestMiB))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	flusher, _ := w.(http.Flusher)
	for range chunks {
		if _, err := w.Write(linkTestBlock); err != nil {
			// The worker restarts a stream that fails, so a broken pipe is
			// the normal end of a download test rather than an incident.
			return
		}
		if flusher != nil {
			// Without this the block would sit in the server's buffer and the
			// browser would see one burst at the end instead of a rate.
			flusher.Flush()
		}
		if r.Context().Err() != nil {
			return
		}
	}
}

func linkTestChunkCount(value string) int {
	chunks, err := strconv.Atoi(value)
	if err != nil || chunks < 1 {
		return 4
	}
	if chunks > linkTestMaxChunks {
		return linkTestMaxChunks
	}
	return chunks
}

// linkTestUpload answers empty.php: read the body and throw it away.
//
// It is drained rather than capped or buffered. The whole measurement is how
// fast the bytes can be pushed in, so the cost has to be the wire, and a
// MaxBytesReader would turn a 20 MB test blob into a 413 that reads as "slow".
func (h *HTTP) linkTestUpload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return
	}
	w.WriteHeader(http.StatusOK)
}

// linkTestPing answers empty.php: an empty body, as fast as the box can say it.
func (h *HTTP) linkTestPing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Length", "0")
	w.WriteHeader(http.StatusOK)
}

// linkTestClientIP answers getIP.php. The worker only uses this to label the
// result, and on a LAN there is no ISP to look up: the useful answer is which
// address the router saw, because that is the path being measured.
func (h *HTTP) linkTestClientIP(w http.ResponseWriter, r *http.Request) {
	address := r.RemoteAddr
	if host, _, err := net.SplitHostPort(address); err == nil {
		address = host
	}
	w.Header().Set("Cache-Control", "no-store")
	core.WriteJSON(w, http.StatusOK, map[string]string{"processedString": address, "rawIspInfo": ""})
}

// linkTestFile serves the vendored page and assets.
func (h *HTTP) linkTestFile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+strings.TrimPrefix(r.URL.Path, LinkTestPrefix)), "/")
	if name == "" || name == "." {
		name = linkTestPage
	}
	file, err := linkTestFS.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	info, statErr := file.Stat()
	file.Close()
	if statErr != nil || info.IsDir() {
		http.NotFound(w, r)
		return
	}
	// Not immutable: these names survive an upgrade, so a cached copy would be
	// the previous release's page talking to this release's endpoints.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, linkTestFS, name)
}

// linkTestLocked explains the refusal where the browser can act on it.
func (h *HTTP) linkTestLocked(w http.ResponseWriter, r *http.Request) {
	if path.Clean(r.URL.Path) != LinkTestPrefix && path.Clean(r.URL.Path) != LinkTestPrefix+"/" {
		// An endpoint: the worker turns any failure into a lost stream, and an
		// HTML body would only become a parse error in its log.
		core.WriteError(w, http.StatusUnauthorized, "link test session required; open this test from the router's tools page")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = io.WriteString(w, `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>LibreSpeed</title>
<style>body{font-family:system-ui,sans-serif;margin:3rem auto;max-width:32rem;padding:0 1.5rem;line-height:1.6;color:#222}a{color:#0b5}</style></head>
<body><h1>LibreSpeed</h1>
<p>This test is opened from this router's own tools page, which authorizes this browser for it.</p>
<p><a href="/tools/librespeed">Open Tools &rarr; LibreSpeed</a></p>
</body></html>
`)
}
