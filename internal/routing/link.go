package routing

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"sort"
	"strings"
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
// each names exactly the facts its own module needs, all of them die the day
// link lands, and none should acquire a second consumer in the meantime. What
// routing needs and the others do not is the *prefixes* — the classifier
// matches `ip saddr`, so a network's addresses are the whole input.
type LinkView interface {
	// Interface returns what is known about an interface, or
	// ErrNoSuchInterface.
	Interface(name string) (LinkInfo, error)

	// Interfaces lists everything link knows about, in a stable order.
	Interfaces() ([]LinkInfo, error)
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

// StaticLinks is a LinkView backed by a map.
//
// It is what the tests use, and it is also the honest stand-in until the link
// module lands: routing run against a config file needs interface facts from
// somewhere, and inventing them from the local kernel would be exactly the
// private copy design.md §4.1 forbids.
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

// LoadLinks reads interface facts from a JSON file keyed by interface name:
//
//	{"br-lan": {"adopted": true, "up": true, "prefixes": ["192.168.1.1/24"]}}
//
// Scaffolding with a known expiry date, shared in shape with internal/dhcp and
// internal/dns so one file feeds all three. Once the link module exists
// (design.md milestone 1) it satisfies LinkView directly and this goes away —
// routing must not grow its own way of discovering interfaces, because a second
// source for the same fact is exactly the drift §4.1 is structured to prevent.
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
