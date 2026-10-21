//go:build linux

package gateway

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strconv"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// ReadRoutes reads every route in every table, both families.
//
// All tables rather than only `main` and ours, because the interesting rows at
// one end are the ones a foreign rule points at and at the other are the ones
// the box's own daemons wrote. The view groups and names them; this only
// translates.
func (LinuxKernel) ReadRoutes(context.Context) ([]KernelRoute, error) {
	var out []KernelRoute
	devs := map[int]string{}
	for _, fam := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		routes, err := allRoutes(fam)
		if err != nil {
			return nil, fmt.Errorf("reading the routing table: %w", err)
		}
		for _, r := range routes {
			row := kernelRoute(r, fam, devs)
			// Broadcast, multicast and anycast routes are the interface
			// addressing protocol talking to itself; they are in every table on
			// every box and no operator has ever looked one up on purpose.
			if row.Kind == "broadcast" || row.Kind == "multicast" || row.Kind == "anycast" {
				continue
			}
			out = append(out, row)
		}
	}
	return out, nil
}

func kernelRoute(r netlink.Route, fam int, devs map[int]string) KernelRoute {
	row := KernelRoute{
		Family: 4,
		Table:  r.Table,
		Metric: r.Priority,
		Kind:   routeKind(r.Type),
		Proto:  routeProto(int(r.Protocol)),
	}
	if fam == netlink.FAMILY_V6 {
		row.Family = 6
	}
	row.Dst = routePrefix(r.Dst, fam)
	if r.Gw != nil {
		if a, ok := netip.AddrFromSlice(r.Gw); ok {
			row.Via = a.Unmap()
		}
	}
	if r.LinkIndex > 0 {
		if name, ok := devs[r.LinkIndex]; ok {
			row.Dev = name
		} else if link, err := netlink.LinkByIndex(r.LinkIndex); err == nil {
			devs[r.LinkIndex] = link.Attrs().Name
			row.Dev = link.Attrs().Name
		}
	}
	return row
}

// routePrefix turns the kernel's destination into a prefix, spelling the default
// route as 0.0.0.0/0 or ::/0 rather than as the nil it arrives as.
func routePrefix(dst *net.IPNet, fam int) netip.Prefix {
	if dst == nil {
		if fam == netlink.FAMILY_V6 {
			return netip.PrefixFrom(netip.IPv6Unspecified(), 0)
		}
		return netip.PrefixFrom(netip.IPv4Unspecified(), 0)
	}
	addr, ok := netip.AddrFromSlice(dst.IP)
	if !ok {
		return netip.Prefix{}
	}
	ones, _ := dst.Mask.Size()
	return netip.PrefixFrom(addr.Unmap(), ones)
}

func routeKind(t int) string {
	switch t {
	case unix.RTN_UNICAST:
		return "unicast"
	case unix.RTN_LOCAL:
		return "local"
	case unix.RTN_BROADCAST:
		return "broadcast"
	case unix.RTN_ANYCAST:
		return "anycast"
	case unix.RTN_MULTICAST:
		return "multicast"
	case unix.RTN_BLACKHOLE:
		return "blackhole"
	case unix.RTN_UNREACHABLE:
		return "unreachable"
	case unix.RTN_PROHIBIT:
		return "prohibit"
	case unix.RTN_THROW:
		return "throw"
	case unix.RTN_NAT:
		return "nat"
	}
	return strconv.Itoa(t)
}

// routeProto is who wrote the route, in the words `ip route` uses for the same
// values — an operator comparing this page against `ip route show` should not
// have to translate.
func routeProto(p int) string {
	switch p {
	case unix.RTPROT_UNSPEC:
		return ""
	case unix.RTPROT_REDIRECT:
		return "redirect"
	case unix.RTPROT_KERNEL:
		return "kernel"
	case unix.RTPROT_BOOT:
		return "boot"
	case unix.RTPROT_STATIC:
		return "static"
	case unix.RTPROT_RA:
		return "ra"
	case unix.RTPROT_DHCP:
		return "dhcp"
	}
	return strconv.Itoa(p)
}
