package firewall

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// The two vocabulary types a forward is made of, and the one place their wire
// format is decided.
//
// Both marshal as strings, which is why schema.go exists: core can tell that a
// text-marshalling type is a JSON string, but only this package knows *which*
// strings are legal, and design.md §3.2 rule 3 makes that gap a real defect —
// an undeclared type publishes as a bare `string` on four surfaces at once.

// Protocol is which transport a forward carries.
type Protocol string

const (
	// ProtocolTCP is the default and the overwhelming majority of forwards.
	ProtocolTCP Protocol = "tcp"

	// ProtocolUDP carries games, voice, WireGuard and anything else that never
	// opens a stream.
	ProtocolUDP Protocol = "udp"

	// ProtocolBoth carries each, as two kernel rules sharing one counter. It is
	// a real value rather than a convenience for typing two forwards, because a
	// service that needs both — a game server, a DNS resolver — is one thing to
	// the operator and should be one row on the screen.
	ProtocolBoth Protocol = "both"
)

// Protocols lists the vocabulary, for flag help and the schema enum.
func Protocols() []Protocol { return []Protocol{ProtocolTCP, ProtocolUDP, ProtocolBoth} }

// Valid reports whether p is known. Empty is valid and means tcp.
func (p Protocol) Valid() bool { return p == "" || slices.Contains(Protocols(), p) }

// Each expands a protocol into the transports it actually programs.
//
// ProtocolBoth becomes two entries, which is what makes the renderer's rule
// loop one line instead of a special case — and what makes `both` cost two
// legible rules in `nft list table inet olr_nat` rather than one clever one.
func (p Protocol) Each() []Protocol {
	switch p {
	case ProtocolBoth:
		return []Protocol{ProtocolTCP, ProtocolUDP}
	case "":
		return []Protocol{ProtocolTCP}
	default:
		return []Protocol{p}
	}
}

// PortRange is one port, or a contiguous run of them.
//
// A struct with a text encoding rather than two integer fields, because the
// operator writes it as one thing — `8080`, or `30000-30010` — and splitting it
// into `port_from` and `port_to` would put a range nobody asked for in front of
// everybody who wants a single port. The cost is this file; the benefit is that
// every surface shows the same spelling.
type PortRange struct {
	// From and To are inclusive. A single port has From == To.
	From, To uint16
}

// SinglePort is the common case, spelled once.
func SinglePort(p uint16) PortRange { return PortRange{From: p, To: p} }

// Valid reports whether this is a usable range.
//
// Port 0 is excluded in both positions. It is not a port anything listens on —
// it is the wildcard the kernel hands out when asked for "any" — so a rule
// naming it would match nothing and read as though it should.
func (r PortRange) Valid() bool { return r.From != 0 && r.To != 0 && r.From <= r.To }

// Single reports whether this is one port.
func (r PortRange) Single() bool { return r.From == r.To }

// Count is how many ports the range covers.
func (r PortRange) Count() int { return int(r.To) - int(r.From) + 1 }

// Overlaps reports whether two ranges share any port.
//
// This is the test behind docs/firewall.md §5.1's refusal: two forwards on the
// same interface and protocol whose ports overlap are genuinely ambiguous, and
// resolving it by rule order would be a precedence model the operator cannot
// see on the screen.
func (r PortRange) Overlaps(other PortRange) bool {
	return r.From <= other.To && other.From <= r.To
}

// String is the canonical spelling, and the one every surface uses.
func (r PortRange) String() string {
	if r.From == r.To {
		return strconv.Itoa(int(r.From))
	}
	return fmt.Sprintf("%d-%d", r.From, r.To)
}

// MarshalText makes PortRange a JSON string.
func (r PortRange) MarshalText() ([]byte, error) { return []byte(r.String()), nil }

// UnmarshalText parses `8080` or `30000-30010`.
//
// The pair has to exist together: without this a stored range could be written
// and never read back, and `olr firewall` is a client of its own module's API.
func (r *PortRange) UnmarshalText(text []byte) error {
	s := strings.TrimSpace(string(text))
	if s == "" {
		return fmt.Errorf("a port is required, for example 8080 or 30000-30010")
	}

	from, to, isRange := strings.Cut(s, "-")
	lo, err := parsePort(from)
	if err != nil {
		return err
	}
	if !isRange {
		*r = PortRange{From: lo, To: lo}
		return nil
	}

	hi, err := parsePort(to)
	if err != nil {
		return err
	}
	if lo > hi {
		return fmt.Errorf("port range %q starts above where it ends", s)
	}
	*r = PortRange{From: lo, To: hi}
	return nil
}

func parsePort(s string) (uint16, error) {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 16)
	if err != nil {
		return 0, fmt.Errorf("%q is not a port number between 1 and 65535", strings.TrimSpace(s))
	}
	if v == 0 {
		return 0, fmt.Errorf("0 is not a port; ports run from 1 to 65535")
	}
	return uint16(v), nil
}
