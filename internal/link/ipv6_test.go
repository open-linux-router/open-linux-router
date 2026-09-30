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

	plans := PlanAddrs(cfg, observed, netip.Prefix{})
	if len(plans) != 1 {
		t.Fatalf("want one plan, got %+v", plans)
	}
	if !slices.Equal(plans[0].Add, []netip.Prefix{netip.MustParsePrefix("2001:db8:1:1::1/64")}) {
		t.Errorf("add = %v, want the v6 router address", plans[0].Add)
	}
	if len(plans[0].Remove) != 0 {
		t.Errorf("a SLAAC address olr never configured was planned for removal: %v", plans[0].Remove)
	}

	d := DesiredFor(cfg, netip.Prefix{})
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

	got := RetiredFor(before, after, netip.Prefix{}, netip.Prefix{})
	if len(got) != 1 || !slices.Equal(got[0].Retire, []netip.Prefix{netip.MustParsePrefix("2001:db8:1:1::1/64")}) {
		t.Errorf("RetiredFor = %+v, want only the old v6 router address", got)
	}

	// Unchanged means nothing to retire, IPv4 included.
	if got := RetiredFor(before, before, netip.Prefix{}, netip.Prefix{}); len(got) != 0 {
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

// --- networks numbered out of the delegated prefix ---------------------------

func delegatedNetwork(name, member string, id int) Network {
	n := network(name, "", member)
	n.IPv6 = &NetworkIPv6{Delegated: &id}
	return n
}

func TestTheNthSubnetOfADelegatedPrefix(t *testing.T) {
	p := netip.MustParsePrefix("2408:8000:1234:5670::/60")
	for n, want := range map[int]string{0: "2408:8000:1234:5670::/64", 1: "2408:8000:1234:5671::/64",
		15: "2408:8000:1234:567f::/64"} {
		if got, ok := NthSubnet64(p, n); !ok || got.String() != want {
			t.Errorf("NthSubnet64(%d) = %s %v, want %s", n, got, ok, want)
		}
	}
	// A /60 has sixteen, and the seventeenth does not exist.
	if _, ok := NthSubnet64(p, 16); ok {
		t.Error("a /60 has no /64 number 16")
	}
	if _, ok := NthSubnet64(netip.Prefix{}, 0); ok {
		t.Error("no prefix delegated resolves to nothing")
	}
}

// Before the ISP answers there is nothing to write, and nothing is planned.
func TestADelegatedNetworkWaitsForItsPrefix(t *testing.T) {
	cfg := Config{Adopted: []string{"lan0"}, Networks: []Network{delegatedNetwork("lan", "lan0", 1)}}
	if d := DesiredFor(cfg, netip.Prefix{}); len(d) != 1 || len(d[0].Addrs6) != 0 {
		t.Errorf("desired with no prefix = %+v", d)
	}
	d := DesiredFor(cfg, netip.MustParsePrefix("2408:8000:1234:5600::/56"))
	if len(d) != 1 || !slices.Equal(d[0].Addrs6, []netip.Prefix{netip.MustParsePrefix("2408:8000:1234:5601::1/64")}) {
		t.Errorf("desired = %+v", d)
	}
}

// The ISP renumbers: the config is the same on both sides and the prefix is
// not. The old router address has to come off, through the same path an edit
// takes.
func TestARenumberedPrefixRetiresTheOldAddress(t *testing.T) {
	cfg := Config{Adopted: []string{"lan0"}, Networks: []Network{delegatedNetwork("lan", "lan0", 0)}}
	was, now := netip.MustParsePrefix("2408:8000:aaaa::/56"), netip.MustParsePrefix("2408:8000:bbbb::/56")
	got := RetiredFor(cfg, cfg, was, now)
	if len(got) != 1 || !slices.Equal(got[0].Retire, []netip.Prefix{netip.MustParsePrefix("2408:8000:aaaa::1/64")}) {
		t.Errorf("RetiredFor = %+v", got)
	}
	// And a prefix that expired takes the address with it.
	if got := RetiredFor(cfg, cfg, was, netip.Prefix{}); len(got) != 1 {
		t.Errorf("an expired prefix retired %+v", got)
	}
}

func TestDelegatedNetworksAreValidatedForWhatTheyCanBe(t *testing.T) {
	both := delegatedNetwork("lan", "lan0", 0)
	both.IPv4 = &NetworkIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")}
	both.IPv6.Subnet = netip.MustParsePrefix("2001:db8::/64")
	if res := validateNetwork(t, Config{Adopted: []string{"lan0"}, Networks: []Network{both}}); res.OK() {
		t.Error("static and delegated at once validated")
	}

	a, b := delegatedNetwork("a", "lan0", 2), delegatedNetwork("b", "lan1", 2)
	a.IPv4 = &NetworkIPv4{Subnet: netip.MustParsePrefix("172.16.1.0/24")}
	b.IPv4 = &NetworkIPv4{Subnet: netip.MustParsePrefix("172.16.2.0/24")}
	res := validateNetwork(t, Config{Adopted: []string{"lan0", "lan1"}, Networks: []Network{a, b}})
	if res.OK() || !strings.Contains(firstError(res), "already") {
		t.Errorf("two networks on one delegated /64: %v", res.Errors)
	}
}
