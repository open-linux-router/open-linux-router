// Package geoip says where an address on the internet is: which country, and
// whose network.
//
// It exists for one question an operator asks of remote access — "who is
// that?" — and it answers from databases kept on the box rather than by asking
// a service. A lookup service would be told the address of everybody who
// connects, which is exactly what an operator would not want a router doing
// behind their back; and it would stop answering the moment the box's way out
// did, which is when an operator most wants to know who is still connected.
//
// The databases are DB-IP's free "lite" editions, country and ASN, published
// monthly under CC BY 4.0 — the attribution is Source, and every surface that
// shows a place carries it. They are fetched when first needed, not shipped in
// the package: a box nobody reaches from outside never asks, and should not
// carry nine megabytes for a question it never has.
//
// Country and network owner, not city. City is 130 MB for the same month, and
// for the addresses this answers about — a phone on a carrier, a laptop at
// home — the network owner is what tells an operator it is theirs.
package geoip

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/netip"
	"path/filepath"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Source is the attribution the licence asks for.
const Source = "DB-IP"

// SourceURL is where Source links to.
const SourceURL = "https://db-ip.com"

// Place is where one address is, as far as the databases know.
type Place struct {
	// Country is the ISO 3166 code, e.g. "CN".
	Country string `json:"country,omitempty"`
	// CountryName is its English name.
	CountryName string `json:"country_name,omitempty"`
	// ASN is the number of the network the address belongs to.
	ASN uint64 `json:"asn,omitempty"`
	// Org is who runs that network — usually an ISP or a mobile carrier.
	Org string `json:"org,omitempty"`
	// Local is an address that is not on the internet at all: a private,
	// carrier-grade NAT or link-local range. Nothing else is set for it.
	Local bool `json:"local,omitempty"`
}

// State is how far the databases are from answering.
type State string

const (
	// Ready: both databases are loaded.
	Ready State = "ready"
	// Fetching: they are being downloaded, and places will appear once they
	// have been.
	Fetching State = "fetching"
	// Unavailable: they are not on the box and the last download failed.
	Unavailable State = "unavailable"
)

// Status says whether places can be trusted to be present, and whose they are.
type Status struct {
	State State  `json:"state"`
	Error string `json:"error,omitempty"`
	// Updated is when the databases on the box were built.
	Updated   *time.Time `json:"updated,omitempty"`
	Source    string     `json:"source"`
	SourceURL string     `json:"source_url"`
}

// maxAge is when a database is fetched again. DB-IP publishes monthly, so this
// is a month and a few days of slack for a release that is late.
const maxAge = 35 * 24 * time.Hour

// retryAfter spaces out downloads that failed, so a box with no way out does
// not try on every status poll.
const retryAfter = time.Hour

// maxDownload bounds what a download may decompress to. The lite databases
// are under 10 MB; this is headroom, not a guess at their size.
const maxDownload = 64 << 20

// editions are the two databases, by the name DB-IP files them under.
var editions = [...]string{"country", "asn"}

// Locator answers Place questions. The zero value is not usable; see New.
type Locator struct {
	dir    string
	client *http.Client
	now    func() time.Time
	// url builds a download address for an edition and a month.
	url func(edition string, month time.Time) string

	mu       sync.Mutex
	dbs      map[string]*DB
	opened   bool
	fetching bool
	lastTry  time.Time
	lastErr  error
}

// New returns a Locator keeping its databases under dir.
func New(dir string) *Locator {
	return &Locator{
		dir:    dir,
		client: &http.Client{Timeout: 5 * time.Minute},
		now:    time.Now,
		url: func(edition string, month time.Time) string {
			return fmt.Sprintf("https://download.db-ip.com/free/dbip-%s-lite-%s.mmdb.gz", edition, month.Format("2006-01"))
		},
	}
}

// RootedDir is where olrd keeps the databases, under an optional root.
func RootedDir(root string) string {
	return filepath.Join(root, "/var/lib/open-linux-router/geoip")
}

// Locate places each address, and reports how complete the answer is.
//
// It never waits on the network. When the databases are missing or old, a
// download starts in the background and this call answers with what is on the
// box — possibly nothing — so a status poll is never the thing that stalls.
func (l *Locator) Locate(addrs []netip.Addr) (map[string]Place, Status) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.opened {
		l.open()
	}
	if len(addrs) > 0 && l.stale() && !l.fetching && l.now().Sub(l.lastTry) >= retryAfter {
		l.fetching, l.lastTry = true, l.now()
		go l.fetch()
	}

	out := make(map[string]Place, len(addrs))
	for _, a := range addrs {
		a = a.Unmap()
		if local(a) {
			out[a.String()] = Place{Local: true}
			continue
		}
		if p, ok := l.place(a); ok {
			out[a.String()] = p
		}
	}
	return out, l.status()
}

