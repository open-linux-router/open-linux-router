package inspection

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

func TestSiteOfReducesToRegistrableDomain(t *testing.T) {
	cases := map[string]string{
		"rr1---sn-4g5e6nez.googlevideo.com": "googlevideo.com",
		"API.Bilibili.com.":                 "bilibili.com",
		"localhost":                         "localhost",
		"":                                  unnamedSite,
	}
	for name, want := range cases {
		if got := siteOf(name); got != want {
			t.Errorf("siteOf(%q) = %q, want %q", name, got, want)
		}
	}
}

// A device's flow lands under the name the relay saw for that address, and an
// unresolved one lands under 未命名 rather than disappearing.
func TestSampleAttributesAndKeepsUnnamed(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	device := netip.MustParseAddr("172.16.1.92")
	video := netip.MustParseAddr("203.0.113.9")
	other := netip.MustParseAddr("203.0.113.10")

	flows := []Flow{
		{Src: device, Dst: video, Sport: 1, Dport: 443, Proto: "tcp", BytesOut: 100, BytesIn: 1000},
		{Src: device, Dst: other, Sport: 2, Dport: 443, Proto: "tcp", BytesOut: 10, BytesIn: 100},
		// LAN-to-LAN and router-originated traffic never belong to a device's chart.
		{Src: device, Dst: netip.MustParseAddr("172.16.1.1"), Sport: 3, Dport: 80, Proto: "tcp", BytesOut: 5, BytesIn: 5},
	}
	a := NewActivity(
		func(context.Context) ([]Flow, error) { return flows, nil },
		func(context.Context) ([]Name, error) {
			return []Name{{Client: device, Addr: video, Name: "rr1---sn-4g5e6nez.googlevideo.com"}}, nil
		},
	)
	a.now = func() time.Time { return now }
	if err := a.Sample(context.Background()); err != nil {
		t.Fatal(err)
	}

	buckets := a.Snapshot([]netip.Addr{device}, now.Add(-time.Hour))
	if len(buckets) != 1 {
		t.Fatalf("got %d buckets, want 1", len(buckets))
	}
	got := map[string][2]uint64{}
	for _, row := range buckets[0].Sites {
		got[row.Site] = [2]uint64{row.Up, row.Down}
	}
	if want := [2]uint64{100, 1000}; got["googlevideo.com"] != want {
		t.Errorf("googlevideo.com = %v, want %v", got["googlevideo.com"], want)
	}
	if want := [2]uint64{10, 100}; got[unnamedSite] != want {
		t.Errorf("%s = %v, want %v", unnamedSite, got[unnamedSite], want)
	}
	if len(got) != 2 {
		t.Errorf("LAN traffic leaked into the chart: %v", got)
	}
}

// Reading the same counters twice adds nothing the second time.
func TestSampleCountsOnlyTheDelta(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	device := netip.MustParseAddr("172.16.1.92")
	remote := netip.MustParseAddr("203.0.113.9")
	flows := []Flow{{Src: device, Dst: remote, Sport: 1, Dport: 443, Proto: "tcp", BytesOut: 100, BytesIn: 1000}}
	a := NewActivity(func(context.Context) ([]Flow, error) { return flows, nil }, nil)
	a.now = func() time.Time { return now }
	_ = a.Sample(context.Background())
	flows[0].BytesOut, flows[0].BytesIn = 300, 5000
	_ = a.Sample(context.Background())

	rows := a.Snapshot([]netip.Addr{device}, now.Add(-time.Hour))[0].Sites
	if rows[0].Up != 300 || rows[0].Down != 5000 {
		t.Errorf("got %d/%d, want 300/5000", rows[0].Up, rows[0].Down)
	}
}
