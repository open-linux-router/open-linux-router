package remote

import (
	"encoding/base64"
	"strings"
	"testing"
)

// Key generation is the one thing here that had to be reimplemented rather than
// driven, so it is the one thing worth testing against the definition rather
// than against itself: 32 bytes, clamped the way RFC 7748 says and `wg genkey`
// does, with a public half X25519 agrees with.

func TestGeneratedKeysAreThirtyTwoClampedBytes(t *testing.T) {
	pair, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}

	raw, err := base64.StdEncoding.DecodeString(pair.Private)
	if err != nil {
		t.Fatalf("private key is not base64: %v", err)
	}
	if len(raw) != KeyLen {
		t.Fatalf("private key is %d bytes, want %d", len(raw), KeyLen)
	}

	// The three clamping invariants, asserted individually so a failure says
	// which one moved rather than "the bytes differ".
	if raw[0]&7 != 0 {
		t.Errorf("low three bits of byte 0 are set: %08b", raw[0])
	}
	if raw[31]&128 != 0 {
		t.Errorf("high bit of byte 31 is set: %08b", raw[31])
	}
	if raw[31]&64 == 0 {
		t.Errorf("second-highest bit of byte 31 is clear: %08b", raw[31])
	}
}

func TestPublicKeyIsDerivedFromThePrivateOne(t *testing.T) {
	pair, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	// The property that makes deriving on demand safe: there is one answer, so
	// a client configuration written today and one written next year name the
	// same key.
	for range 3 {
		got, err := PublicKeyFor(pair.Private)
		if err != nil {
			t.Fatal(err)
		}
		if got != pair.Public {
			t.Fatalf("derived %q, want %q", got, pair.Public)
		}
	}
}

func TestTwoKeysAreNotTheSameKey(t *testing.T) {
	a, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if a.Private == b.Private || a.Public == b.Public {
		t.Fatal("two generated key pairs are identical; the randomness is not")
	}
}

func TestValidKeyRejectsTheWaysAPasteGoesWrong(t *testing.T) {
	good := mustKey().Public

	for _, tc := range []struct {
		name string
		key  string
		want string
	}{
		{"empty", "", "empty"},
		{"not base64", "this is not a key at all!!", "base64"},
		// The one an operator actually produces: a key that lost its tail to a
		// terminal wrap. Still valid base64, so only the length catches it.
		{"truncated", base64.StdEncoding.EncodeToString(make([]byte, 16)), "bytes"},
		{"too long", base64.StdEncoding.EncodeToString(make([]byte, 64)), "bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidKey(tc.key)
			if err == nil {
				t.Fatalf("ValidKey(%q) = nil, want an error", tc.key)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}

	if err := ValidKey(good); err != nil {
		t.Errorf("ValidKey rejected a generated key: %v", err)
	}
	// Whitespace from a copy-paste is tolerated rather than refused: the value
	// is unambiguous and refusing it would be a lesson in shell quoting.
	if err := ValidKey("  " + good + "\n"); err != nil {
		t.Errorf("ValidKey rejected a key with surrounding whitespace: %v", err)
	}
}
