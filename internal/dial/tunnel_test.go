package dial

import (
	"net/netip"
	"strings"
	"testing"
)

// The 6in4 tunnel: a device olr creates and owns outright, carrying this box's
// IPv6 to a broker.

func tunnelConfig() Config {
	c := uplinkConfig()
	c.Uplink.IPv6 = &UplinkIPv6{
		Via:     Via6in4,
		Server:  netip.MustParseAddr("216.66.80.26"),
		Address: netip.MustParsePrefix("2001:db8:1f0a:123::2/64"),
	}
	return c
}

func validateTunnel(t *testing.T, edit func(*UplinkIPv6)) Result {
	t.Helper()
	c := tunnelConfig()
	edit(c.Uplink.IPv6)
	return Validate(c, uplinkLinks())
}

func hasProblem(list []Problem, path, substr string) bool {
	for _, p := range list {
		if p.Path == path && strings.Contains(p.Message, substr) {
			return true
		}
	}
	return false
}

func TestATunnelWithAllItsFieldsValidates(t *testing.T) {
	res := validateTunnel(t, func(*UplinkIPv6) {})
	if !res.OK() {
		t.Fatalf("errors: %v", res.Errors)
	}
}

func TestTunnelValidationNamesTheField(t *testing.T) {
	cases := []struct {
		name, path string
		edit       func(*UplinkIPv6)
	}{
		{"no server", "uplink.ipv6.server", func(v *UplinkIPv6) { v.Server = netip.Addr{} }},
		{"v6 server", "uplink.ipv6.server", func(v *UplinkIPv6) { v.Server = netip.MustParseAddr("2001:db8::1") }},
		{"private server", "uplink.ipv6.server", func(v *UplinkIPv6) { v.Server = netip.MustParseAddr("10.0.0.1") }},
		{"no address", "uplink.ipv6.address", func(v *UplinkIPv6) { v.Address = netip.Prefix{} }},
		{"v4 address", "uplink.ipv6.address", func(v *UplinkIPv6) { v.Address = netip.MustParsePrefix("192.0.2.2/24") }},
		// The typo worth a sentence of its own: the network, not ::2.
		{"network address", "uplink.ipv6.address", func(v *UplinkIPv6) { v.Address = netip.MustParsePrefix("2001:db8:1f0a:123::/64") }},
		{"mtu too big", "uplink.ipv6.mtu", func(v *UplinkIPv6) { v.MTU = 1500 }},
		{"unknown via", "uplink.ipv6.via", func(v *UplinkIPv6) { v.Via = "gre" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res := validateTunnel(t, tc.edit)
			if !hasProblem(res.Errors, tc.path, "") {
				t.Errorf("want an error at %s, got %v", tc.path, res.Errors)
			}
		})
	}
}

// 6in4 is protocol 41, which a port forward cannot pass. Behind another
// router that is the first thing that will not work, so it is said up front.
func TestATunnelBehindAnotherRouterWarnsAboutProtocol41(t *testing.T) {
	res := validateTunnel(t, func(*UplinkIPv6) {})
	if !hasProblem(res.Warnings, "uplink.ipv6", "protocol 41") {
		t.Errorf("192.168.2.9 is private; want the protocol 41 warning, got %v", res.Warnings)
	}
}

// The static address is the tunnel's local end — the one address a broker
// behind-NAT setup needs to see, and the one that keeps the source stable.
func TestDesiredForCarriesTheTunnelFromTheStaticAddress(t *testing.T) {
	d := DesiredFor(tunnelConfig())
	if d.Tunnel == nil {
		t.Fatal("no tunnel in the desired state")
	}
	want := Tunnel{
		Local:   netip.MustParseAddr("192.168.2.9"),
		Remote:  netip.MustParseAddr("216.66.80.26"),
		Address: netip.MustParsePrefix("2001:db8:1f0a:123::2/64"),
		MTU:     DefaultTunnelMTU,
	}
	if *d.Tunnel != want {
		t.Errorf("tunnel = %+v, want %+v", *d.Tunnel, want)
	}
}

