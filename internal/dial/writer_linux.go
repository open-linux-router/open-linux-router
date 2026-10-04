//go:build linux

package dial

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"slices"
	"strings"

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
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if d.Interface == "" {
		// The uplink went, and with it any tunnel it carried: the one thing an
		// uplink leaves that olr removes, because olr created it (UplinkIPv6).
		if d.RemoveTunnel {
			var steps []Step
			failed := 0
			removeTunnel(&steps, &failed)
			if failed > 0 {
				return steps, fmt.Errorf("removing %s failed", TunnelInterface)
			}
			return steps, nil
		}
		return nil, nil
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

	// accept_ra before anything IPv6 depends on it. Written on every apply the
	// uplink asks for it, not only when it reads wrong: dhcpcd sets it to 0 on
	// the interfaces it manages, and one reload is enough to undo it.
	if d.AcceptRA {
		key := "/proc/sys/net/ipv6/conf/" + d.Interface + "/accept_ra"
		if v, err := os.ReadFile(key); err != nil || strings.TrimSpace(string(v)) != "2" {
			run(fmt.Sprintf("keep taking router advertisements on %s (accept_ra 2)", d.Interface), func() error {
				return os.WriteFile(key, []byte("2\n"), 0o644)
			})
		}
	}

	// The tunnel after the IPv4 half, because it rides on it: a tunnel whose
	// local address has not landed yet is a device that sends nothing.
	switch {
	case d.Tunnel != nil:
		applyTunnel(*d.Tunnel, &steps, &failed)
	case d.RemoveTunnel:
		removeTunnel(&steps, &failed)
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

// applyTunnel brings TunnelInterface to t: the device, its MTU, its address,
// up, and the IPv6 default route through it — the same address, up, route
// order Apply follows for the uplink itself, for the same reasons.
//
// A device with different endpoints or MTU is replaced rather than edited.
// netlink's LinkModify does not cover every sit attribute across kernels, and
// the device is olr's alone, so delete-and-add is the one path that always
// lands in the state asked for. The cost is a moment without IPv6, on a change
// the operator just asked for.
func applyTunnel(t Tunnel, steps *[]Step, failed *int) {
	run := func(description string, fn func() error) bool {
		step := Step{Description: description}
		err := fn()
		if err != nil {
			step.Error = err.Error()
			*failed++
		} else {
			step.Done = true
		}
		*steps = append(*steps, step)
		return err == nil
	}

	existing, err := netlink.LinkByName(TunnelInterface)
	if err == nil {
		sit, ok := existing.(*netlink.Sittun)
		if !ok {
			// Not ours. A device by this name that is not a sit tunnel was put
			// there by somebody else, and deleting it to make room would be
			// removing what olr did not create.
			*steps = append(*steps, Step{
				Description: fmt.Sprintf("create %s", TunnelInterface),
				Error: fmt.Sprintf("%s already exists and is a %s device, not a 6in4 tunnel; "+
					"olr will not replace it", TunnelInterface, existing.Type()),
			})
			*failed++
			return
		}
		if !sitMatches(sit, t) {
			if !run(fmt.Sprintf("remove %s to recreate it", TunnelInterface), func() error {
				return netlink.LinkDel(existing)
			}) {
				return
			}
			existing = nil
		}
	} else {
		existing = nil
	}

	if existing == nil {
		local := "any"
		if t.Local.IsValid() {
			local = t.Local.String()
		}
		if !run(fmt.Sprintf("create %s to %s from %s", TunnelInterface, t.Remote, local), func() error {
			return netlink.LinkAdd(newSit(t))
		}) {
			return
		}
		existing, err = netlink.LinkByName(TunnelInterface)
		if err != nil {
			*steps = append(*steps, Step{
				Description: fmt.Sprintf("find %s", TunnelInterface),
				Error:       err.Error(),
			})
			*failed++
			return
		}
	}

	if have, err := readV6(existing); err == nil && !slices.Contains(have, t.Address) {
		run(fmt.Sprintf("add %s to %s", t.Address, TunnelInterface), func() error {
			return netlink.AddrAdd(existing, &netlink.Addr{IPNet: &net.IPNet{
				IP:   net.IP(t.Address.Addr().AsSlice()),
				Mask: net.CIDRMask(t.Address.Bits(), 128),
			}})
		})
	}
	if existing.Attrs().Flags&net.FlagUp == 0 {
		run(fmt.Sprintf("bring %s up", TunnelInterface), func() error {
			return netlink.LinkSetUp(existing)
		})
	}
	run(fmt.Sprintf("route IPv6 out through %s", TunnelInterface), func() error {
		return netlink.RouteReplace(&netlink.Route{
			LinkIndex: existing.Attrs().Index,
			Table:     unix.RT_TABLE_MAIN,
			Family:    netlink.FAMILY_V6,
			Protocol:  unix.RTPROT_STATIC,
			Type:      unix.RTN_UNICAST,
			Dst:       &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)},
		})
	})
}

