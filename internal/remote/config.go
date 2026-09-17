// Package remote owns the way an operator reaches their own network from
// outside it.
//
// # What this is, and what it is not
//
// design.md §4 reserved the name `vpn` for "wireguard — remote access,
// site-to-site". This is the remote-access half under a different name, and the
// rename is argued in docs/remote-access.md §10: two of the three objects this
// module is meant to hold are not VPNs, so the reserved word would describe a
// third of it and mis-describe the rest.
//
// Three objects are planned and one is built. WireGuard, Shadowsocks and SOCKS5
// share no field — peers and public keys, a port and a cipher, a listen scope
// and credentials — so they are three concrete objects rather than one
// abstraction with three backends. This package contains the first; the other
// two arrive beside it, not underneath it.
//
// What makes it olr's business rather than something an operator installs
// themselves: a device that dials in is a device on a network at home, so it
// inherits `dns`'s names, `devices`' inventory and `gateway`'s `Internet via`
// ladder. Rolling your own gets none of the three. The first of those
// integrations is not built yet (docs/remote-access.md §3.1) and the argument
// is what the module is for.
//
// Unlike every other module with a backend, there is no unit here. WireGuard's
// data path is in the kernel, so what this module configures *is* kernel state
// — which makes it shaped like internal/gateway and internal/firewall, applied
// at olrd startup because a reboot is what kernel state does not survive.
package remote

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ModuleName is the path segment, config section and event label for this
// module.
const ModuleName = "remote"

// DefaultInterface is the kernel name of the tunnel olr creates.
//
// `wg0` and not `olr-wg0`, which would have matched the naming everywhere else
// in olr. Two reasons went the other way: every piece of WireGuard
// documentation an operator will read says `wg0`, and unlike a unit name or a
// config path there is nothing here to collide with — a box already running
// WireGuard on `wg0` is a box this module refuses to touch (validate.go)
// rather than one it has to coexist with on the same name.
const DefaultInterface = "wg0"

// DefaultListenPort is WireGuard's own default, and the number every guide
// tells an operator to forward.
const DefaultListenPort = 51820

// DefaultKeepalive is how often a peer sends a keepalive packet, in seconds.
//
// Rendered into every client configuration rather than offered as a field. A
// peer behind NAT is reachable from home only while the NAT mapping is open, so
// without this the operator's phone is contactable for a minute after it last
// spoke and then silently is not — which is precisely the situation somebody
// notices at the worst moment. 25 is upstream's recommendation and sits below
// every NAT timeout worth worrying about.
const DefaultKeepalive = 25

// DefaultSubnet is the dial-in network when the operator does not choose one.
//
// Arbitrary, and chosen only to be unlikely: home networks cluster on
// 192.168.0.0/16 and the low end of 10.0.0.0/8, and a default that collided
// with the LAN would produce a tunnel that comes up and reaches nothing. An
// overlap is refused rather than worked around (validate.go), so the cost of a
// bad guess here is one clear error naming `--subnet`.
var DefaultSubnet = netip.MustParsePrefix("10.6.0.0/24")

// Config is the remote-access module's intent.
//
// One section today. It is a section rather than a flat set of fields because
// the nesting is what the *stored* document has to get right: adding
// `shadowsocks` beside `wireguard` later must not move a key anybody's backup
// contains. The CLI is flat and will have to change; docs/remote-access.md §11
// #4 records that as a decision rather than an oversight.
type Config struct {
	// WireGuard is the tunnel that puts a device inside the network.
	WireGuard WireGuard `json:"wireguard"`
}

