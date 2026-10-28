package geoip

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

const cityCapacity = 2000
const cityTTL = 30 * 24 * time.Hour
const cityInterval = 600 * time.Millisecond

type cityEntry struct {
	place         Place
	fetched, used time.Time
	retry         time.Time
	touched       time.Time
}

// CityLocator shares a bounded persistent LRU and a single rate-limited worker
// between world traffic and remote clients. Locate never waits for HTTP.
type CityLocator struct {
	mu       sync.Mutex
	entries  map[string]cityEntry
	pending  map[string]bool
	queue    chan string
	db       *runtimedb.Store
	client   *http.Client
	endpoint string
	now      func() time.Time
	lastErr  string
	cancel   context.CancelFunc
	done     chan struct{}
}

func NewCity(db *runtimedb.Store) *CityLocator {
	ctx, cancel := context.WithCancel(context.Background())
	l := &CityLocator{entries: map[string]cityEntry{}, pending: map[string]bool{}, queue: make(chan string, cityCapacity), db: db, client: &http.Client{Timeout: 5 * time.Second}, endpoint: "https://api.ip.sb/geoip/", now: time.Now, cancel: cancel, done: make(chan struct{})}
	if db != nil {
		rows, err := db.GeoIPs()
		if err != nil {
			l.lastErr = err.Error()
			slog.Warn("loading IP location cache", "error", err)
		}
		for _, row := range rows {
			var p Place
			if a, err := netip.ParseAddr(row.IP); err == nil && !local(a) && json.Unmarshal(row.Data, &p) == nil {
				l.entries[row.IP] = cityEntry{place: p, fetched: time.Unix(row.Fetched, 0), used: time.Unix(0, row.Used), touched: time.Unix(0, row.Used)}
			}
		}
	}
	go l.run(ctx)
	return l
}

func (l *CityLocator) Close() { l.cancel(); <-l.done }

func (l *CityLocator) Locate(addrs []netip.Addr) (map[string]Place, Status) {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]Place{}
	now := l.now()
	s := Status{State: Ready, Source: "ip.sb", SourceURL: "https://ip.sb", Error: l.lastErr}
	missing, fetching := false, false
	for _, a := range addrs {
		a = a.Unmap()
		if !a.IsValid() {
			continue
		}
		ip := a.String()
		if local(a) || !a.IsGlobalUnicast() {
			out[ip] = Place{Local: true}
			continue
		}
		e, exists := l.entries[ip]
		if exists {
			e.used = now
			if !e.fetched.IsZero() {
				out[ip] = e.place
				if s.Updated == nil || e.fetched.Before(*s.Updated) {
					t := e.fetched
					s.Updated = &t
				}
			}
			// Bound disk touches to once per minute rather than every UI poll.
			if l.db != nil && !e.fetched.IsZero() && now.Sub(e.touched) >= time.Minute {
				l.touch(ip, now)
				e.touched = now
			}
			l.entries[ip] = e
		}
		if (!exists || e.fetched.IsZero() || now.Sub(e.fetched) >= cityTTL) && !now.Before(e.retry) && !l.pending[ip] {
			select {
			case l.queue <- ip:
				l.pending[ip] = true
			default:
			}
		}
		if e.fetched.IsZero() {
			missing = true
			fetching = fetching || l.pending[ip]
		}
	}
	if missing {
		s.State = Unavailable
		if fetching {
			s.State = Fetching
		}
	}
	return out, s
}

// LocateNow resolves the addresses and waits for the answers.
//
// Locate is the page-polling path: it never waits for HTTP and reports what the
// cache happens to hold. A diagnostic that answers once cannot rely on a second
// poll, so it asks here and gets the place in the same response. Fresh cached
// entries are reused, which keeps a repeated check off the network; the same
// endpoint, validation and cache are used, so the two paths cannot disagree.
func (l *CityLocator) LocateNow(ctx context.Context, addrs []netip.Addr) (map[string]Place, Status) {
	out := map[string]Place{}
	s := Status{State: Ready, Source: "ip.sb", SourceURL: "https://ip.sb"}
	now := l.now()
	for _, a := range addrs {
		a = a.Unmap()
		if !a.IsValid() {
			continue
		}
		ip := a.String()
		if local(a) || !a.IsGlobalUnicast() {
			out[ip] = Place{Local: true}
			continue
		}
		l.mu.Lock()
		e, exists := l.entries[ip]
		l.mu.Unlock()
		if exists && !e.fetched.IsZero() && now.Sub(e.fetched) < cityTTL {
			out[ip] = e.place
			continue
		}
		place, _, err := l.lookup(ctx, ip)
		if err != nil {
			s.State = Unavailable
			s.Error = err.Error()
			continue
		}
		l.mu.Lock()
		l.put(ip, cityEntry{place: place, fetched: l.now(), used: l.now(), touched: l.now()})
		l.mu.Unlock()
		out[ip] = place
	}
	return out, s
}

