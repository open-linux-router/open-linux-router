package firewall

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

// LinkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: firewall depends on link, reads link's
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
// (docs/firewall.md §4). The rule has to say *which source addresses* are on the
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
