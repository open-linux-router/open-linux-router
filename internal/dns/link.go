package dns

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

// LinkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: dns depends on link, reads link's facts
// through link, and never keeps its own copy. The interface is declared here,
// by the consumer, for the reason internal/dhcp/link.go gives — it states
// exactly the facts dns needs rather than exposing all of link's surface, and
// dns imports nothing to get them. cmd/olrd adapts link's neutral Info into it.
//
// It is a near-twin of dhcp's, with one method more, and the extra method is
// why they are not one type. dhcp names an interface and asks about it; dns
// names an *address* and has to find which interface, if any, owns it.
type LinkView interface {
	// Interface returns what is known about an interface, or
	// ErrNoSuchInterface.
	Interface(name string) (LinkInfo, error)

	// Interfaces lists everything link knows about, in a stable order.
	Interfaces() ([]LinkInfo, error)
}

// LinkInfo is the subset of an interface's state that DNS decisions depend on.
type LinkInfo struct {
	// Name is the kernel interface name.
	Name string `json:"name,omitempty"`

	// Adopted reports whether the operator handed this interface to olr
	// (design.md §7). We refuse to answer DNS on anything else: a resolver that
	// appeared on an interface nobody adopted is precisely the surprise §3.4
	// forbids, and on a WAN-facing one it is an open resolver.
	Adopted bool `json:"adopted"`

	// Up reports the operational state. Not an error for configuration
	// purposes: an interface can be legitimately configured while down, so this
	// only ever produces a warning.
	Up bool `json:"up"`

	// Prefixes are the addresses configured on the interface, with masks.
	Prefixes []netip.Prefix `json:"prefixes"`
}

// ErrNoSuchInterface is returned by LinkView.Interface for an unknown name.
var ErrNoSuchInterface = errors.New("no such interface")

// HasAddress reports whether addr is one of the interface's own addresses.
func (l LinkInfo) HasAddress(addr netip.Addr) bool {
	for _, p := range l.Prefixes {
		if p.Addr() == addr {
			return true
		}
	}
	return false
}

// FindPrefix returns the interface prefix containing addr.
func (l LinkInfo) FindPrefix(addr netip.Addr) (netip.Prefix, bool) {
	for _, p := range l.Prefixes {
		if p.Contains(addr) {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// InterfaceWithAddress finds the interface that owns an address.
//
// This is the question a listen address asks, and it is why LinkView carries
// Interfaces(): "is 192.168.1.1 ours, and is it adopted" cannot be answered by
// naming an interface, because the operator did not name one.
func InterfaceWithAddress(links LinkView, addr netip.Addr) (LinkInfo, bool) {
	all, err := links.Interfaces()
	if err != nil {
		return LinkInfo{}, false
	}
	for _, info := range all {
		if info.HasAddress(addr) {
			return info, true
		}
	}
	return LinkInfo{}, false
}

// LANPrefixes returns the subnets behind the given listen addresses.
//
// It is what an empty allow_from resolves to: the networks the relay is
// listening on are exactly the networks that should be able to ask it. Deriving
// this rather than defaulting to "everyone" is the difference between a LAN
// resolver and an amplifier (docs/dns.md §5).
func LANPrefixes(links LinkView, listen []netip.AddrPort) []netip.Prefix {
	var out []netip.Prefix
	seen := map[netip.Prefix]bool{}
	for _, l := range listen {
		info, ok := InterfaceWithAddress(links, l.Addr())
		if !ok {
			continue
		}
		prefix, ok := info.FindPrefix(l.Addr())
		if !ok {
			continue
		}
		masked := prefix.Masked()
		if !seen[masked] {
			seen[masked] = true
			out = append(out, masked)
		}
	}
	sort.Slice(out, func(i, j int) bool { return comparePrefix(out[i], out[j]) < 0 })
	return out
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

// Interfaces implements LinkView, sorted by name so callers are deterministic.
func (s StaticLinks) Interfaces() ([]LinkInfo, error) {
	names := make([]string, 0, len(s))
	for name := range s {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]LinkInfo, 0, len(s))
	for _, name := range names {
		info := s[name]
		if info.Name == "" {
			info.Name = name
		}
		out = append(out, info)
	}
	return out, nil
}
