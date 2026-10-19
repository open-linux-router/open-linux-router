package daemon

import (
	"github.com/open-linux-router/open-linux-router/internal/dns"
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
// Keyed by network rather than by interface, like dhcpNetworkView and unlike the
// four that still read interfaces. That is right for the same reason: what a
// dial-in client needs pushed into its tunnel is *the subnets somebody
// declared*, not the addresses some interface happens to hold — and a client
// configuration is written once and then lives on a phone, so reading an
// address that later turns out to have been transient would be worse here than
// anywhere else in olr.

// remoteNetworks is remote's window onto link's networks.
type remoteNetworks struct{ facts link.Facts }

// Networks implements remote.NetworkView.
func (r remoteNetworks) Networks() ([]remote.NetworkInfo, error) {
	networks, err := r.facts.Networks()
	if err != nil {
		return nil, err
	}
	out := make([]remote.NetworkInfo, 0, len(networks))
	for _, n := range networks {
		// `Up` is dropped on the way through, deliberately. Whether a network's
		// interface is up right now says nothing about whether its subnet
		// belongs in a client configuration — the phone is being handed a route
		// for the next six months, not for this second.
		out = append(out, remote.NetworkInfo{Name: n.Name, Subnet: n.Subnet})
	}
	return out, nil
}

// dnsDialIn is the dns module's window onto the tunnel (internal/dns/dialin.go).
//
// It exists so that the address in a client configuration's `DNS =` line and
// the address the resolver answers on are the same number, computed once. That
// is the whole fix for "the phone says connected and nothing resolves": the
// resolver used to answer only where an operator had pinned it, while this line
// was written by a module that had never asked.
//
// Read through remote.RouterAddr rather than recomputed here: it is the exact
// function wg_render uses for the client file, so the two cannot drift apart by
// a derivation that was copied instead of called.
type dnsDialIn struct{ store remote.Store }

// DialIn implements dns.DialInView.
func (d dnsDialIn) DialIn() (dns.DialInInfo, error) {
	cfg, err := d.store.Load()
	if err != nil {
		return dns.DialInInfo{}, err
	}
	if !cfg.WireGuard.Enabled {
		return dns.DialInInfo{}, nil
	}
	return dns.DialInInfo{Enabled: true, Addr: cfg.WireGuard.RouterAddr()}, nil
}
