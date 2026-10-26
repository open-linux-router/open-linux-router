// Package brandlogo holds the brand marks the device and service pickers
// offer, bundled into olrd.
//
// Every one of them used to be hot-linked from raw.githubusercontent.com while
// a page was being drawn. That made a router's own UI depend on GitHub being
// reachable from the browser that is configuring it, and it left a pending
// request in the network panel whenever it was not — which is exactly what a
// box meant to run a home network offline cannot afford. The artwork is the
// same for every installation, so it belongs in the binary, next to the device
// pictures and the OS marks.
//
// # Why a bundle rather than a directory of files
//
// The catalogue is ~14 MB of SVG and ~6 MB once each mark is gzipped. Shipping
// it as one file per mark would put 6,418 entries into the embedded filesystem
// and into git for no gain: nothing reads them but this package, and no one
// edits a brand mark by hand. The two generated files are
//
//	logos.bin   one gzip stream per mark, concatenated in slug order
//	logos.idx   "slug<TAB>offset<TAB>length", one line per mark, sorted by slug
//
// which lets the handler answer with a slice of logos.bin and the browser
// decompress it — no decompression on the router, and no per-request memory
// that grows with the catalogue. The index is slugs and offsets only, so the
// search endpoint searches it rather than making the SPA download 6,400 names.
//
// Both files are generated, not committed: web/scripts/build-brand-logos.mjs
// packs them from the @thesvg/icons dev dependency as part of `npm run build`
// (and `make web`), so updating the artwork is a version bump in
// web/package.json and nothing in the repository diff. They are gitignored,
// and a binary built without Node — `go build` on a fresh clone, or CI's Go
// job — carries no marks: see Built.
package brandlogo

import (
	"bytes"
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
)

// The generated catalogue, produced by web/scripts/build-brand-logos.mjs from
// the @thesvg/icons dev dependency and gitignored like the built SPA. It sits
// next to the code that reads it rather than under internal/webui/assets
// because these are brand marks and not SPA files: the UI asks for them over
// HTTP like any other client.
//
// The directory is embedded rather than the two files so that `go build` works
// on a clone with no Node installed — the same reason internal/webui/assets
// keeps a committed placeholder. A binary built that way simply has no marks,
// and the pickers fall back to the category and vendor pictures.
//
//go:embed data
var assets embed.FS

const (
	bundlePath = "data/logos.bin"
	indexPath  = "data/logos.idx"
)

// Prefix is where the daemon serves the catalogue, outside /api.
//
// Outside, because of the rule internal/daemon already applies to the SPA and
// to /download: static artwork is not a secret, and wrapping it in the API's
// authentication would mean an <img> could only render for a browser that
// happens to carry a session cookie or a bearer token. A mark is the same
// bytes for every installation.
const Prefix = "/brand-logos"

// Limit is the most slugs one search will return.
const Limit = 32

// mark is one catalogue entry: where its gzip stream sits in the bundle.
type mark struct {
	slug   string
	offset int
	length int
}

type catalogue struct {
	marks  []mark
	bySlug map[string]int
	// bundle is the concatenated gzip streams the marks' offsets point into.
	bundle []byte
}

// errNotBuilt is the state of a binary compiled without the web build. It is
// reported by Lookup/Serve as an ordinary absence, not logged per request.
var errNotBuilt = errors.New("brandlogo: this build has no brand-logo catalogue")

var (
	loadOnce sync.Once
	loaded   *catalogue
	loadErr  error
)

