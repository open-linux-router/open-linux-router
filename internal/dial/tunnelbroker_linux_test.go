//go:build linux

package dial

import (
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"net"
	"testing"
)

func TestBrokerRulesKeepConnectedTrafficLocalAndInternetOnHE(t *testing.T) {
	c := brokerConfig()
	c.TunnelBroker.Enabled = true
	rules := brokerRules(*DesiredFor(c).Tunnel)
	if len(rules) != 6 {
		t.Fatalf("rules: %+v", rules)
	}
	for i := 0; i < len(rules); i += 2 {
		if rules[i].SuppressPrefixlen != 0 || rules[i].Table != 254 || rules[i+1].Table != BrokerTable || rules[i].Src.String() != rules[i+1].Src.String() {
			t.Fatalf("bad route pair: %+v", rules[i:i+2])
		}
		if !ownsBrokerRule(rules[i]) || !ownsBrokerRule(rules[i+1]) {
			t.Fatal("own rules not recognized")
		}
	}
	foreign := netlink.NewRule()
	foreign.Priority = BrokerPriority
	foreign.Table = 123
	if ownsBrokerRule(*foreign) {
		t.Fatal("foreign rule claimed")
	}
}

func TestBrokerFallbackDoesNotClaimForeignRoutes(t *testing.T) {
	r := netlink.Route{Table: BrokerTable, Protocol: unix.RTPROT_STATIC, Type: unix.RTN_UNREACHABLE, Priority: 42760, Dst: &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)}}
	if !brokerFallback(r) {
		t.Fatal("fallback not recognized")
	}
	r.Priority = 100
	if brokerFallback(r) {
		t.Fatal("foreign unreachable claimed")
	}
	r.Priority = 42760
	r.Table = 123
	if brokerFallback(r) {
		t.Fatal("foreign table claimed")
	}
}