func TestAMissingTunnelIsCreatedAndRouted(t *testing.T) {
	plan := buildPlan(uplinkConfig(), tunnelConfig(), testLinks(), agreeingUplink())
	diff := planDiff(plan, "interfaces["+TunnelInterface+"]")
	for _, want := range []string{"ip tunnel add olr-6in4 mode sit remote 216.66.80.26 local 192.168.2.9",
		"ip addr add 2001:db8:1f0a:123::2/64 dev olr-6in4", "ip -6 route replace default dev olr-6in4"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff lacks %q:\n%s", want, diff)
		}
	}
	if plan.Impact == impactDisruptive {
		t.Errorf("a first IPv6 route takes nothing away, but impact = %s", plan.Impact)
	}
}

// Taking the IPv6 default route from an interface that has one — an ISP's RA,
// say — can drop IPv6 that works today.
func TestTakingTheIPv6RouteFromAnotherInterfaceIsDisruptive(t *testing.T) {
	obs := agreeingUplink()
	obs.V6DefaultDev = "enp2s0"
	plan := buildPlan(uplinkConfig(), tunnelConfig(), testLinks(), obs)
	if plan.Impact != impactDisruptive || !hasWarning(plan, "takes it over") {
		t.Errorf("impact = %s, warnings %+v", plan.Impact, plan.Warnings)
	}
}

func TestAnAgreeingTunnelPlansNothing(t *testing.T) {
	obs := agreeingUplink()
	obs.Tunnel = TunnelObserved{
		Present: true, Sit: true, Up: true, MTU: DefaultTunnelMTU,
		Local:  netip.MustParseAddr("192.168.2.9"),
		Remote: netip.MustParseAddr("216.66.80.26"),
		Addrs:  []netip.Prefix{netip.MustParsePrefix("2001:db8:1f0a:123::2/64")},
	}
	obs.V6DefaultDev = TunnelInterface
	if p := PlanTunnel(*DesiredFor(tunnelConfig()).Tunnel, obs); p != nil {
		t.Errorf("an agreeing kernel planned %+v", *p)
	}

	// A different endpoint is a new device, not an edit.
	obs.Tunnel.Remote = netip.MustParseAddr("216.66.80.30")
	if p := PlanTunnel(*DesiredFor(tunnelConfig()).Tunnel, obs); p == nil || !p.Create {
		t.Errorf("a moved endpoint should recreate the tunnel, got %+v", p)
	}
}

// Somebody else's device by our name is not ours to delete.
func TestADeviceByTheTunnelsNameThatIsNotSitIsLeftAlone(t *testing.T) {
	obs := agreeingUplink()
	obs.Tunnel = TunnelObserved{Present: true, Sit: false}
	plan := buildPlan(uplinkConfig(), tunnelConfig(), testLinks(), obs)
	if planDiff(plan, "interfaces["+TunnelInterface+"]") != "" || !hasWarning(plan, "not a 6in4 tunnel") {
		t.Errorf("want no change and a warning, got %+v / %+v", plan.Changes, plan.Warnings)
	}
}

// The one thing an uplink leaves that olr removes: it created the tunnel.
func TestDroppingTheTunnelRemovesTheDevice(t *testing.T) {
	obs := agreeingUplink()
	obs.Tunnel = TunnelObserved{Present: true, Sit: true}
	plan := buildPlan(tunnelConfig(), uplinkConfig(), testLinks(), obs)
	if !strings.Contains(planDiff(plan, "interfaces["+TunnelInterface+"]"), "ip link del olr-6in4") {
		t.Errorf("no removal planned: %+v", plan.Changes)
	}
	if !RemovingTunnel(tunnelConfig(), Config{}) {
		t.Error("removing the whole uplink should take the tunnel with it")
	}
	if RemovingTunnel(tunnelConfig(), tunnelConfig()) {
		t.Error("an unchanged tunnel is not being removed")
	}
}

func agreeingUplink() Observed {
	return Observed{
		Present: true, Up: true,
		Addrs:      []netip.Prefix{netip.MustParsePrefix("192.168.2.9/24")},
		Gateway:    netip.MustParseAddr("192.168.2.1"),
		GatewayDev: "enp2s0",
	}
}

func planDiff(p planView, path string) string {
	var b strings.Builder
	for _, c := range p.Changes {
		if c.Path == path {
			b.WriteString(c.Diff)
		}
	}
	return b.String()
}
