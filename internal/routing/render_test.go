package routing

import (
	"fmt"
	"strings"
	"testing"
)

func lines(t *testing.T, c Config, health Health) []string {
	t.Helper()
	c.Normalize()
	if res := Validate(c, testLinks()); !res.OK() {
		t.Fatalf("config under test does not validate: %v", res.Errors)
	}
	return Render(c, testLinks(), health).Lines()
}

func contains(lines []string, want string) bool {
	for _, l := range lines {
		if l == want {
			return true
		}
	}
	return false
}

func containsPrefix(lines []string, prefix string) bool {
	for _, l := range lines {
		if strings.HasPrefix(l, prefix) {
			return true
		}
	}
	return false
}

func dump(lines []string) string { return "\n" + strings.Join(lines, "\n") }

func sprintf(format string, args ...any) string { return fmt.Sprintf(format, args...) }

// setExit edits one exit by name, because Normalize sorts the list and an index
// written today is a different exit tomorrow.
func setExit(c *Config, name string, edit func(*Exit)) {
	for i := range c.Exits {
		if c.Exits[i].Name == name {
			edit(&c.Exits[i])
			return
		}
	}
	panic("no exit named " + name)
}

// The rule §3.3 calls load-bearing and the most common way a hand-rolled setup
// breaks: without it an exit table's default route swallows traffic to your own
// LAN, and the router stops being able to reach the devices behind it.
func TestTheLocalGuardIsAlwaysPresentInBothFamilies(t *testing.T) {
	got := lines(t, testConfig(), nil)
	for _, fam := range []string{"ip", "ip6"} {
		want := "rule " + fam + " priority 8100 from all lookup main suppress_prefixlength 0"
		if !contains(got, want) {
			t.Errorf("missing %q in %s", want, dump(got))
		}
	}
}

func TestEachExitGetsOneRuleAndOneRoutePerFamily(t *testing.T) {
	c := testConfig()
	c.Normalize()
	clash, _ := c.Find("Clash")

	got := lines(t, c, nil)

	// Derived from the slot rather than hardcoded, because which slot an exit
	// lands in is an allocation detail and asserting on it would make this test
	// fail every time an unrelated exit was renamed.
	wantRule := sprintf("rule ip priority %d fwmark %#08x/%#08x lookup %d",
		clash.Priority(), clash.Mark(), MarkMask, clash.Table())
	if !contains(got, wantRule) {
		t.Errorf("missing %q in %s", wantRule, dump(got))
	}
	wantRoute := sprintf("route ip table %d default via 192.168.1.50 dev br-lan", clash.Table())
	if !contains(got, wantRoute) {
		t.Errorf("missing %q in %s", wantRoute, dump(got))
	}
}

// §5.4: a v4-only exit with working IPv6 on the clients sends everything with
// an AAAA record out the default path at full speed, unnoticed.
func TestAV4OnlyNextHopBlocksIPv6RatherThanLeakingIt(t *testing.T) {
	c := testConfig()
	c.Normalize()
	clash, _ := c.Find("Clash")

	got := lines(t, c, nil)
	want := sprintf("route ip6 table %d unreachable default", clash.Table())
	if !contains(got, want) {
		t.Errorf("IPv6 should be refused, not left to the default path: %s", dump(got))
	}
}

func TestIPv6DirectInstallsNoV6RuleAtAll(t *testing.T) {
	c := testConfig()
	c.Normalize()
	setExit(&c, "Clash", func(e *Exit) { e.IPv6 = IPv6Direct })
	clash, _ := c.Find("Clash")
	got := lines(t, c, nil)

	if containsPrefix(got, sprintf("route ip6 table %d", clash.Table())) {
		t.Errorf("direct means no v6 route, so the mark selects nothing: %s", dump(got))
	}
	if containsPrefix(got, sprintf("rule ip6 priority %d", clash.Priority())) {
		t.Errorf("a v6 rule with no table behind it would fall through to main: %s", dump(got))
	}
}