func local(a netip.Addr) bool {
	return a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() || core.IsCGNAT(a)
}

func (l *Locator) place(a netip.Addr) (Place, bool) {
	var p Place
	if db := l.dbs["country"]; db != nil {
		if v, ok, _ := db.Lookup(a); ok {
			m, _ := v.(map[string]any)
			country, _ := m["country"].(map[string]any)
			p.Country, _ = country["iso_code"].(string)
			names, _ := country["names"].(map[string]any)
			p.CountryName, _ = names["en"].(string)
		}
	}
	if db := l.dbs["asn"]; db != nil {
		if v, ok, _ := db.Lookup(a); ok {
			m, _ := v.(map[string]any)
			p.ASN = asUint(m["autonomous_system_number"])
			p.Org, _ = m["autonomous_system_organization"].(string)
		}
	}
	return p, p != (Place{})
}

// open loads whatever databases are on disk. Called with mu held.
func (l *Locator) open() {
	l.opened = true
	dbs := map[string]*DB{}
	for _, e := range editions {
		if db, err := Open(l.path(e)); err == nil {
			dbs[e] = db
		}
	}
	l.dbs = dbs
}

// stale reports whether a database is missing or due. Called with mu held.
func (l *Locator) stale() bool {
	for _, e := range editions {
		db := l.dbs[e]
		if db == nil || l.now().Sub(time.Unix(int64(db.Built), 0)) > maxAge {
			return true
		}
	}
	return false
}

func (l *Locator) status() Status {
	s := Status{Source: Source, SourceURL: SourceURL}
	ready := len(l.dbs) == len(editions)
	switch {
	case ready:
		s.State = Ready
	case l.fetching:
		s.State = Fetching
	default:
		s.State = Unavailable
	}
	if l.lastErr != nil && !l.fetching {
		s.Error = l.lastErr.Error()
	}
	var oldest time.Time
	for _, db := range l.dbs {
		if t := time.Unix(int64(db.Built), 0); oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	if !oldest.IsZero() {
		s.Updated = &oldest
	}
	return s
}

func (l *Locator) path(edition string) string {
	return filepath.Join(l.dir, "dbip-"+edition+"-lite.mmdb")
}

// fetch downloads both databases and swaps them in.
func (l *Locator) fetch() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	fresh := map[string]*DB{}
	var errs []error
	for _, e := range editions {
		db, err := l.download(ctx, e)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s database: %w", e, err))
			continue
		}
		fresh[e] = db
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	l.fetching = false
	l.lastErr = errors.Join(errs...)
	for e, db := range fresh {
		l.dbs[e] = db
	}
	if len(errs) == 0 && l.stale() {
		// The newest file there is is already old: the month's release is
		// late. Ask again tomorrow rather than every hour.
		l.lastTry = l.now().Add(24*time.Hour - retryAfter)
	}
}

// download fetches one edition, this month's or else last month's — a new
// month's file appears some days into it — and keeps it on disk.
func (l *Locator) download(ctx context.Context, edition string) (*DB, error) {
	now := l.now().UTC()
	this := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var last error
	for _, month := range []time.Time{this, this.AddDate(0, -1, 0)} {
		b, err := l.get(ctx, l.url(edition, month))
		if err != nil {
			last = err
			continue
		}
		db, err := Parse(b)
		if err != nil {
			return nil, err
		}
		if err := core.WriteFileAtomic(l.path(edition), b, 0o644); err != nil {
			return nil, err
		}
		return db, nil
	}
	return nil, last
}

func (l *Locator) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	z, err := gzip.NewReader(resp.Body)
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(io.LimitReader(z, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxDownload {
		return nil, fmt.Errorf("%s: larger than %d bytes", url, maxDownload)
	}
	return b, nil
}

// NewForTest returns a Locator that downloads from url instead of DB-IP and
// reads the clock from now.
func NewForTest(dir string, url func(edition string, month time.Time) string, now func() time.Time) *Locator {
	l := New(dir)
	l.url, l.now = url, now
	return l
}

// Wait blocks until a download in progress has finished. For tests.
func (l *Locator) Wait() {
	for {
		l.mu.Lock()
		busy := l.fetching
		l.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
