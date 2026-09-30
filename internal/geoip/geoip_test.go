package geoip

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// --- a writer, for the tests only --------------------------------------------

// buildDB writes a MaxMind DB holding the given IPv4 networks, laid out the
// way DB-IP's are: an IPv6 tree with IPv4 at ::/96, 24-bit records.
func buildDB(t *testing.T, dbType string, built time.Time, nets map[string]map[string]any) []byte {
	t.Helper()
	type node struct {
		kids [2]*node
		data int
	}
	root := &node{data: -1}
	var data bytes.Buffer
	for cidr, rec := range nets {
		p := netip.MustParsePrefix(cidr)
		off := data.Len()
		encode(&data, rec)
		// IPv4 lives at ::/96.
		var a [16]byte
		v4 := p.Addr().As4()
		copy(a[12:], v4[:])
		n := root
		for i := 0; i < 96+p.Bits(); i++ {
			bit := (a[i/8] >> (7 - i%8)) & 1
			if n.kids[bit] == nil {
				n.kids[bit] = &node{data: -1}
			}
			n = n.kids[bit]
		}
		n.data = off
	}
	// Number internal nodes breadth-first.
	var order []*node
	index := map[*node]int{}
	queue := []*node{root}
	for len(queue) > 0 {
		n := queue[0]
		queue = queue[1:]
		if n.data >= 0 {
			continue
		}
		index[n] = len(order)
		order = append(order, n)
		for _, k := range n.kids {
			if k != nil {
				queue = append(queue, k)
			}
		}
	}
	count := len(order)
	var tree bytes.Buffer
	put24 := func(v int) { tree.Write([]byte{byte(v >> 16), byte(v >> 8), byte(v)}) }
	for _, n := range order {
		for _, k := range n.kids {
			switch {
			case k == nil:
				put24(count)
			case k.data >= 0:
				put24(count + 16 + k.data)
			default:
				put24(index[k])
			}
		}
	}
	var out bytes.Buffer
	out.Write(tree.Bytes())
	out.Write(make([]byte, 16))
	out.Write(data.Bytes())
	out.Write(metadataMarker)
	encode(&out, map[string]any{
		"node_count":    uint64(count),
		"record_size":   uint64(24),
		"ip_version":    uint64(6),
		"database_type": dbType,
		"build_epoch":   uint64(built.Unix()),
	})
	return out.Bytes()
}

func encode(b *bytes.Buffer, v any) {
	ctrl := func(typ int, size int) {
		var c byte
		ext := typ > 7
		if !ext {
			c = byte(typ) << 5
		}
		switch {
		case size < 29:
			c |= byte(size)
			b.WriteByte(c)
			if ext {
				b.WriteByte(byte(typ - 7))
			}
		default:
			c |= 29
			b.WriteByte(c)
			if ext {
				b.WriteByte(byte(typ - 7))
			}
			b.WriteByte(byte(size - 29))
		}
	}
	switch x := v.(type) {
	case string:
		ctrl(typeString, len(x))
		b.WriteString(x)
	case uint64:
		var tmp [8]byte
		binary.BigEndian.PutUint64(tmp[:], x)
		i := 0
		for i < 8 && tmp[i] == 0 {
			i++
		}
		ctrl(typeUint32, 8-i)
		b.Write(tmp[i:])
	case bool:
		n := 0
		if x {
			n = 1
		}
		ctrl(typeBool, n)
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		ctrl(typeMap, len(keys))
		for _, k := range keys {
			encode(b, k)
			encode(b, x[k])
		}
	}
}

func country(code, name string) map[string]any {
	return map[string]any{"country": map[string]any{
		"iso_code": code, "is_in_european_union": false,
		"names": map[string]any{"en": name, "de": name + "!"},
	}}
}

func asn(n uint64, org string) map[string]any {
	return map[string]any{"autonomous_system_number": n, "autonomous_system_organization": org}
}

// --- the reader ---------------------------------------------------------------

func TestLookupFindsTheLongestNetworkAndNothingElse(t *testing.T) {
	db, err := Parse(buildDB(t, "Test", time.Unix(1_700_000_000, 0), map[string]map[string]any{
		"203.0.113.0/24":  country("CN", "China"),
		"198.51.100.0/25": country("US", "United States"),
	}))
	if err != nil {
		t.Fatal(err)
	}
	if db.Type != "Test" || db.Built != 1_700_000_000 {
		t.Errorf("metadata = %q %d", db.Type, db.Built)
	}
	for addr, want := range map[string]string{
		"203.0.113.7":    "CN",
		"198.51.100.20":  "US",
		"198.51.100.200": "",
		"8.8.8.8":        "",
	} {
		v, ok, err := db.Lookup(netip.MustParseAddr(addr))
		if err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
		got := ""
		if ok {
			got = v.(map[string]any)["country"].(map[string]any)["iso_code"].(string)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", addr, got, want)
		}
	}
}

