package core

import "testing"

func TestNormalizeMAC(t *testing.T) {
	tests := []struct{ in, want string }{
		{"aa:bb:cc:dd:ee:ff", "aa:bb:cc:dd:ee:ff"},
		{"AA:BB:CC:DD:EE:FF", "aa:bb:cc:dd:ee:ff"},
		{"AA-BB-CC-DD-EE-FF", "aa:bb:cc:dd:ee:ff"},
		{"  aa:bb:cc:dd:ee:ff  ", "aa:bb:cc:dd:ee:ff"},
	}
	for _, tc := range tests {
		got, err := NormalizeMAC(tc.in)
		if err != nil {
			t.Errorf("NormalizeMAC(%q) failed: %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("NormalizeMAC(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// The whole reason this lives in core: every spelling of one address has to
// collapse to one key, or a device's fixed address belongs to nobody.
func TestNormalizeMACAgreesAcrossSpellings(t *testing.T) {
	forms := []string{
		"aa:bb:cc:dd:ee:ff",
		"AA:BB:CC:DD:EE:FF",
		"aa-bb-cc-dd-ee-ff",
		"Aa:Bb:Cc:Dd:Ee:Ff",
	}
	var first string
	for i, f := range forms {
		got, err := NormalizeMAC(f)
		if err != nil {
			t.Fatalf("NormalizeMAC(%q) failed: %v", f, err)
		}
		if i == 0 {
			first = got
			continue
		}
		if got != first {
			t.Errorf("NormalizeMAC(%q) = %q, but NormalizeMAC(%q) = %q", f, got, forms[0], first)
		}
	}
}

func TestNormalizeMACRejects(t *testing.T) {
	for _, in := range []string{"", "  ", "nonsense", "aa:bb:cc:dd:ee", "aa:bb:cc:dd:ee:gg"} {
		if got, err := NormalizeMAC(in); err == nil {
			t.Errorf("NormalizeMAC(%q) = %q, want an error", in, got)
		}
	}
}

func TestOUI(t *testing.T) {
	got, ok := OUI("B8:27:EB:12:34:56")
	if !ok {
		t.Fatal("OUI returned false for a globally-administered address")
	}
	if got != "b8:27:eb" {
		t.Errorf("OUI = %q, want %q", got, "b8:27:eb")
	}
}

// A randomised MAC has the locally-administered bit set, and its vendor bits
// are invented. Reporting a vendor for one would be a confident lie about an
// address that was made up moments earlier — which is the case every modern
// phone presents by default.
func TestOUIRefusesLocallyAdministeredAddresses(t *testing.T) {
	// Second-least-significant bit of the first octet set: 0x02, 0x06, 0xaa…
	for _, mac := range []string{
		"02:00:00:11:22:33",
		"a2:83:e7:11:22:33",
		"aa:bb:cc:dd:ee:ff",
	} {
		if got, ok := OUI(mac); ok {
			t.Errorf("OUI(%q) = %q, want false: the address is locally administered", mac, got)
		}
	}
}

func TestOUIRejectsUnparseable(t *testing.T) {
	if got, ok := OUI("nonsense"); ok {
		t.Errorf("OUI = %q, want false", got)
	}
}
