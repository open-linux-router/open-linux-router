package remote

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Turning intent into the two things that leave this package: the kernel state
// the tunnel is, and the file a client imports.
//
// Both are produced here and neither is stored. The first is compared against
// what the kernel actually holds, which is what makes drift free (design.md
// §5.4); the second is handed to the operator once and forgotten, because the
// key it contains is never written down (docs/remote-access.md §4.1).

// Desired is the tunnel as it should be, in values.
//
// No policy below this line: every decision — which address a peer holds, what
// a peer's AllowedIPs are, whether the interface should exist at all — is made
// here against a config, so a Kernel implementation only has to make the kernel
// say this.
type Desired struct {
	// Enabled is false for a tunnel that should not exist. The rest of the
	// struct is still populated, because tearing down needs to know the
	// interface's name.
	Enabled bool

	// Interface is the kernel name.
	Interface string

	// ListenPort is the UDP port.
	ListenPort uint16

	// Addrs are the addresses on the interface. One today — this box's address
	// on the dial-in network, with the network's mask so the kernel gets a
	// connected route covering every peer.
	Addrs []netip.Prefix

	// PrivateKey is the box's own half, base64.
	//
	// Deliberately absent from Lines: the canonical form is printed in plans,
	// diffs and status, and a key that is correct is no less a key. PublicKey
	// stands in for it there, which detects exactly the same changes.
	PrivateKey string

	// PublicKey is derived from PrivateKey, and is what the canonical form
	// carries.
	PublicKey string

	// Peers are the accepted keys.
	Peers []DesiredPeer

	// ExtraConf is the escape hatch, appended verbatim to the file `wg setconf`
	// reads (design.md §3.2 rule 5).
	ExtraConf string
}

// DesiredPeer is one accepted key and what it may send.
type DesiredPeer struct {
	// Name is the operator's label. It is not kernel state — WireGuard knows
	// only keys — so it never reaches Lines, and exists here so that a plan can
	// say "phone" where the kernel would say a base64 blob.
	Name string

	// PublicKey identifies the peer.
	PublicKey string

	// AllowedIPs is the *filter*: which source addresses this key may use. One
	// /32, always. The other meaning of the phrase — what the client sends into
	// the tunnel — is Peer.Routes and lives only in the client configuration
	// (docs/remote-access.md §6.2).
	AllowedIPs []netip.Prefix
}

// Render turns intent into the desired kernel state.
//
// It assumes the config has been validated; an unvalidated one produces
// nonsense rather than an error, which is why every caller validates first
// (design.md §5.3.1 — the whole value of validation is that it happens before
// anything is written).
func Render(c Config) Desired {
	w := c.WireGuard
	d := Desired{
		Enabled:    w.Enabled,
		Interface:  w.InterfaceOrDefault(),
		ListenPort: w.PortOrDefault(),
		PrivateKey: w.PrivateKey,
		ExtraConf:  strings.TrimSpace(w.ExtraConf),
	}
	if !w.Enabled {
		// Nothing else is rendered for a tunnel that should not exist. Filling
		// the peers in would make a disabled config diff against an absent
		// interface as though every peer were about to be removed, which is a
		// plan an operator would read as an outage rather than as "off".
		return d
	}

	if public, err := PublicKeyFor(w.PrivateKey); err == nil {
		d.PublicKey = public
	}
	d.Addrs = []netip.Prefix{w.RouterPrefix()}

	for _, p := range w.Peers {
		if !p.Address.IsValid() {
			// Refused by the validator, so this is unreachable in practice. A
			// peer with no address still must not be rendered with an empty
			// AllowedIPs, which WireGuard reads as "this key may claim any
			// source address".
			continue
		}
		d.Peers = append(d.Peers, DesiredPeer{
			Name:       p.Name,
			PublicKey:  p.PublicKey,
			AllowedIPs: []netip.Prefix{netip.PrefixFrom(p.Address, p.Address.BitLen())},
		})
	}
	sort.SliceStable(d.Peers, func(i, j int) bool { return d.Peers[i].PublicKey < d.Peers[j].PublicKey })
	return d
}

// --- canonical text ---------------------------------------------------------

// Lines renders the desired state as canonical text.
//
// One representation does three jobs, the same way internal/gateway's does: it
// is the diff basis, so `olr diff` shows a tunnel change as lines rather than
// as "3 things will change"; it is what the CLI prints; and it is what the
// kernel's own state is reduced to before the two are compared, so drift is a
// string comparison rather than a structural walk.
//
// The private key is not here. See Desired.PrivateKey — the public half changes
// exactly when the private half does, so nothing is lost by printing it
// instead.
func (d Desired) Lines() []string {
	if !d.Enabled {
		return nil
	}

	out := []string{
		fmt.Sprintf("interface %s", d.Interface),
		fmt.Sprintf("listen-port %d", d.ListenPort),
	}
	if d.PublicKey != "" {
		out = append(out, fmt.Sprintf("public-key %s", d.PublicKey))
	}
	for _, a := range d.Addrs {
		out = append(out, fmt.Sprintf("address %s", a))
	}
	for _, p := range d.Peers {
		out = append(out, peerLine(p.PublicKey, p.AllowedIPs))
	}
	sort.Strings(out)
	return out
}

// peerLine is the canonical form of one peer, used for both the desired and the
// observed side so that the comparison is like with like.
func peerLine(publicKey string, allowed []netip.Prefix) string {
	return fmt.Sprintf("peer %s allowed-ips %s", publicKey, describePrefixes(allowed))
}

// --- the file `wg setconf` reads --------------------------------------------

