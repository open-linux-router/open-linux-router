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
// The catalogue is ~16 MB of SVG and ~7 MB once each mark is gzipped. Shipping
// it as one file per mark would put 7,630 entries into the embedded filesystem
// and into git for no gain: nothing reads them but this package, and no one
// edits a brand mark by hand. The two generated files are
//
//	logos.bin   one gzip stream per mark, concatenated in slug order
//	logos.idx   "slug<TAB>offset<TAB>length", one line per mark, sorted by slug
//
// which lets the handler answer with a slice of logos.bin and the browser
// decompress it — no decompression on the router, and no per-request memory
// that grows with the catalogue. The index is slugs and offsets only, so the
// search endpoint searches it rather than making the SPA download 7,600 names.
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
	"cmp"
	"compress/gzip"
	"embed"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
	"unicode/utf8"
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

// Search returns the slugs matching query, best match first.
//
// Substring rather than prefix matching is what the picker has always done:
// "link" finds "tp-link", which is the way somebody types a brand they are
// half-remembering. Slugs are the only searchable text, so the catalogue
// carries no names to translate and no categories to keep in sync.
//
// Ranking is exact, then prefix, then anywhere-in-the-slug. Plain catalogue
// order answers "grafana" with the two cloud managed services that also
// contain the word, and the picker shows at most Limit tiles.
//
// Those three passes are literal, so a query that is close but not literal —
// "openlsit" for openlist-badge, "opnls" for the letters a hand actually
// reaches — would otherwise answer with nothing. A fourth pass covers that, and
// it is the last one on purpose: it only runs once the literal passes have left
// room, so a near miss never displaces a mark the query names outright.
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
	// One or two letters are a query the picker is still typing: guessing at
	// them would fill the tiles with noise before the operator reaches the word
	// they meant, and the literal passes already answer every short query.
	if len(found) < Limit && utf8.RuneCountInString(q) > 2 {
		for _, slug := range nearMisses(c, q) {
			if len(found) == Limit {
				break
			}
			if taken[slug] {
				continue
			}
			taken[slug] = true
			found = append(found, slug)
		}
	}
	return found
}

// nearMisses returns every mark close enough to be worth offering for q, best
// guess first.
//
// "Close" is measured in characters the guess has to skip or rewrite, which is
// what makes the order sensible: the pickers draw Limit tiles and a near miss
// is only useful if it is near the top. Alphabetical order would answer a
// mistyped "openlist" with the whole aws-amazon-opensearch family.
func nearMisses(c *catalogue, q string) []string {
	type guess struct {
		slug string
		cost int
		// Where the query's first letter lands in the slug. A guess that
		// starts the name beats one buried inside it: "cady" should reach
		// caddy before adyen.
		anchor int
	}
	// Rows for the edit-distance scan, allocated once: this runs per debounced
	// keystroke on a router, and scoring 7,600 marks does not need 15,000
	// allocations to do it.
	runes := []rune(q)
	rows := make([]int, 2*(len(runes)+1))
	guesses := make([]guess, 0, Limit*2)
	for _, m := range c.marks {
		cost, ok := nearMissCost(runes, m.slug, rows[:len(runes)+1], rows[len(runes)+1:])
		if !ok {
			continue
		}
		anchor := strings.IndexRune(m.slug, runes[0])
		if anchor < 0 {
			anchor = len(m.slug)
		}
		guesses = append(guesses, guess{m.slug, cost, anchor})
	}
	slices.SortFunc(guesses, func(a, b guess) int {
		if a.cost != b.cost {
			return cmp.Compare(a.cost, b.cost)
		}
		if a.anchor != b.anchor {
			return cmp.Compare(a.anchor, b.anchor)
		}
		// Same distance, same start: the shorter name is the more likely
		// brand, and the slug keeps the answer stable between two builds of
		// the catalogue.
		if len(a.slug) != len(b.slug) {
			return cmp.Compare(len(a.slug), len(b.slug))
		}
		return strings.Compare(a.slug, b.slug)
	})
	out := make([]string, len(guesses))
	for i, g := range guesses {
		out[i] = g.slug
	}
	return out
}

// nearMissCost is how far slug is from q, or false when it is too far to show.
//
// Two shapes of near miss: the query's letters are all there in order but with
// something skipped ("opnls" for openlist-badge), or one or two of them are
// wrong or swapped ("openlsit"), which is what a keyboard produces. Both are
// counted in characters, so a skipped letter and a mistyped one cost the same
// and the cheapest guesses sort above the rest.
func nearMissCost(q []rune, slug string, prev, curr []int) (int, bool) {
	if cost, ok := skippedCost(q, slug); ok {
		return cost, true
	}
	return typoCost(q, slug, prev, curr)
}

// skippedCost is the number of characters between the query's first and last
// letter that the query does not account for, when every letter appears in
// order. A contiguous occurrence costs nothing, which is why this only ever
// adds to the substring pass: it finds marks whose letters are in there with
// gaps between them.
func skippedCost(q []rune, slug string) (int, bool) {
	first, last, at := -1, 0, 0
	for i, r := range slug {
		if at == len(q) {
			break
		}
		if r == q[at] {
			if first < 0 {
				first = i
			}
			last = i
			at++
		}
	}
	if at < len(q) {
		return 0, false
	}
	return last - first + 1 - len(q), true
}

// typoCost is the smallest number of single-character edits that turns q into
// part of slug — an insertion, a deletion or a substitution — or false when
// even the closest part of the slug needs more than maxTypos.
//
// This is Levenshtein distance with two changes: the first column is zeroed on
// every row, which lets the match start anywhere in the slug, and the best
// result is kept across rows, which lets it end anywhere. Together they answer
// "how close is q to some substring of slug", which is the question a search
// box asks.
func typoCost(q []rune, slug string, prev, curr []int) (int, bool) {
	const maxTypos, ceiling = 2, 3
	if len(slug)+maxTypos < len(q) {
		return 0, false
	}
	for j := 0; j <= len(q); j++ {
		prev[j] = j
	}
	best := len(q)
	for _, r := range slug {
		curr[0] = 0
		for j := 1; j <= len(q); j++ {
			edit := prev[j-1]
			if q[j-1] != r {
				edit++
			}
			// Everything above the ceiling is the same answer — too far —
			// so the numbers stay small and the arithmetic cannot drift.
			curr[j] = min(edit, prev[j]+1, curr[j-1]+1, ceiling)
		}
		best = min(best, curr[len(q)])
		if best == 0 {
			return 0, true
		}
		copy(prev, curr)
	}
	if best > maxTypos {
		return 0, false
	}
	return best, true
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
