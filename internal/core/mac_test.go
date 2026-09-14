package core

import (
	"strings"
	"testing"
)

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

func TestOUIPrefixes(t *testing.T) {
	got, ok := OUIPrefixes("8c:1f:64:af:a0:01")
	if !ok {
		t.Fatal("OUIPrefixes returned false for a globally-administered address")
	}
	// MA-S, MA-M, MA-L: 36, 28 and 24 bits, which is 9, 7 and 6 hex digits.
	// Upper case and unseparated, because that is how IEEE publishes an
	// assignment and the caller uses these as table keys unchanged.
	want := []string{"8C1F64AFA", "8C1F64A", "8C1F64"}
	if len(got) != len(want) {
		t.Fatalf("OUIPrefixes = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("OUIPrefixes[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// The ordering is the whole point: 8c:1f:64 is an MA-L that IEEE kept and
// subdivided, so hundreds of companies share it. A caller that tried the short
// prefix first would match the block IEEE registered to itself and stop, and
// every device in the range would be reported as made by the same company.
func TestOUIPrefixesAreLongestFirst(t *testing.T) {
	got, ok := OUIPrefixes("aa:bb:cc:dd:ee:ff")
	if ok {
		t.Fatalf("OUIPrefixes = %q for a locally-administered address, want false", got)
	}

	got, ok = OUIPrefixes("b8:27:eb:12:34:56")
	if !ok {
		t.Fatal("OUIPrefixes returned false for a globally-administered address")
	}
	for i := 1; i < len(got); i++ {
		if len(got[i]) >= len(got[i-1]) {
			t.Fatalf("OUIPrefixes = %q, which is not ordered longest first", got)
		}
		if !strings.HasPrefix(got[i-1], got[i]) {
			t.Errorf("%q is not a prefix of %q; they do not describe one address",
				got[i], got[i-1])
		}
	}
}

// Same rejections as OUI, and for the same reasons — a caller that switched
// from one to the other must not silently start answering for addresses the
// other refused.
func TestOUIPrefixesRefusesWhatOUIRefuses(t *testing.T) {
	for _, mac := range []string{
		"nonsense",
		"",
		"02:00:00:11:22:33",
		"aa:bb:cc:dd:ee:ff",
	} {
		_, wantOK := OUI(mac)
		if _, ok := OUIPrefixes(mac); ok != wantOK {
			t.Errorf("OUIPrefixes(%q) ok = %v, but OUI(%q) ok = %v", mac, ok, mac, wantOK)
		}
	}
}
