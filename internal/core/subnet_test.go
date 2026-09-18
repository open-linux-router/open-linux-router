package core

import (
	"net/netip"
	"testing"
)

// These rules moved here from internal/link when internal/dhcp needed the same
// arithmetic to derive a pool from a network's subnet. Two implementations of
// "which addresses may be handed out" would eventually disagree about a /31,
// and the disagreement would surface as a range that validates on one surface
// and not on another.

func TestHostRange(t *testing.T) {
	for _, tc := range []struct {
		prefix     string
		start, end string
		ok         bool
	}{
		{prefix: "192.168.1.2/24", start: "192.168.1.1", end: "192.168.1.254", ok: true},
		{prefix: "192.168.1.2/16", start: "192.168.0.1", end: "192.168.255.254", ok: true},
		{prefix: "10.0.4.1/20", start: "10.0.0.1", end: "10.0.15.254", ok: true},
		{prefix: "10.0.0.1/30", start: "10.0.0.1", end: "10.0.0.2", ok: true},

		// No assignable addresses at all, which is a real answer rather than a
		// range of zero.
		{prefix: "10.0.0.1/31"},
		{prefix: "10.0.0.1/32"},
		{prefix: "fd00::1/64"},
	} {
		t.Run(tc.prefix, func(t *testing.T) {
			start, end, ok := HostRange(netip.MustParsePrefix(tc.prefix))
			if ok != tc.ok {
				t.Fatalf("HostRange(%s) ok = %v, want %v", tc.prefix, ok, tc.ok)
			}
			if !ok {
				return
			}
			if start.String() != tc.start || end.String() != tc.end {
				t.Errorf("HostRange(%s) = %s-%s, want %s-%s", tc.prefix, start, end, tc.start, tc.end)
			}
		})
	}
}

func TestBroadcast(t *testing.T) {
	for _, tc := range []struct{ prefix, want string }{
		{"192.168.1.5/24", "192.168.1.255"},
		{"10.0.0.1/8", "10.255.255.255"},
		{"172.16.4.1/20", "172.16.15.255"},
		// A /0 exercises the shift-by-32 case, which yields 0 in Go and would
		// silently produce the network address instead of the broadcast one.
		{"0.0.0.0/0", "255.255.255.255"},
	} {
		got, ok := Broadcast(netip.MustParsePrefix(tc.prefix))
		if !ok || got.String() != tc.want {
			t.Errorf("Broadcast(%s) = %s (ok=%v), want %s", tc.prefix, got, ok, tc.want)
		}
	}
	if _, ok := Broadcast(netip.MustParsePrefix("fd00::/64")); ok {
		t.Error("Broadcast answered for IPv6, which has no broadcast address")
	}
}

func TestFirstHostIsTheConventionalRouterAddress(t *testing.T) {
	for _, tc := range []struct{ prefix, want string }{
		{"172.16.1.0/24", "172.16.1.1"},
		{"10.0.0.0/8", "10.0.0.1"},
		{"192.168.4.0/22", "192.168.4.1"},
	} {
		got, ok := FirstHost(netip.MustParsePrefix(tc.prefix))
		if !ok || got.String() != tc.want {
			t.Errorf("FirstHost(%s) = %s (ok=%v), want %s", tc.prefix, got, ok, tc.want)
		}
	}
}

// design.md §11.2: the derived range has to leave a low block free, because the
// collision DHCP cannot defend against is a statically configured device inside
// the dynamic range. dnsmasq has no exclusion primitive, so the range itself is
// the only defence.
func TestSuggestRangeLeavesTheStaticBlockFree(t *testing.T) {
	start, end, ok := SuggestRange(netip.MustParsePrefix("172.16.1.0/24"), netip.MustParseAddr("172.16.1.1"))
	if !ok {
		t.Fatal("SuggestRange refused an ordinary /24")
	}
	if start.String() != "172.16.1.100" || end.String() != "172.16.1.254" {
		t.Errorf("= %s-%s, want 172.16.1.100-172.16.1.254", start, end)
	}
}

// Whatever it picks, it must never contain the three addresses a range may not
// contain — otherwise a form prefills something dhcp then refuses, which reads
// as the router contradicting itself.
func TestSuggestRangeAvoidsTheRouterNetworkAndBroadcast(t *testing.T) {
	for _, tc := range []struct{ prefix, router string }{
		{"192.168.1.0/24", "192.168.1.1"},
		{"192.168.1.0/24", "192.168.1.2"},
		{"192.168.1.0/24", "192.168.1.100"},
		{"192.168.1.0/24", "192.168.1.200"},
		{"192.168.1.0/24", "192.168.1.254"},
		{"10.0.0.0/22", "10.0.0.1"},
		{"10.0.0.0/28", "10.0.0.1"},
	} {
		t.Run(tc.prefix+"@"+tc.router, func(t *testing.T) {
			prefix := netip.MustParsePrefix(tc.prefix)
			router := netip.MustParseAddr(tc.router)

			start, end, ok := SuggestRange(prefix, router)
			if !ok {
				t.Fatalf("SuggestRange(%s, %s) refused", tc.prefix, tc.router)
			}
			if InRange(start, end, router) {
				t.Errorf("%s-%s contains the router address %s", start, end, router)
			}
			if network := prefix.Masked().Addr(); InRange(start, end, network) {
				t.Errorf("%s-%s contains the network address %s", start, end, network)
			}
			if bcast, _ := Broadcast(prefix); InRange(start, end, bcast) {
				t.Errorf("%s-%s contains the broadcast address %s", start, end, bcast)
			}
		})
	}
}