// newSit is the netlink form of
// `ip tunnel add olr-6in4 mode sit remote <r> local <l> ttl 255`.
//
// TTL 255 rather than inheriting the inner packet's hop limit, which is what
// every broker's instructions say and what keeps a traceroute through the
// tunnel from dying at the first IPv4 hop. PMTU discovery is on, which a fixed
// TTL requires.
func newSit(t Tunnel) *netlink.Sittun {
	attrs := netlink.NewLinkAttrs()
	attrs.Name = TunnelInterface
	attrs.MTU = t.MTU
	sit := &netlink.Sittun{
		LinkAttrs: attrs,
		Remote:    net.IP(t.Remote.AsSlice()),
		Ttl:       255,
		PMtuDisc:  1,
	}
	if t.Local.IsValid() {
		sit.Local = net.IP(t.Local.AsSlice())
	}
	return sit
}

// sitMatches reports whether an existing sit device is the tunnel t asks for.
func sitMatches(sit *netlink.Sittun, t Tunnel) bool {
	return ipv4Of(sit.Local) == t.Local && ipv4Of(sit.Remote) == t.Remote && sit.MTU == t.MTU
}

// ipv4Of converts a tunnel endpoint, with 0.0.0.0 — "any" — as invalid.
func ipv4Of(ip net.IP) netip.Addr {
	addr, ok := netip.AddrFromSlice(ip.To4())
	if !ok || addr.IsUnspecified() {
		return netip.Addr{}
	}
	return addr
}

// removeTunnel deletes TunnelInterface if it is there and is a sit device.
// Its address and route go with it.
func removeTunnel(steps *[]Step, failed *int) {
	link, err := netlink.LinkByName(TunnelInterface)
	if err != nil {
		return
	}
	if _, ok := link.(*netlink.Sittun); !ok {
		return
	}
	step := Step{Description: fmt.Sprintf("remove %s", TunnelInterface)}
	if err := netlink.LinkDel(link); err != nil {
		step.Error = err.Error()
		*failed++
	} else {
		step.Done = true
	}
	*steps = append(*steps, step)
}

// observeTunnel reads TunnelInterface and the IPv6 default route.
func observeTunnel(obs *Observed) {
	if link, err := netlink.LinkByName(TunnelInterface); err == nil {
		obs.Tunnel.Present = true
		obs.Tunnel.Up = link.Attrs().Flags&net.FlagUp != 0
		obs.Tunnel.MTU = link.Attrs().MTU
		if sit, ok := link.(*netlink.Sittun); ok {
			obs.Tunnel.Sit = true
			obs.Tunnel.Local, obs.Tunnel.Remote = ipv4Of(sit.Local), ipv4Of(sit.Remote)
		}
		if addrs, err := readV6(link); err == nil {
			obs.Tunnel.Addrs = addrs
		}
	}

	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6,
		&netlink.Route{Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return
	}
	for _, r := range routes {
		if r.Dst != nil {
			if ones, _ := r.Dst.Mask.Size(); ones != 0 {
				continue
			}
		}
		if link, err := netlink.LinkByIndex(r.LinkIndex); err == nil {
			obs.V6DefaultDev = link.Attrs().Name
		}
		if gw, ok := netip.AddrFromSlice(r.Gw); ok {
			obs.V6DefaultVia = gw
		}
		return
	}
}

// Unreachable implements Writer.
func (linuxWriter) Unreachable(_ context.Context, old, next netip.Prefix) error {
	route := func(p netip.Prefix) *netlink.Route {
		return &netlink.Route{
			Table:    unix.RT_TABLE_MAIN,
			Family:   netlink.FAMILY_V6,
			Protocol: unix.RTPROT_STATIC,
			Type:     unix.RTN_UNREACHABLE,
			Dst:      &net.IPNet{IP: net.IP(p.Addr().AsSlice()), Mask: net.CIDRMask(p.Bits(), 128)},
		}
	}
	if old.IsValid() && old != next {
		// Gone already is fine: a reboot took it.
		if err := netlink.RouteDel(route(old)); err != nil && !errors.Is(err, unix.ESRCH) {
			return fmt.Errorf("removing the unreachable route for %s: %w", old, err)
		}
	}
	if next.IsValid() {
		if err := netlink.RouteReplace(route(next)); err != nil {
			return fmt.Errorf("adding the unreachable route for %s: %w", next, err)
		}
	}
	return nil
}

// readV6 lists a device's IPv6 addresses, link-local excluded.
func readV6(link netlink.Link) ([]netip.Prefix, error) {
	addrs, err := netlink.AddrList(link, netlink.FAMILY_V6)
	if err != nil {
		return nil, err
	}
	var out []netip.Prefix
	for _, a := range addrs {
		if a.IPNet == nil {
			continue
		}
		addr, ok := netip.AddrFromSlice(a.IPNet.IP)
		if !ok || addr.Is4In6() || addr.IsLinkLocalUnicast() {
			continue
		}
		ones, _ := a.IPNet.Mask.Size()
		out = append(out, netip.PrefixFrom(addr, ones))
	}
	return out, nil
}

