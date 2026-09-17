package daemon

import (
	"github.com/open-linux-router/open-linux-router/internal/link"
	"github.com/open-linux-router/open-linux-router/internal/remote"
)

// The adapter joining the remote-access module to `link`.
//
// Here rather than inside either, for the reason links.go gives about the other
// five: `remote` declares the facts it needs as its own NetworkView and imports
// `link` nowhere, so design.md §4.1's arrows keep pointing one way and the
// introduction happens at the one place that already knows the whole module
// list.
//
// Keyed by network rather than by interface, like dhcpGroupView and unlike the
// four that still read interfaces. That is right for the same reason: what a
// dial-in client needs pushed into its tunnel is *the subnets somebody
// declared*, not the addresses some interface happens to hold — and a client
// configuration is written once and then lives on a phone, so reading an
// address that later turns out to have been transient would be worse here than
// anywhere else in olr.

// remoteNetworks is remote's window onto link's networks.
type remoteNetworks struct{ facts link.Facts }

// Networks implements remote.NetworkView.
func (n remoteNetworks) Networks() ([]remote.NetworkInfo, error) {
	groups, err := n.facts.Groups()
	if err != nil {
		return nil, err
	}
	out := make([]remote.NetworkInfo, 0, len(groups))
	for _, g := range groups {
		// `Up` is dropped on the way through, deliberately. Whether a network's
		// interface is up right now says nothing about whether its subnet
		// belongs in a client configuration — the phone is being handed a route
		// for the next six months, not for this second.
		out = append(out, remote.NetworkInfo{Name: g.Name, Subnet: g.Subnet})
	}
	return out, nil
}
