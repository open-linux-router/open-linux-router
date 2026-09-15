package core

import (
	"encoding/binary"
	"net/netip"
)

// IPv4 subnet arithmetic, shared because three modules were each deriving it.
//
// `link` needed it to suggest a range for a form to prefill, `dhcp` needed it
// to reject a range containing the broadcast address, and both needed to count
// the addresses between two of them. Three implementations of the same eight
// lines is three chances for them to disagree about a /31 — and the one place
// that disagreement would surface is a pool that validates on one surface and
// not on another.
//
// It lives in core rather than in `link` because §4.1's arrow has to keep
// pointing one way: `dhcp` declares the interface facts it needs and never
// imports `link`, so a helper owned by `link` is not one `dhcp` can reach.
// This is arithmetic, not policy — there is no model here for either module to
// own.

// Broadcast returns the IPv4 broadcast address of a prefix.
//
// False for an IPv6 prefix, which has no such address. IPv6 dropped broadcast
// entirely in favour of multicast, so the honest answer is "not applicable"
// rather than some address that happens to be all-ones.
func Broadcast(prefix netip.Prefix) (netip.Addr, bool) {
	prefix = prefix.Masked()
	if !prefix.Addr().Is4() {
		return netip.Addr{}, false
	}
	b := prefix.Addr().As4()
	host := uint(32 - prefix.Bits())
	// A shift of 32 yields 0 in Go, so 1<<32-1 is the all-ones mask a /0 wants.
	v := binary.BigEndian.Uint32(b[:]) | (uint32(1)<<host - 1)
	binary.BigEndian.PutUint32(b[:], v)
	return netip.AddrFrom4(b), true
}

// HostRange returns the assignable addresses in an IPv4 prefix: everything
// between the network and broadcast addresses, exclusive of both.
//
// False for /31 and /32, which have no such addresses. Those are legitimate
// prefixes — a point-to-point link, a host route — and the honest answer is
// that there is nothing to hand out, not a range of zero.
func HostRange(prefix netip.Prefix) (netip.Addr, netip.Addr, bool) {
	if !prefix.Addr().Is4() || prefix.Bits() > 30 {
		return netip.Addr{}, netip.Addr{}, false
	}
	network := prefix.Masked().Addr()
	bcast, ok := Broadcast(prefix)
	if !ok {
		return netip.Addr{}, netip.Addr{}, false
	}
	return network.Next(), bcast.Prev(), true
}

// FirstHost returns the first assignable address in a prefix — the conventional
// router address, and what a group gets when nobody says otherwise.
//
// design.md §11.2 makes this a product requirement rather than a convenience:
// `olr net add iot` has to derive a gateway, and every home network on earth
// puts it at .1.
func FirstHost(prefix netip.Prefix) (netip.Addr, bool) {
	lo, _, ok := HostRange(prefix)
	return lo, ok
}

// MaxSuggested caps how many addresses a derived range offers.
//
// Without it a /16 would derive sixty-five thousand addresses, which is a legal
// range and an absurd default. Not a limit on what an operator may configure —
// `dhcp` will take the whole subnet if asked for it explicitly.
const MaxSuggested = 254

// StaticReserve is the host number a derived range starts at, leaving the
// addresses below it free.
//
// design.md §11.2: the real collision hazard is not a fixed address inside the
// dynamic range, it is a *statically configured* device inside it, which DHCP
// will hand to somebody else. dnsmasq has no exclusion primitive, so the only
// defence is a range that deliberately leaves a static block alone. Deriving it
// gives that for free; asking the operator to type it does not.
//
// 100 rather than some rounder fraction of the prefix because `.100` is what a
// person would have typed, and a derived value that matches the convention is
// one nobody has to check.
const StaticReserve = 100

// MinSuggested is the smallest derived range worth having.
//
// It decides when to give the static block up: on a /26 reserving a hundred
// addresses would leave nothing to hand out, and a range is worth more than a
// convention.
const MinSuggested = 32

// SuggestRange derives a dynamic range inside prefix, avoiding the three
// addresses that cannot be handed out — the network, the broadcast, and router.
//
// On the ordinary case, a /24 with the router at .1, this gives .100–.254: the
// low block stays free for statics, and the result is the range a person would
// have typed. It degrades on small prefixes by giving up the static block
// before it gives up having a range at all.
func SuggestRange(prefix netip.Prefix, router netip.Addr) (netip.Addr, netip.Addr, bool) {
	lo, hi, ok := HostRange(prefix)
	if !ok {
		return netip.Addr{}, netip.Addr{}, false
	}

	start, end := lo, hi
	if InRange(lo, hi, router) {
		// The router splits the host range in two and the larger side wins. With
		// the router at either end — which is every ordinary network — that is
		// simply "everything except the router".
		below, above := RangeSize(lo, router.Prev()), RangeSize(router.Next(), hi)
		if above >= below {
			start, end = router.Next(), hi
		} else {
			start, end = lo, router.Prev()
		}
		if start.Compare(end) > 0 {
			return netip.Addr{}, netip.Addr{}, false
		}
	}

	// The static block is measured from the network address, not from start, so
	// that a /24 yields `.100` regardless of where the router sits.
	if reserved := AddTo(prefix.Masked().Addr(), StaticReserve); InRange(start, end, reserved) &&
		RangeSize(reserved, end) >= MinSuggested {
		start = reserved
	}

	if RangeSize(start, end) > MaxSuggested {
		end = AddTo(start, MaxSuggested-1)
	}
	return start, end, true
}

// RangeSize counts the addresses from start to end inclusive, or 0 if the range
// is empty, backwards, or not IPv4.
func RangeSize(start, end netip.Addr) int {
	if !start.Is4() || !end.Is4() || start.Compare(end) > 0 {
		return 0
	}
	s, e := start.As4(), end.As4()
	return int(binary.BigEndian.Uint32(e[:]) - binary.BigEndian.Uint32(s[:]) + 1)
}

// AddTo returns addr advanced by n addresses, saturating at the end of the
// address space rather than wrapping.
//
// Wrapping would turn "the last address plus one" into 0.0.0.0, which every
// caller here would then treat as a valid range bound.
func AddTo(addr netip.Addr, n int) netip.Addr {
	if !addr.Is4() || n < 0 {
		return addr
	}
	v := addr.As4()
	cur := binary.BigEndian.Uint32(v[:])
	if uint64(cur)+uint64(n) > 0xffffffff {
		return netip.AddrFrom4([4]byte{0xff, 0xff, 0xff, 0xff})
	}
	binary.BigEndian.PutUint32(v[:], cur+uint32(n))
	return netip.AddrFrom4(v)
}

// FirstIPv4 returns the first IPv4 prefix in a list.
//
// First rather than only: an interface can carry several. A box with two IPv4
// subnets on one interface is unusual enough that taking the first is a better
// answer than refusing to answer.
func FirstIPv4(prefixes []netip.Prefix) (netip.Prefix, bool) {
	for _, p := range prefixes {
		if p.Addr().Is4() {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// InRange reports whether addr falls within [start, end] inclusive.
func InRange(start, end, addr netip.Addr) bool {
	if !start.IsValid() || !end.IsValid() || !addr.IsValid() {
		return false
	}
	return start.Compare(addr) <= 0 && addr.Compare(end) <= 0
}