func TestAnInterfaceExitCarriesBothFamilies(t *testing.T) {
	c := Config{
		Enabled:    true,
		Exits:      []Exit{{Name: "Office", Via: Via{Kind: ViaInterface, Interface: "wg0"}}},
		Interfaces: []Assignment{{Interface: "br-lan", Exit: "Office"}},
	}
	got := lines(t, c, nil)

	if !contains(got, "route ip table 8101 default dev wg0") {
		t.Errorf("missing the v4 route: %s", dump(got))
	}
	if !contains(got, "route ip6 table 8101 default dev wg0") {
		t.Errorf("an interface carries v6 too: %s", dump(got))
	}
	_ = c
}

// §3.7: unreachable, never blackhole. Blackhole drops silently and every
// connection hangs for thirty seconds.
func TestBlockedIsUnreachable(t *testing.T) {
	c := testConfig()
	c.Normalize()
	blocked, _ := c.Find("Blocked")

	got := lines(t, c, nil)
	want := "route ip table " + itoa(blocked.Table()) + " unreachable default"
	if !contains(got, want) {
		t.Errorf("missing %q in %s", want, dump(got))
	}
}

func TestClassifyRulesMatchTheNetworksOwnPrefixes(t *testing.T) {
	c := testConfig()
	c.Normalize()
	clash, _ := c.Find("Clash")

	got := lines(t, c, nil)

	if !contains(got, sprintf("nft source ip 192.168.1.0/24 mark %#08x from br-lan via Clash", clash.Mark())) {
		t.Errorf("missing the classify rule for br-lan: %s", dump(got))
	}
	// The prefix is masked, so a link reporting 192.168.1.1/24 classifies the
	// whole subnet rather than one address.
	if containsPrefix(got, "nft source ip 192.168.1.1/24") {
		t.Errorf("prefix was not masked: %s", dump(got))
	}
}

// §3.4's flow stickiness, which is what keeps an assignment edit `reload`
// rather than `disruptive`.
func TestRestoreAndAccountRulesExistPerExitInUse(t *testing.T) {
	c := testConfig()
	c.Normalize()
	clash, _ := c.Find("Clash")

	got := lines(t, c, nil)

	if !contains(got, sprintf("nft restore mark %#08x for Clash", clash.Mark())) {
		t.Errorf("no ct-mark restore for Clash: %s", dump(got))
	}
	if !contains(got, sprintf("nft account mark %#08x counter exit%d for Clash", clash.Mark(), clash.Slot)) {
		t.Errorf("no accounting rule for Clash: %s", dump(got))
	}
	// §7.3: show what you cannot account for.
	if !contains(got, "nft count-unpoliced") {
		t.Errorf("the residual counter is missing: %s", dump(got))
	}
	if !contains(got, "nft counter "+UnpolicedCounter) {
		t.Errorf("the residual counter object is missing: %s", dump(got))
	}
}

func TestRulesAreInEvaluationOrder(t *testing.T) {
	got := lines(t, testConfig(), nil)
	index := func(prefix string) int {
		for i, l := range got {
			if strings.HasPrefix(l, prefix) {
				return i
			}
		}
		return -1
	}

	restore, source := index("nft restore"), index("nft source")
	account, unpoliced := index("nft account"), index("nft count-unpoliced")
	if !(restore < source && source < account && account < unpoliced) {
		t.Errorf("rules out of evaluation order (restore %d, source %d, account %d, unpoliced %d): %s",
			restore, source, account, unpoliced, dump(got))
	}
}

// §5.3, and §11 open decision 2 resolved as the doc leans.
func TestANextHopSNATsByDefault(t *testing.T) {
	c := testConfig()
	c.Normalize()
	clash, _ := c.Find("Clash")

	got := lines(t, c, nil)
	want := sprintf("nft snat mark %#08x dev br-lan for Clash", clash.Mark())
	if !contains(got, want) {
		t.Errorf("missing %q in %s", want, dump(got))
	}
}

func TestSNATCanBeTurnedOff(t *testing.T) {
	c := testConfig()
	c.Normalize()
	off := false
	setExit(&c, "Clash", func(e *Exit) { e.SNAT = &off })
	got := lines(t, c, nil)

	if containsPrefix(got, "nft snat") {
		t.Errorf("snat:false should install nothing: %s", dump(got))
	}
}

