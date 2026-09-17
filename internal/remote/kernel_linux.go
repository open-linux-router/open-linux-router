//go:build linux

package remote

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vishvananda/netlink"
)

// The kernel half, and the only file in this module that knows what netlink or
// `wg` is.
//
// The division inside it is the whole of docs/remote-access.md §5.1, so it is
// worth stating once at the top:
//
//	netlink   creates the interface, addresses it, brings it up
//	wg        loads the keys and the peers
//	nothing   installs a route or an ip rule
//
// That last line is the one that matters. `wg-quick` would have done all three,
// and its third — an `ip rule` at priority 32765 selecting a table with a
// default route — is exactly what internal/gateway's observeRules flags as a
// second owner of the routing decision. Using it would mean olr's tunnel
// breaking olr's own routing, with a refusal message blaming a third party.

// linuxWGType is the kernel's name for a WireGuard link, as netlink reports it.
const linuxWGType = "wireguard"

// NewKernel returns the kernel implementation for this platform.
func NewKernel() Kernel { return linuxKernel{} }

type linuxKernel struct{}

// --- observing --------------------------------------------------------------

// Observe reads the interface, its addresses and its peers, fresh.
func (k linuxKernel) Observe(ctx context.Context, iface string) (Observed, error) {
	obs := Observed{Known: true}

	link, err := netlink.LinkByName(iface)
	if err != nil {
		var missing netlink.LinkNotFoundError
		if errors.As(err, &missing) {
			// Not an error. An absent interface is the ordinary state of a box
			// where remote access has never been turned on, and reporting it as
			// a failure would make `olr remote status` red on every box that is
			// working exactly as configured.
			return obs, nil
		}
		return Observed{}, fmt.Errorf("reading %s: %w", iface, err)
	}

	obs.Present = true
	obs.Up = link.Attrs().Flags&net.FlagUp != 0
	if link.Type() != linuxWGType {
		// Somebody else's interface wearing the name we wanted. Reported and
		// never touched (design.md §3.4); the plan turns this into a refusal.
		obs.Foreign = true
		return obs, nil
	}

	lines := []string{"interface " + iface}

	addrs, err := readV4(link)
	if err != nil {
		return Observed{}, fmt.Errorf("reading addresses on %s: %w", iface, err)
	}
	for _, a := range addrs {
		lines = append(lines, "address "+a.String())
	}

	dump, err := wgDump(ctx, iface)
	if err != nil {
		return Observed{}, err
	}
	if dump.listenPort != 0 {
		lines = append(lines, fmt.Sprintf("listen-port %d", dump.listenPort))
	}
	if dump.publicKey != "" {
		lines = append(lines, "public-key "+dump.publicKey)
	}
	for _, p := range dump.peers {
		lines = append(lines, peerLine(p.PublicKey, p.allowed))
		obs.Peers = append(obs.Peers, p.PeerState)
	}

	sort.Strings(lines)
	obs.Lines = lines
	return obs, nil
}

// --- applying ---------------------------------------------------------------