// A /8 would otherwise derive sixteen million addresses: legal, and an absurd
// default.
func TestSuggestRangeIsCapped(t *testing.T) {
	start, end, ok := SuggestRange(netip.MustParsePrefix("10.0.0.0/8"), netip.MustParseAddr("10.0.0.1"))
	if !ok {
		t.Fatal("SuggestRange refused a /8")
	}
	if n := RangeSize(start, end); n != MaxSuggested {
		t.Errorf("%s-%s is %d addresses, want the cap of %d", start, end, n, MaxSuggested)
	}
}

// On a small prefix the static block would eat the whole range, so it is given
// up rather than leaving nothing to hand out.
func TestSuggestRangeGivesUpTheStaticBlockBeforeTheRange(t *testing.T) {
	start, end, ok := SuggestRange(netip.MustParsePrefix("10.0.0.0/28"), netip.MustParseAddr("10.0.0.1"))
	if !ok {
		t.Fatal("SuggestRange refused a /28")
	}
	if start.String() != "10.0.0.2" || end.String() != "10.0.0.14" {
		t.Errorf("= %s-%s, want the whole host range 10.0.0.2-10.0.0.14", start, end)
	}
}

func TestSuggestRangeRefusesWhenThereIsNothingToSuggest(t *testing.T) {
	if _, _, ok := SuggestRange(netip.MustParsePrefix("10.0.0.0/31"), netip.MustParseAddr("10.0.0.0")); ok {
		t.Error("SuggestRange offered a range on a /31")
	}
}

func TestRangeSize(t *testing.T) {
	for _, tc := range []struct {
		start, end string
		want       int
	}{
		{"10.0.0.1", "10.0.0.1", 1},
		{"10.0.0.1", "10.0.0.10", 10},
		{"10.0.0.0", "10.0.0.255", 256},
		// Backwards is zero rather than negative: every caller treats the result
		// as a count, and a negative one would silently lower a lease ceiling.
		{"10.0.0.10", "10.0.0.1", 0},
	} {
		got := RangeSize(netip.MustParseAddr(tc.start), netip.MustParseAddr(tc.end))
		if got != tc.want {
			t.Errorf("RangeSize(%s, %s) = %d, want %d", tc.start, tc.end, got, tc.want)
		}
	}
}

// Wrapping would turn "the last address plus one" into 0.0.0.0, which every
// caller here would then treat as a valid range bound.
func TestAddToSaturates(t *testing.T) {
	got := AddTo(netip.MustParseAddr("255.255.255.250"), 100)
	if got.String() != "255.255.255.255" {
		t.Errorf("AddTo saturated to %s, want 255.255.255.255", got)
	}
}

// The diagnosis nothing else in the product can offer (docs/ddns.md §3.2), and
// now the one that keeps a port forward from being debugged for an afternoon
// (docs/firewall.md §5.4). The boundaries are the whole point: /10 is an
// unusual mask and off-by-one here would clear a genuinely unreachable box or
// condemn a reachable one.
func TestCGNATIsRecognised(t *testing.T) {
	for addr, want := range map[string]bool{
		"100.64.0.0":      true, // first
		"100.64.0.1":      true,
		"100.100.100.1":   true,
		"100.127.255.255": true, // last
		"100.63.255.255":  false,
		"100.128.0.0":     false,
		"203.0.113.9":     false,
		"192.168.1.1":     false,
	} {
		if got := IsCGNAT(netip.MustParseAddr(addr)); got != want {
			t.Errorf("IsCGNAT(%s) = %v, want %v", addr, got, want)
		}
	}
}

// An IPv6 address is never CGNAT, and the guard matters: 100.64.0.0/10 is an
// IPv4 prefix, and Prefix.Contains on a mismatched family would answer false
// anyway — but only by accident. IsCGNAT says so on purpose.
func TestCGNATIsIPv4Only(t *testing.T) {
	for _, addr := range []string{"::ffff:100.64.0.1", "fd00::1", "2001:db8::1"} {
		if IsCGNAT(netip.MustParseAddr(addr)) {
			t.Errorf("IsCGNAT(%s) = true, want false", addr)
		}
	}
}
