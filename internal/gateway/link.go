package gateway

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

// LinkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: routing depends on link, reads link's
// facts through link, and never keeps its own copy. That is what makes drift
// between "the interface's subnet" and "the prefix we classify on"
// structurally impossible rather than merely unlikely.
//
// The third near-twin of this interface in the tree, after internal/dhcp's and
// internal/dns's, and the third for the reason internal/dns/link.go states:
// each names exactly the facts its own module needs, and internal/daemon adapts link's
// neutral Info into all three. What routing needs and the others do not is the
// *prefixes* — the classifier matches `ip saddr`, so a network's addresses are
// the whole input.
type LinkView interface {
	// Interface returns what is known about an interface, or
	// ErrNoSuchInterface.
	Interface(name string) (LinkInfo, error)

	// Interfaces lists everything link knows about, in a stable order.
	Interfaces() ([]LinkInfo, error)

	// Networks lists the subnets link declares, which is what the egress
	// masquerade's source set is built from (docs/gateway.md §3.9).
	//
	// Intent, not the addresses observed on an interface. A network that has
	// been declared and whose interface has not come up yet still gets its
	// rule, which is the same reason `dhcp` validates a range against a network
	// rather than against a NIC — and it is design.md §4.1's own instruction
	// that dependents read a *network* rather than restating a subnet link
	// already owns.
	Networks() ([]netip.Prefix, error)
}

// UplinkView is this module's read-only window onto `dial`.
//
// One method and one fact: which interface, if any, is the box's own way out.
// design.md §4.1 draws that arrow (`dial → gateway`, "NAT egress iface") and
// this is the first thing to walk it.
//
// It is a second interface rather than a method on LinkView because it names a
// different module's fact. Collapsing them would put `dial` behind a name that
// says `link`, and the day one of them is unavailable the caller could not tell
// which.
type UplinkView interface {
	// Uplink returns the uplink's interface name, or "" when olr does not own
	// the box's way out — which is the reference topology and most boxes.
	Uplink() (string, error)
}

// LinkInfo is the subset of an interface's state that routing decisions depend
// on.
type LinkInfo struct {
	// Name is the kernel interface name.
	Name string `json:"name,omitempty"`

	// Adopted reports whether the operator handed this interface to olr
	// (design.md §7). We refuse to classify traffic from an interface nobody
	// adopted: silently re-routing a network olr was never given is precisely
	// the surprise §3.4 exists to prevent, and it is worse here than in the
	// other two modules because the traffic ends up somewhere else entirely.
	Adopted bool `json:"adopted"`

	// Up reports the operational state. Not an error for configuration
	// purposes: an interface can be legitimately configured while down, so this
	// only ever produces a warning.
	Up bool `json:"up"`

	// Prefixes are the addresses configured on the interface, with masks.
	//
	// Load-bearing twice over. They are what the classifier matches a source
	// against, and they are how a next hop is checked for being directly
	// reachable — §5.1's second row, which catches the operator who typed the
	// proxy's public address instead of its LAN one.
	Prefixes []netip.Prefix `json:"prefixes"`
}

// ErrNoSuchInterface is returned by LinkView.Interface for an unknown name.
var ErrNoSuchInterface = errors.New("no such interface")

// NoUplink is an UplinkView for a box where olr does not own the way out, and
// for the tests that do not care.
type NoUplink struct{}

// Uplink implements UplinkView.
func (NoUplink) Uplink() (string, error) { return "", nil }

// StaticUplink is an UplinkView backed by a name, for tests.
type StaticUplink string

// Uplink implements UplinkView.
func (s StaticUplink) Uplink() (string, error) { return string(s), nil }

// Contains reports whether addr falls inside one of the interface's prefixes.
func (l LinkInfo) Contains(addr netip.Addr) bool {
	for _, p := range l.Prefixes {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}

// PrefixesFor returns the interface's prefixes of one family, sorted, so that
// a rendered ruleset does not churn on map order.
func (l LinkInfo) PrefixesFor(v6 bool) []netip.Prefix {
	var out []netip.Prefix
	for _, p := range l.Prefixes {
		if p.Addr().Is6() == v6 {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// InterfaceWithPrefixContaining finds the adopted interface whose own subnet
// holds addr.
//
// This is the question a next hop asks: *is this address on a network we are
// actually attached to?* An exit pointing at something we cannot reach directly
// produces a route the kernel refuses, so answering it before applying is what
// turns a cryptic netlink error into §5.1's sentence about the wrong address.
func InterfaceWithPrefixContaining(links LinkView, addr netip.Addr) (LinkInfo, bool) {
	all, err := links.Interfaces()
	if err != nil {
		return LinkInfo{}, false
	}
	for _, l := range all {
		if l.Adopted && l.Contains(addr) {
			return l, true
		}
	}
	return LinkInfo{}, false
}

// StaticLinks is a LinkView backed by a map, for tests.
//
// It used to be a production path too: olrd read a hand-written JSON file into
// one of these because there was no link module to ask. There is now, and it
// reads the kernel — so this is the fixture and nothing else.
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

// Networks implements LinkView.
//
// Derived from the adopted interfaces' prefixes rather than from declared
// networks, because a map keyed by interface has nowhere to put the latter.
// That is a test affordance and not the shape the daemon uses — internal/daemon
// reads link's networks, which is what design.md §4.1 asks for.
func (s StaticLinks) Networks() ([]netip.Prefix, error) {
	var out []netip.Prefix
	all, err := s.Interfaces()
	if err != nil {
		return nil, err
	}
	for _, info := range all {
		if !info.Adopted {
			continue
		}
		for _, p := range info.Prefixes {
			if p.Addr().Is4() {
				out = append(out, p.Masked())
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out, nil
}
