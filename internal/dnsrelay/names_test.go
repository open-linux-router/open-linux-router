package dnsrelay

import (
	"net/netip"
	"testing"
	"time"
)

func obsOf(name string, ttl time.Duration, addrs ...string) Observation {
	o := Observation{Name: name, TTL: ttl}
	for _, a := range addrs {
		o.Addrs = append(o.Addrs, netip.MustParseAddr(a))
	}
	return o
}

func TestNameMapRecordsAndExpires(t *testing.T) {
	m := NewNameMap()
	now := time.Now()
	client := netip.MustParseAddr("192.168.1.10")

	m.Record(client, obsOf("example.com", time.Hour, "93.184.216.34"), now)

	got := m.Snapshot(now)
	if len(got) != 1 || got[0].Name != "example.com" {
		t.Fatalf("Snapshot = %+v", got)
	}
	// TTL plus grace: conntrack flows routinely outlive the TTL that created
	// them, and dropping the entry first loses attribution for a connection
	// that is still open.
	if got[0].Expires.Before(now.Add(time.Hour + NameGrace - time.Second)) {
		t.Errorf("Expires = %s, want TTL plus grace", got[0].Expires.Sub(now))
	}

	if later := m.Snapshot(now.Add(2*time.Hour + NameGrace)); len(later) != 0 {
		t.Errorf("an expired entry survived: %+v", later)
	}
}

// CDNs answer with 30-second TTLs as a matter of course. Without a floor the
// map would be almost always empty, and the feature would look broken rather
// than short-lived.
func TestNameMapHasAMinimumLifetime(t *testing.T) {
	m := NewNameMap()
	now := time.Now()
	m.Record(netip.MustParseAddr("192.168.1.10"), obsOf("cdn.example", 30*time.Second, "1.2.3.4"), now)

	got := m.Snapshot(now)
	if len(got) != 1 {
		t.Fatal("nothing recorded")
	}
	if got[0].Expires.Sub(now) < MinNameLifetime {
		t.Errorf("lifetime = %s, want at least %s", got[0].Expires.Sub(now), MinNameLifetime)
	}
}

// Keyed by (device, address), not by address alone: two devices reaching one
// CDN address by different names is the ordinary case, and global keying would
// attribute one device's traffic to the other's name.
func TestNameMapKeysByDeviceAndAddress(t *testing.T) {
	m := NewNameMap()
	now := time.Now()
	shared := "151.101.1.140"

	m.Record(netip.MustParseAddr("192.168.1.10"), obsOf("news.example", time.Hour, shared), now)
	m.Record(netip.MustParseAddr("192.168.1.20"), obsOf("shop.example", time.Hour, shared), now)

	got := m.Snapshot(now)
	if len(got) != 2 {
		t.Fatalf("held %d entries, want one per device", len(got))
	}
	byClient := map[string]string{}
	for _, n := range got {
		byClient[n.Client.String()] = n.Name
	}
	if byClient["192.168.1.10"] != "news.example" || byClient["192.168.1.20"] != "shop.example" {
		t.Errorf("addresses were cross-attributed between devices: %v", byClient)
	}
}

// One name resolves to several addresses, and nothing here tries to reduce that
// to a single owner.
func TestNameMapIsManyToMany(t *testing.T) {
	m := NewNameMap()
	now := time.Now()
	client := netip.MustParseAddr("192.168.1.10")

	m.Record(client, obsOf("example.com", time.Hour, "1.2.3.4", "5.6.7.8", "2606:2800::1"), now)

	if got := m.Snapshot(now); len(got) != 3 {
		t.Errorf("held %d entries, want one per address", len(got))
	}
}

func TestNameMapKeepsTheChain(t *testing.T) {
	m := NewNameMap()
	now := time.Now()
	o := obsOf("www.example.com", time.Hour, "93.184.216.34")
	o.Chain = []string{"example.cdn.net"}

	m.Record(netip.MustParseAddr("192.168.1.10"), o, now)

	got := m.Snapshot(now)
	if len(got) != 1 || len(got[0].Chain) != 1 || got[0].Chain[0] != "example.cdn.net" {
		t.Errorf("the CNAME chain was lost: %+v", got)
	}
}

// The cap is what stops one device enumerating a subdomain wildcard from
// growing this without limit — the same class of defence as a lease ceiling.
func TestNameMapIsBounded(t *testing.T) {
	m := NewNameMap()
	now := time.Now()
	client := netip.MustParseAddr("192.168.1.10")

	for i := range MaxNames + 500 {
		addr := netip.AddrFrom4([4]byte{byte(i >> 24), byte(i >> 16), byte(i >> 8), byte(i)})
		m.Record(client, Observation{
			Name: "x.example", TTL: time.Hour, Addrs: []netip.Addr{addr},
		}, now)
	}
	if m.Len() > MaxNames {
		t.Errorf("held %d entries, above the %d cap", m.Len(), MaxNames)
	}
}

func TestNameMapRecordIgnoresAnswersWithNoAddresses(t *testing.T) {
	m := NewNameMap()
	now := time.Now()
	m.Record(netip.MustParseAddr("192.168.1.10"), Observation{Name: "nope.example"}, now)
	if m.Len() != 0 {
		t.Errorf("an NXDOMAIN produced a map entry: %d", m.Len())
	}
}

// Two reads with nothing observed between them must agree, or the API reshuffles
// rows under a UI that is polling it.
func TestNameMapSnapshotIsTotallyOrdered(t *testing.T) {
	m := NewNameMap()
	now := time.Now()
	for _, addr := range []string{"1.1.1.1", "2.2.2.2", "3.3.3.3"} {
		m.Record(netip.MustParseAddr("192.168.1.10"), obsOf("example.com", time.Hour, addr), now)
	}

	first := m.Snapshot(now)
	for range 10 {
		next := m.Snapshot(now)
		for i := range first {
			if first[i].Addr != next[i].Addr {
				t.Fatalf("snapshot order is unstable at %d: %s vs %s", i, first[i].Addr, next[i].Addr)
			}
		}
	}
}
