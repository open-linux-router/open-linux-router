package dns

import (
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// The case this exists for: the operator turned DNS on and said nothing else,
// which is the first switch on a fresh box.
func TestDerivedListenFillsFromAdoptedInterfaces(t *testing.T) {
	cfg := Config{Enabled: true}

	got, notes := cfg.WithDerivedListen(testLinks())

	// lan0's two addresses, and neither of the other interfaces': guest0 is up
	// with an address and was never handed to us, and wan0 *was* — adopting the
	// uplink is normal, gateway needs it — but its address is public, and
	// choosing it by ourselves would be how a helpful default turns this box
	// into an open resolver.
	want := []netip.AddrPort{
		netip.MustParseAddrPort("192.168.1.1:53"),
		netip.MustParseAddrPort("[fd00::1]:53"),
	}
	if !slices.Equal(got.Listen, want) {
		t.Errorf("listen = %v, want %v", got.Listen, want)
	}
	if len(notes) != len(want) {
		t.Fatalf("got %d notes for %d addresses: %v", len(notes), len(want), notes)
	}
	// The note is what the CLI prints and the UI toasts, so it has to name both
	// the address and where it came from.
	if !strings.Contains(notes[0], "192.168.1.1:53") || !strings.Contains(notes[0], "lan0") {
		t.Errorf("note %q names neither the address nor the interface", notes[0])
	}
	if !Validate(got, testLinks(), nil).OK() {
		t.Error("the derived config does not pass validation, which is the only thing it is for")
	}
}

// Derivation is a blank-filler, never an override.
func TestDerivedListenLeavesAnAnswerAlone(t *testing.T) {
	cfg := validConfig()

	got, notes := cfg.WithDerivedListen(testLinks())

	if !slices.Equal(got.Listen, cfg.Listen) {
		t.Errorf("listen = %v, want it untouched at %v", got.Listen, cfg.Listen)
	}
	if notes != nil {
		t.Errorf("reported %v for a config that already said where to listen", notes)
	}
}

// Turning DNS off must not write an address in on the way past: a disabled
// config is still one the operator reads, and a listen address appearing in it
// would look like something they set.
func TestDerivedListenDoesNothingWhileOff(t *testing.T) {
	cfg := Config{Enabled: false}

	got, notes := cfg.WithDerivedListen(testLinks())

	if len(got.Listen) != 0 || notes != nil {
		t.Errorf("listen = %v, notes = %v, want neither", got.Listen, notes)
	}
}

// Nothing adopted is the one case that stays an error, because the missing
// thing is an interface rather than a setting.
func TestDerivedListenWithNothingAdopted(t *testing.T) {
	links := StaticLinks{
		"eth0": {Name: "eth0", Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.2/24")}},
	}

	got, notes := Config{Enabled: true}.WithDerivedListen(links)

	if len(got.Listen) != 0 || notes != nil {
		t.Fatalf("listen = %v, notes = %v, want neither", got.Listen, notes)
	}
	res := Validate(got, links, nil)
	if res.OK() {
		t.Fatal("enabled with nowhere to answer was accepted")
	}
	if !strings.Contains(res.Errors[0].Message, "interface") {
		t.Errorf("message %q does not point at the missing interface", res.Errors[0].Message)
	}
}

// Adopted, but nothing here is a home network. Still an error — it is just a
// different one, and telling this operator to go adopt an interface would be
// advice they have already taken.
func TestDerivedListenWithNoPrivateAddress(t *testing.T) {
	links := StaticLinks{
		"wan0": {Name: "wan0", Adopted: true, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("203.0.113.7/24")}},
	}

	got, notes := Config{Enabled: true}.WithDerivedListen(links)

	if len(got.Listen) != 0 || notes != nil {
		t.Fatalf("listen = %v, notes = %v, want neither", got.Listen, notes)
	}
	res := Validate(got, links, nil)
	if res.OK() {
		t.Fatal("enabled with nowhere to answer was accepted")
	}
	if strings.Contains(res.Errors[0].Message, "not been given an interface") {
		t.Errorf("message %q tells an operator who has adopted one to adopt one", res.Errors[0].Message)
	}
}

// An fe80:: address is on every IPv6 interface and is reachable from nowhere
// useful, so binding it would produce a listen address that looks configured
// and answers almost nobody.
func TestDerivedListenSkipsLinkLocalAndLoopback(t *testing.T) {
	links := StaticLinks{
		"lan0": {Name: "lan0", Adopted: true, Up: true, Prefixes: []netip.Prefix{
			netip.MustParsePrefix("fe80::1/64"),
			netip.MustParsePrefix("127.0.0.1/8"),
			netip.MustParsePrefix("10.0.0.1/24"),
		}},
	}

	got, _ := Config{Enabled: true}.WithDerivedListen(links)

	want := []netip.AddrPort{netip.MustParseAddrPort("10.0.0.1:53")}
	if !slices.Equal(got.Listen, want) {
		t.Errorf("listen = %v, want %v", got.Listen, want)
	}
}
