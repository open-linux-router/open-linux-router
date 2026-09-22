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
// dns imports nothing to get them. internal/daemon adapts link's neutral Info into it.
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

// LANPrefixes returns the private networks this router has been given.
//
// It is what an empty allow_from resolves to: the networks olr was handed are
// exactly the networks that should be able to ask it. Deriving this rather than
// defaulting to "everyone" is the difference between a LAN resolver and an
// amplifier (docs/dns.md §5), and with the relay bound to the wildcard it is
// the *only* thing holding that line — there is no input filter chain on this
// box yet (docs/port-forwarding.md §8 has it under "Later").
//
// # Why this reads the interfaces rather than the listen addresses
//
// It used to take each listen address and look up the subnet around it, which
// made it a second consumer of a field that goes stale. When the network
// changed under a box — a new lease, a renumbered upstream, a cable moved — the
// stored listen address stopped existing, this came back empty, and an empty
// allow list denies everybody (Relay.allowed). So a network change took DNS
// down twice over: the relay could not bind, and if it had bound it would have
// answered nobody.
//
// Read straight from link, none of it is stored. renderRelay calls this on
// every render with a live LinkView, so the allow list follows the box without
// anybody having to notice that it moved. design.md §5.2 rules out cross-module
// transactions and there is no notification path between link and dns — this is
// what makes one unnecessary rather than missing.
//
// Private prefixes only, and that is the load-bearing filter now that the
// listen address no longer narrows anything: the WAN is adopted too (gateway
// and firewall need it), so taking every prefix on every adopted interface
// would put the uplink's own subnet in the allow list.
func LANPrefixes(links LinkView) []netip.Prefix {
	infos, err := links.Interfaces()
	if err != nil {
		// Same deferral servedAddress's callers make: every rule that needs this
		// view already reports an unreadable one, and an empty list here is
		// read as "deny everybody" rather than as "we could not tell".
		return nil
	}

	var out []netip.Prefix
	seen := map[netip.Prefix]bool{}
	for _, info := range infos {
		if !info.Adopted {
			// Adoption is the permission (design.md §3.4/§7). A network nobody
			// handed us is not one we start answering for.
			continue
		}
		for _, prefix := range info.Prefixes {
			if !servedAddress(prefix.Addr()) {
				continue
			}
			masked := prefix.Masked()
			if !seen[masked] {
				seen[masked] = true
				out = append(out, masked)
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return comparePrefix(out[i], out[j]) < 0 })
	return out
}

// anyAdopted reports whether the operator has handed this router anything at
// all. It separates "DNS has nothing to serve" from "DNS is misconfigured",
// which are different problems with different fixes.
func anyAdopted(links LinkView) bool {
	infos, err := links.Interfaces()
	if err != nil {
		// Unreadable is not the same as empty, and guessing "empty" here would
		// tell an operator with a working box to go adopt an interface they
		// already adopted.
		return true
	}
	for _, info := range infos {
		if info.Adopted {
			return true
		}
	}
	return false
}

// servedAddress reports whether an address sits on a network this router serves
// — a home network's own address, and nothing else.
//
// Named for what it decides now. It was usableListen, back when it chose which
// addresses the relay would bind; the relay binds the wildcard, so the only
// question left is which prefixes belong in the allow list.
//
// IsPrivate is RFC 1918 and RFC 4193, so it reads both halves of a dual-stack
// LAN and neither half of an uplink. Link-local is excluded separately and is
// worth naming: every IPv6 interface has an fe80:: address, it is not routable
// off the segment, and allowing it would widen the allow list by a prefix that
// every interface on the box shares.
func servedAddress(addr netip.Addr) bool {
	switch {
	case !addr.IsValid(), addr.IsUnspecified(), addr.IsLoopback():
		return false
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast(), addr.IsMulticast():
		return false
	}
	return addr.IsPrivate()
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

// DerivedNotes says, in the operator's words, what this module decided for a
// caller who did not.
//
// design.md §5.6 attaches a condition to behaviour olr supplies by itself: it
// has to say so, on every surface, in the same breath as confirming the change.
// What is supplied by itself has moved — it used to be the listen address, and
// the note read "answering DNS on 192.168.1.91:53 (lan0)". That address is no
// longer chosen by us, because choosing it once was what went stale.
//
// What is chosen for the operator now is who may resolve, and it is the more
// important of the two to publish: the listen address was visible in the
// configuration, while an empty allow_from looks like "no restriction" and
// means the opposite.
func DerivedNotes(c Config, links LinkView) []string {
	if !c.Enabled {
		return nil
	}

	var notes []string
	if len(c.Listen) == 0 {
		notes = append(notes, fmt.Sprintf(
			"answering DNS on every address this router holds, port %d", DNSPort))
	}
	if len(c.AllowFrom) == 0 {
		for _, p := range LANPrefixes(links) {
			notes = append(notes, fmt.Sprintf("resolving for %s", p))
		}
	}
	return notes
}