// WireGuard is everything about the tunnel.
type WireGuard struct {
	// Enabled controls whether the tunnel exists at all. Disabling removes the
	// interface and leaves the configuration in place, so peers do not have to
	// be re-issued to turn remote access back on.
	Enabled bool `json:"enabled"`

	// Interface is the kernel name. Empty means DefaultInterface.
	//
	// A field rather than a constant because a box may already have a `wg0`
	// that belongs to somebody else, and the answer to that has to be "use a
	// different name" rather than "uninstall your other tunnel".
	Interface string `json:"interface,omitempty"`

	// ListenPort is the UDP port peers dial. Empty means DefaultListenPort.
	ListenPort uint16 `json:"listen_port,omitempty"`

	// Endpoint is the public name or address a client dials — the one thing in
	// this module olr cannot derive.
	//
	// Nothing on the box knows it: the uplink may be behind a carrier NAT, and
	// the name that tracks the address lives at a DNS provider. Reading it from
	// `dial` was considered and declined — that module holds a *list* of
	// records and none of them is marked "this box's front door", so olr would
	// be guessing at a value whose wrongness presents as "the VPN just does not
	// connect" (docs/remote-access.md §6.1).
	//
	// A port may be included ("home.example.net:51821") for a box whose
	// forwarded port differs from the one it listens on. Without one,
	// ListenPort is used.
	Endpoint string `json:"endpoint,omitempty"`

	// Subnet is the dial-in network. Empty means DefaultSubnet.
	//
	// This is a network in design.md §4.4's sense and behaves like one in every
	// respect but the one that matters most: **its addresses are not served.**
	// They are written into each client configuration when it is generated,
	// because the address has to be known before the tunnel that would carry a
	// DHCP request exists. docs/remote-access.md §3.2 is that asymmetry, and it
	// is why changing this field is disruptive even though nothing disconnects
	// at the instant it is applied — every configuration already on a phone
	// becomes wrong.
	Subnet netip.Prefix `json:"subnet,omitempty"`

	// Address is this box's own address on the dial-in network. Nil derives the
	// first host address, which is `.1` on every ordinary prefix — the same
	// rule link.GroupIPv4 uses, deliberately, so the two kinds of network do
	// not answer "where is the router" differently.
	Address *netip.Addr `json:"address,omitempty"`

	// PrivateKey is the box's own half of the key pair, base64 as WireGuard
	// spells it.
	//
	// It carries the obligations docs/ingress.md §4.1 states for the other
	// credentials olr holds: redacted on every surface that prints config or a
	// plan, and never returned by the API. Unlike those it is generated here
	// rather than typed, so an operator never sees it at all — the only thing
	// that needs the public half is a client configuration, and this module
	// writes those itself.
	PrivateKey string `json:"private_key,omitempty" jsonschema:"description=Generated by olr and never shown. It is redacted on every surface; sending the mask back means 'unchanged'."`

	// Peers are the devices that may dial in, keyed by Name.
	Peers []Peer `json:"peers,omitempty"`

	// ExtraConf is the module's declared escape hatch (design.md §3.2 rule 5):
	// appended verbatim to what `wg setconf` is given. WireGuard's own format
	// has a handful of knobs olr does not model — a pre-shared key, an FwMark —
	// and this field plus upstream's documentation is the whole of our answer
	// to them.
	ExtraConf string `json:"raw_wireguard_conf,omitempty"`
}

// Peer is one device that may dial in.
//
// Two fields the operator gives and two that are derived, which is the module's
// whole claim: `olr remote add peer phone` prints a configuration that works.
type Peer struct {
	// Name is the peer's identity — what this device is called. Positional on
	// the CLI and the key of the item routes (docs/cli.md R2).
	//
	// Constrained to a DNS label by the validator, because this is the name
	// `devices` and `dns` will use when the dial-in segment is registered as a
	// network (docs/remote-access.md §3.1). Choosing a name today that cannot
	// be a name then would be a migration nobody needs.
	Name string `json:"name"`

	// PublicKey is the device's half of the key pair, base64.
	//
	// Normally generated here along with the private half, which is handed back
	// once and never stored (keys.go). An operator who would rather generate
	// the pair on the device supplies this instead, and gets tunnel settings to
	// paste rather than a finished file — olr cannot write an `[Interface]`
	// section for a private key it does not have.
	PublicKey string `json:"public_key"`

	// Address is the peer's address on the dial-in network, allocated when the
	// peer is added and fixed for its lifetime.
	//
	// Stored rather than derived from position in the list: deriving would
	// renumber every peer after a removal, and a renumbered peer is a client
	// configuration that stops working for a reason nobody can see.
	Address netip.Addr `json:"address,omitempty"`

	// Routes is what the *device* sends through the tunnel. Empty means
	// RouteHome.
	//
	// Not to be confused with the peer's `AllowedIPs` on this side, which is
	// derived and is not a field: on the server side that word means "which
	// source addresses this key may use" and the only defensible answer is the
	// peer's own address. docs/remote-access.md §6.2 is the two meanings.
	Routes RouteScope `json:"routes,omitempty"`
}