// Apply brings the interface to the state Desired describes.
//
// It follows ApplyOrder, and it does not stop at the first failure for the
// reason internal/link's writer does not: one operation being refused says
// little about the next, and an operator looking at a half-applied change is
// better served by a complete list of what happened than by a report that stops
// at the first line. The exception is creating the interface — every step after
// it addresses something that does not exist.
func (k linuxKernel) Apply(ctx context.Context, d Desired) ([]Step, error) {
	if !d.Enabled {
		// Tearing down needs netlink and nothing else, so a box with no
		// wireguard-tools can still turn remote access off. Refusing here would
		// mean the missing package also blocked the way out of needing it.
		return k.remove(ctx, d.Interface)
	}

	// Checked before a single netlink call, because the alternative is worse
	// than a refusal: the interface would be created and addressed, `wg setconf`
	// would fail, and the box would be left holding a tunnel that accepts
	// nobody. There is no rollback (design.md §5.3.2), so the cheapest place to
	// stop is before anything has landed.
	if _, err := FindBinary(); err != nil {
		return nil, ErrBinaryMissing()
	}

	var steps []Step
	var failed int
	run := func(description string, fn func() error) bool {
		err := fn()
		step := Step{Description: description, Done: err == nil}
		if err != nil {
			step.Error = err.Error()
			failed++
		}
		steps = append(steps, step)
		return err == nil
	}

	link, err := netlink.LinkByName(d.Interface)
	if err != nil {
		var missing netlink.LinkNotFoundError
		if !errors.As(err, &missing) {
			return steps, fmt.Errorf("reading %s: %w", d.Interface, err)
		}
		if !run("create "+d.Interface, func() error {
			return netlink.LinkAdd(&netlink.Wireguard{
				LinkAttrs: netlink.LinkAttrs{Name: d.Interface},
			})
		}) {
			// Nothing below this can succeed, and each failure would name the
			// same missing interface. One honest error beats four.
			return steps, fmt.Errorf("creating %s: the kernel may have no WireGuard support", d.Interface)
		}
		if link, err = netlink.LinkByName(d.Interface); err != nil {
			return steps, fmt.Errorf("reading %s after creating it: %w", d.Interface, err)
		}
	}
	if link.Type() != linuxWGType {
		// Defence in depth: the plan refuses this already, and the window
		// between planning and applying is exactly where an interface could
		// appear. Writing addresses onto somebody else's device is the one
		// mistake in this file that is not ours to make.
		return steps, fmt.Errorf("%s exists and is a %s interface, not WireGuard; olr will not touch it",
			d.Interface, link.Type())
	}

	k.syncAddrs(link, d, run)

	// Keys and peers, over a pipe. The configuration carries this box's private
	// key and is never written to a filesystem: `wg` reads it from standard
	// input, which is the same mechanism `wg-quick` uses (it passes a process
	// substitution) and which keeps the one copy of the key in the
	// configuration document where design.md §3.4 says the operator's data
	// lives.
	run("load the peer configuration", func() error { return setConf(ctx, d) })

	// Last, per ApplyOrder: an interface that is up before its peers are loaded
	// is a socket rejecting handshakes it is about to start accepting.
	if link.Attrs().Flags&net.FlagUp == 0 {
		run("bring "+d.Interface+" up", func() error { return netlink.LinkSetUp(link) })
	}

	if failed > 0 {
		return steps, fmt.Errorf("%d of %d operations on %s failed", failed, len(steps), d.Interface)
	}
	return steps, nil
}

// syncAddrs brings the interface's IPv4 addressing to what the dial-in network
// calls for.
//
// Add before remove, which is internal/link's rule and applies here for a
// milder version of the same reason: the interface always holds an address, so
// the connected route covering the peers never disappears mid-change.
//
// olr owns this interface's IPv4 addressing outright — it created the device,
// so unlike internal/link's claim over an adopted NIC there is nobody else's
// address that could legitimately be here.
func (k linuxKernel) syncAddrs(link netlink.Link, d Desired, run func(string, func() error) bool) {
	have, err := readV4(link)
	if err != nil {
		run("read addresses on "+d.Interface, func() error { return err })
		return
	}

	for _, want := range d.Addrs {
		if slices.Contains(have, want) {
			continue
		}
		run(fmt.Sprintf("add %s to %s", want, d.Interface), func() error {
			return netlink.AddrAdd(link, toNetlinkAddr(want))
		})
	}
	for _, got := range have {
		if slices.Contains(d.Addrs, got) {
			continue
		}
		run(fmt.Sprintf("remove %s from %s", got, d.Interface), func() error {
			return netlink.AddrDel(link, toNetlinkAddr(got))
		})
	}
}

// remove tears the tunnel down.
//
// Deleting the interface rather than emptying it: a WireGuard interface with no
// configuration is indistinguishable, to anybody reading `ip link`, from one
// that is broken, and leaving it behind would mean `olr remote disable` still
// shows a tunnel on the box.
func (k linuxKernel) remove(_ context.Context, iface string) ([]Step, error) {
	link, err := netlink.LinkByName(iface)
	if err != nil {
		var missing netlink.LinkNotFoundError
		if errors.As(err, &missing) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading %s: %w", iface, err)
	}
	if link.Type() != linuxWGType {
		// Not ours. Silently leaving it is right: the operator has asked for
		// remote access to be off, and it is.
		return nil, nil
	}

	step := Step{Description: "remove " + iface}
	if err := netlink.LinkDel(link); err != nil {
		step.Error = err.Error()
		return []Step{step}, fmt.Errorf("removing %s: %w", iface, err)
	}
	step.Done = true
	return []Step{step}, nil
}

// --- the `wg` binary --------------------------------------------------------

