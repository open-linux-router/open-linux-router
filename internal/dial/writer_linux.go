//go:build linux

package dial

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// The kernel half, and the only file in this module that knows what netlink is.
//
// The dependency is already in design.md §8's budget and `internal/link` and
// `internal/gateway` both carry it, so this costs nothing new. Shelling out to
// `ip` is forbidden by §3.6 and there is no way to add an address or replace a
// route through stdlib `net`.

// NewWriter returns the writer for this platform.
func NewWriter() Writer { return linuxWriter{} }

type linuxWriter struct{}

// Apply brings the uplink to the state its configuration calls for.
//
// Three operations, in this order, and the order is the argument:
//
//  1. **Add the address.** Additive — a foreign v4 address on this interface is
//     left exactly where it is. PlanUplink has the whole argument; the short
//     version is that this is the interface a distribution's DHCP client is
//     most likely to also be acting on.
//  2. **Bring the interface up.** After the address, so the link comes up
//     already carrying it rather than carrying nothing for a moment.
//  3. **Replace the default route.** Last, because it is the only one of the
//     three that can be true and useless — a route out of an interface with no
//     address is a route nothing can use.
//
// `RouteReplace`, not delete-then-add. Replacing the default route is the one
// operation that must not leave a window with no route at all, and `replace` is
// atomic where internal/link's add-before-remove trick does not apply: there is
// only ever one default route in the main table, so there is no "add the new
// one first".
//
// It does not stop at the first failure. An address being refused says nothing
// about whether the route can be written, and an operator looking at a
// half-applied change is better served by the complete list than by a report
// that stops at line one.
func (linuxWriter) Apply(ctx context.Context, d Desired) ([]Step, error) {
	if d.Interface == "" {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	link, err := netlink.LinkByName(d.Interface)
	if err != nil {
		return []Step{{
			Description: fmt.Sprintf("find %s", d.Interface),
			Error:       err.Error(),
		}}, fmt.Errorf("the uplink interface %q is not present: %w", d.Interface, err)
	}

	var (
		steps  []Step
		failed int
	)
	run := func(description string, fn func() error) {
		step := Step{Description: description}
		if err := fn(); err != nil {
			step.Error = err.Error()
			failed++
		} else {
			step.Done = true
		}
		steps = append(steps, step)
	}

	if d.Address.IsValid() {
		have, err := readUplinkV4(link)
		switch {
		case err != nil:
			steps = append(steps, Step{
				Description: fmt.Sprintf("read addresses on %s", d.Interface),
				Error:       err.Error(),
			})
			failed++
		case !slices.Contains(have, d.Address):
			run(fmt.Sprintf("add %s to %s", d.Address, d.Interface), func() error {
				return netlink.AddrAdd(link, toUplinkAddr(d.Address))
			})
		}
	}

	if d.Up && link.Attrs().Flags&net.FlagUp == 0 {
		run(fmt.Sprintf("bring %s up", d.Interface), func() error {
			return netlink.LinkSetUp(link)
		})
	}

	if d.Gateway.IsValid() {
		run(fmt.Sprintf("route traffic out via %s on %s", d.Gateway, d.Interface), func() error {
			return netlink.RouteReplace(defaultRoute(link, d.Gateway))
		})
	}

	// The previous uplink's address, last and only on a clean run. Everything
	// above is what replaces it; if any of that was refused, the old address may
	// be the only way this box still reaches anything, and taking it away then
	// would turn a half-applied change into an unreachable one.
	if d.Retire.IsValid() && failed == 0 {
		retire(d, &steps, &failed)
	}

	if failed > 0 {
		return steps, fmt.Errorf("%d of %d uplink operations failed", failed, len(steps))
	}
	return steps, nil
}

// retire takes the previous uplink's address off its interface, if it is
// still there. An interface that has gone has taken its addresses with it, so
// that is not a failure.
func retire(d Desired, steps *[]Step, failed *int) {
	old, err := netlink.LinkByName(d.RetireFrom)
	if err != nil {
		return
	}
	have, err := readUplinkV4(old)
	if err != nil {
		*steps = append(*steps, Step{
			Description: fmt.Sprintf("read addresses on %s", d.RetireFrom),
			Error:       err.Error(),
		})
		*failed++
		return
	}
	if !slices.Contains(have, d.Retire) {
		return
	}
	step := Step{Description: fmt.Sprintf("remove %s from %s", d.Retire, d.RetireFrom)}
	if err := netlink.AddrDel(old, toUplinkAddr(d.Retire)); err != nil {
		step.Error = err.Error()
		*failed++
	} else {
		step.Done = true
	}
	*steps = append(*steps, step)
}

// Observe reads back the interface and the main table's default route.
func (linuxWriter) Observe(ctx context.Context, iface string) (Observed, error) {
	if iface == "" {
		return Observed{}, nil
	}
	if err := ctx.Err(); err != nil {
		return Observed{}, err
	}

	var obs Observed
	link, err := netlink.LinkByName(iface)
	if err == nil {
		obs.Present = true
		obs.Up = link.Attrs().Flags&net.FlagUp != 0
		if addrs, err := readUplinkV4(link); err == nil {
			obs.Addrs = addrs
		} else {
			return obs, err
		}
	}

	gw, dev, err := defaultVia()
	if err != nil {
		return obs, err
	}
	obs.Gateway, obs.GatewayDev = gw, dev
	if gw.IsValid() {
		obs.GatewayState, obs.GatewaySeenOn = gatewayNeighbour(gw, dev)
	}
	return obs, nil
}

// gatewayNeighbour reads whether the gateway answers on dev, and names another
// interface it answers on.
//
// Passive: it reads what the kernel already learned and sends nothing. A box
// with a default route sends traffic through it constantly — the resolver alone
// sees to that — so an entry that is missing is the brief moment after the
// route was written, and saying "not known yet" for it is honest. A read that
// probed would make every status poll send a packet.
//
// A failed read is "not known" rather than an error: the rest of Observe is
// still true, and a status that refused to render over this line would hide
// everything else on it.
func gatewayNeighbour(gw netip.Addr, dev string) (state, seenOn string) {
	neighs, err := netlink.NeighList(0, netlink.FAMILY_V4)
	if err != nil {
		return "", ""
	}
	for _, n := range neighs {
		ip, ok := netip.AddrFromSlice(n.IP)
		if !ok || ip.Unmap() != gw {
			continue
		}
		name := ""
		if link, err := netlink.LinkByIndex(n.LinkIndex); err == nil {
			name = link.Attrs().Name
		}
		switch got := neighbourState(n.State); {
		case name == dev:
			state = got
		case got == GatewayAnswers:
			seenOn = name
		}
	}
	return state, seenOn
}

// neighbourState folds the kernel's NUD states into the two that matter here.
//
// Stale, delay and probe all mean the neighbour answered once and has not been
// re-confirmed lately — it answers, as far as anything can say. Incomplete and
// failed both mean it was asked and has not: incomplete is the kernel still
// asking, and on a gateway that never answers the entry cycles between the two
// for as long as traffic keeps trying, so splitting them would make the status
// flicker between two readings of one fact.
func neighbourState(nud int) string {
	switch {
	case nud&(netlink.NUD_REACHABLE|netlink.NUD_STALE|netlink.NUD_DELAY|netlink.NUD_PROBE|
		netlink.NUD_PERMANENT|netlink.NUD_NOARP) != 0:
		return GatewayAnswers
	case nud&(netlink.NUD_INCOMPLETE|netlink.NUD_FAILED) != 0:
		return GatewaySilent
	default:
		return ""
	}
}

// defaultRoute is the netlink form of "default via <gw> dev <link>".
//
// RTPROT_STATIC, matching internal/gateway's routes: it marks the entry as
// somebody's deliberate configuration rather than a protocol daemon's, which is
// what `ip route show` prints and what tells an operator reading the table that
// this line was put there on purpose.
func defaultRoute(link netlink.Link, gw netip.Addr) *netlink.Route {
	return &netlink.Route{
		LinkIndex: link.Attrs().Index,
		Table:     unix.RT_TABLE_MAIN,
		Family:    netlink.FAMILY_V4,
		Protocol:  unix.RTPROT_STATIC,
		Type:      unix.RTN_UNICAST,
		Dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
		Gw:        net.IP(gw.AsSlice()),
	}
}

// defaultVia returns the next hop of the main table's IPv4 default route, and
// the interface it leaves by.
//
// Whichever interface that is. See Observed.Gateway: "there is a default route
// and it goes somewhere else" is the answer worth having, and filtering by
// interface would render it as "there is none".
func defaultVia() (netip.Addr, string, error) {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4,
		&netlink.Route{Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return netip.Addr{}, "", err
	}
	for _, r := range routes {
		if r.Dst != nil {
			if ones, _ := r.Dst.Mask.Size(); ones != 0 {
				continue
			}
		}
		gw, ok := netip.AddrFromSlice(r.Gw)
		if !ok {
			continue
		}
		dev := ""
		if link, err := netlink.LinkByIndex(r.LinkIndex); err == nil {
			dev = link.Attrs().Name
		}
		return gw.Unmap(), dev, nil
	}
	return netip.Addr{}, "", nil
}

// readUplinkV4 lists an interface's IPv4 addresses as prefixes.
//
// Link-local is excluded to match what internal/link publishes, so that the
// plan an operator approved and the set this file compares against cannot
// disagree about whether 169.254.x.x counts.
func readUplinkV4(link netlink.Link) ([]netip.Prefix, error) {
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
		if !prefix.IsValid() || prefix.Addr().IsLinkLocalUnicast() {
			continue
		}
		out = append(out, prefix)
	}
	return out, nil
}

// toUplinkAddr converts a prefix into the shape netlink wants.
//
// A duplicate of internal/link's toNetlinkAddr rather than a shared helper, and
// the duplication is the cheaper of the two options: sharing it would mean
// either `dial` importing `link` — the arrow design.md §4.1 forbids — or a
// netlink helper in `core`, which would put the one dependency §8 keeps at
// arm's length into the package every module imports. It carries the same
// caveat: the mask is built from the prefix length rather than copied from
// anywhere, so a /24 cannot arrive here as a /120.
func toUplinkAddr(p netip.Prefix) *netlink.Addr {
	addr := p.Addr().As4()
	return &netlink.Addr{
		IPNet: &net.IPNet{
			IP:   net.IP(addr[:]),
			Mask: net.CIDRMask(p.Bits(), 32),
		},
	}
}
