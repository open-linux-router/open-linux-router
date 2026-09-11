package firewall

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
)

// The shared fixture: a box with one uplink and one LAN, and a web server
// forwarded in.
func testLinks() StaticLinks {
	return StaticLinks{
		"wan0": {
			Adopted:  true,
			Up:       true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.7/24")},
		},
		"br-lan": {
			Adopted:  true,
			Up:       true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.1/24")},
		},
		"eth2": {Adopted: false, Up: true},
	}
}

func testForward() Forward {
	return Forward{
		Name: "web",
		In:   "wan0",
		Port: SinglePort(8080),
		To:   netip.MustParseAddrPort("192.168.1.10:80"),
	}
}

func testConfig() Config {
	c := Config{Enabled: true, Forwards: []Forward{testForward()}}
	c.Normalize()
	return c
}

func TestPortRangeRoundTrips(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want PortRange
	}{
		{"8080", PortRange{8080, 8080}},
		{"1", PortRange{1, 1}},
		{"65535", PortRange{65535, 65535}},
		{"30000-30010", PortRange{30000, 30010}},
		{" 443 ", PortRange{443, 443}},
	} {
		var got PortRange
		if err := got.UnmarshalText([]byte(tc.in)); err != nil {
			t.Errorf("parsing %q: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parsing %q = %v, want %v", tc.in, got, tc.want)
		}
		// The spelling has to survive the round trip, because it is what is
		// stored, diffed and printed — three readers of one string.
		if reparsed := (PortRange{}); true {
			if err := reparsed.UnmarshalText([]byte(got.String())); err != nil || reparsed != tc.want {
				t.Errorf("%q did not round-trip through %q: %v, %v", tc.in, got.String(), reparsed, err)
			}
		}
	}
}

func TestPortRangeRefusesNonsense(t *testing.T) {
	for _, in := range []string{"", "0", "0-10", "65536", "8080/tcp", "8080:80", "-5", "30010-30000", "abc"} {
		var got PortRange
		if err := got.UnmarshalText([]byte(in)); err == nil {
			t.Errorf("parsing %q was accepted as %v; want an error", in, got)
		}
	}
}

