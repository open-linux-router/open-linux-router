package firewall

import (
	"net/netip"
	"strings"
	"testing"
)

func rendered(t *testing.T, c Config) []string {
	t.Helper()
	c.Normalize()
	if res := Validate(c, testLinks()); !res.OK() {
		t.Fatalf("the config under test does not validate: %v", res.Errors)
	}
	return Render(c, testLinks()).Lines()
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func countPrefix(lines []string, prefix string) int {
	n := 0
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func dump(lines []string) string { return "\n" + strings.Join(lines, "\n") }

func TestDisabledRendersNothing(t *testing.T) {
	c := testConfig()
	c.Enabled = false
	if got := rendered(t, c); len(got) != 0 {
		t.Errorf("a disabled module rendered %s", dump(got))
	}
}

func TestForwardRendersTheInboundRule(t *testing.T) {
	got := rendered(t, testConfig())

	want := "nft dnat in wan0 tcp port 8080 to 192.168.1.10:80 counter fwd1 for web"
	if !contains(got, want) {
		t.Errorf("missing %q in %s", want, dump(got))
	}
	if !contains(got, "nft counter fwd1") {
		t.Errorf("the named counter object was not declared: %s", dump(got))
	}
}

// docs/firewall.md §4: the hairpin is two rules and neither works alone.
func TestHairpinRendersBothHalves(t *testing.T) {
	got := rendered(t, testConfig())

	want := "nft hairpin in br-lan tcp port 8080 to 192.168.1.10:80 for web"
	if !contains(got, want) {
		t.Errorf("missing the inside-facing DNAT: %s", dump(got))
	}
	masq := "nft masq from 192.168.1.0/24 to 192.168.1.10 tcp port 80 for web"
	if !contains(got, masq) {
		t.Errorf("missing the masquerade, without which the reply bypasses the router: %s", dump(got))
	}
	if !contains(got, "nft chain postrouting postrouting srcnat") {
		t.Errorf("the masquerade has no chain to live in: %s", dump(got))
	}
}

// HairpinRule deliberately has no counter: the question the counter answers is
// "did anything from outside arrive", and a hairpinned connection from the next
// room would answer it yes while the port was shut to the world.
func TestHairpinRulesDoNotCount(t *testing.T) {
	for _, l := range rendered(t, testConfig()) {
		if strings.HasPrefix(l, "nft hairpin ") && strings.Contains(l, "counter") {
			t.Errorf("a hairpin rule counts, which would make the counter agree with a "+
				"local test and disagree with reality: %q", l)
		}
	}
}

func TestNoHairpinRendersNeitherHalf(t *testing.T) {
	off := false
	c := testConfig()
	c.Forwards[0].Hairpin = &off

	got := rendered(t, c)
	if n := countPrefix(got, "nft hairpin "); n != 0 {
		t.Errorf("--no-hairpin still rendered %d inside-facing rules: %s", n, dump(got))
	}
	if n := countPrefix(got, "nft masq "); n != 0 {
		t.Errorf("--no-hairpin still rendered %d masquerade rules: %s", n, dump(got))
	}
	if contains(got, "nft chain postrouting postrouting srcnat") {
		t.Errorf("a postrouting chain was created with nothing to put in it: %s", dump(got))
	}
}

// The hairpin never points back out the interface the forward faces, because a
// connection arriving there is the inbound case and already has a rule.
func TestHairpinSkipsTheOutwardInterface(t *testing.T) {
	for _, l := range rendered(t, testConfig()) {
		if strings.HasPrefix(l, "nft hairpin in wan0 ") {
			t.Errorf("hairpin rendered for the outward interface: %q", l)
		}
	}
}

// `both` is two rules sharing one counter, which is what makes a service that
// needs TCP and UDP one row on the screen.
func TestProtocolBothRendersTwoRulesOneCounter(t *testing.T) {
	c := testConfig()
	c.Forwards[0].Protocol = ProtocolBoth

	got := rendered(t, c)
	for _, want := range []string{
		"nft dnat in wan0 tcp port 8080 to 192.168.1.10:80 counter fwd1 for web",
		"nft dnat in wan0 udp port 8080 to 192.168.1.10:80 counter fwd1 for web",
	} {
		if !contains(got, want) {
			t.Errorf("missing %q in %s", want, dump(got))
		}
	}
	if n := countPrefix(got, "nft counter "); n != 1 {
		t.Errorf("got %d counters for one forward, want 1: %s", n, dump(got))
	}
}

// An identity range is rendered as a range on both sides, which is what makes
// netfilter keep each connection's own port.
func TestPortRangeRendersAsARange(t *testing.T) {
	c := testConfig()
	c.Forwards[0].Port = PortRange{30000, 30010}
	c.Forwards[0].To = netip.MustParseAddrPort("192.168.1.10:30000")

	got := rendered(t, c)
	want := "nft dnat in wan0 tcp port 30000-30010 to 192.168.1.10:30000-30010 counter fwd1 for web"
	if !contains(got, want) {
		t.Errorf("missing %q in %s", want, dump(got))
	}
}

// The masquerade only covers networks that actually hold the destination: a
// client elsewhere already replies through this router, so masquerading it would
// hide the real source address for nothing.
func TestMasqueradeOnlyCoversNetworksHoldingTheDestination(t *testing.T) {
	links := StaticLinks{
		"wan0":   {Adopted: true, Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.7/24")}},
		"br-lan": {Adopted: true, Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.1/24")}},
		"br-iot": {Adopted: true, Up: true, Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.9.1/24")}},
	}
	c := testConfig()
	got := Render(c, links).Lines()

	if !contains(got, "nft masq from 192.168.1.0/24 to 192.168.1.10 tcp port 80 for web") {
		t.Errorf("no masquerade for the network holding the device: %s", dump(got))
	}
	for _, l := range got {
		if strings.HasPrefix(l, "nft masq from 192.168.9.0/24") {
			t.Errorf("masqueraded a network that does not hold the device: %q", l)
		}
	}
	// The IoT network still gets a hairpin DNAT: a client there reaching the
	// public address does need the translation, it just does not need its source
	// hidden.
	if !contains(got, "nft hairpin in br-iot tcp port 8080 to 192.168.1.10:80 for web") {
		t.Errorf("no hairpin for a network that can still reach the public address: %s", dump(got))
	}
}

// Every rule line has to end `for <name>` — plan.go reads the forward's name
// back off the line to decide whether removing it is disruptive.
func TestEveryRuleLineNamesItsForward(t *testing.T) {
	for _, l := range rendered(t, testConfig()) {
		if !strings.HasPrefix(l, "nft dnat ") && !strings.HasPrefix(l, "nft hairpin ") &&
			!strings.HasPrefix(l, "nft masq ") {
			continue
		}
		name, ok := forwardOfLine(l)
		if !ok || name != "web" {
			t.Errorf("cannot read the forward's name off %q (got %q, %v)", l, name, ok)
		}
	}
}

// The userdata limit is 256 bytes, and a line that overflowed it would be
// silently truncated — so the rule would read back as drift on every plan
// forever.
func TestRuleLinesFitInUserdata(t *testing.T) {
	c := Config{Enabled: true, Forwards: []Forward{{
		Name: strings.Repeat("n", MaxNameLen),
		In:   "verylongifname0",
		Port: PortRange{10000, 20000},
		To:   netip.MustParseAddrPort("192.168.1.100:10000"),
	}}}
	c.Normalize()

	for _, l := range Render(c, testLinks()).Lines() {
		if len(l) > 200 {
			t.Errorf("line is %d bytes, too close to the 256-byte userdata limit: %q", len(l), l)
		}
	}
}

func TestRenderIsStable(t *testing.T) {
	// Rendered twice from the same input: an unstable order would make the
	// ruleset churn between applies and make two boxes impossible to compare.
	c := testConfig()
	first := Render(c, testLinks()).Text()
	second := Render(c, testLinks()).Text()
	if first != second {
		t.Errorf("two renders of one config differ:\n%s\n---\n%s", first, second)
	}
}