// §5.2, in both directions: the client is told to bypass us, and the proxy box
// tells us to add routes we did not choose.
func TestRedirectSysctlsAreSetOnTheNextHopsInterface(t *testing.T) {
	got := lines(t, testConfig(), nil)
	for _, key := range []string{
		"sysctl net.ipv4.conf.br-lan.send_redirects = 0",
		"sysctl net.ipv4.conf.br-lan.accept_redirects = 0",
		"sysctl net.ipv6.conf.br-lan.accept_redirects = 0",
	} {
		if !contains(got, key) {
			t.Errorf("missing %q in %s", key, dump(got))
		}
	}
	// Never machine-wide: those interfaces were not handed to us.
	if containsPrefix(got, "sysctl net.ipv4.conf.all") {
		t.Errorf("we must not write all.*: %s", dump(got))
	}
}

// §5.5: block is the default, and the UI can then say "no internet — Clash is
// down", which is a sentence somebody can act on.
func TestADownExitBlocksItsTraffic(t *testing.T) {
	c := testConfig()
	c.Normalize()
	clash, _ := c.Find("Clash")

	got := lines(t, c, Health{"Clash": false})
	want := sprintf("route ip table %d unreachable default", clash.Table())
	if !contains(got, want) {
		t.Errorf("a down exit should refuse rather than leak: %s", dump(got))
	}
}

func TestADownExitSetToDirectInstallsNothing(t *testing.T) {
	c := testConfig()
	c.Normalize()
	setExit(&c, "Clash", func(e *Exit) { e.OnFailure = FailDirect })
	clash, _ := c.Find("Clash")

	got := lines(t, c, Health{"Clash": false})

	if containsPrefix(got, sprintf("route ip table %d ", clash.Table())) {
		t.Errorf("failing open means no table entry at all: %s", dump(got))
	}
	if containsPrefix(got, sprintf("rule ip priority %d ", clash.Priority())) {
		t.Errorf("failing open means no policy rule, so traffic uses main: %s", dump(got))
	}
}

func TestAnExitNobodyUsesIsNotProgrammed(t *testing.T) {
	c := testConfig()
	c.Exits = append(c.Exits, Exit{Name: "Spare", Via: Via{Kind: ViaInterface, Interface: "wg0"}})
	c.Normalize()
	spare, _ := c.Find("Spare")

	got := lines(t, c, nil)
	if containsPrefix(got, "route ip table "+itoa(spare.Table())) {
		t.Errorf("a configured but unassigned exit should not reach the kernel: %s", dump(got))
	}
}

func TestDisabledRendersNoRoutingPolicy(t *testing.T) {
	c := testConfig()
	c.Enabled = false
	got := lines(t, c, nil)

	for _, prefix := range []string{"rule ", "route ", "nft source", "nft snat", "sysctl "} {
		if containsPrefix(got, prefix) {
			t.Errorf("disabling should leave the box routing normally, but found %q: %s",
				prefix, dump(got))
		}
	}
}

// §7.1: the accounting table does not depend on the routing one. With no policy
// installed every packet carries mark 0 and the same structure degrades to
// plain per-device totals — so a box that only wants to know who is using the
// bandwidth gets that without routing anything.
func TestAccountingSurvivesRoutingBeingDisabled(t *testing.T) {
	c := testConfig()
	c.Enabled = false
	got := lines(t, c, nil)

	if !contains(got, "nft table inet "+StatTableName) {
		t.Errorf("the accounting table should still be installed: %s", dump(got))
	}
	for _, s := range StatSets() {
		if !contains(got, s.Line()) {
			t.Errorf("missing accounting set %q: %s", s.Name, dump(got))
		}
	}
}

func TestAccountingCanBeTurnedOff(t *testing.T) {
	// §7.5 asks for a visible off switch, and this is it.
	c := testConfig()
	off := false
	c.Stats = &off
	got := lines(t, c, nil)

	if containsPrefix(got, "nft table inet "+StatTableName) || containsPrefix(got, "nft stat set") {
		t.Errorf("stats:false should install no accounting at all: %s", dump(got))
	}
	// And it must not have taken the routing half with it.
	if !containsPrefix(got, "route ip table ") {
		t.Errorf("turning accounting off must not disturb routing: %s", dump(got))
	}
}