// RouteScope is what a client sends into the tunnel.
type RouteScope string

// The route scopes.
const (
	// RouteHome sends the home networks and the dial-in network, and nothing
	// else. The device reaches the NAS and the router's own UI while its video
	// calls keep going out of whatever network it is actually on.
	RouteHome RouteScope = "home"

	// RouteEverything sends all of it, so the device appears to be at home for
	// every purpose including its public address.
	//
	// It is not the default, and the reason is not caution: the client is a
	// phone out in the world, and full-tunnel drags everything it does across
	// the operator's home upload. The "configure it once on the router"
	// instinct that is right for devices at home is wrong here, because olr is
	// not in the path of anything that phone does until it decides otherwise.
	RouteEverything RouteScope = "everything"
)

// RouteScopes lists the vocabulary. One source, so the enum published in the
// schema and the check in the validator cannot disagree.
func RouteScopes() []RouteScope { return []RouteScope{RouteHome, RouteEverything} }

// Valid reports whether s is a known scope. The empty string is valid and means
// RouteHome.
func (s RouteScope) Valid() bool { return s == "" || slices.Contains(RouteScopes(), s) }

// OrDefault resolves the empty value.
func (s RouteScope) OrDefault() RouteScope {
	if s == "" {
		return RouteHome
	}
	return s
}

// RedactedKey is what stands in for the private key on a printed surface.
//
// A fixed string rather than a length-preserving mask, so it cannot be mistaken
// for the real value and gives away nothing about it. Same constant shape as
// ingress.RedactedToken, for the same reason.
const RedactedKey = "********"

// --- resolved defaults ------------------------------------------------------

// InterfaceOrDefault resolves the empty interface name.
func (w WireGuard) InterfaceOrDefault() string {
	if name := strings.TrimSpace(w.Interface); name != "" {
		return name
	}
	return DefaultInterface
}

// PortOrDefault resolves the zero listen port.
func (w WireGuard) PortOrDefault() uint16 {
	if w.ListenPort != 0 {
		return w.ListenPort
	}
	return DefaultListenPort
}

// SubnetOrDefault resolves the unset subnet.
func (w WireGuard) SubnetOrDefault() netip.Prefix {
	if w.Subnet.IsValid() {
		return w.Subnet
	}
	return DefaultSubnet
}

// RouterAddr is this box's address on the dial-in network.
func (w WireGuard) RouterAddr() netip.Addr {
	if w.Address != nil && w.Address.IsValid() {
		return *w.Address
	}
	addr, _ := core.FirstHost(w.SubnetOrDefault())
	return addr
}

// RouterPrefix is the router address with the network's mask — the form
// netlink wants, and the form that gives the kernel a connected route covering
// every peer.
//
// The mask is the subnet's rather than a /32 deliberately. A /32 would leave
// the kernel with no route to the peers at all, so every peer would need one
// written by hand; the connected route is what makes cryptokey routing and the
// routing table agree without a second mechanism.
func (w WireGuard) RouterPrefix() netip.Prefix {
	return netip.PrefixFrom(w.RouterAddr(), w.SubnetOrDefault().Bits())
}

// EndpointWithPort renders the endpoint as a client dials it.
//
// A port already in the value wins, so a box whose router forwards 51821 to our
// 51820 can say so. Bare IPv6 is bracketed, which is the spelling wg's parser
// wants and the one an operator is least likely to produce by hand.
func (w WireGuard) EndpointWithPort() string {
	host := strings.TrimSpace(w.Endpoint)
	if host == "" {
		return ""
	}
	if _, _, ok := splitEndpoint(host); ok {
		return host
	}
	if addr, err := netip.ParseAddr(host); err == nil && addr.Is6() {
		return fmt.Sprintf("[%s]:%d", addr, w.PortOrDefault())
	}
	return fmt.Sprintf("%s:%d", host, w.PortOrDefault())
}

