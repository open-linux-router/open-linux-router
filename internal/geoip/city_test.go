package geoip

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/runtimedb"
)

func awaitCity(t *testing.T, l *CityLocator, ip netip.Addr) Place {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		places, _ := l.Locate([]netip.Addr{ip})
		if p := places[ip.String()]; p.Country != "" {
			return p
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("location did not arrive")
	return Place{}
}

func TestCityLookupDedupPersistenceAndLocal(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/8.8.8.8" || r.Header.Get("User-Agent") != "open-linux-router" {
			t.Errorf("request: %s %v", r.URL.Path, r.Header)
		}
		w.Write([]byte(`{"ip":"8.8.8.8","country":"United States","country_code":"US","city":"Mountain View","region":"California","latitude":0,"longitude":0,"asn":15169,"asn_organization":"Google LLC"}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "runtime.db")
	db, err := runtimedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	l := NewCity(db)
	l.endpoint = server.URL + "/"
	ip := netip.MustParseAddr("8.8.8.8")
	for range 20 {
		l.Locate([]netip.Addr{ip, ip, netip.MustParseAddr("192.168.1.2")})
	}
	p := awaitCity(t, l, ip)
	if p.City != "Mountain View" || p.Latitude == nil || *p.Latitude != 0 || p.Longitude == nil || p.Org != "Google LLC" {
		t.Fatalf("place: %+v", p)
	}
	l.Close()
	if calls.Load() != 1 {
		t.Fatalf("calls: %d", calls.Load())
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = runtimedb.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	l = NewCity(db)
	defer l.Close()
	got, status := l.Locate([]netip.Addr{ip, netip.MustParseAddr("100.64.1.2")})
	if got[ip.String()].City != p.City || status.Source != "ip.sb" || status.State != Ready || !got["100.64.1.2"].Local {
		t.Fatalf("restart: %+v %+v", got, status)
	}
	if len(l.pending) != 0 {
		t.Fatal("cache hit queued a request")
	}
}

func TestCityStaleAndLRU(t *testing.T) {
	now := time.Now()
	l := &CityLocator{entries: map[string]cityEntry{}, pending: map[string]bool{}, queue: make(chan string, cityCapacity), now: func() time.Time { return now }}
	ip := netip.MustParseAddr("8.8.8.8")
	l.put(ip.String(), cityEntry{place: Place{Country: "US", City: "Old city"}, fetched: now.Add(-cityTTL - time.Hour), used: now.Add(-time.Hour)})
	got, _ := l.Locate([]netip.Addr{ip, ip})
	if got[ip.String()].City != "Old city" || len(l.queue) != 1 {
		t.Fatalf("stale data/queue: %+v %d", got, len(l.queue))
	}
	for i := 0; i < cityCapacity; i++ {
		l.put(netip.AddrFrom4([4]byte{1, byte(i >> 8), byte(i), 1}).String(), cityEntry{used: now.Add(-time.Minute)})
	}
	if len(l.entries) != cityCapacity || l.entries[ip.String()].place.City != "Old city" {
		t.Fatal("recently used entry was evicted")
	}
}

func TestCityRateLimitAndResponseValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/8.8.8.8":
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(429)
		default:
			w.Write([]byte(`{"ip":"1.1.1.1","country_code":"US"}`))
		}
	}))
	defer server.Close()
	l := &CityLocator{client: server.Client(), endpoint: server.URL + "/"}
	_, backoff, err := l.lookup(context.Background(), "8.8.8.8")
	if err == nil || backoff != 2*time.Minute {
		t.Fatalf("429: %v %v", backoff, err)
	}
	if _, _, err := l.lookup(context.Background(), "9.9.9.9"); err == nil {
		t.Fatal("accepted response for another IP")
	}
}

func TestCityWorkerPacingAndShutdownDuringBackoff(t *testing.T) {
	requests := make(chan time.Time, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests <- time.Now()
		if r.URL.Path == "/9.9.9.9" {
			w.WriteHeader(429)
			return
		}
		fmt.Fprintf(w, `{"ip":%q,"country_code":"US","country":"United States"}`, r.URL.Path[1:])
	}))
	defer server.Close()
	l := NewCity(nil)
	l.endpoint = server.URL + "/"
	defer l.Close()
	l.Locate([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("9.9.9.9"), netip.MustParseAddr("1.1.1.1")})
	var times []time.Time
	for range 2 {
		select {
		case at := <-requests:
			times = append(times, at)
		case <-time.After(3 * time.Second):
			t.Fatal("worker stalled")
		}
	}
	if times[1].Sub(times[0]) < cityInterval {
		t.Fatal("worker exceeded request rate")
	}
	select {
	case <-requests:
		t.Fatal("429 did not pause shared queue")
	case <-time.After(800 * time.Millisecond):
	}
	// Deferred Close must cancel the minute-long backoff immediately.
}

func TestLocateNowWaitsForTheAnswerAndReusesTheCache(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/8.8.8.8" {
			t.Errorf("request: %s", r.URL.Path)
		}
		w.Write([]byte(`{"ip":"8.8.8.8","country":"United States","country_code":"US","city":"Mountain View","region":"California","latitude":37.4,"longitude":-122.1,"asn":15169,"asn_organization":"Google LLC"}`))
	}))
	defer server.Close()
	l := NewCity(nil)
	defer l.Close()
	l.endpoint = server.URL + "/"

	ips := []netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("192.168.1.10")}
	places, status := l.LocateNow(context.Background(), ips)
	if status.State != Ready {
		t.Fatalf("status: %+v", status)
	}
	if p := places["8.8.8.8"]; p.City != "Mountain View" || p.ASN != 15169 {
		t.Fatalf("place: %+v", p)
	}
	if !places["192.168.1.10"].Local {
		t.Fatalf("private address was not reported local: %+v", places)
	}
	if calls.Load() != 1 {
		t.Fatalf("calls: %d", calls.Load())
	}
	if _, _ = l.LocateNow(context.Background(), ips); calls.Load() != 1 {
		t.Fatalf("a fresh cache entry was fetched again: %d", calls.Load())
	}
}

func TestLocateNowReportsAFailedLookup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusTooManyRequests)
	}))
	defer server.Close()
	l := NewCity(nil)
	defer l.Close()
	l.endpoint = server.URL + "/"

	places, status := l.LocateNow(context.Background(), []netip.Addr{netip.MustParseAddr("8.8.8.8")})
	if status.State != Unavailable || status.Error == "" {
		t.Fatalf("status: %+v", status)
	}
	if _, ok := places["8.8.8.8"]; ok {
		t.Fatalf("failed lookup produced a place: %+v", places)
	}
}
