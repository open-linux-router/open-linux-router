package routing

import (
	"net/netip"
	"testing"
)

func hop(s string) *netip.Addr {
	a := netip.MustParseAddr(s)
	return &a
}

// testConfig is the reference topology of docs/dns.md §1: a modem and a proxy
// box, both next hops, neither of them something a device is ever told about.
func testConfig() Config {
	return Config{
		Enabled: true,
		Exits: []Exit{
			{Name: "Clash", Via: Via{Kind: ViaNextHop, NextHop: hop("192.168.1.50")}},
			{Name: "Blocked", Via: Via{Kind: ViaBlocked}},
		},
		Interfaces: []Assignment{
			{Interface: "br-lan", Exit: "Clash"},
			{Interface: "br-iot", Exit: "Blocked"},
		},
	}
}

func testLinks() StaticLinks {
	return StaticLinks{
		"br-lan": {Adopted: true, Up: true, Prefixes: []netip.Prefix{
			netip.MustParsePrefix("192.168.1.1/24"),
		}},
		"br-iot": {Adopted: true, Up: true, Prefixes: []netip.Prefix{
			netip.MustParsePrefix("192.168.30.1/24"),
		}},
		"wg0": {Adopted: true, Up: true},
	}
}

func TestNormalizeSortsAndAllocatesSlots(t *testing.T) {
	c := Config{Exits: []Exit{
		{Name: "  Zeta  "},
		{Name: "Alpha"},
	}}
	c.Normalize()

	if c.Exits[0].Name != "Alpha" || c.Exits[1].Name != "Zeta" {
		t.Fatalf("exits not sorted or trimmed: %+v", c.Exits)
	}
	if c.Exits[0].Slot != 1 || c.Exits[1].Slot != 2 {
		t.Fatalf("slots not allocated: %d, %d", c.Exits[0].Slot, c.Exits[1].Slot)
	}
}

// The property the whole slot design exists for: adding an exit must not move
// an existing one, because moving it would move its route table out from under
// every flow already marked for it.
func TestAddingAnExitKeepsEveryOtherSlot(t *testing.T) {
	c := Config{Exits: []Exit{{Name: "Clash"}, {Name: "Modem"}}}
	c.Normalize()
	before := map[string]int{}
	for _, e := range c.Exits {
		before[e.Name] = e.Slot
	}

	// Sorts to the front, which is exactly the case a position-derived slot
	// would get wrong.
	c.Upsert(Exit{Name: "Backup", Via: Via{Kind: ViaBlocked}})

	for _, e := range c.Exits {
		if was, existed := before[e.Name]; existed && e.Slot != was {
			t.Errorf("%s moved from slot %d to %d", e.Name, was, e.Slot)
		}
	}
	if _, ok := c.Find("Backup"); !ok {
		t.Fatal("Backup was not added")
	}
}

func TestUpsertKeepsTheSlotWhenReplacing(t *testing.T) {
	c := Config{Exits: []Exit{{Name: "Clash"}, {Name: "Modem"}}}
	c.Normalize()
	clash, _ := c.Find("Clash")

	c.Upsert(Exit{Name: "Clash", Via: Via{Kind: ViaInterface, Interface: "wg0"}})

	got, _ := c.Find("Clash")
	if got.Slot != clash.Slot {
		t.Fatalf("slot changed on replace: was %d, now %d", clash.Slot, got.Slot)
	}
	if got.Via.Interface != "wg0" {
		t.Fatalf("via not replaced: %+v", got.Via)
	}
}

func TestAllocateSlotsClearsDuplicatesFromAHandEditedFile(t *testing.T) {
	// Two exits sharing a slot means two exits sharing a route table, and one
	// silently carrying the other's traffic.
	c := Config{Exits: []Exit{
		{Name: "A", Slot: 4},
		{Name: "B", Slot: 4},
	}}
	c.Normalize()

	if c.Exits[0].Slot == c.Exits[1].Slot {
		t.Fatalf("duplicate slot survived: %+v", c.Exits)
	}
	if c.Exits[0].Slot != 4 {
		t.Errorf("the first claimant should keep slot 4, got %d", c.Exits[0].Slot)
	}
}

func TestAllocateSlotsRejectsOutOfRange(t *testing.T) {
	c := Config{Exits: []Exit{{Name: "A", Slot: MaxSlot + 1}, {Name: "B", Slot: -3}}}
	c.Normalize()
	for _, e := range c.Exits {
		if !Slot(e.Slot).Valid() {
			t.Errorf("%s kept an out-of-range slot %d", e.Name, e.Slot)
		}
	}
}

func TestSlotResources(t *testing.T) {
	// The property that makes a slot legible on sight: one number, three uses.
	s := Slot(3)
	if got, want := s.Mark(), uint32(0x00030000); got != want {
		t.Errorf("mark = %#08x, want %#08x", got, want)
	}
	if got, want := s.Table(), 8103; got != want {
		t.Errorf("table = %d, want %d", got, want)
	}
	if s.Priority() != s.Table() {
		t.Errorf("priority %d and table %d should read alike", s.Priority(), s.Table())
	}
	if s.Mark()&^MarkMask != 0 {
		t.Errorf("mark %#08x escapes the documented mask %#08x", s.Mark(), MarkMask)
	}
}

