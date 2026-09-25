package remote

import (
	"net/netip"
	"slices"
	"strings"
)

// This module's read-only window onto `link`.
//
// Declared here by the consumer and adapted in internal/daemon, which is the
// pattern every other module uses: `remote` imports `link` nowhere, states
// exactly the fact it needs, and keeps no copy of it (design.md §4.1).
//
// One fact, and it does two jobs. It is what `routes: home` expands to in a
// client configuration, and it is what the validator checks the dial-in subnet
// does not overlap. Both would otherwise be a subnet typed twice — once in the
// network and once here — which is precisely the private copy §4.1 exists to
// prevent.

// NetworkView is the window onto the networks this box serves.
type NetworkView interface {
	// Networks returns the configured networks and their subnets.
	//
	// Read per request rather than cached, because a network created after a
	// peer was added has to appear in the next client configuration olr writes.
	Networks() ([]NetworkInfo, error)
}

// NetworkInfo is the subset of a network this module depends on.
type NetworkInfo struct {
	// Name is the operator's word for it — `lan`, `iot`.
	Name string `json:"name"`

	// Subnet is the network's prefix. Invalid for a network that serves no
	// IPv4, which is legal (link.Network.IPv4 is a pointer) and simply means
	// there is nothing here to route home.
	Subnet netip.Prefix `json:"subnet,omitempty"`
}

// HomePrefixes is what `routes: home` means, in order.
//
// The dial-in network first, then the home networks. Its own subnet is included
// because the client has to reach the router's address on it — that is where
// DNS answers from (docs/remote-access.md §6.3) — and a client configuration
// that routed the LAN but not the tunnel would resolve nothing.
//
// IPv4 only, deliberately. olr assigns no IPv6 address inside the tunnel, so a
// v6 prefix in AllowedIPs would send the device's IPv6 traffic into a tunnel
// that cannot carry it: the connection does not fail over to v4, it hangs. The
// day this module addresses the segment in v6 is the day this list grows.
func HomePrefixes(w WireGuard, networks []NetworkInfo) []netip.Prefix {
	out := []netip.Prefix{w.SubnetOrDefault()}
	for _, n := range networks {
		if !n.Subnet.IsValid() || !n.Subnet.Addr().Is4() {
			continue
		}
		if !slices.Contains(out, n.Subnet) {
			out = append(out, n.Subnet)
		}
	}
	return out
}

// StaticNetworks is a NetworkView backed by literal values, for tests and for
// the surfaces that have a config but no daemon to resolve it with.
type StaticNetworks []NetworkInfo

// Networks implements NetworkView.
func (s StaticNetworks) Networks() ([]NetworkInfo, error) { return slices.Clone(s), nil }

// networksOf reads a view that may be nil.
//
// Nil is a legitimate caller rather than a bug: `olr remote show` renders a
// stored config with no view at all, and a nil check here is cheaper than a
// no-op implementation every such caller would have to remember to pass.
func networksOf(v NetworkView) []NetworkInfo {
	if v == nil {
		return nil
	}
	out, err := v.Networks()
	if err != nil {
		return nil
	}
	return out
}

// describePrefixes renders a prefix list the way a WireGuard config spells it.
func describePrefixes(prefixes []netip.Prefix) string {
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		out = append(out, p.String())
	}
	return strings.Join(out, ", ")
}
