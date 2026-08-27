package dhcp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// LinkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: dhcp depends on link, reads link's facts
// through link, and never keeps its own copy. That is what makes drift between
// "the interface's subnet" and "the pool's subnet" structurally impossible
// rather than merely unlikely.
//
// The interface is declared here, by the consumer, for two reasons. It states
// exactly the four facts dhcp needs instead of exposing all of link's surface,
// and it lets this module be built and tested before link exists — a real
// constraint today, since link is milestone 1 and not yet written.
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

// StaticLinks is a LinkView backed by a map.
//
// It is what the tests use, and it is also the honest stand-in until the link
// module lands: `olr dhcp` run against a config file needs interface facts from
// somewhere, and inventing them from the local kernel would be exactly the
// private copy §4.1 forbids.
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

// LoadLinks reads interface facts from a JSON file keyed by interface name:
//
//	{"br-lan": {"adopted": true, "up": true, "prefixes": ["192.168.1.1/24"]}}
//
// This is scaffolding with a known expiry date. Once the link module exists
// (milestone 1) it satisfies LinkView directly and this goes away — dhcp must
// not grow its own way of discovering interfaces, because a second source for
// the same fact is exactly the drift §4.1 is structured to prevent.
func LoadLinks(path string) (StaticLinks, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading interface facts: %w", err)
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var links StaticLinks
	if err := dec.Decode(&links); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return links, nil
}