// The whole reason schema.go exists: these types marshal as strings, and a
// document that wrote them as anything else would be a document no other surface
// could read.
func TestForwardMarshalsPortsAndAddressesAsStrings(t *testing.T) {
	data, err := json.Marshal(testForward())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"port":"8080"`, `"to":"192.168.1.10:80"`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("missing %s in %s", want, data)
		}
	}
}

func TestUnmarshalRejectsUnknownFields(t *testing.T) {
	// A mistyped key that silently did nothing would be the worst outcome: a
	// 200, an operator who believes the setting took, and a screen that
	// disagrees.
	_, err := UnmarshalConfig([]byte(`{"enabled":true,"forwardz":[]}`))
	if err == nil {
		t.Fatal("an unknown field was accepted")
	}
}

func TestNormalizeSortsAndAllocatesSlots(t *testing.T) {
	c := Config{Forwards: []Forward{
		{Name: "web", In: "wan0", Port: SinglePort(80), To: netip.MustParseAddrPort("192.168.1.10:80")},
		{Name: "asterisk", In: "wan0", Port: SinglePort(5060), To: netip.MustParseAddrPort("192.168.1.11:5060")},
	}}
	c.Normalize()

	if c.Forwards[0].Name != "asterisk" {
		t.Errorf("forwards are not sorted by name: %v", names(c.Forwards))
	}
	for _, f := range c.Forwards {
		if !Slot(f.Slot).Valid() {
			t.Errorf("%q got no slot", f.Name)
		}
	}
	if c.Forwards[0].Slot == c.Forwards[1].Slot {
		t.Error("two forwards share a slot, so they would share a counter")
	}
}

// The point of storing the slot rather than deriving it from position: adding a
// forward that sorts earlier must not renumber the others, because the number an
// operator is watching answers "has this ever been hit?".
func TestAddingAForwardKeepsTheOthersSlots(t *testing.T) {
	c := testConfig()
	before := c.Forwards[0].Slot

	c.Upsert(Forward{
		Name: "aaa-sorts-first", In: "wan0",
		Port: SinglePort(9999), To: netip.MustParseAddrPort("192.168.1.11:9999"),
	})

	web, ok := c.Find("web")
	if !ok {
		t.Fatal("web disappeared")
	}
	if web.Slot != before {
		t.Errorf("web's slot moved from %d to %d when an unrelated forward was added", before, web.Slot)
	}
}

// Renaming keeps the counter, which is the only reason Rename exists rather than
// a delete and an add.
func TestRenameKeepsTheSlot(t *testing.T) {
	c := testConfig()
	before := c.Forwards[0].Slot

	if !c.Rename("web", "website") {
		t.Fatal("rename reported the forward was not there")
	}
	f, ok := c.Find("website")
	if !ok {
		t.Fatal("the renamed forward is gone")
	}
	if f.Slot != before {
		t.Errorf("slot changed from %d to %d, so the counter would reset", before, f.Slot)
	}
	if _, ok := c.Find("web"); ok {
		t.Error("the old name is still there")
	}
}

// Upsert replaces, and replacing must not reallocate the slot either.
func TestUpsertKeepsTheSlot(t *testing.T) {
	c := testConfig()
	before := c.Forwards[0].Slot

	edited := testForward()
	edited.Port = SinglePort(9090)
	c.Upsert(edited)

	f, _ := c.Find("web")
	if f.Slot != before {
		t.Errorf("slot changed from %d to %d on an edit", before, f.Slot)
	}
	if f.Port != SinglePort(9090) {
		t.Errorf("the edit did not take: port is %v", f.Port)
	}
}

// A hand-edited file with two forwards claiming one slot must not end up with
// them sharing a counter; allocateSlots clears the duplicate and Validate
// reports it.
func TestDuplicateSlotsAreReallocated(t *testing.T) {
	c := Config{Forwards: []Forward{
		{Name: "a", Slot: 3, In: "wan0", Port: SinglePort(1), To: netip.MustParseAddrPort("192.168.1.10:1")},
		{Name: "b", Slot: 3, In: "wan0", Port: SinglePort(2), To: netip.MustParseAddrPort("192.168.1.10:2")},
	}}
	c.Normalize()

	if c.Forwards[0].Slot == c.Forwards[1].Slot {
		t.Fatalf("duplicate slot survived normalisation: %d", c.Forwards[0].Slot)
	}
}

func TestProtocolEachExpandsBoth(t *testing.T) {
	for _, tc := range []struct {
		in   Protocol
		want []Protocol
	}{
		{"", []Protocol{ProtocolTCP}},
		{ProtocolTCP, []Protocol{ProtocolTCP}},
		{ProtocolUDP, []Protocol{ProtocolUDP}},
		{ProtocolBoth, []Protocol{ProtocolTCP, ProtocolUDP}},
	} {
		got := tc.in.Each()
		if len(got) != len(tc.want) {
			t.Errorf("%q.Each() = %v, want %v", tc.in, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("%q.Each() = %v, want %v", tc.in, got, tc.want)
				break
			}
		}
	}
}

func TestHairpinDefaultsOn(t *testing.T) {
	// On by default because off-by-default is a setting nobody discovers: the
	// failure it prevents presents as the port forward being broken, tested from
	// the one machine the operator has to hand.
	if !testForward().HairpinOrDefault() {
		t.Error("hairpin is off by default")
	}

	off := false
	f := testForward()
	f.Hairpin = &off
	if f.HairpinOrDefault() {
		t.Error("--no-hairpin did not take")
	}

	// The pointer has to survive a round trip, or an older document changes
	// behaviour on upgrade.
	data, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	var back Forward
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	if back.HairpinOrDefault() {
		t.Errorf("hairpin:false did not survive the round trip: %s", data)
	}
}

func TestFromDocumentTreatsAMissingSectionAsEmpty(t *testing.T) {
	// A fresh install has no firewall key, and that is an empty state rather
	// than a failure.
	c, err := UnmarshalConfig([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !c.Empty() || c.Enabled {
		t.Errorf("an empty document produced %+v", c)
	}
}

// mustAddrPort is netip.MustParseAddrPort under a local name, so the test files
// read as one fixture set rather than as four importers of netip.
func mustAddrPort(s string) netip.AddrPort { return netip.MustParseAddrPort(s) }
