package dial

import (
	"errors"
	"fmt"
	"net/netip"
	"sort"
)

// LinkView is this module's read-only window onto the link module.
//
// design.md §4.1 fixes the direction: `dial` depends on `link`, reads link's
// facts through link, and never keeps its own copy. The fifth near-twin of this
// interface in the tree, after dhcp's, dns's, gateway's and firewall's, and the
// fifth for the reason internal/daemon/links.go gives: each names exactly the
// facts its own module needs, and the binary that mounts them all adapts link's
// neutral Info into every one of them.
//
// What `dial` needs and the others do not is the smallest set of the five: an
// interface's addresses, because publishing one is the whole job, and whether
// the operator adopted it, because reading an address off an interface nobody
// handed over is the surprise design.md §3.4 exists to prevent. It also needs
// the *pair* — an adopted interface with no address is the ordinary state of a
// WAN link that has not come up yet, and saying so is different from saying the
// interface does not exist.
type LinkView interface {
	// Interface returns what is known about an interface, or ErrNoSuchInterface.
	Interface(name string) (LinkInfo, error)

	// Interfaces lists everything link knows about, in a stable order. Used by
	// the CLI's completion and by nothing that decides anything.
	Interfaces() ([]LinkInfo, error)
}

// LinkInfo is the subset of an interface's state a published address depends
// on.
type LinkInfo struct {
	// Name is the kernel interface name.
	Name string `json:"name,omitempty"`

	// Adopted reports whether the operator handed this interface to olr.
	Adopted bool `json:"adopted"`

	// Up reports the operational state. A warning rather than an error: an
	// uplink that is down now will be up later, and refusing to store the
	// record until then would mean the config cannot be written before the line
	// comes up.
	Up bool `json:"up"`

	// Prefixes are the addresses configured on the interface, with masks.
	Prefixes []netip.Prefix `json:"prefixes"`
}

// ErrNoSuchInterface is returned by LinkView.Interface for an unknown name.
var ErrNoSuchInterface = errors.New("no such interface")

// PublicIPv4 returns the address this interface would publish.
//
// First IPv4 that is not loopback and not link-local. "First" rather than
// "only" because an uplink can legitimately carry several and there is no fact
// available here that would choose between them — which is itself an argument
// for the reflector form, since that one asks the internet instead of guessing.
//
// A private address is *returned*, not refused. Behind a modem it is the
// correct reading of this interface and the wrong thing to publish, and saying
// which is the operator's decision to have made when they chose the source
// (docs/ddns.md §3.1) — so the check reports it rather than silently switching
// to the other form.
func (l LinkInfo) PublicIPv4() (netip.Addr, bool) {
	for _, p := range l.Prefixes {
		addr := p.Addr()
		if !addr.Is4() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsUnspecified() {
			continue
		}
		return addr, true
	}
	return netip.Addr{}, false
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
