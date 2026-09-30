package firewall

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// Boundary is everything this module reads from the rest of olr.
//
// Declared here and answered in internal/daemon, like every other module's
// view, so that `firewall` imports none of the modules it reads and design.md
// §4.1's arrows keep pointing one way.
type Boundary interface {
	// Inside lists the interfaces whose traffic is trusted: every member of a
	// `link` network, and the dial-in tunnel when `remote` has one.
	Inside() ([]string, error)

	// Openings lists what other modules are serving to the outside, and so
	// what must stay reachable from there.
	Openings() ([]Opening, error)

	// InterfaceOf names the interface holding a local address, for the
	// lockout warning. False when no interface holds it.
	InterfaceOf(addr netip.Addr) (string, bool)
}

// Protocol is a transport.
type Protocol string

const (
	TCP Protocol = "tcp"
	UDP Protocol = "udp"
)

// Opening is one port this router serves to the outside, and who asked for it.
//
// Nobody types these. Each is derived from an object that already exists — the
// WireGuard server listens on a port, so that port is open — which is why
// there is no way to leave one behind: remove the object and the next apply
// has one opening fewer.
type Opening struct {
	// For is who asked, in the operator's words: "remote access (WireGuard)".
	For string `json:"for"`

	Protocol Protocol `json:"protocol"`
	Port     uint16   `json:"port"`
}

// Line is the opening's canonical form. It is also the text of the rule that
// implements it, so it names the owner — `nft list table inet olr_filter`
// should say why a port is open, not just that it is.
func (o Opening) Line() string {
	return fmt.Sprintf("nft allow %s port %d for %s", o.Protocol, o.Port, o.For)
}

// StaticBoundary is a Boundary backed by values. What the tests use.
type StaticBoundary struct {
	Interfaces []string
	Open       []Opening
	Addresses  map[netip.Addr]string
	Err        error
}

// Inside implements Boundary.
func (b StaticBoundary) Inside() ([]string, error) { return b.Interfaces, b.Err }

// Openings implements Boundary.
func (b StaticBoundary) Openings() ([]Opening, error) { return b.Open, b.Err }

// InterfaceOf implements Boundary.
func (b StaticBoundary) InterfaceOf(addr netip.Addr) (string, bool) {
	name, ok := b.Addresses[addr.Unmap()]
	return name, ok
}

// normalizeInside sorts and de-duplicates, dropping blanks.
func normalizeInside(in []string) []string {
	out := make([]string, 0, len(in))
	for _, name := range in {
		if name = strings.TrimSpace(name); name != "" {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// normalizeOpenings sorts and de-duplicates by (protocol, port). Two owners of
// one port is a conflict the owners' own validation reports; here the first
// name in sort order is kept, so the rule text is stable.
func normalizeOpenings(in []Opening) []Opening {
	out := append([]Opening{}, in...)
	slices.SortFunc(out, func(a, b Opening) int {
		if c := strings.Compare(string(a.Protocol), string(b.Protocol)); c != 0 {
			return c
		}
		if a.Port != b.Port {
			return int(a.Port) - int(b.Port)
		}
		return strings.Compare(a.For, b.For)
	})
	return slices.CompactFunc(out, func(a, b Opening) bool {
		return a.Protocol == b.Protocol && a.Port == b.Port
	})
}
