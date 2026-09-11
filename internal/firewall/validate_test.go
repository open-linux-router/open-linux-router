package firewall

import (
	"net/netip"
	"strings"
	"testing"
)

func validateOf(t *testing.T, c Config) Result {
	t.Helper()
	c.Normalize()
	return Validate(c, testLinks())
}

func errorAt(r Result, path string) (Problem, bool) {
	for _, p := range r.Errors {
		if p.Path == path {
			return p, true
		}
	}
	return Problem{}, false
}

func warningAt(r Result, path string) (Problem, bool) {
	for _, p := range r.Warnings {
		if p.Path == path {
			return p, true
		}
	}
	return Problem{}, false
}

func TestValidConfigPasses(t *testing.T) {
	if res := validateOf(t, testConfig()); !res.OK() {
		t.Fatalf("the fixture does not validate: %v", res.Errors)
	}
}

// docs/firewall.md §1.3, and the most important check in the module: what it
// prevents is a rule that *works* and delivers to the wrong place.
func TestShiftedPortRangeIsRefused(t *testing.T) {
	c := testConfig()
	c.Forwards[0].Port = PortRange{30000, 30010}
	c.Forwards[0].To = netip.MustParseAddrPort("192.168.1.10:40000")

	res := validateOf(t, c)
	p, ok := errorAt(res, "forwards[0].to")
	if !ok {
		t.Fatalf("a shifted range was accepted: %+v", res)
	}
	// The message has to say what to do instead, because the operator's next
	// question is "then how do I remap a range".
	if !strings.Contains(p.Message, "one forward per port") {
		t.Errorf("the refusal does not say what to do instead: %q", p.Message)
	}
}

func TestIdenticalPortRangeIsAccepted(t *testing.T) {
	c := testConfig()
	c.Forwards[0].Port = PortRange{30000, 30010}
	c.Forwards[0].To = netip.MustParseAddrPort("192.168.1.10:30000")

	if res := validateOf(t, c); !res.OK() {
		t.Errorf("an identity range mapping was refused: %v", res.Errors)
	}
}

func TestSinglePortMayBeRemapped(t *testing.T) {
	// 8080 outside to 80 inside is the motivating case; refusing it would refuse
	// the feature.
	if res := validateOf(t, testConfig()); !res.OK() {
		t.Errorf("8080 → 80 was refused: %v", res.Errors)
	}
}

// docs/firewall.md §7. An enum value or a field that every surface offers and
// which reliably does nothing is worse than a missing feature.
func TestIPv6DestinationIsRefusedWithAReason(t *testing.T) {
	c := testConfig()
	c.Forwards[0].To = netip.MustParseAddrPort("[2001:db8::10]:80")

	res := validateOf(t, c)
	p, ok := errorAt(res, "forwards[0].to")
	if !ok {
		t.Fatalf("an IPv6 destination was accepted: %+v", res)
	}
	if !strings.Contains(p.Message, "no NAT in IPv6") {
		t.Errorf("the refusal does not explain itself: %q", p.Message)
	}
}

func TestForwardingToThisBoxIsRefused(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:80", "192.168.1.1:80", "203.0.113.7:80"} {
		c := testConfig()
		c.Forwards[0].To = netip.MustParseAddrPort(addr)

		res := validateOf(t, c)
		if _, ok := errorAt(res, "forwards[0].to"); !ok {
			t.Errorf("forwarding to %s — one of this box's own addresses — was accepted", addr)
		}
	}
}

func TestArrivalInterfaceMustBeAdopted(t *testing.T) {
	c := testConfig()
	c.Forwards[0].In = "eth2" // present, not adopted

	res := validateOf(t, c)
	p, ok := errorAt(res, "forwards[0].in")
	if !ok {
		t.Fatalf("an unadopted interface was accepted: %+v", res)
	}
	if !strings.Contains(p.Message, "olr adopt eth2") {
		t.Errorf("the refusal does not say how to fix it: %q", p.Message)
	}

	c.Forwards[0].In = "nosuch0"
	if _, ok := errorAt(validateOf(t, c), "forwards[0].in"); !ok {
		t.Error("an unknown interface was accepted")
	}
}

// docs/firewall.md §5.1's first row: a packet cannot go to two places, and
// resolving it by rule order would be a precedence model the operator cannot see.
func TestOverlappingForwardsAreRefused(t *testing.T) {
	c := Config{Enabled: true, Forwards: []Forward{
		{Name: "a", In: "wan0", Port: PortRange{8000, 8100}, To: netip.MustParseAddrPort("192.168.1.10:8000")},
		{Name: "b", In: "wan0", Port: PortRange{8050, 8050}, To: netip.MustParseAddrPort("192.168.1.11:8050")},
	}}

	res := validateOf(t, c)
	if res.OK() {
		t.Fatal("two forwards claiming port 8050 on wan0 were both accepted")
	}
	joined := res.Err().Error()
	if !strings.Contains(joined, "8050") {
		t.Errorf("the refusal does not name the port they collide on: %q", joined)
	}
}

