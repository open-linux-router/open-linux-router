// Package webui serves the single-page app that ships inside olrd.
//
// The SPA is embedded in the binary and is a pure client of the API
// (design.md §6.3) — it has no privileged path of its own and can do nothing
// the CLI or an agent cannot. That is the property worth protecting here: if
// something in this package ever grows the ability to change the system without
// going through /api, the UI has stopped being an equal client (§1).
//
// Composite operations belong in core, not here, for the same reason. "Set up a
// guest network" touching link, dhcp, dns and firewall must be a core route, or
// the CLI and agents cannot do it — which would rebuild the exact UniFi
// limitation the project exists to beat.
package webui

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// assets holds the built SPA. `make web` writes vite's output here.
//
// The directory is committed with only a .gitkeep so that `go build` works on a
// fresh clone with no Node installed — embed fails on a missing directory, and
// requiring npm to compile the daemon would be a poor trade for a router.
//
//go:embed all:assets
var assets embed.FS

// Handler serves the SPA, or an explanatory page if it was never built.
func Handler() http.Handler {
	sub, err := fs.Sub(assets, "assets")
	if err != nil {
		return notBuilt("the embedded asset directory is unreadable")
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return notBuilt("this olrd was built without the web UI")
	}
	return &spa{files: sub}
}

type spa struct {
	files fs.FS
}

func (s *spa) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || name == "." {
		s.serveIndex(w, r)
		return
	}

	if f, err := s.files.Open(name); err == nil {
		info, statErr := f.Stat()
		f.Close()
		if statErr == nil && !info.IsDir() {
			s.serveFile(w, r, name)
			return
		}
	}

	// The file does not exist. A client-side route (/dhcp) must still load the
	// app, but a genuinely missing asset must not: returning index.html for a
	// missing .js turns "I forgot to rebuild" into an inscrutable syntax error
	// in the browser console.
	if looksLikeAsset(name) {
		http.NotFound(w, r)
		return
	}
	s.serveIndex(w, r)
}

func (s *spa) serveFile(w http.ResponseWriter, r *http.Request, name string) {
	// Vite fingerprints everything under assets/, so those URLs are immutable
	// and can be cached hard. Anything else might be replaced by an upgrade
	// under the same name.
	if strings.HasPrefix(name, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-cache")
	}
	http.ServeFileFS(w, r, s.files, name)
}

func (s *spa) serveIndex(w http.ResponseWriter, r *http.Request) {
	// Never cached: it names the fingerprinted bundles, so a stale copy after
	// an upgrade points the browser at files that no longer exist.
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFileFS(w, r, s.files, "index.html")
}

// looksLikeAsset reports whether a missing path should 404 rather than fall
// back to the app shell.
func looksLikeAsset(name string) bool {
	if strings.HasPrefix(name, "assets/") {
		return true
	}
	// A dot in the last segment means a file extension, and client-side routes
	// do not have one.
	return strings.Contains(path.Base(name), ".")
}

// notBuilt explains the missing UI rather than 404-ing, which would look
// identical to a broken route.
func notBuilt(reason string) http.Handler {
	body := []byte(`<!doctype html>
<html lang="en">
<head><meta charset="utf-8"><title>olr</title></head>
<body style="font-family: system-ui, sans-serif; margin: 4rem auto; max-width: 40rem; line-height: 1.6">
<h1>Web UI not available</h1>
<p>` + reason + `.</p>
<p>Build it with <code>make web</code>, then rebuild olrd.</p>
<p>The API is unaffected and is serving at <code>/api</code>.</p>
</body>
</html>
`)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write(body)
	})
}