func TestOwnsPriorityAndTable(t *testing.T) {
	if !OwnsPriority(LocalPriority) {
		t.Error("the local guard's own priority should be ours")
	}
	if OwnsTable(Base) {
		t.Error("Base is the local guard's priority, not a table of ours")
	}
	if OwnsPriority(Base + MaxSlot + 1) {
		t.Error("the range should stop at Base+MaxSlot")
	}
	for _, prio := range []int{0, 100, 9000, 32766} {
		if OwnsPriority(prio) {
			t.Errorf("priority %d is not in our documented range", prio)
		}
	}
}

func TestAssignedReportsItsSource(t *testing.T) {
	c := testConfig()
	c.Default = "Clash"
	c.Normalize()

	exit, source := c.Assigned("br-iot")
	if exit != "Blocked" || source != SourceInterface {
		t.Errorf("br-iot = %q from %q, want Blocked from interface", exit, source)
	}

	// A network nobody has configured follows the box-wide setting, and says so.
	exit, source = c.Assigned("br-guest")
	if exit != "Clash" || source != SourceDefault {
		t.Errorf("br-guest = %q from %q, want Clash from default", exit, source)
	}
}

// §2.1's motivating case: everything through Clash except one network, which is
// a default plus one override rather than a negation or a rule at position 1.
func TestEverythingThroughOneExitExceptOne(t *testing.T) {
	c := Config{
		Enabled: true,
		Default: "Clash",
		Exits:   []Exit{{Name: "Clash", Via: Via{Kind: ViaBlocked}}},
		Interfaces: []Assignment{
			{Interface: "br-lan"},
			{Interface: "br-nas", Exit: ""},
		},
	}
	c.Normalize()

	// An explicitly-empty assignment inherits, and reports the default as the
	// source so that changing the default makes the consequence visible.
	exit, source := c.Assigned("br-nas")
	if exit != "Clash" || source != SourceDefault {
		t.Errorf("explicit inherit = %q from %q, want Clash from default", exit, source)
	}
}

func TestRenameMovesEveryReference(t *testing.T) {
	c := testConfig()
	c.Default = "Clash"
	c.Normalize()

	if !c.Rename("Clash", "Proxy") {
		t.Fatal("rename reported no such exit")
	}
	if c.Default != "Proxy" {
		t.Errorf("default still names the old exit: %q", c.Default)
	}
	for _, a := range c.Interfaces {
		if a.Exit == "Clash" {
			t.Errorf("%s still points at the old name", a.Interface)
		}
	}
	if _, ok := c.Find("Proxy"); !ok {
		t.Error("the renamed exit is missing")
	}
}

func TestRenameKeepsTheSlot(t *testing.T) {
	c := testConfig()
	c.Normalize()
	before, _ := c.Find("Clash")

	c.Rename("Clash", "Proxy")

	after, _ := c.Find("Proxy")
	if after.Slot != before.Slot {
		t.Errorf("renaming moved the route table: slot %d became %d", before.Slot, after.Slot)
	}
}

func TestRemoveLeavesDanglingReferencesForValidateToReport(t *testing.T) {
	c := testConfig()
	c.Normalize()
	c.Remove("Clash")

	// Deliberate: silently re-pointing br-lan at the modem because an exit was
	// deleted elsewhere is worse than refusing.
	res := Validate(c, testLinks())
	if res.OK() {
		t.Fatal("a dangling assignment should be an error")
	}
}

func TestSNATDefaultsOnForANextHopAndOffOtherwise(t *testing.T) {
	next := Exit{Via: Via{Kind: ViaNextHop, NextHop: hop("192.168.1.50")}}
	if !next.SNATOrDefault() {
		t.Error("a next hop should SNAT by default (§5.3)")
	}
	off := false
	next.SNAT = &off
	if next.SNATOrDefault() {
		t.Error("an explicit false should be honoured")
	}
	iface := Exit{Via: Via{Kind: ViaInterface, Interface: "wg0"}}
	if iface.SNATOrDefault() {
		t.Error("an interface exit has no next hop to SNAT toward")
	}
}

func TestIPv6DefaultsToBlock(t *testing.T) {
	// The conservative answer, because an exit whose v6 capability we cannot
	// see is one where carrying it might work and might leak silently.
	if got := (Exit{}).IPv6OrDefault(); got != IPv6Block {
		t.Errorf("default IPv6 handling = %q, want %q", got, IPv6Block)
	}
}

func TestRoundTripThroughJSON(t *testing.T) {
	c := testConfig()
	c.Default = "Clash"
	c.Normalize()

	data, err := MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("re-reading what we wrote: %v", err)
	}
	if len(back.Exits) != len(c.Exits) || back.Default != c.Default {
		t.Fatalf("round trip lost data: %+v", back)
	}
	got, _ := back.Find("Clash")
	if got.Via.NextHop == nil || got.Via.NextHop.String() != "192.168.1.50" {
		t.Errorf("next hop did not survive: %+v", got.Via)
	}
	if got.Slot == 0 {
		t.Error("the slot did not survive, which would renumber every exit on reload")
	}
}

func TestUnmarshalRejectsUnknownFields(t *testing.T) {
	_, err := UnmarshalConfig([]byte(`{"enabled":true,"exitz":[]}`))
	if err == nil {
		t.Fatal("a mistyped key should be an error, not a silent no-op")
	}
}

func TestDurationRoundTripsAsAString(t *testing.T) {
	var d Duration
	if err := d.UnmarshalText([]byte("30s")); err != nil {
		t.Fatal(err)
	}
	text, err := d.MarshalText()
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != "30s" {
		t.Errorf("duration serialised as %q, want 30s", text)
	}
}