// splitEndpoint reports whether a value already carries a port, and splits it.
//
// Hand-rolled rather than net.SplitHostPort because that function accepts an
// empty port and reports a bare IPv6 address as an error whose text mentions
// "too many colons" — neither of which is useful to an operator here.
func splitEndpoint(s string) (host, port string, ok bool) {
	if strings.HasPrefix(s, "[") {
		end := strings.LastIndex(s, "]")
		if end < 0 || end+1 >= len(s) || s[end+1] != ':' {
			return "", "", false
		}
		return s[1:end], s[end+2:], s[end+2:] != ""
	}
	i := strings.LastIndex(s, ":")
	if i < 0 || i+1 >= len(s) || strings.Contains(s[:i], ":") {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// --- peers ------------------------------------------------------------------

// Peer returns a peer by name.
func (w WireGuard) Peer(name string) (Peer, bool) {
	name = normalizeName(name)
	i := slices.IndexFunc(w.Peers, func(p Peer) bool { return p.Name == name })
	if i < 0 {
		return Peer{}, false
	}
	return w.Peers[i], true
}

// SetPeer adds or replaces a peer, keyed by name.
//
// An address already held is kept when the replacement does not name one, which
// is what makes editing a peer's routes free of a renumbering: the client
// configuration on the device says an address, and changing it here would
// silently invalidate a file olr cannot reach.
func (w *WireGuard) SetPeer(p Peer) {
	p.Name = normalizeName(p.Name)
	if i := slices.IndexFunc(w.Peers, func(e Peer) bool { return e.Name == p.Name }); i >= 0 {
		if !p.Address.IsValid() {
			p.Address = w.Peers[i].Address
		}
		if p.PublicKey == "" {
			p.PublicKey = w.Peers[i].PublicKey
		}
		w.Peers[i] = p
		w.Normalize()
		return
	}
	w.Peers = append(w.Peers, p)
	w.Normalize()
}

// RemovePeer drops a peer, reporting whether there was one.
func (w *WireGuard) RemovePeer(name string) bool {
	name = normalizeName(name)
	i := slices.IndexFunc(w.Peers, func(p Peer) bool { return p.Name == name })
	if i < 0 {
		return false
	}
	w.Peers = slices.Delete(w.Peers, i, i+1)
	return true
}

// NextAddress allocates the lowest free address on the dial-in network.
//
// Lowest rather than "one past the last", so that removing a peer returns its
// address to the pool instead of leaving a hole that grows until the subnet is
// exhausted by a network with three devices on it.
//
// The router's own address is skipped, and so are the network and broadcast
// addresses — core.HostRange already excludes the second pair, and the first is
// ours.
func (w WireGuard) NextAddress() (netip.Addr, bool) {
	subnet := w.SubnetOrDefault()
	start, end, ok := core.HostRange(subnet)
	if !ok {
		return netip.Addr{}, false
	}

	taken := map[netip.Addr]bool{w.RouterAddr(): true}
	for _, p := range w.Peers {
		if p.Address.IsValid() {
			taken[p.Address] = true
		}
	}

	for addr := start; addr.IsValid() && core.InRange(start, end, addr); addr = addr.Next() {
		if !taken[addr] {
			return addr, true
		}
	}
	return netip.Addr{}, false
}

// --- canonical form ---------------------------------------------------------

// normalizeName reduces a peer name to its stored form.
//
// Lower-cased, unlike an interface name: this is a DNS label in waiting
// (Peer.Name), and `Phone` and `phone` resolving to two different devices is
// not a distinction anybody meant to draw.
func normalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

// Normalize puts the config in its canonical form.
//
// Sorting is not cosmetic: the document is compared as bytes downstream and the
// rendered configuration is diffed against the kernel, so a config that
// reordered itself between two identical edits would report a change nobody
// made.
func (c *Config) Normalize() { c.WireGuard.Normalize() }

// Normalize puts the tunnel's config in canonical form.
func (w *WireGuard) Normalize() {
	w.Interface = strings.TrimSpace(w.Interface)
	w.Endpoint = strings.TrimSpace(w.Endpoint)
	// The key is not trimmed or re-cased. It is opaque base64 and "helpfully"
	// altering it would produce a tunnel nobody can connect to, with a diff
	// that looks right.

	if w.Subnet.IsValid() {
		// Masked, so that 10.6.0.1/24 typed into a form is stored as
		// 10.6.0.0/24. Without it one network could be written two ways and
		// every byte comparison downstream would call the difference drift.
		w.Subnet = w.Subnet.Masked()
	}
	// An explicit address equal to what would be derived is dropped, so the
	// stored file says `.1` exactly once — in the subnet — and a form that
	// helpfully fills the field in does not turn a default into a pin.
	if w.Address != nil {
		if derived, ok := core.FirstHost(w.SubnetOrDefault()); ok && *w.Address == derived {
			w.Address = nil
		}
	}

	out := make([]Peer, 0, len(w.Peers))
	seen := map[string]bool{}
	for _, p := range w.Peers {
		p.Name = normalizeName(p.Name)
		p.PublicKey = strings.TrimSpace(p.PublicKey)
		if p.Name == "" || seen[p.Name] {
			continue
		}
		seen[p.Name] = true
		out = append(out, p)
	}
	slices.SortStableFunc(out, func(a, b Peer) int { return strings.Compare(a.Name, b.Name) })
	if len(out) == 0 {
		out = nil
	}
	w.Peers = out
}

// Clone returns a deep copy, so a caller may edit a config without disturbing
// the one held by the store.
func (c Config) Clone() Config {
	out := c
	out.WireGuard.Peers = slices.Clone(c.WireGuard.Peers)
	if c.WireGuard.Address != nil {
		addr := *c.WireGuard.Address
		out.WireGuard.Address = &addr
	}
	return out
}

// Redacted returns a copy safe to print, log, or return over the API.
//
// Applied by every surface rather than by the ones that looked risky, because
// the failure mode is silent: nothing breaks when a key is printed, it just
// ends up in a scrollback buffer and then in a bug report.
func (c Config) Redacted() Config {
	out := c.Clone()
	if out.WireGuard.PrivateKey != "" {
		out.WireGuard.PrivateKey = RedactedKey
	}
	return out
}

// Empty reports whether the module has been configured at all.
func (c Config) Empty() bool {
	w := c.WireGuard
	return !w.Enabled && w.PrivateKey == "" && len(w.Peers) == 0 &&
		w.Endpoint == "" && !w.Subnet.IsValid() && w.Interface == "" && w.ListenPort == 0
}

// MarshalConfig encodes a config for the store, normalising first so that two
// equivalent configs produce identical bytes.
func MarshalConfig(c Config) ([]byte, error) {
	c.Normalize()
	return json.Marshal(c)
}

// UnmarshalConfig parses a config, rejecting unknown fields.
//
// Strictness is deliberate: a typo'd key that is silently ignored produces a
// box that is quietly not doing what its config says. Here that means an
// operator who believes they narrowed a peer's routes and did not, or a
// misspelled `endpoint` that leaves every client configuration pointing
// nowhere while the old value keeps working until it moves.
func UnmarshalConfig(data []byte) (Config, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	c.Normalize()
	return c, nil
}

// FromDocument reads this module's section out of the store's document.
//
// A document without a "remote" key is not an error — it means nobody has set
// up remote access, which is what a fresh install looks like and what the page
// must render as an empty state rather than a failure.
func FromDocument(d core.Document) (Config, error) {
	raw, ok := d.Raw(ModuleName)
	if !ok {
		return Config{}, nil
	}
	var c Config
	if err := json.Unmarshal(raw, &c); err != nil {
		return Config{}, fmt.Errorf("%s configuration: %w", ModuleName, err)
	}
	c.Normalize()
	return c, nil
}
