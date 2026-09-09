package dhcp

import (
	"errors"
	"fmt"
	"net/netip"
)

// LinkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: dhcp depends on link, reads link's facts
// through link, and never keeps its own copy. That is what makes drift between
// "the interface's subnet" and "the pool's subnet" structurally impossible
// rather than merely unlikely.
//
// The interface is declared here, by the consumer, and stays that way now that
// `link` exists: it states exactly the four facts dhcp needs instead of
// exposing all of link's surface, and dhcp still imports nothing. internal/daemon
// adapts link's neutral Info into this. Three near-identical LinkInfo structs
// across dhcp, dns and routing is the cost, and the thing it buys is that the
// day one of them needs an MTU, the other two do not grow a field they never
// read.
type LinkView interface {
	// Interface returns what is known about an interface, or
	// ErrNoSuchInterface.
	Interface(name string) (LinkInfo, error)
}

// LinkInfo is the subset of an interface's state that DHCP decisions depend on.
type LinkInfo struct {
	// Name is the kernel interface name.
	Name string `json:"name,omitempty"`

	// Adopted reports whether the operator handed this interface to olr
	// (design.md §7). We refuse to serve DHCP on anything else — silently
	// answering DHCP on an interface nobody adopted is precisely the kind of
	// surprise §3.4 exists to prevent.
	Adopted bool `json:"adopted"`

	// Up reports the operational state. Not an error for configuration
	// purposes: an interface can be legitimately configured while down, so this
	// only ever produces a warning.
	Up bool `json:"up"`

	// Prefixes are the addresses configured on the interface, with masks. A
	// pool's range must fall inside one of them.
	Prefixes []netip.Prefix `json:"prefixes"`
}

// ErrNoSuchInterface is returned by LinkView.Interface for an unknown name.
var ErrNoSuchInterface = errors.New("no such interface")

// FindPrefix returns the interface prefix containing addr.
func (l LinkInfo) FindPrefix(addr netip.Addr) (netip.Prefix, bool) {
	for _, p := range l.Prefixes {
		if p.Contains(addr) {
			return p, true
		}
	}
	return netip.Prefix{}, false
}

// Address returns the interface's own address within prefix — the address a
// client should use as its gateway when the pool does not override it.
func (l LinkInfo) Address(prefix netip.Prefix) (netip.Addr, bool) {
	for _, p := range l.Prefixes {
		if p == prefix {
			return p.Addr(), true
		}
	}
	return netip.Addr{}, false
}

// StaticLinks is a LinkView backed by a map, for tests.
//
// It used to be a production path too: olrd read a hand-written JSON file into
// one of these because there was no link module to ask. There is now, and it
// reads the kernel — so this is the fixture and nothing else, which is the
// right size for it. Validation rules are the largest thing in this module and
// the whole point of §5.3.1 is that they can be exercised without a network.
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