func TestAccountingIsOnByDefault(t *testing.T) {
	// A stored document from before this field existed reads as nil, which has
	// to mean on — otherwise upgrading silently stops counting.
	var c Config
	if !c.StatsOrDefault() {
		t.Error("accounting should default on")
	}
	on := true
	c.Stats = &on
	if !c.StatsOrDefault() {
		t.Error("an explicit true should be honoured")
	}
}

// The bug this pairing exists to prevent: keying the *down* set on `ip daddr`
// with nothing qualifying it counts the far end of every outbound packet, so
// the set fills with one row per web server instead of one row per device.
//
// Asserted on the canonical line rather than on the expressions, because that
// line is what an upgraded box compares itself against — see
// TestAccountingLineDistinguishesTheDirections.
func TestAccountingKeysTheConnectionOpenerInBothDirections(t *testing.T) {
	for _, s := range StatSets() {
		line := s.Line()
		switch {
		case s.Down && !strings.Contains(line, "ct direction reply"):
			t.Errorf("%s keys the destination, so it must match the reply "+
				"direction or it counts remote addresses: %q", s.Name, line)
		case !s.Down && !strings.Contains(line, "ct direction original"):
			t.Errorf("%s keys the source, so it must match the original "+
				"direction or it counts remote addresses: %q", s.Name, line)
		}
	}
}

// An older box has a table whose rules are shaped differently but whose sets
// have the same names. The set names alone therefore cannot decide whether it
// needs rebuilding, and the canonical line is what does — so every one of the
// four has to be distinct from every other.
func TestAccountingLineDistinguishesTheDirections(t *testing.T) {
	seen := map[string]string{}
	for _, s := range StatSets() {
		if prev, dup := seen[s.Line()]; dup {
			t.Errorf("%s and %s render the same line %q, so drift between them "+
				"would be invisible", prev, s.Name, s.Line())
		}
		seen[s.Line()] = s.Name
	}
}

func TestAccountingKeyWidthsMatchTheKernelPadding(t *testing.T) {
	// Each concatenated component is padded to the four-byte register the
	// kernel builds the key in, which is why a v4 key is eight bytes and not
	// five. decodeStatKey splits on exactly these offsets.
	for _, s := range StatSets() {
		want := 8
		if s.V6 {
			want = 20
		}
		if got := s.KeyBytes(); got != want {
			t.Errorf("%s key is %d bytes, want %d", s.Name, got, want)
		}
	}
}

func TestInheritingTheBoxDefaultMarksNothing(t *testing.T) {
	// Nothing to mark: unmarked traffic uses `main`, which is exactly what "use
	// the box's own connection" asks for.
	c := Config{
		Enabled:    true,
		Exits:      []Exit{{Name: "Clash", Via: Via{Kind: ViaBlocked}}},
		Interfaces: []Assignment{{Interface: "br-lan"}},
	}
	got := lines(t, c, nil)

	if containsPrefix(got, "nft source") {
		t.Errorf("no classify rule should be emitted: %s", dump(got))
	}
}

func TestCanonicalLinesFitTheKernelsUserdataLimit(t *testing.T) {
	// Each nft line is stored in that rule's netlink userdata, which the kernel
	// caps at 256 bytes. A name at the limit must still fit.
	c := Config{
		Enabled: true,
		Exits: []Exit{{
			Name: strings.Repeat("x", MaxNameLen),
			Via:  Via{Kind: ViaNextHop, NextHop: hop("192.168.1.50")},
		}},
		Interfaces: []Assignment{{Interface: "br-lan", Exit: strings.Repeat("x", MaxNameLen)}},
	}
	for _, l := range lines(t, c, nil) {
		if strings.HasPrefix(l, "nft ") && len(l) > 200 {
			t.Errorf("line is %d bytes, too close to the 256-byte userdata limit: %q", len(l), l)
		}
	}
}