func load() (*catalogue, error) {
	loadOnce.Do(func() {
		bundle, err := assets.ReadFile(bundlePath)
		if err != nil {
			// Built without Node. Not an error to report on every request:
			// the empty catalogue is a state the handler answers for.
			loadErr = errNotBuilt
			return
		}
		index, err := assets.ReadFile(indexPath)
		if err != nil || len(bundle) == 0 || len(index) == 0 {
			loadErr = errors.New("brandlogo: the embedded catalogue is incomplete")
			return
		}
		lines := strings.Split(strings.TrimSpace(string(index)), "\n")
		c := &catalogue{marks: make([]mark, 0, len(lines)), bySlug: make(map[string]int, len(lines))}
		for _, line := range lines {
			slug, rest, ok := strings.Cut(line, "\t")
			if !ok {
				loadErr = fmt.Errorf("brandlogo: malformed index line %q", line)
				return
			}
			var offset, length int
			if _, err := fmt.Sscanf(rest, "%d\t%d", &offset, &length); err != nil {
				loadErr = fmt.Errorf("brandlogo: malformed index line %q", line)
				return
			}
			if offset < 0 || length <= 0 || offset+length > len(bundle) {
				// A truncated logos.bin is a corrupted build, not a request to
				// fail at. Refuse the catalogue rather than serving slices of
				// another mark.
				loadErr = fmt.Errorf("brandlogo: entry %q falls outside the bundle", slug)
				return
			}
			c.bySlug[slug] = len(c.marks)
			c.marks = append(c.marks, mark{slug: slug, offset: offset, length: length})
		}
		c.bundle = bundle
		loaded = c
	})
	return loaded, loadErr
}

// Built reports whether this binary carries a catalogue at all.
//
// False is a binary compiled without running the web build, and the daemon says
// so at startup rather than leaving an operator to wonder why the picker is
// empty.
func Built() bool { return Count() > 0 }

// Count is the number of marks in the binary.
func Count() int {
	c, err := load()
	if err != nil {
		return 0
	}
	return len(c.marks)
}

// Search returns the slugs containing query, best match first.
//
// Substring rather than prefix matching is what the picker has always done:
// "link" finds "tp-link", which is the way somebody types a brand they are
// half-remembering. Slugs are the only searchable text, so the catalogue
// carries no names to translate and no categories to keep in sync.
//
// Ranking is exact, then prefix, then anywhere-in-the-slug. Plain catalogue
// order answers "grafana" with the two cloud managed services that also
// contain the word, and the picker shows at most Limit tiles.
func Search(query string) []string {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	c, err := load()
	if err != nil {
		return nil
	}
	found := make([]string, 0, Limit)
	taken := make(map[string]bool, Limit)
	add := func(want func(string) bool) {
		for _, m := range c.marks {
			if len(found) == Limit || taken[m.slug] || !want(m.slug) {
				continue
			}
			taken[m.slug] = true
			found = append(found, m.slug)
		}
	}
	add(func(slug string) bool { return slug == q })
	add(func(slug string) bool { return strings.HasPrefix(slug, q) })
	add(func(slug string) bool { return strings.Contains(slug, q) })
	return found
}

// Packed returns one mark as a gzip stream, ready to be served with
// Content-Encoding: gzip and decompressed by the browser.
func Packed(slug string) ([]byte, bool) {
	c, err := load()
	if err != nil {
		return nil, false
	}
	i, ok := c.bySlug[slug]
	if !ok {
		return nil, false
	}
	m := c.marks[i]
	return c.bundle[m.offset : m.offset+m.length], true
}

// SVG returns one mark decompressed.
//
// For callers that hand the bytes on to something other than a browser — the
// ingress icon route, which answers with the mark itself.
func SVG(slug string) ([]byte, error) {
	packed, ok := Packed(slug)
	if !ok {
		return nil, fmt.Errorf("no brand logo named %q", slug)
	}
	reader, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		return nil, fmt.Errorf("brand logo %q: %w", slug, err)
	}
	defer reader.Close()
	// Bounded well above the largest mark in the catalogue so a corrupted
	// offset cannot be turned into an unbounded allocation.
	const ceiling = 512 << 10
	svg, err := io.ReadAll(io.LimitReader(reader, ceiling+1))
	if err != nil {
		return nil, fmt.Errorf("brand logo %q: %w", slug, err)
	}
	if len(svg) > ceiling {
		return nil, fmt.Errorf("brand logo %q is larger than expected", slug)
	}
	return svg, nil
}