func TestNonOverlappingForwardsAreFine(t *testing.T) {
	for _, tc := range []struct {
		name string
		b    Forward
	}{
		{"different port", Forward{Name: "b", In: "wan0", Port: SinglePort(9090),
			To: netip.MustParseAddrPort("192.168.1.11:9090")}},
		{"different interface", Forward{Name: "b", In: "br-lan", Port: SinglePort(8080),
			To: netip.MustParseAddrPort("192.168.1.11:8080")}},
		{"different protocol", Forward{Name: "b", In: "wan0", Protocol: ProtocolUDP,
			Port: SinglePort(8080), To: netip.MustParseAddrPort("192.168.1.11:8080")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{Enabled: true, Forwards: []Forward{testForward(), tc.b}}
			if res := validateOf(t, c); !res.OK() {
				t.Errorf("refused a pair that does not collide: %v", res.Errors)
			}
		})
	}
}

// `both` overlapping with `udp` collides on udp only, and the message should say
// so rather than naming a protocol neither forward carries alone.
func TestBothOverlapsUDP(t *testing.T) {
	c := Config{Enabled: true, Forwards: []Forward{
		{Name: "a", In: "wan0", Protocol: ProtocolBoth, Port: SinglePort(5060),
			To: netip.MustParseAddrPort("192.168.1.10:5060")},
		{Name: "b", In: "wan0", Protocol: ProtocolUDP, Port: SinglePort(5060),
			To: netip.MustParseAddrPort("192.168.1.11:5060")},
	}}

	res := validateOf(t, c)
	if res.OK() {
		t.Fatal("both/udp on one port was accepted")
	}
	if msg := res.Err().Error(); !strings.Contains(msg, "udp port 5060") {
		t.Errorf("the refusal does not name the shared protocol: %q", msg)
	}
}

// docs/firewall.md §4.1: what hairpin costs is real, and it is a cost the
// operator only discovers much later, so it is said on every surface rather than
// only in a document.
func TestHairpinWarnsAboutHidingTheClient(t *testing.T) {
	res := validateOf(t, testConfig())
	p, ok := warningAt(res, "forwards[0].hairpin")
	if !ok {
		t.Fatalf("no warning about what hairpin costs: %+v", res.Warnings)
	}
	if !strings.Contains(p.Message, "192.168.1.10") {
		t.Errorf("the warning does not name the device it affects: %q", p.Message)
	}

	off := false
	c := testConfig()
	c.Forwards[0].Hairpin = &off
	if _, ok := warningAt(validateOf(t, c), "forwards[0].hairpin"); ok {
		t.Error("warned about hairpin on a forward that has it off")
	}
}

// A destination on no network of ours is a warning rather than a refusal: the
// interface may be configured after the forward, and refusing would make the
// correct order of operations backwards.
func TestUnreachableDestinationWarnsRatherThanRefusing(t *testing.T) {
	c := testConfig()
	c.Forwards[0].To = netip.MustParseAddrPort("10.9.9.9:80")

	res := validateOf(t, c)
	if !res.OK() {
		t.Fatalf("a destination off our networks was refused: %v", res.Errors)
	}
	if _, ok := warningAt(res, "forwards[0].to"); !ok {
		t.Errorf("no warning about an unreachable destination: %+v", res.Warnings)
	}
}

func TestNamesMustBeUsable(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"", "forwards[0].name"},
		{strings.Repeat("x", MaxNameLen+1), "forwards[0].name"},
		{"web\nserver", "forwards[0].name"},
	} {
		c := testConfig()
		c.Forwards[0].Name = tc.name
		if _, ok := errorAt(validateOf(t, c), tc.want); !ok {
			t.Errorf("name %q was accepted", tc.name)
		}
	}
}

func TestDuplicateNamesAreRefused(t *testing.T) {
	c := Config{Enabled: true, Forwards: []Forward{
		{Name: "web", In: "wan0", Port: SinglePort(80), To: netip.MustParseAddrPort("192.168.1.10:80")},
		{Name: "web", In: "wan0", Port: SinglePort(443), To: netip.MustParseAddrPort("192.168.1.10:443")},
	}}
	if res := validateOf(t, c); res.OK() {
		t.Fatal("two forwards called web were accepted; they are referred to by name")
	}
}
