package gateway

import (
	"net/netip"
	"testing"
	"time"
)

func TestActivityDifferencesAndGaps(t *testing.T) {
	a := NewActivity()
	at := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
	addr := netip.MustParseAddr("192.168.1.2")
	sample := func(up uint64) []Usage { return []Usage{{Addr: addr, UpBytes: up}} }
	a.observe(at, sample(100), true)
	if len(a.snapshot([]netip.Addr{addr})) != 0 {
		t.Fatal("baseline is not activity")
	}
	a.observe(at.Add(time.Minute), sample(140), true)
	a.observe(at.Add(2*time.Minute), sample(10), true)
	a.observe(at.Add(3*time.Minute), sample(15), true)
	result := a.snapshot([]netip.Addr{addr})
	if len(result) != 1 || result[0].Bytes.Up != 45 {
		t.Fatalf("reset corrupted deltas: %+v", result)
	}
	a.observe(at.Add(10*time.Minute), sample(900), true)
	if a.snapshot([]netip.Addr{addr})[0].Bytes.Up != 45 {
		t.Fatal("gap counted as activity")
	}
	a.observe(at.Add(11*time.Minute), nil, false)
	a.observe(at.Add(12*time.Minute), sample(1000), true)
	if a.snapshot([]netip.Addr{addr})[0].Bytes.Up != 45 {
		t.Fatal("failed sample retained baseline")
	}
	a.observe(at.Add(15*24*time.Hour), sample(2000), true)
	if len(a.snapshot([]netip.Addr{addr})) != 0 {
		t.Fatal("history was not expired")
	}
}

func TestActivityBoundaryZeroAndExitAggregation(t *testing.T) {
	a := NewActivity()
	at := time.Date(2026, 10, 14, 12, 29, 0, 0, time.UTC)
	addr := netip.MustParseAddr("192.168.1.2")
	sample := func(up uint64) []Usage {
		return []Usage{{Addr: addr, UpBytes: up}, {Addr: addr, Exit: "vpn", DownBytes: up}}
	}
	a.observe(at, sample(100), true)
	a.observe(at.Add(time.Minute), sample(200), true)
	if len(a.snapshot([]netip.Addr{addr})) != 0 {
		t.Fatal("boundary interval was attributed to one bucket")
	}
	a.observe(at.Add(2*time.Minute), sample(200), true)
	result := a.snapshot([]netip.Addr{addr})
	if len(result) != 1 || result[0].Bytes.Up != 0 {
		t.Fatal("observed zero was lost")
	}
	a.observe(at.Add(3*time.Minute), sample(250), true)
	result = a.snapshot([]netip.Addr{addr})
	if result[0].Bytes.Up != 50 || result[0].Bytes.Down != 50 {
		t.Fatalf("exits were not aggregated: %+v", result)
	}
}
