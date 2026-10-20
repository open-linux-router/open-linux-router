package daemon

import (
	"encoding/json"
	"errors"
	"net"

	"github.com/vishvananda/netlink"
)

type connectivitySnapshot struct {
	Addresses  map[string][]string `json:"addresses"`
	Routes     []netlink.Route     `json:"routes"`
	Interfaces map[int]string      `json:"interfaces"`
}

// Preserve pre-existing management addresses/routes as well as OLR intent.
// A first adoption may replace an address supplied by the distribution.
func captureConnectivity() (json.RawMessage, error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	s := connectivitySnapshot{Addresses: map[string][]string{}, Interfaces: map[int]string{}}
	for _, l := range links {
		s.Interfaces[l.Attrs().Index] = l.Attrs().Name
		addrs, err := netlink.AddrList(l, netlink.FAMILY_V4)
		if err != nil {
			return nil, err
		}
		for _, a := range addrs {
			s.Addresses[l.Attrs().Name] = append(s.Addresses[l.Attrs().Name], a.IPNet.String())
		}
	}
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		routes, err := netlink.RouteListFiltered(family, &netlink.Route{Table: 254}, netlink.RT_FILTER_TABLE)
		if err != nil {
			return nil, err
		}
		for _, route := range routes {
			if route.Dst == nil || route.Dst.String() == "0.0.0.0/0" || route.Dst.String() == "::/0" {
				s.Routes = append(s.Routes, route)
			}
		}
	}
	return json.Marshal(s)
}

func restoreConnectivity(data json.RawMessage) error {
	if len(data) == 0 {
		return nil
	}
	var s connectivitySnapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	var failures []error
	for name, addresses := range s.Addresses {
		l, err := netlink.LinkByName(name)
		if err != nil {
			continue
		} // An unplugged USB NIC is not recoverable here.
		for _, address := range addresses {
			ip, network, err := net.ParseCIDR(address)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			network.IP = ip
			failures = append(failures, netlink.AddrReplace(l, &netlink.Addr{IPNet: network}))
		}
	}
	for _, route := range s.Routes {
		if name := s.Interfaces[route.LinkIndex]; name != "" {
			l, err := netlink.LinkByName(name)
			if err != nil {
				failures = append(failures, err)
				continue
			}
			route.LinkIndex = l.Attrs().Index
		}
		for _, next := range route.MultiPath {
			if name := s.Interfaces[next.LinkIndex]; name != "" {
				if l, err := netlink.LinkByName(name); err == nil {
					next.LinkIndex = l.Attrs().Index
				}
			}
		}
		failures = append(failures, netlink.RouteReplace(&route))
	}
	return errors.Join(failures...)
}
