package link

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// A network's static IPv6: a /64 olr writes to the member, and the one v6
// address on it that olr claims.

func withIPv6(n Network, subnet string) Network {
	n.IPv6 = &NetworkIPv6{Subnet: netip.MustParsePrefix(subnet)}
	return n
}

func TestTheIPv6RouterDefaultsToColonColonOne(t *testing.T) {
	v6 := NetworkIPv6{Subnet: netip.MustParsePrefix("2001:db8:1:1::/64")}
	if got := v6.RouterPrefix().String(); got != "2001:db8:1:1::1/64" {
		t.Errorf("router = %s, want 2001:db8:1:1::1/64", got)
	}
}

// The same rule as IPv4: a form filling in the default does not pin it.
func TestNormalizeMasksTheIPv6SubnetAndDropsADerivedRouter(t *testing.T) {
	router := netip.MustParseAddr("2001:db8:1:1::1")
	cfg := Config{Networks: []Network{{Name: "lan", Members: []string{"lan0"},
		IPv6: &NetworkIPv6{Subnet: netip.MustParsePrefix("2001:db8:1:1::5/64"), Router: &router}}}}
	cfg.Normalize()

	v6 := cfg.Networks[0].IPv6
	if v6.Subnet.String() != "2001:db8:1:1::/64" || v6.Router != nil {
		t.Errorf("got subnet %s router %v, want the /64 and no pinned router", v6.Subnet, v6.Router)
	}
}

// A /48 is what a tunnel broker hands out and the commonest thing to paste in.
// It is refused with the /64 to use instead, because SLAAC does nothing on it.
func TestAnIPv6SubnetMustBeASlash64(t *testing.T) {
	cfg := Config{
		Adopted:  []string{"lan0"},
		Networks: []Network{withIPv6(network("lan", "172.16.1.0/24", "lan0"), "2001:db8:1::/48")},
	}
	res := validateNetwork(t, cfg)
	if res.OK() || !strings.Contains(firstError(res), "2001:db8:1::/64") {
		t.Errorf("want a refusal suggesting 2001:db8:1::/64, got %v", res.Errors)
	}
}

func TestTwoNetworksCannotShareAnIPv6Subnet(t *testing.T) {
	cfg := Config{
		Adopted: []string{"lan0", "lan1"},
		Networks: []Network{
			withIPv6(network("a", "172.16.1.0/24", "lan0"), "2001:db8:1:1::/64"),
			withIPv6(network("b", "172.16.2.0/24", "lan1"), "2001:db8:1:1::/64"),
		},
	}
	if res := validateNetwork(t, cfg); res.OK() {
		t.Error("two networks on one /64 validated")
	}
}

// The subnet-router anycast address is every router's on the link; it is not
// one to hold as this router's host address.
func TestTheIPv6RouterCannotBeTheSubnetAddress(t *testing.T) {
	zero := netip.MustParseAddr("2001:db8:1:1::")
	n := withIPv6(network("lan", "172.16.1.0/24", "lan0"), "2001:db8:1:1::/64")
	n.IPv6.Router = &zero
	cfg := Config{Adopted: []string{"lan0"}, Networks: []Network{n}}
	if res := validateNetwork(t, cfg); res.OK() {
		t.Error("the anycast address was accepted as the router")
	}
}

// The narrow claim. The member already has a SLAAC address from somewhere
// else; olr adds its own and plans no removal of anything v6.
func TestPlanAddsTheIPv6RouterAndLeavesOtherV6Alone(t *testing.T) {
	cfg := Config{Adopted: []string{"lan0"},
		Networks: []Network{withIPv6(network("lan", "172.16.1.0/24", "lan0"), "2001:db8:1:1::/64")}}
	cfg.Normalize()
	observed := []Interface{{Name: "lan0", Up: true, Prefixes: []netip.Prefix{
		netip.MustParsePrefix("172.16.1.1/24"),
		netip.MustParsePrefix("2408:8000:1:2:aaaa:bbbb:cccc:dddd/64"),
	}}}

	plans := PlanAddrs(cfg, observed)
	if len(plans) != 1 {
		t.Fatalf("want one plan, got %+v", plans)
	}
	if !slices.Equal(plans[0].Add, []netip.Prefix{netip.MustParsePrefix("2001:db8:1:1::1/64")}) {
		t.Errorf("add = %v, want the v6 router address", plans[0].Add)
	}
	if len(plans[0].Remove) != 0 {
		t.Errorf("a SLAAC address olr never configured was planned for removal: %v", plans[0].Remove)
	}

	d := DesiredFor(cfg)
	if len(d) != 1 || !slices.Equal(d[0].Addrs6, []netip.Prefix{netip.MustParsePrefix("2001:db8:1:1::1/64")}) ||
		slices.ContainsFunc(d[0].Addrs, func(p netip.Prefix) bool { return p.Addr().Is6() }) {
		t.Errorf("desired = %+v, want the v6 address in Addrs6 and only there", d)
	}
}

// Because the v6 claim does not remove what it does not call for, a renumbered
// prefix would leave the old router address behind for good unless retire
// takes it — and it has to, even though the member is still in a network.
func TestRenumberingTheIPv6SubnetRetiresTheOldRouterAddress(t *testing.T) {
	before := Config{Adopted: []string{"lan0"},
		Networks: []Network{withIPv6(network("lan", "172.16.1.0/24", "lan0"), "2001:db8:1:1::/64")}}
	after := Config{Adopted: []string{"lan0"},
		Networks: []Network{withIPv6(network("lan", "172.16.1.0/24", "lan0"), "2001:db8:1:2::/64")}}

	got := RetiredFor(before, after)
	if len(got) != 1 || !slices.Equal(got[0].Retire, []netip.Prefix{netip.MustParsePrefix("2001:db8:1:1::1/64")}) {
		t.Errorf("RetiredFor = %+v, want only the old v6 router address", got)
	}

	// Unchanged means nothing to retire, IPv4 included.
	if got := RetiredFor(before, before); len(got) != 0 {
		t.Errorf("an unchanged network retired %+v", got)
	}
}

func TestDroppingTheIPv6BlockIsDisruptive(t *testing.T) {
	before := withIPv6(network("lan", "172.16.1.0/24", "lan0"), "2001:db8:1:1::/64")
	after := network("lan", "172.16.1.0/24", "lan0")
	if got := networkImpact(before, after); got != impactDisruptive {
		t.Errorf("impact = %s, want disruptive", got)
	}
	if got := networkImpact(after, before); got != impactRestart {
		t.Errorf("gaining a prefix: impact = %s, want restart", got)
	}
}
