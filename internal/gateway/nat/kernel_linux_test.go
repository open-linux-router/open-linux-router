//go:build linux

package nat

import (
	"net/netip"
	"slices"
	"testing"

	"github.com/google/nftables"
)

// The exclusive upper bound is the address after the prefix's *last* one. The
// /24 row is the one that shipped wrong: its bound came out as 172.16.1.1, and
// the egress masquerade matched no device on the network.
func TestNextPrefix(t *testing.T) {
	for _, tc := range []struct {
		prefix string
		want   []byte
	}{
		{"172.16.1.0/24", []byte{172, 16, 2, 0}},
		{"192.168.0.0/16", []byte{192, 169, 0, 0}},
		{"10.0.0.0/8", []byte{11, 0, 0, 0}},
		{"10.1.2.128/25", []byte{10, 1, 3, 0}},
		{"10.1.2.3/32", []byte{10, 1, 2, 4}},
		// Host bits in the operator's spelling do not move the bound.
		{"172.16.1.77/24", []byte{172, 16, 2, 0}},
		// Saturates rather than wrapping to 0.0.0.0.
		{"255.255.255.0/24", []byte{255, 255, 255, 255}},
		{"0.0.0.0/0", []byte{255, 255, 255, 255}},
	} {
		if got := nextPrefix(netip.MustParsePrefix(tc.prefix)); !slices.Equal(got, tc.want) {
			t.Errorf("nextPrefix(%s) = %v; want %v", tc.prefix, got, tc.want)
		}
	}
}

// Every address in a source prefix falls inside the set's [start, end) pair,
// which is the property the masquerade depends on and the one the comment
// lines that drift detection compares cannot see.
func TestEgressSetCoversTheWholeNetwork(t *testing.T) {
	_, elements := egressSet(&nftables.Table{}, []netip.Prefix{
		netip.MustParsePrefix("172.16.1.0/24"),
	})
	if len(elements) != 2 || elements[0].IntervalEnd || !elements[1].IntervalEnd {
		t.Fatalf("elements = %+v; want one start and one interval end", elements)
	}
	start := netip.AddrFrom4([4]byte(elements[0].Key))
	end := netip.AddrFrom4([4]byte(elements[1].Key))

	for _, s := range []string{"172.16.1.0", "172.16.1.1", "172.16.1.123", "172.16.1.255"} {
		a := netip.MustParseAddr(s)
		if a.Less(start) || !a.Less(end) {
			t.Errorf("%s is outside [%s, %s)", a, start, end)
		}
	}
	if out := netip.MustParseAddr("172.16.2.0"); out.Less(end) {
		t.Errorf("%s is inside [%s, %s)", out, start, end)
	}
}

// What egressSet writes, egressSources reads back — in whatever order the
// kernel returns the elements, and for networks that sit edge to edge.
func TestEgressSourcesRoundTrip(t *testing.T) {
	want := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("172.16.1.0/24"),
		netip.MustParsePrefix("172.16.2.0/24"),
		netip.MustParsePrefix("172.16.10.0/24"),
	}
	_, elements := egressSet(&nftables.Table{}, want)
	slices.Reverse(elements)

	got, ok := egressSources(elements)
	if !ok || !slices.Equal(got, want) {
		t.Fatalf("egressSources = %v, %v; want %v", got, ok, want)
	}

	rendered := EgressRule{Out: "ens18", Sources: want, Counter: EgressCounter}
	read := EgressRule{Out: "ens18", Sources: got, Counter: EgressCounter}
	if rendered.Line() != read.Line() {
		t.Errorf("a set read back as\n  %s\nbut renders as\n  %s", read.Line(), rendered.Line())
	}
}

// The table v0.2.4-dev left on boxes. Its comment says /24; its set says /32,
// and reading the set is what makes the difference drift, so that the fixed
// binary replaces it rather than leaving it in place.
func TestTheBrokenEgressSetReadsAsDrift(t *testing.T) {
	broken := []nftables.SetElement{
		{Key: []byte{172, 16, 1, 1}, IntervalEnd: true},
		{Key: []byte{172, 16, 1, 0}},
	}
	got, ok := egressSources(broken)
	if !ok {
		t.Fatal("egressSources refused a well-formed set")
	}
	read := EgressRule{Out: "ens18", Sources: got}
	want := EgressRule{Out: "ens18", Sources: []netip.Prefix{netip.MustParsePrefix("172.16.1.0/24")},
		Counter: EgressCounter}
	if read.Line() == want.Line() {
		t.Errorf("the broken set reads back as the rule it should have been: %s", read.Line())
	}
}

// Elements that are not start/end pairs of prefixes are refused, which the
// caller turns into a line no config renders.
func TestEgressSourcesRefusesWhatItDidNotWrite(t *testing.T) {
	for name, elements := range map[string][]nftables.SetElement{
		"a start with no end": {{Key: []byte{172, 16, 1, 0}}},
		"a range that is not a prefix": {
			{Key: []byte{172, 16, 1, 0}},
			{Key: []byte{172, 16, 1, 100}, IntervalEnd: true},
		},
		"nft's leading end marker": {
			{Key: []byte{0, 0, 0, 0}, IntervalEnd: true},
			{Key: []byte{172, 16, 1, 0}},
			{Key: []byte{172, 16, 2, 0}, IntervalEnd: true},
		},
	} {
		if got, ok := egressSources(elements); ok {
			t.Errorf("%s: egressSources = %v; want refused", name, got)
		}
	}
}
