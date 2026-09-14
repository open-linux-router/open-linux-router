package core

import (
	"fmt"
	"net"
	"strings"
)

// NormalizeMAC lowercases and canonicalises a hardware address, so that
// "AA-BB-CC-DD-EE-FF" and "aa:bb:cc:dd:ee:ff" are one device rather than two.
// It returns an error for anything net.ParseMAC rejects.
//
// It lives in core rather than in a module because a MAC is the key two modules
// join on: `dhcp` keys a reservation by it, `devices` keys identity by it, and
// the device list is that join (design.md §4.4). Two implementations of "the
// same address" that disagreed in any corner — a stray space, an upper-case
// hex digit, a hyphenated form — would be exactly the drift §4.1 exists to make
// structurally impossible, and it would show up as a fixed address belonging to
// a device that the list says does not have one.
func NormalizeMAC(s string) (string, error) {
	hw, err := net.ParseMAC(strings.TrimSpace(s))
	if err != nil {
		return "", fmt.Errorf("invalid MAC address %q", s)
	}
	return strings.ToLower(hw.String()), nil
}

// OUI is the vendor half of a MAC: the first three octets, lower-cased and
// colon-separated. It returns false for an address NormalizeMAC would reject,
// and for a locally-administered address, where the vendor bits carry no
// meaning at all.
//
// That second case is not a detail. Every modern phone randomises its MAC per
// network by default, which sets the locally-administered bit — so a vendor
// lookup on one is not merely unhelpful, it is an invitation to report a
// confident vendor for an address that was invented moments ago.
func OUI(mac string) (string, bool) {
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return "", false
	}
	octets := strings.Split(norm, ":")
	if len(octets) < 3 {
		// A 20-octet InfiniBand address parses but has no OUI worth reading.
		return "", false
	}

	first, err := hexOctet(octets[0])
	if err != nil {
		return "", false
	}
	// Bit 1 of the first octet is the locally-administered flag.
	if first&0x02 != 0 {
		return "", false
	}

	return strings.Join(octets[:3], ":"), true
}

// OUIPrefixes returns the registry prefixes of mac, longest first: the 36-bit
// MA-S block, then the 28-bit MA-M block, then the 24-bit MA-L block. Each is
// upper-case hex with no separators, the form IEEE publishes assignments in, so
// a caller can use them as table keys unchanged.
//
// Three prefixes rather than one, because IEEE sells three sizes of block and a
// lookup that only ever reads three octets does not merely miss the small ones
// — it answers them wrongly. 8c:1f:64 is an MA-L that IEEE assigned to itself
// and then subdivided into 36-bit blocks, so several hundred unrelated
// companies share those three octets, and reading only them names whichever of
// those companies a table happened to list first. Longest match is the only
// correct read, which is why OUI alone was never enough.
//
// The loop belongs to the caller because only the table knows which block sizes
// it holds: a prefix that is absent has to fall through to the next one rather
// than end the search.
//
// ok is false in exactly the cases OUI's is, and for the same reasons: an
// address NormalizeMAC rejects, and a locally-administered address, where the
// vendor bits carry no meaning at all.
func OUIPrefixes(mac string) ([]string, bool) {
	oui, ok := OUI(mac)
	if !ok {
		return nil, false
	}

	// OUI has done the rejecting; what is left is the form change. IEEE writes
	// assignments as bare upper-case hex, and its block sizes are counted in
	// bits rather than octets — 36 bits is four octets and a half, so the
	// prefixes have to be cut from the hex string, not from the octets.
	norm, err := NormalizeMAC(mac)
	if err != nil {
		return nil, false
	}
	hex := strings.ToUpper(strings.ReplaceAll(norm, ":", ""))
	if len(hex) < 9 {
		// Unreachable through net.ParseMAC, which accepts nothing shorter than
		// six octets. Guarded anyway because the alternative to a cheap check
		// is a slice past the end of a string, and this function's input is a
		// value read off the wire.
		return []string{strings.ToUpper(strings.ReplaceAll(oui, ":", ""))}, true
	}
	return []string{hex[:9], hex[:7], hex[:6]}, true
}

func hexOctet(s string) (byte, error) {
	var b byte
	if _, err := fmt.Sscanf(s, "%02x", &b); err != nil {
		return 0, err
	}
	return b, nil
}
