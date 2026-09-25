package dial

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNamesAreCanonicalised(t *testing.T) {
	c := Config{Records: []Record{
		{Name: "  Home.Example.NET. ", Provider: " Cloudflare "},
		{Name: "a.example.net"},
	}}
	c.Normalize()

	// Sorted, so two identical configs produce identical bytes downstream.
	if c.Records[0].Name != "a.example.net" || c.Records[1].Name != "home.example.net" {
		t.Fatalf("names = %v", c.Names())
	}
	if c.Records[1].Provider != "cloudflare" {
		t.Errorf("provider = %q", c.Records[1].Provider)
	}
}

// "Helpfully" altering a credential produces an authentication failure whose
// cause is invisible in every diff, because the diff looks right.
func TestTheTokenIsNeitherTrimmedNorCased(t *testing.T) {
	c := Config{Records: []Record{{Name: "home.example.net", Token: "  AbC  "}}}
	c.Normalize()
	if c.Records[0].Token != "  AbC  " {
		t.Errorf("token = %q; it is opaque and must survive normalisation", c.Records[0].Token)
	}
}

func TestZoneIsDerivedFromThePublicSuffixList(t *testing.T) {
	for name, want := range map[string]string{
		"home.example.net":     "example.net",
		"a.b.home.example.net": "example.net",
		"home.example.co.uk":   "example.co.uk",
		"example.net":          "example.net",
		// An unlisted final label is treated as the suffix, which is the right
		// answer for a split-horizon zone rather than a refusal.
		"home.example.internal": "example.internal",
		// Nothing left to be the registered domain. The validator asks for the
		// field rather than letting a provider answer "no such zone".
		"co.uk": "",
		"net":   "",
	} {
		if got := (Record{Name: name}).ZoneOrDerived(); got != want {
			t.Errorf("ZoneOrDerived(%q) = %q, want %q", name, got, want)
		}
	}

	// An explicit zone always wins: the list cannot know about a zone delegated
	// below a registrar's own suffix.
	r := Record{Name: "home.corp.example.net", Zone: "corp.example.net"}
	if got := r.ZoneOrDerived(); got != "corp.example.net" {
		t.Errorf("an explicit zone was overridden: %q", got)
	}
}

// Both credential-bearing fields go, including the callback URL — for that
// provider the URL *is* the credential.
func TestRedactedHidesBothCredentialFields(t *testing.T) {
	c := Config{Records: []Record{{
		Name:         "home.example.net",
		Token:        "secret",
		CallbackURL:  "https://www.duckdns.org/update?token=secret&ip=#{ip}",
		KeyID:        "LTAIexample",
		ReflectorURL: "https://reflector.example/",
	}}}

	got := c.Redacted().Records[0]
	if got.Token != RedactedToken || got.CallbackURL != RedactedToken {
		t.Fatalf("redacted = %+v", got)
	}
	// The key ID and the reflector are not secrets, and hiding them would cost
	// the operator the two fields most worth checking when a record stops
	// updating.
	if got.KeyID != "LTAIexample" || got.ReflectorURL != "https://reflector.example/" {
		t.Errorf("redacted too much: %+v", got)
	}
	// The original is untouched, so redacting for a printing surface cannot
	// store the mask.
	if c.Records[0].Token != "secret" {
		t.Error("Redacted mutated the config it was called on")
	}
}

// A typo'd key that is silently ignored produces a box quietly not doing what
// its config says — here, a credential that keeps working until it is rotated.
func TestUnknownFieldsAreRefused(t *testing.T) {
	_, err := UnmarshalConfig([]byte(`{"records":[{"name":"a.example.net","provider_secret":"x"}]}`))
	if err == nil {
		t.Fatal("an unknown field should be refused")
	}
}