// ServerConf renders the configuration handed to `wg setconf`.
//
// It is written mode 0600, read once, and removed (kernel_linux.go). Keeping it
// would mean a second copy of the box's private key at rest, read by nothing —
// the document already holds the one copy design.md §3.4 says the operator's
// data lives in.
//
// Note what is *not* in it: the address, and any route. `wg setconf` configures
// keys and peers; addressing the interface is olr's, through netlink. That
// division is the whole of docs/remote-access.md §5.1 — it is the part
// `wg-quick` would have done, and the part that would have made `olr gateway`
// refuse to apply.
func (d Desired) ServerConf() string {
	var b strings.Builder
	b.WriteString(header(d.Interface))

	b.WriteString("\n[Interface]\n")
	fmt.Fprintf(&b, "PrivateKey = %s\n", d.PrivateKey)
	fmt.Fprintf(&b, "ListenPort = %d\n", d.ListenPort)

	for _, p := range d.Peers {
		b.WriteString("\n[Peer]\n")
		if p.Name != "" {
			fmt.Fprintf(&b, "# %s\n", p.Name)
		}
		fmt.Fprintf(&b, "PublicKey = %s\n", p.PublicKey)
		// The filter, not the client's routes. A peer that claimed a wider
		// range here could send traffic that appears to come from any device on
		// the network, which is the one thing cryptokey routing is for.
		fmt.Fprintf(&b, "AllowedIPs = %s\n", describePrefixes(p.AllowedIPs))
	}

	if d.ExtraConf != "" {
		b.WriteString("\n# --- raw_wireguard_conf (design.md §3.2 rule 5) ---\n")
		b.WriteString(d.ExtraConf)
		b.WriteString("\n")
	}
	return b.String()
}

// header is the ownership banner every generated file carries (design.md §7),
// kept even though this one is deleted moments after it is written: an operator
// who catches it mid-apply, or finds one left by a crash, should not have to
// guess what wrote it.
func header(iface string) string {
	return fmt.Sprintf(`# WireGuard configuration for %s
#
# Generated by open-linux-router from %s and handed to `+"`wg setconf`"+`.
# It is removed as soon as it has been read. Do not edit: use `+"`olr remote`"+`.
`, iface, core.ConfigPath)
}

// --- the file a client imports ----------------------------------------------

// ClientConfig renders the configuration a device imports.
//
// The private key is passed in rather than read from anywhere, because there is
// nowhere to read it from: a peer's private half is generated, handed back in
// the response to the request that created the peer, and never stored
// (docs/remote-access.md §4.1). An empty value produces the settings without
// an `[Interface]` key, which is the form for an operator who generated the
// pair on the device themselves.
func ClientConfig(w WireGuard, p Peer, privateKey string, networks []NetworkInfo) (string, error) {
	public, err := PublicKeyFor(w.PrivateKey)
	if err != nil {
		return "", fmt.Errorf("this box has no usable key yet: %w", err)
	}
	endpoint := w.EndpointWithPort()
	if endpoint == "" {
		return "", fmt.Errorf("no endpoint is set, so there is no address for %q to dial", p.Name)
	}

	var b strings.Builder
	fmt.Fprintf(&b, "# %s — generated by open-linux-router\n", p.Name)
	if privateKey == "" {
		b.WriteString("#\n# The private key is yours: olr did not generate this pair and cannot\n" +
			"# write the PrivateKey line. Fill it in on the device.\n")
	} else {
		b.WriteString("#\n# This is the only time this file exists. olr keeps the public half and\n" +
			"# nothing else, so it cannot show you this again — losing it means\n" +
			"# removing the peer and adding it back.\n")
	}

	b.WriteString("\n[Interface]\n")
	if privateKey != "" {
		fmt.Fprintf(&b, "PrivateKey = %s\n", privateKey)
	} else {
		b.WriteString("PrivateKey = <the private key for the public key you supplied>\n")
	}
	// A /32 on the client, unlike the router's own address: the device has one
	// address inside the tunnel and no business claiming a route to the rest of
	// the dial-in network, which is behind the router rather than beside it.
	fmt.Fprintf(&b, "Address = %s\n", netip.PrefixFrom(p.Address, p.Address.BitLen()))
	// So that a dial-in device resolves local names the way a device at home
	// does. The address is inside AllowedIPs in both route scopes, so this
	// works for a split tunnel too (docs/remote-access.md §6.3).
	fmt.Fprintf(&b, "DNS = %s\n", w.RouterAddr())

	b.WriteString("\n[Peer]\n")
	fmt.Fprintf(&b, "PublicKey = %s\n", public)
	fmt.Fprintf(&b, "Endpoint = %s\n", endpoint)
	fmt.Fprintf(&b, "AllowedIPs = %s\n", describePrefixes(ClientRoutes(w, p, networks)))
	// See DefaultKeepalive: without this the device is reachable *from* home
	// only while it happens to be sending.
	fmt.Fprintf(&b, "PersistentKeepalive = %d\n", DefaultKeepalive)

	return b.String(), nil
}

// ClientRoutes is what the device sends into the tunnel — the client-side
// meaning of AllowedIPs (docs/remote-access.md §6.2).
func ClientRoutes(w WireGuard, p Peer, networks []NetworkInfo) []netip.Prefix {
	if p.Routes.OrDefault() == RouteEverything {
		// IPv4 only, and this is not an oversight. olr assigns no IPv6 address
		// inside the tunnel, so `::/0` here would send the device's IPv6
		// traffic into a tunnel that cannot carry it — and a device with a
		// working v6 network would stop reaching v6 destinations rather than
		// falling back to v4.
		return []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	}
	return HomePrefixes(w, networks)
}
