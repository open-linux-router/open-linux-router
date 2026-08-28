package dhcp

import (
	"encoding/json"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in   string
		want time.Duration
	}{
		{"12h", 12 * time.Hour},
		{"45m", 45 * time.Minute},
		{"90s", 90 * time.Second},
		{"2d", 48 * time.Hour},
		{"1w", 7 * 24 * time.Hour},
		{"1h30m", 90 * time.Minute},
		{"1w2d3h", (7*24 + 2*24 + 3) * time.Hour},
		// dnsmasq reads a unitless lease time as seconds, so an operator who
		// types one gets what they expect rather than an error.
		{"3600", time.Hour},
		{"  12h  ", 12 * time.Hour},
	}
	for _, tc := range tests {
		got, err := ParseDuration(tc.in)
		if err != nil {
			t.Errorf("ParseDuration(%q) failed: %v", tc.in, err)
			continue
		}
		if time.Duration(got) != tc.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tc.in, time.Duration(got), tc.want)
		}
	}
}

func TestParseDurationRejects(t *testing.T) {
	for _, in := range []string{"", "  ", "h", "12y", "12hh", "abc", "-5m", "12h-"} {
		if got, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) = %v, want an error", in, time.Duration(got))
		}
	}
}

func TestDurationString(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		// The point of the wrapper: not "12h0m0s".
		{12 * time.Hour, "12h"},
		{90 * time.Minute, "1h30m"},
		{7 * 24 * time.Hour, "1w"},
		{8 * 24 * time.Hour, "1w1d"},
		{2 * time.Minute, "2m"},
		{0, "0s"},
	}
	for _, tc := range tests {
		if got := Duration(tc.in).String(); got != tc.want {
			t.Errorf("Duration(%v).String() = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestDurationRoundTripsThroughJSON(t *testing.T) {
	for _, in := range []string{"12h", "1h30m", "2m", "1w2d"} {
		parsed, err := ParseDuration(in)
		if err != nil {
			t.Fatalf("ParseDuration(%q): %v", in, err)
		}
		encoded, err := json.Marshal(parsed)
		if err != nil {
			t.Fatalf("Marshal(%q): %v", in, err)
		}
		if want := `"` + in + `"`; string(encoded) != want {
			t.Errorf("Marshal(%q) = %s, want %s", in, encoded, want)
		}
		var back Duration
		if err := json.Unmarshal(encoded, &back); err != nil {
			t.Fatalf("Unmarshal(%s): %v", encoded, err)
		}
		if back != parsed {
			t.Errorf("round trip of %q gave %v", in, back)
		}
	}
}

func TestDurationSeconds(t *testing.T) {
	d, err := ParseDuration("12h")
	if err != nil {
		t.Fatal(err)
	}
	if got := d.Seconds(); got != "43200" {
		t.Errorf("Seconds() = %q, want %q", got, "43200")
	}
}

func TestNormalizeMAC(t *testing.T) {
	tests := []struct{ in, want string }{
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff"},
		{"aa-bb-cc-dd-ee-ff", "aa:bb:cc:dd:ee:ff"},
		{"  aa:bb:cc:dd:ee:ff  ", "aa:bb:cc:dd:ee:ff"},
	}
	for _, tc := range tests {
		got, err := NormalizeMAC(tc.in)
		if err != nil {
			t.Errorf("NormalizeMAC(%q): %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
	for _, in := range []string{"", "not-a-mac", "aa:bb:cc", "zz:bb:cc:dd:ee:ff"} {
		if _, err := NormalizeMAC(in); err == nil {
			t.Errorf("NormalizeMAC(%q) succeeded, want an error", in)
		}
	}
}

// Canonical ordering is what makes drift detection meaningful: rendering
// compares bytes, so if array order leaked through, reordering a JSON list
// would read as a configuration change.
func TestNormalizeIsCanonical(t *testing.T) {
	c := Config{
		Pools: []Pool{
			{Interface: "br-guest", Start: addr(t, "10.10.0.10"), End: addr(t, "10.10.0.20")},
			{Interface: "br-lan", Start: addr(t, "192.168.1.100"), End: addr(t, "192.168.1.200")},
		},
		Reservations: []Reservation{
			{MAC: "FF:EE:DD:CC:BB:AA", IP: addr(t, "192.168.1.50")},
			{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.51")},
		},
	}
	c.Normalize()

	if c.Pools[0].Interface != "br-guest" || c.Pools[1].Interface != "br-lan" {
		t.Errorf("pools not sorted by interface: %v", c.Pools)
	}
	if c.Reservations[0].MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("reservations not sorted by MAC: %v", c.Reservations)
	}
	if c.Reservations[1].MAC != "ff:ee:dd:cc:bb:aa" {
		t.Errorf("MAC not normalized during sort: %q", c.Reservations[1].MAC)
	}
}

// A silently ignored typo produces a router that is quietly not doing what its
// config says, which is this module's worst failure mode.
func TestUnmarshalRejectsUnknownFields(t *testing.T) {
	_, err := UnmarshalConfig([]byte(`{"enabled": true, "enbaled": true}`))
	if err == nil {
		t.Fatal("UnmarshalConfig accepted an unknown field")
	}
	if !strings.Contains(err.Error(), "enbaled") {
		t.Errorf("error does not name the offending field: %v", err)
	}
}

func TestConfigRoundTripsThroughJSON(t *testing.T) {
	gw := addr(t, "192.168.1.254")
	lease, _ := ParseDuration("6h")
	original := Config{
		Enabled: true,
		Pools: []Pool{{
			Interface: "br-lan",
			Start:     addr(t, "192.168.1.100"),
			End:       addr(t, "192.168.1.200"),
			LeaseTime: lease,
			Gateway:   &gw,
			DNS:       []netip.Addr{addr(t, "192.168.1.1")},
			Domain:    "lan",
			RA:        RASLAAC,
			Options:   []Option{{Option: "252", Value: "http://wpad/wpad.dat"}},
		}},
		Reservations: []Reservation{{
			MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.50"), Hostname: "nas",
		}},
		ExtraConf: "log-dhcp",
	}

	data, err := MarshalConfig(original)
	if err != nil {
		t.Fatalf("MarshalConfig: %v", err)
	}
	// The ownership header is the document's "$schema" (core.SchemaURL), and
	// there is exactly one. A module's subtree must not carry a second — it
	// would be a claim about a document this module does not own.
	if strings.Contains(string(data), `"$schema"`) {
		t.Error("module subtree carries its own $schema key; the document owns that")
	}

	back, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatalf("UnmarshalConfig: %v\n%s", err, data)
	}

	original.Normalize()
	if diff := configDiff(original, back); diff != "" {
		t.Errorf("config did not survive the round trip: %s", diff)
	}
}

// configDiff compares via the canonical encoding, which is the only
// representation that matters for drift.
func configDiff(a, b Config) string {
	ja, _ := json.Marshal(a)
	jb, _ := json.Marshal(b)
	if string(ja) == string(jb) {
		return ""
	}
	return "\n  want: " + string(ja) + "\n  got:  " + string(jb)
}

// A document without a dhcp section means "never configured", which is what a
// fresh install looks like — not an error to be reported to the operator.
func TestMissingSectionIsEmptyNotAnError(t *testing.T) {
	store := core.NewStore(t.TempDir()+"/olr.json", ModuleName)
	doc, err := store.Load()
	if err != nil {
		t.Fatalf("Load on a missing document: %v", err)
	}

	c, err := FromDocument(doc)
	if err != nil {
		t.Fatalf("FromDocument: %v", err)
	}
	if c.Enabled || len(c.Pools) != 0 {
		t.Errorf("expected a zero config, got %+v", c)
	}
}

func TestCloneDoesNotShareBackingArrays(t *testing.T) {
	gw := addr(t, "192.168.1.254")
	original := Config{Pools: []Pool{{
		Interface: "br-lan",
		Gateway:   &gw,
		DNS:       []netip.Addr{addr(t, "1.1.1.1")},
		Options:   []Option{{Option: "42", Value: "x"}},
	}}}

	clone := original.Clone()
	clone.Pools[0].DNS[0] = addr(t, "8.8.8.8")
	clone.Pools[0].Options[0].Value = "y"
	*clone.Pools[0].Gateway = addr(t, "10.0.0.1")

	if original.Pools[0].DNS[0].String() != "1.1.1.1" {
		t.Error("Clone shared the DNS slice")
	}
	if original.Pools[0].Options[0].Value != "x" {
		t.Error("Clone shared the options slice")
	}
	if original.Pools[0].Gateway.String() != "192.168.1.254" {
		t.Error("Clone shared the gateway pointer")
	}
}

func TestPoolAndReservationAccessors(t *testing.T) {
	c := validConfig(t)

	c.SetReservation(Reservation{MAC: "AA:BB:CC:DD:EE:FF", IP: addr(t, "192.168.1.50")})
	if _, ok := c.Reservation("aa-bb-cc-dd-ee-ff"); !ok {
		t.Error("Reservation lookup should not care about MAC formatting")
	}

	// Setting the same MAC again replaces rather than duplicates.
	c.SetReservation(Reservation{MAC: "aa:bb:cc:dd:ee:ff", IP: addr(t, "192.168.1.51")})
	if len(c.Reservations) != 1 {
		t.Fatalf("expected 1 reservation, got %d", len(c.Reservations))
	}
	if got, _ := c.Reservation("aa:bb:cc:dd:ee:ff"); got.IP.String() != "192.168.1.51" {
		t.Errorf("SetReservation did not replace: %v", got.IP)
	}

	if !c.RemoveReservation("AA:BB:CC:DD:EE:FF") {
		t.Error("RemoveReservation should match regardless of formatting")
	}
	if c.RemoveReservation("aa:bb:cc:dd:ee:ff") {
		t.Error("RemoveReservation reported success twice")
	}

	if !c.RemovePool("br-lan") {
		t.Error("RemovePool(br-lan) reported nothing removed")
	}
	if c.RemovePool("br-lan") {
		t.Error("RemovePool reported success twice")
	}
}
