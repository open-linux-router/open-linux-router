package nat

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

// LinkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: gateway depends on link, reads link's
// facts through link, and never keeps its own copy. That is what makes drift
// between "the interface's subnet" and "the prefix we masquerade from"
// structurally impossible rather than merely unlikely.
//
// The fourth near-twin of this interface in the tree, after internal/dhcp's,
// internal/dns's and internal/gateway's, and the fourth for the reason
// internal/dns/link.go states: each names exactly the facts its own module
// needs, and internal/daemon adapts link's neutral Info into all of them.
//
// What this module needs the prefixes for is the hairpin masquerade
// (docs/port-forwarding.md §4). The rule has to say *which source addresses* are on the
// same network as the forward's destination, because that is the condition under
// which a reply would otherwise go straight back over shared L2 and bypass the
// router. Deriving that from link rather than from our own config is what stops
// the masquerade range drifting away from the addresses the interface actually
// holds.
type LinkView interface {
	// Interface returns what is known about an interface, or
	// ErrNoSuchInterface.
	Interface(name string) (LinkInfo, error)

	// Interfaces lists everything link knows about, in a stable order.
	Interfaces() ([]LinkInfo, error)
}

// LinkInfo is the subset of an interface's state a forward depends on.
type LinkInfo struct {
	// Name is the kernel interface name.
	Name string `json:"name,omitempty"`

	// Adopted reports whether the operator handed this interface to olr
	// (design.md §7). We refuse to translate traffic arriving on an interface
	// nobody adopted, for the reason internal/gateway gives about classifying
	// it: programming rules on a network olr was never given is precisely the
	// surprise §3.4 exists to prevent.
	Adopted bool `json:"adopted"`

	// Up reports the operational state. Not an error for configuration
	// purposes: an interface can be legitimately configured while down, so this
	// only ever produces a warning.
	Up bool `json:"up"`

	// Prefixes are the addresses configured on the interface, with masks.
	//
	// Load-bearing twice over. They are what the hairpin masquerade matches a
	// source against, and they are how a forward's destination is checked for
	// being somewhere this box can actually deliver to — the check that catches
	// an operator forwarding to an address on a network the router is not
	// attached to.
	Prefixes []netip.Prefix `json:"prefixes"`
}

// ErrNoSuchInterface is returned by LinkView.Interface for an unknown name.
var ErrNoSuchInterface = errors.New("no such interface")

// Contains reports whether addr falls inside one of the interface's prefixes.
func (l LinkInfo) Contains(addr netip.Addr) bool {
	for _, p := range l.Prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// Holds reports whether addr is one of the interface's own addresses, rather
// than merely on one of its networks.
//
// The distinction is what separates "forward to the NAS" from "forward to
// ourselves". The second is a loop that would send the connection back into the
// same prerouting chain, and what the operator meant was a service on this box,
// which needs no forward at all.
func (l LinkInfo) Holds(addr netip.Addr) bool {
	for _, p := range l.Prefixes {
		if p.Addr() == addr {
			return true
		}
	}
	return false
}

// OutwardIPv4 returns the address this interface faces the world with, if it has
// one.
//
// First IPv4 that is not loopback, link-local or unspecified — the same
// selection internal/dial's LinkInfo.PublicIPv4 makes, and for the same stated
// reason: an uplink can legitimately carry several addresses and there is no
// fact available here that would choose between them.
//
// Deliberately *not* named Public, which dial's name has to apologise for in its
// own comment. A private address is returned rather than skipped, because for
// this module that is the interesting answer — it is how validateIn can say
// "this router is behind another one" instead of "no address".
func (l LinkInfo) OutwardIPv4() (netip.Addr, bool) {
	for _, p := range l.Prefixes {
		addr := p.Addr()
		if !addr.Is4() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
			continue
		}
		return addr, true
	}
	return netip.Addr{}, false
}

