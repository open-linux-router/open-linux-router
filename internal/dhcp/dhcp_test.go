package dhcp

import (
	"net/netip"
	"strings"
	"testing"
)

// testGroups is the fake set of networks every test in this package plans
// against.
//
// It is the reason the whole validation and rendering surface is testable
// without root, without netlink and without a second NIC: GroupView is declared
// by this module (link.go), so the facts it depends on can simply be stated.
//
// It got smaller when pools moved off interfaces. It used to carry observed
// prefixes and an `adopted` flag, because the rules read both; a network states
// its subnet, so a fixture is now the three fields a rule actually consults.
// There is no unadopted entry any more either — being in a network means having
// been adopted, and `link` is where that is enforced.
func testGroups() StaticGroups {
	return StaticGroups{
		"lan": {
			Members: []string{"br-lan"}, Up: true,
			Subnet: netip.MustParsePrefix("192.168.1.0/24"),
			Router: netip.MustParseAddr("192.168.1.1"),
		},
		"guest": {
			Members: []string{"br-guest"}, Up: true,
			Subnet: netip.MustParsePrefix("10.10.0.0/24"),
			Router: netip.MustParseAddr("10.10.0.1"),
		},
		"down": {
			Members: []string{"br-down"}, Up: false,
			Subnet: netip.MustParsePrefix("172.16.0.0/24"),
			Router: netip.MustParseAddr("172.16.0.1"),
		},
		// A network that serves no IPv4 at all — RA only. Not expressible
		// before the v4/v6 split, and now the thing a pool with no ipv4 block
		// is served on.
		"v6only": {
			Members: []string{"br-v6"}, Up: true,
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

// lanPool is a valid pool on the lan network, with an explicit range.
func lanPool(t *testing.T) Pool {
	t.Helper()
	return Pool{
		Group: "lan",
		IPv4: &PoolIPv4{
			Start: addr(t, "192.168.1.100"),
			End:   addr(t, "192.168.1.200"),
		},
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

// mustGroup resolves a fixture network, failing the test rather than the
// assertion that uses it.
func mustGroup(t *testing.T, name string) GroupInfo {
	t.Helper()
	info, err := testGroups().Group(name)
	if err != nil {
		t.Fatalf("no fixture network %q: %v", name, err)
	}
	return info
}