func TestParseRefusesWhatIsNotADatabase(t *testing.T) {
	if _, err := Parse([]byte("hello")); err == nil {
		t.Error("parsed a file with no metadata")
	}
	good := buildDB(t, "Test", time.Now(), map[string]map[string]any{"203.0.113.0/24": country("CN", "China")})
	// Cut into the data section: every read past the end is an error, not a panic.
	cut := append([]byte{}, good[:len(good)/2]...)
	cut = append(cut, good[bytes.LastIndex(good, metadataMarker):]...)
	if db, err := Parse(cut); err == nil {
		_, _, _ = db.Lookup(netip.MustParseAddr("203.0.113.1"))
	}
}

// The real thing, when there is one to hand: OLR_GEOIP_DIR holding DB-IP's
// dbip-country-lite.mmdb and dbip-asn-lite.mmdb. A reader tested only against
// its own writer can share that writer's misreading of the format.
func TestAgainstRealDatabases(t *testing.T) {
	dir := os.Getenv("OLR_GEOIP_DIR")
	if dir == "" {
		t.Skip("OLR_GEOIP_DIR not set")
	}
	l := New(dir)
	places, st := l.Locate([]netip.Addr{netip.MustParseAddr("8.8.8.8"), netip.MustParseAddr("1.1.1.1"), netip.MustParseAddr("2001:4860:4860::8888")})
	if st.State != Ready {
		t.Fatalf("status = %+v", st)
	}
	t.Logf("%+v", places)
	if p := places["8.8.8.8"]; p.Country != "US" || !strings.Contains(strings.ToLower(p.Org), "google") {
		t.Errorf("8.8.8.8 = %+v", p)
	}
	if p := places["2001:4860:4860::8888"]; p.Country == "" {
		t.Errorf("an IPv6 address was not placed: %+v", p)
	}
}

// --- the locator --------------------------------------------------------------

func gz(b []byte) []byte {
	var out bytes.Buffer
	w := gzip.NewWriter(&out)
	w.Write(b)
	w.Close()
	return out.Bytes()
}

func TestLocateFetchesInTheBackgroundAndKeepsWhatItGot(t *testing.T) {
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	files := map[string][]byte{
		// This month's file is not out yet; last month's is.
		"/dbip-country-lite-2026-08.mmdb.gz": gz(buildDB(t, "c", now.AddDate(0, 0, -30), map[string]map[string]any{"203.0.113.0/24": country("CN", "China")})),
		"/dbip-asn-lite-2026-08.mmdb.gz":     gz(buildDB(t, "a", now.AddDate(0, 0, -30), map[string]map[string]any{"203.0.113.0/24": asn(4134, "Chinanet")})),
	}
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		b, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Write(b)
	}))
	defer srv.Close()
	url := func(edition string, month time.Time) string {
		return srv.URL + "/dbip-" + edition + "-lite-" + month.Format("2006-01") + ".mmdb.gz"
	}
	dir := t.TempDir()
	l := NewForTest(dir, url, func() time.Time { return now })

	addrs := []netip.Addr{netip.MustParseAddr("203.0.113.7"), netip.MustParseAddr("192.168.1.9")}
	places, st := l.Locate(addrs)
	if st.State != Fetching {
		t.Errorf("first answer state = %s, want fetching", st.State)
	}
	if !places["192.168.1.9"].Local {
		t.Errorf("a private address is not local: %+v", places)
	}
	l.Wait()

	places, st = l.Locate(addrs)
	if st.State != Ready || st.Source != Source {
		t.Fatalf("after the fetch: %+v", st)
	}
	want := Place{Country: "CN", CountryName: "China", ASN: 4134, Org: "Chinanet"}
	if places["203.0.113.7"] != want {
		t.Errorf("placed %+v, want %+v", places["203.0.113.7"], want)
	}

	// Kept on disk: a new Locator over the same directory answers at once,
	// without asking the network.
	before := hits
	again := NewForTest(dir, url, func() time.Time { return now })
	if p, st := again.Locate(addrs); st.State != Ready || p["203.0.113.7"] != want {
		t.Errorf("from disk: %+v %+v", p, st)
	}
	again.Wait()
	if hits != before {
		t.Errorf("a fresh database was fetched again")
	}
	if _, err := os.Stat(filepath.Join(dir, "dbip-asn-lite.mmdb")); err != nil {
		t.Error(err)
	}
}

func TestAFailedFetchIsReportedAndNotRetriedAtOnce(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer srv.Close()
	now := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	l := NewForTest(t.TempDir(), func(e string, m time.Time) string { return srv.URL + "/" + e }, func() time.Time { return now })
	addrs := []netip.Addr{netip.MustParseAddr("203.0.113.7")}
	l.Locate(addrs)
	l.Wait()
	_, st := l.Locate(addrs)
	if st.State != Unavailable || st.Error == "" {
		t.Errorf("after a failed fetch: %+v", st)
	}
	n := hits
	l.Locate(addrs)
	l.Wait()
	if hits != n {
		t.Errorf("fetched again within the hour")
	}
}

// Nothing to place, nothing fetched: a box nobody reaches from outside never
// downloads anything.
func TestNoAddressesNoDownload(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	l := NewForTest(t.TempDir(), func(e string, m time.Time) string { return srv.URL }, time.Now)
	l.Locate(nil)
	l.Wait()
	if hits != 0 {
		t.Errorf("fetched with nothing to place")
	}
}