func (l *CityLocator) touch(ip string, now time.Time) {
	if err := l.db.TouchGeoIP(ip, now.UnixNano()); err != nil {
		l.lastErr = err.Error()
		slog.Warn("touching IP location cache", "error", err)
	}
}

func (l *CityLocator) put(ip string, e cityEntry) {
	if len(l.entries) >= cityCapacity {
		if _, exists := l.entries[ip]; !exists {
			var oldest string
			for key, v := range l.entries {
				if oldest == "" || v.used.Before(l.entries[oldest].used) {
					oldest = key
				}
			}
			delete(l.entries, oldest)
		}
	}
	l.entries[ip] = e
}

func (l *CityLocator) run(ctx context.Context) {
	defer close(l.done)
	var next time.Time
	for {
		var ip string
		select {
		case <-ctx.Done():
			return
		case ip = <-l.queue:
		}
		timer := time.NewTimer(time.Until(next))
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
		p, backoff, err := l.lookup(ctx, ip)
		next = time.Now().Add(cityInterval)
		if backoff > cityInterval {
			next = time.Now().Add(backoff)
		}
		l.mu.Lock()
		delete(l.pending, ip)
		now := l.now()
		e := l.entries[ip]
		e.used = now
		if err != nil {
			l.lastErr = err.Error()
			e.retry = now.Add(time.Minute)
			if backoff > time.Minute {
				e.retry = now.Add(backoff)
			}
		} else {
			e.place, e.fetched, e.retry, e.touched = p, now, time.Time{}, now
			l.lastErr = ""
			if l.db != nil {
				data, _ := json.Marshal(p)
				if err := l.db.SaveGeoIP(runtimedb.GeoIP{IP: ip, Data: data, Fetched: now.Unix(), Used: now.UnixNano()}); err != nil {
					l.lastErr = err.Error()
					slog.Warn("saving IP location cache", "error", err)
				}
			}
		}
		l.put(ip, e)
		l.mu.Unlock()
	}
}

func (l *CityLocator) lookup(ctx context.Context, ip string) (Place, time.Duration, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.endpoint+ip, nil)
	if err != nil {
		return Place{}, 0, err
	}
	req.Header.Set("User-Agent", "open-linux-router")
	resp, err := l.client.Do(req)
	if err != nil {
		return Place{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var backoff time.Duration
		if resp.StatusCode == http.StatusTooManyRequests {
			backoff = time.Minute
			if secs, err := strconv.ParseUint(resp.Header.Get("Retry-After"), 10, 32); err == nil && secs > 60 {
				backoff = time.Duration(secs) * time.Second
			} else if at, err := http.ParseTime(resp.Header.Get("Retry-After")); err == nil && time.Until(at) > backoff {
				backoff = time.Until(at)
			}
		}
		return Place{}, backoff, fmt.Errorf("ip.sb: HTTP %d", resp.StatusCode)
	}
	var v struct {
		IP, Country, City, Region, Organization, ISP string
		ASN                                          uint64   `json:"asn"`
		Latitude                                     *float64 `json:"latitude"`
		Longitude                                    *float64 `json:"longitude"`
		Code                                         string   `json:"country_code"`
		Network                                      string   `json:"asn_organization"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 16384)).Decode(&v); err != nil {
		return Place{}, 0, err
	}
	if a, err := netip.ParseAddr(v.IP); err != nil || a.Unmap().String() != ip || len(v.Code) != 2 {
		return Place{}, 0, fmt.Errorf("ip.sb: invalid location response")
	}
	if v.Latitude == nil || v.Longitude == nil || math.IsNaN(*v.Latitude) || math.IsNaN(*v.Longitude) || math.Abs(*v.Latitude) > 90 || math.Abs(*v.Longitude) > 180 {
		v.Latitude, v.Longitude = nil, nil
	}
	org := v.Network
	if org == "" {
		org = v.Organization
	}
	if org == "" {
		org = v.ISP
	}
	return Place{Country: v.Code, CountryName: v.Country, City: v.City, Region: v.Region, Latitude: v.Latitude, Longitude: v.Longitude, ASN: v.ASN, Org: org}, 0, nil
}
