package brandlogo

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
)

// Handler serves the bundled marks. Mount it at Prefix and Prefix+"/":
//
//	GET /brand-logos?q=grafana   search the catalogue, at most Limit slugs
//	GET /brand-logos/grafana     one mark, as image/svg+xml
//
// It lives outside /api with the SPA and /download, for the reason Prefix
// gives: an <img> has to be able to draw a mark without carrying a session.
func Handler() http.Handler { return catalogueHandler{} }

type catalogueHandler struct{}

func (catalogueHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if !Built() {
		// A binary compiled without the web build. Say so instead of 404-ing a
		// mark that exists in the library, and let the pickers fall back.
		http.Error(w, "this olrd was built without the brand-logo catalogue; run `make web` before `make build`", http.StatusServiceUnavailable)
		return
	}

	name := strings.Trim(strings.TrimPrefix(r.URL.Path, Prefix), "/")
	if name == "" {
		serveSearch(w, r)
		return
	}
	if !validSlug(name) {
		http.NotFound(w, r)
		return
	}
	serveMark(w, r, name)
}

// serveSearch answers the picker's query.
//
// The SPA no longer downloads the slug list: 7,630 names is more than a picker
// will ever show and more than a router should send to paint a search box. The
// index is here, so the question comes here too.
func serveSearch(w http.ResponseWriter, r *http.Request) {
	slugs := Search(r.URL.Query().Get("q"))
	if slugs == nil {
		slugs = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	// The answer is a pure function of the query and the binary, so a browser
	// may keep it briefly without any invalidation story.
	w.Header().Set("Cache-Control", "public, max-age=300")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	if err := json.NewEncoder(w).Encode(map[string][]string{"slugs": slugs}); err != nil {
		// Nothing useful to do: the status line is already on the wire.
		return
	}
}

func serveMark(w http.ResponseWriter, r *http.Request, slug string) {
	packed, ok := Packed(slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	// The catalogue is fixed at build time, so a mark never changes under its
	// own URL and can be cached hard. The ETag is the content's own hash, which
	// is what makes that safe across an upgrade that changes the artwork.
	sum := sha256.Sum256(packed)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "public, max-age=604800")
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Vary", "Accept-Encoding")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if acceptsGzip(r) {
		// Hand the browser the bytes as they ship. Decompressing here would
		// turn every thumbnail in a picker into work on the router.
		w.Header().Set("Content-Encoding", "gzip")
		if r.Method == http.MethodHead {
			w.Header().Set("Content-Length", strconv.Itoa(len(packed)))
			w.WriteHeader(http.StatusOK)
			return
		}
		_, _ = w.Write(packed)
		return
	}
	// A client that did not ask for gzip — curl, an agent, a cache — gets the
	// mark itself rather than a stream of compressed bytes it cannot read.
	svg, err := SVG(slug)
	if err != nil {
		http.Error(w, "brand logo unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(svg)))
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	_, _ = w.Write(svg)
}

// validSlug mirrors the shape internal/devices and internal/ingress accept for
// a thesvg: value, so a stored icon can always be asked for.
func validSlug(slug string) bool {
	if slug == "" || len(slug) > 96 {
		return false
	}
	for i, r := range slug {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '-':
			if i == 0 {
				return false
			}
		default:
			return false
		}
	}
	return slug[0] != '-'
}

// acceptsGzip reads Accept-Encoding far enough to honour both "gzip" and
// "gzip;q=0", which is a cache saying it will store the compressed form but
// wants something else now.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		encoding, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(encoding), "gzip") {
			continue
		}
		for _, param := range strings.Split(params, ";") {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.EqualFold(key, "q") {
				if weight, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && weight <= 0 {
					return false
				}
			}
		}
		return true
	}
	return false
}
