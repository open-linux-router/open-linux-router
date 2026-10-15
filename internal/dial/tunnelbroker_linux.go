//go:build linux

package dial

import (
	"errors"
	"fmt"
	"net"
	"net/netip"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func brokerIPNet(p netip.Prefix) *net.IPNet {
	return &net.IPNet{IP: net.IP(p.Addr().AsSlice()), Mask: net.CIDRMask(p.Bits(), 128)}
}

func brokerRules(t Tunnel) []netlink.Rule {
	var rules []netlink.Rule
	for _, src := range t.Sources {
		if !src.IsValid() {
			continue
		}
		local := netlink.NewRule()
		local.Family, local.Priority, local.Table = netlink.FAMILY_V6, BrokerPriority, unix.RT_TABLE_MAIN
		local.Src, local.SuppressPrefixlen = brokerIPNet(src), 0
		remote := netlink.NewRule()
		remote.Family, remote.Priority, remote.Table = netlink.FAMILY_V6, BrokerPriority+1, BrokerTable
		remote.Src = brokerIPNet(src)
		rules = append(rules, *local, *remote)
	}
	return rules
}

func ownsBrokerRule(r netlink.Rule) bool {
	return OwnsBrokerRule(r)
}

// OwnsBrokerRule lets Gateway recognize dial's source policy without claiming
// its table or deleting its rules during Gateway reconciliation.
func OwnsBrokerRule(r netlink.Rule) bool {
	return r.Family == netlink.FAMILY_V6 && r.Src != nil && r.Mark == 0 && r.IifName == "" && r.OifName == "" &&
		((r.Priority == BrokerPriority && r.Table == unix.RT_TABLE_MAIN && r.SuppressPrefixlen == 0) ||
			(r.Priority == BrokerPriority+1 && r.Table == BrokerTable))
}

func sameBrokerRule(a, b netlink.Rule) bool {
	return a.Priority == b.Priority && a.Table == b.Table && a.SuppressPrefixlen == b.SuppressPrefixlen && a.Src != nil && b.Src != nil && a.Src.String() == b.Src.String()
}

func applyBrokerRouting(t Tunnel, index int) error {
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: BrokerTable}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return err
	}
	for _, r := range routes {
		if brokerFallback(r) {
			continue
		}
		if r.Protocol != unix.RTPROT_STATIC || r.LinkIndex != index || (r.Dst != nil && r.Dst.String() != "::/0") {
			return fmt.Errorf("HE routing table is occupied by a foreign route")
		}
	}
	have, err := netlink.RuleList(netlink.FAMILY_V6)
	if err != nil {
		return err
	}
	for _, r := range have {
		if (r.Priority == BrokerPriority || r.Priority == BrokerPriority+1 || r.Table == BrokerTable) && !ownsBrokerRule(r) {
			return fmt.Errorf("HE routing table or priorities are occupied by a foreign rule")
		}
	}
	// If the device disappears, HE source traffic must not fall through to the
	// ISP's default. Keep a lower-priority unreachable default in our own table.
	if err := netlink.RouteReplace(&netlink.Route{Table: BrokerTable, Family: netlink.FAMILY_V6, Protocol: unix.RTPROT_STATIC, Type: unix.RTN_UNREACHABLE, Priority: 42760, Dst: brokerIPNet(netip.MustParsePrefix("::/0"))}); err != nil {
		return err
	}
	if err := netlink.RouteReplace(&netlink.Route{LinkIndex: index, Table: BrokerTable, Family: netlink.FAMILY_V6, Protocol: unix.RTPROT_STATIC, Type: unix.RTN_UNICAST, Dst: brokerIPNet(netip.MustParsePrefix("::/0"))}); err != nil {
		return err
	}
	want := brokerRules(t)
	for _, rule := range want {
		exists := false
		for _, r := range have {
			if sameBrokerRule(r, rule) {
				exists = true
			}
		}
		if !exists {
			if err := netlink.RuleAdd(&rule); err != nil {
				return err
			}
		}
	}
	for _, rule := range have {
		if !ownsBrokerRule(rule) {
			continue
		}
		keep := false
		for _, r := range want {
			if sameBrokerRule(rule, r) {
				keep = true
			}
		}
		if !keep {
			if err := netlink.RuleDel(&rule); err != nil {
				return err
			}
		}
	}
	return nil
}

func brokerFallback(r netlink.Route) bool {
	return r.Table == BrokerTable && r.Protocol == unix.RTPROT_STATIC && r.Type == unix.RTN_UNREACHABLE && r.Priority == 42760 && (r.Dst == nil || r.Dst.String() == "::/0")
}

func clearBrokerRouting() error {
	rules, err := netlink.RuleList(netlink.FAMILY_V6)
	if err != nil {
		return err
	}
	for _, rule := range rules {
		if ownsBrokerRule(rule) {
			if err := netlink.RuleDel(&rule); err != nil {
				return err
			}
		}
	}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: BrokerTable}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return err
	}
	for _, route := range routes {
		link, linkErr := netlink.LinkByName(TunnelInterface)
		if brokerFallback(route) || (linkErr == nil && route.LinkIndex == link.Attrs().Index && route.Protocol == unix.RTPROT_STATIC && (route.Dst == nil || route.Dst.String() == "::/0")) {
			if err := netlink.RouteDel(&route); err != nil && !errors.Is(err, unix.ESRCH) {
				return err
			}
		}
	}
	return nil
}

func observeBrokerRouting(obs *Observed) {
	rules, err := netlink.RuleList(netlink.FAMILY_V6)
	if err != nil {
		return
	}
	for _, r := range rules {
		if !ownsBrokerRule(r) || r.Table != BrokerTable {
			continue
		}
		// Both the connected-route lookup and the tunnel lookup must exist.
		local := r
		local.Priority, local.Table, local.SuppressPrefixlen = BrokerPriority, unix.RT_TABLE_MAIN, 0
		found := false
		for _, other := range rules {
			if sameBrokerRule(other, local) {
				found = true
			}
		}
		if !found {
			continue
		}
		if p, err := netip.ParsePrefix(r.Src.String()); err == nil {
			obs.BrokerSources = append(obs.BrokerSources, p)
		}
	}
	link, err := netlink.LinkByName(TunnelInterface)
	if err != nil {
		return
	}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: BrokerTable}, netlink.RT_FILTER_TABLE)
	if err != nil {
		return
	}
	routed, fallback := false, false
	for _, r := range routes {
		if brokerFallback(r) {
			fallback = true
		}
		if r.LinkIndex == link.Attrs().Index && r.Type == unix.RTN_UNICAST && (r.Dst == nil || r.Dst.String() == "::/0") {
			routed = true
		}
	}
	obs.BrokerRoute = routed && fallback
}