// Observe reads back the interface and the main table's default route.
func (linuxWriter) Observe(ctx context.Context, iface string) (Observed, error) {
	if err := ctx.Err(); err != nil {
		return Observed{}, err
	}
	byRoute := iface == ""
	routes, err := readDefaultRoutes()
	if err != nil {
		return Observed{}, err
	}
	if byRoute {
		for _, route := range routes {
			if route.Family == netlink.FAMILY_V4 {
				iface = route.Dev
				break
			}
		}
		if iface == "" && len(routes) > 0 {
			iface = routes[0].Dev
		}
	}

	obs := Observed{DefaultRoutes: routes}
	if iface != "" {
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
	}

	observeTunnel(&obs)
	if v, err := os.ReadFile("/proc/sys/net/ipv6/conf/" + iface + "/accept_ra"); err == nil {
		obs.AcceptRA = strings.TrimSpace(string(v))
	}

	gw, dev, err := defaultVia()
	if err != nil {
		return obs, err
	}
	obs.Gateway, obs.GatewayDev = gw, dev
	if byRoute && dev == "" {
		// defaultVia skips a route with no next hop; keep the IPv4
		// interface even when the only observed default is IPv6.
		for _, route := range routes {
			if route.Family == netlink.FAMILY_V4 {
				obs.GatewayDev = route.Dev
				break
			}
		}
	}
	if gw.IsValid() {
		obs.GatewayState, obs.GatewaySeenOn = gatewayNeighbour(gw, dev)
	}
	for i := range obs.DefaultRoutes {
		r := &obs.DefaultRoutes[i]
		if !r.Via.IsValid() {
			continue
		}
		if r.Family == netlink.FAMILY_V4 {
			r.GatewayState, _ = gatewayNeighbour(r.Via, r.Dev)
		}
		if r.Family == netlink.FAMILY_V6 {
			r.GatewayMAC = gatewayNeighbourMAC(r.Via, r.Dev)
		}
	}
	return obs, nil
}

// gatewayNeighbourMAC only trusts an IPv6 neighbour on the route's own
// interface with a resolved hardware address. A missing or incomplete entry
// leaves the gateway unidentified instead of guessing from its fe80 address.
func gatewayNeighbourMAC(gw netip.Addr, dev string) string {
	link, err := netlink.LinkByName(dev)
	if err != nil {
		return ""
	}
	neighs, err := netlink.NeighList(link.Attrs().Index, netlink.FAMILY_V6)
	if err != nil {
		return ""
	}
	return neighbourMAC(neighs, gw, link.Attrs().Index)
}

func neighbourMAC(neighs []netlink.Neigh, gw netip.Addr, index int) string {
	for _, n := range neighs {
		ip, ok := netip.AddrFromSlice(n.IP)
		if !ok || ip != gw || n.LinkIndex != index || len(n.HardwareAddr) == 0 ||
			neighbourState(n.State) != GatewayAnswers {
			continue
		}
		return n.HardwareAddr.String()
	}
	return ""
}

// readDefaultRoutes preserves every main-table default route, including the
// individual legs of a multipath route. It never infers a gateway from the
// address on an interface.
func readDefaultRoutes() ([]DefaultRoute, error) {
	var out []DefaultRoute
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		routes, err := netlink.RouteListFiltered(family,
			&netlink.Route{Table: unix.RT_TABLE_MAIN}, netlink.RT_FILTER_TABLE)
		if family == netlink.FAMILY_V6 && (errors.Is(err, unix.EAFNOSUPPORT) || errors.Is(err, unix.EPROTONOSUPPORT)) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, r := range routes {
			if r.Dst != nil {
				if ones, _ := r.Dst.Mask.Size(); ones != 0 {
					continue
				}
			}
			add := func(index int, gw net.IP) {
				link, err := netlink.LinkByIndex(index)
				if err != nil {
					return
				}
				entry := DefaultRoute{Family: family, Dev: link.Attrs().Name, Metric: r.Priority}
				if via, ok := netip.AddrFromSlice(gw); ok {
					entry.Via = via.Unmap()
				}
				out = append(out, entry)
			}
			if len(r.MultiPath) > 0 {
				for _, hop := range r.MultiPath {
					add(hop.LinkIndex, hop.Gw)
				}
			} else {
				add(r.LinkIndex, r.Gw)
			}
		}
	}
	return out, nil
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
		if len(r.MultiPath) > 0 {
			for _, hop := range r.MultiPath {
				if gw, ok := netip.AddrFromSlice(hop.Gw); ok {
					dev := ""
					if link, err := netlink.LinkByIndex(hop.LinkIndex); err == nil {
						dev = link.Attrs().Name
					}
					return gw.Unmap(), dev, nil
				}
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
