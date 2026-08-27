package dhcp

import (
	"net/netip"
	"strings"
	"testing"
)

// testLinks is the fake network every test in this package plans against.
//
// It is the reason the whole validation and rendering surface is testable
// without root, without netlink and without a second NIC: LinkView is declared
// by this module (link.go), so the facts it depends on can simply be stated.
func testLinks() StaticLinks {
	return StaticLinks{
		"br-lan": {
			Adopted: true, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("192.168.1.1/24")},
		},
		"br-guest": {
			Adopted: true, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("10.10.0.1/24")},
		},
		"br-down": {
			Adopted: true, Up: false,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("172.16.0.1/24")},
		},
		"eth-foreign": {
			Adopted: false, Up: true,
			Prefixes: []netip.Prefix{netip.MustParsePrefix("10.99.0.1/24")},
		},
	}
}

func addr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("bad test address %q: %v", s, err)
	}
	return a
}

// lanPool is a valid pool on br-lan.
func lanPool(t *testing.T) Pool {
	t.Helper()
	return Pool{
		Interface: "br-lan",
		Start:     addr(t, "192.168.1.100"),
		End:       addr(t, "192.168.1.200"),
	}
}

// validConfig is the baseline every validation test mutates one thing away
// from, so a failure names exactly one rule.
func validConfig(t *testing.T) Config {
	t.Helper()
	return Config{Enabled: true, Pools: []Pool{lanPool(t)}}
}

// hasProblem reports whether any problem's path and message match.
func hasProblem(problems []Problem, path, substring string) bool {
	for _, p := range problems {
		if p.Path == path && strings.Contains(p.Message, substring) {
			return true
		}
	}
	return false
}

func problemStrings(problems []Problem) string {
	if len(problems) == 0 {
		return "(none)"
	}
	out := make([]string, len(problems))
	for i, p := range problems {
		out[i] = p.String()
	}
	return strings.Join(out, "\n    ")
}