func TestIntervalRoundTripsAsAString(t *testing.T) {
	c := Config{Records: []Record{{
		Name: "home.example.net", Interval: Duration(90 * time.Second),
	}}}

	data, err := MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"1m30s"`) {
		t.Fatalf("marshalled as %s; want a readable duration", data)
	}

	back, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatal(err)
	}
	if back.Records[0].Interval != c.Records[0].Interval {
		t.Errorf("round-tripped to %s", back.Records[0].Interval)
	}
}

func TestSetAndRemoveKeyByName(t *testing.T) {
	var c Config
	c.SetRecord(Record{Name: "Home.Example.NET", Provider: "cloudflare"})
	c.SetRecord(Record{Name: "home.example.net", Provider: "alidns"})

	if len(c.Records) != 1 {
		t.Fatalf("two spellings of one name became %d records", len(c.Records))
	}
	if c.Records[0].Provider != "alidns" {
		t.Errorf("the second write did not replace the first: %+v", c.Records[0])
	}

	if !c.RemoveRecord("home.example.net.") {
		t.Fatal("removing by another spelling of the name failed")
	}
	if !c.Empty() {
		t.Error("the record survived removal")
	}
	if c.RemoveRecord("home.example.net") {
		t.Error("removing a record twice reported a second removal")
	}
}

func TestResolvedIntervalFillsInTheDefault(t *testing.T) {
	if got := (Record{}).ResolvedInterval(); got != DefaultInterval {
		t.Errorf("ResolvedInterval() = %s, want %s", got, DefaultInterval)
	}
	if got := (Record{Interval: Duration(time.Minute)}).ResolvedInterval(); got != time.Minute {
		t.Errorf("ResolvedInterval() = %s", got)
	}
}

// The one enum in this tree with no legal empty value: choosing on the
// operator's behalf is the inference docs/ddns.md §3.1 refuses.
func TestTheEmptySourceIsNotValid(t *testing.T) {
	if Source("").Valid() {
		t.Fatal("the empty source must not be valid; there is no safe default")
	}
	for _, s := range Sources() {
		if !s.Valid() {
			t.Errorf("%q is in Sources() but not Valid()", s)
		}
	}
}

// A fresh install has no records, and that has to read as an empty state rather
// than as a failure.
func TestAnEmptySectionParsesAsNoRecords(t *testing.T) {
	cfg, err := UnmarshalConfig([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Empty() {
		t.Error("an empty object should parse as no records")
	}
}

// --- the uplink -------------------------------------------------------------

func testUplink() Uplink {
	return Uplink{
		Interface: "enp2s0",
		IPv4: &UplinkIPv4{
			Address: netip.MustParsePrefix("192.168.2.9/24"),
			Gateway: netip.MustParseAddr("192.168.2.1"),
		},
	}
}

// The difference from link.NetworkIPv4.Subnet, which *is* masked. Masking this
// one would quietly turn the box's own address into the network address —
// something netlink accepts and nothing can reach.
func TestTheUplinkAddressKeepsItsHostBits(t *testing.T) {
	c := Config{Uplink: &Uplink{
		Interface: "enp2s0",
		IPv4: &UplinkIPv4{
			Address: netip.MustParsePrefix("192.168.2.9/24"),
			Gateway: netip.MustParseAddr("192.168.2.1"),
		},
	}}
	c.Normalize()

	if got := c.Uplink.IPv4.Address.String(); got != "192.168.2.9/24" {
		t.Fatalf("address = %s; Normalize must not mask it", got)
	}
}

func TestTheUplinkRoundTrips(t *testing.T) {
	u := testUplink()
	u.DNS = []netip.Addr{netip.MustParseAddr("9.9.9.9")}
	c := Config{Uplink: &u}

	data, err := MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("%v (from %s)", err, data)
	}
	if !back.Uplink.Equal(c.Uplink) {
		t.Errorf("round-tripped %s to %+v", data, back.Uplink)
	}
}

// Order is meaning here — resolvers are tried in the order given — so unlike
// every other list in this tree it is not sorted. Duplicates still go.
func TestUplinkResolversKeepTheirOrderAndLoseDuplicates(t *testing.T) {
	c := Config{Uplink: &Uplink{Interface: "enp2s0", DNS: []netip.Addr{
		netip.MustParseAddr("9.9.9.9"),
		netip.MustParseAddr("1.1.1.1"),
		netip.MustParseAddr("9.9.9.9"),
	}}}
	c.Normalize()

	want := []netip.Addr{netip.MustParseAddr("9.9.9.9"), netip.MustParseAddr("1.1.1.1")}
	if !slices.Equal(c.Uplink.DNS, want) {
		t.Errorf("dns = %v, want %v", c.Uplink.DNS, want)
	}
}

// Empty gates whether startDial restores anything, so a box with an uplink and
// no records must not read as unconfigured — that would leave the default route
// gone after every reboot.
func TestAnUplinkAloneIsNotEmpty(t *testing.T) {
	u := testUplink()
	if (Config{Uplink: &u}).Empty() {
		t.Error("a box with an uplink and no records reads as unconfigured")
	}
	if !(Config{}).Empty() {
		t.Error("a box with neither should be empty")
	}
}

func TestCloneDoesNotShareTheUplink(t *testing.T) {
	u := testUplink()
	u.DNS = []netip.Addr{netip.MustParseAddr("9.9.9.9")}
	c := Config{Uplink: &u}

	clone := c.Clone()
	clone.Uplink.Interface = "enp3s0"
	clone.Uplink.IPv4.Gateway = netip.MustParseAddr("10.0.0.1")
	clone.Uplink.DNS[0] = netip.MustParseAddr("8.8.8.8")

	if c.Uplink.Interface != "enp2s0" ||
		c.Uplink.IPv4.Gateway != netip.MustParseAddr("192.168.2.1") ||
		c.Uplink.DNS[0] != netip.MustParseAddr("9.9.9.9") {
		t.Errorf("editing the clone reached the original: %+v", *c.Uplink)
	}
}

// Removing the uplink stops olr owning the way out. It deliberately does not
// tear the route down — Config.RemoveUplink has the argument — so this only
// asserts the document half.
func TestRemoveUplinkReportsWhetherThereWasOne(t *testing.T) {
	var c Config
	c.SetUplink(testUplink())
	if !c.RemoveUplink() {
		t.Fatal("removing a configured uplink reported nothing to remove")
	}
	if c.Uplink != nil {
		t.Error("the uplink survived removal")
	}
	if c.RemoveUplink() {
		t.Error("removing twice reported a second removal")
	}
}