// setConf hands the rendered configuration to `wg setconf` over standard input.
func setConf(ctx context.Context, d Desired) error {
	binary, err := FindBinary()
	if err != nil {
		return ErrBinaryMissing()
	}
	// /dev/stdin rather than a temporary file, so the private key never reaches
	// a filesystem. `wg` opens the path with fopen and a pipe is a legitimate
	// thing to find there — this is the same shape as wg-quick's
	// `wg setconf "$iface" <(echo "$config")`.
	cmd := exec.CommandContext(ctx, binary, "setconf", d.Interface, "/dev/stdin")
	cmd.Stdin = strings.NewReader(d.ServerConf())
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		// `wg`'s own words. It names the line it could not parse, and we have
		// nothing better to say about a file we generated than what the thing
		// that had to read it said about it.
		return fmt.Errorf("wg rejected the configuration: %s", msg)
	}
	return fmt.Errorf("wg rejected the configuration: %w", err)
}

// wgState is one parse of `wg show <iface> dump`.
type wgState struct {
	publicKey  string
	listenPort uint16
	peers      []observedPeer
}

type observedPeer struct {
	PeerState
	allowed []netip.Prefix
}

// wgDump reads the live tunnel configuration.
//
// `dump` rather than `show`: it is the stable, tab-separated form wireguard-
// tools documents for exactly this, where plain `show` is laid out for a human
// and has changed. One call answers both halves — the configuration to diff and
// the handshake times that are the only liveness signal WireGuard offers.
func wgDump(ctx context.Context, iface string) (wgState, error) {
	binary, err := FindBinary()
	if err != nil {
		// Not fatal to an observation. The interface exists and netlink has
		// already told us its addresses; what is missing is the tool, which the
		// dependency blocker reports in its own words. Returning an error here
		// would replace a specific, actionable message with a generic one.
		return wgState{}, nil
	}

	out, err := exec.CommandContext(ctx, binary, "show", iface, "dump").Output()
	if err != nil {
		return wgState{}, fmt.Errorf("reading the tunnel's configuration: %w", err)
	}
	return parseDump(string(out)), nil
}

// parseDump reads `wg show <iface> dump` output.
//
// The first line is the interface — private key, public key, listen port,
// fwmark — and every line after it is a peer. Split out as a pure function so
// the format is tested without a kernel; the sample it is tested against was
// taken from a real `wg`, because a format invented from documentation is a
// format that parses nothing.
//
// The private key in field 1 is deliberately dropped on the floor rather than
// returned. Nothing above this layer needs it: the public key detects exactly
// the same changes, and a value that is never carried cannot be printed.
func parseDump(out string) wgState {
	var state wgState
	for i, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		fields := strings.Split(line, "\t")
		if i == 0 {
			if len(fields) >= 3 {
				state.publicKey = cleanField(fields[1])
				if n, err := strconv.ParseUint(fields[2], 10, 16); err == nil {
					state.listenPort = uint16(n)
				}
			}
			continue
		}
		if len(fields) < 8 {
			continue
		}
		p := observedPeer{PeerState: PeerState{
			PublicKey: cleanField(fields[0]),
			Endpoint:  cleanField(fields[2]),
		}}
		p.allowed = parsePrefixes(splitList(cleanField(fields[3])))
		if secs, err := strconv.ParseInt(fields[4], 10, 64); err == nil && secs > 0 {
			p.LastHandshake = time.Unix(secs, 0)
		}
		p.RxBytes, _ = strconv.ParseUint(fields[5], 10, 64)
		p.TxBytes, _ = strconv.ParseUint(fields[6], 10, 64)
		state.peers = append(state.peers, p)
	}
	return state
}

// cleanField turns `wg`'s placeholder for an absent value into an empty string.
func cleanField(s string) string {
	s = strings.TrimSpace(s)
	if s == "(none)" || s == "off" {
		return ""
	}
	return s
}

// splitList splits an allowed-ips field. `wg` writes them comma-separated; the
// space is tolerated because the same parser reads what a human pasted.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// --- netlink helpers --------------------------------------------------------

// readV4 lists an interface's IPv4 addresses as prefixes.
func readV4(link netlink.Link) ([]netip.Prefix, error) {
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, a := range addrs {
		if a.IPNet == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(a.IPNet.IP.To4())
		if !ok {
			continue
		}
		ones, _ := a.IPNet.Mask.Size()
		prefix := netip.PrefixFrom(addr.Unmap(), ones)
		if prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		out = append(out, prefix)
	}
	return out, nil
}

// toNetlinkAddr converts a prefix into the shape netlink wants.
//
// The mask is built from the prefix length rather than copied from anywhere, so
// a /24 cannot arrive here as a /120 through the v4-in-v6 route.
func toNetlinkAddr(p netip.Prefix) *netlink.Addr {
	addr := p.Addr().As4()
	return &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.IP(addr[:]),
			Mask: net.CIDRMask(p.Bits(), 32),
		},
	}
}