// PrefixesContaining returns every adopted interface prefix that holds addr,
// sorted, so a rendered ruleset does not churn on map order.
//
// This is the hairpin masquerade's input: the source ranges from which a client
// could reach the destination directly over shared L2, and therefore the ranges
// whose replies have to be brought back through the router.
func PrefixesContaining(links LinkView, addr netip.Addr) []netip.Prefix {
	all, err := links.Interfaces()
	if err != nil {
		return nil
	}
	var out []netip.Prefix
	for _, l := range all {
		if !l.Adopted {
			continue
		}
		for _, p := range l.Prefixes {
			if p.Contains(addr) {
				out = append(out, p.Masked())
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// IsLocalAddress reports whether addr is one of this box's own addresses.
func IsLocalAddress(links LinkView, addr netip.Addr) bool {
	all, err := links.Interfaces()
	if err != nil {
		return false
	}
	for _, l := range all {
		if l.Holds(addr) {
			return true
		}
	}
	return false
}

// InsideInterfaces lists the adopted interfaces a hairpinned connection could
// arrive on: everything except the one the forward faces outward on.
//
// Sorted, and it excludes interfaces with no addresses — an interface holding
// none has no clients on it to hairpin, so a rule for it would match nothing and
// read as though it should.
func InsideInterfaces(links LinkView, outward string) []LinkInfo {
	all, err := links.Interfaces()
	if err != nil {
		return nil
	}
	var out []LinkInfo
	for _, l := range all {
		if !l.Adopted || l.Name == outward || len(l.Prefixes) == 0 {
			continue
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// DialInInterfaces lists the adopted interfaces a dial-in peer's traffic can
// leave by: every network this box serves, minus the tunnel itself and minus
// `dial`'s uplink.
//
// The tunnel is skipped because a rule scoped to it would rewrite peer-to-peer
// packets to this box's address, which is wrong. The uplink is skipped because
// the egress masquerade already owns that direction — the tunnel is in its
// source set — and a second rule matching the same packet would be a line that
// never runs.
//
// An interface with no IPv4 address is skipped too: the rule is scoped by
// output interface, so a network with no IPv4 clients on it has nothing that
// could reply to a peer, and a rule for it would only be text to read past.
func DialInInterfaces(links LinkView, tunnel, uplink string) []LinkInfo {
	all, err := links.Interfaces()
	if err != nil {
		return nil
	}
	var out []LinkInfo
	for _, l := range all {
		if !l.Adopted || l.Name == "" || l.Name == tunnel || l.Name == uplink {
			continue
		}
		if !hasIPv4(l.Prefixes) {
			continue
		}
		out = append(out, l)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// OverlapsServed reports whether prefix shares addresses with a network this
// box serves, excluding the tunnel and the uplink.
//
// The dial-in masquerade is safe only while the tunnel's addresses are foreign
// to every network it rewrites traffic onto. `remote` refuses to create a tunnel
// that overlaps one, but the rule is built from stored intent, and `link` can
// move a network onto the tunnel's prefix afterwards — at which point the rule
// would rewrite the LAN's own traffic to this router and break every device on
// it. So the check is repeated where the rule is built, and an unreadable link
// view counts as an overlap: writing no rule is the safe direction.
//
// The tunnel itself is skipped because its prefix **is** the prefix being
// checked — `link` adopts `wg0` like any other interface, and counting that as a
// conflict would suppress the rule on every box. The uplink is skipped for the
// same reason DialInInterfaces skips it.
func OverlapsServed(links LinkView, prefix netip.Prefix, tunnel, uplink string) bool {
	all, err := links.Interfaces()
	if err != nil {
		return true
	}
	for _, l := range all {
		if !l.Adopted || l.Name == tunnel || l.Name == uplink {
			continue
		}
		for _, p := range l.Prefixes {
			if p.Overlaps(prefix) {
				return true
			}
		}
	}
	return false
}

// hasIPv4 reports whether the interface holds an IPv4 network, which is the
// only family a dial-in peer's traffic can arrive on: `remote` refuses a v6
// subnet, so there is no v6 source address to rewrite.
func hasIPv4(prefixes []netip.Prefix) bool {
	for _, p := range prefixes {
		if p.Addr().Is4() {
			return true
		}
	}
	return false
}

// StaticLinks is a LinkView backed by a map, for tests.
type StaticLinks map[string]LinkInfo

// Interface implements LinkView.
func (s StaticLinks) Interface(name string) (LinkInfo, error) {
	info, ok := s[name]
	if !ok {
		return LinkInfo{}, fmt.Errorf("%q: %w", name, ErrNoSuchInterface)
	}
	if info.Name == "" {
		info.Name = name
	}
	return info, nil
}

// Interfaces implements LinkView, sorted by name so callers get a stable order.
func (s StaticLinks) Interfaces() ([]LinkInfo, error) {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]LinkInfo, 0, len(names))
	for _, name := range names {
		info := s[name]
		if info.Name == "" {
			info.Name = name
		}
		out = append(out, info)
	}
	return out, nil
}
