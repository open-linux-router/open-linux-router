package remote

import (
	"net/netip"
	"strings"
	"testing"
)

// The validator is the largest thing in this module and none of it needs a
// kernel, which is what design.md §5.3.1 is for. It carries more weight here
// than in most modules for a reason worth restating: **a client configuration
// leaves the box.** Every other module's mistakes are fixed by re-applying;
// this one's are fixed by finding whoever has the file.

func errorPaths(r Result) []string {
	out := make([]string, 0, len(r.Errors))
	for _, p := range r.Errors {
		out = append(out, p.Path)
	}
	return out
}

func hasPath(r Result, path string) bool {
	for _, p := range r.Errors {
		if p.Path == path {
			return true
		}
	}
	return false
}

func TestValidConfigPasses(t *testing.T) {
	c, _ := withPeer(enabledConfig(), "phone", RouteHome)
	if res := Validate(c, testNetworks); !res.OK() {
		t.Fatalf("a valid config was refused: %v", errorPaths(res))
	}
}

func TestEndpointIsRequiredWhenOn(t *testing.T) {
	c := enabledConfig()
	c.WireGuard.Endpoint = ""

	res := Validate(c, testNetworks)
	if !hasPath(res, "wireguard.endpoint") {
		t.Fatalf("no endpoint error: %v", errorPaths(res))
	}
	// The refusal has to point somewhere. `dial` is where a name that tracks
	// this box already lives, and an operator who has one should not have to
	// know that is what it is for.
	if !strings.Contains(res.Errors[0].Message, "olr dial") {
		t.Errorf("the refusal names no way forward: %q", res.Errors[0].Message)
	}

	// Off, it is not required: a configuration being assembled is not a
	// configuration that is wrong.
	c.WireGuard.Enabled = false
	if res := Validate(c, testNetworks); !res.OK() {
		t.Errorf("a switched-off tunnel was refused for a missing endpoint: %v", errorPaths(res))
	}
}

// The overlap is the check that matters most, because what it prevents has no
// error message of its own: the tunnel connects perfectly and reaches nothing.
func TestSubnetMayNotOverlapAHomeNetwork(t *testing.T) {
	c := enabledConfig()
	c.WireGuard.Subnet = netip.MustParsePrefix("192.168.1.0/25")

	res := Validate(c, testNetworks)
	if !hasPath(res, "wireguard.subnet") {
		t.Fatalf("an overlapping dial-in network was accepted: %v", errorPaths(res))
	}
	if !strings.Contains(res.Errors[0].Message, "lan") {
		t.Errorf("the error does not name the network it collides with: %q", res.Errors[0].Message)
	}
}

func TestSubnetRules(t *testing.T) {
	for _, tc := range []struct {
		name   string
		subnet string
		want   bool // valid?
	}{
		{"an ordinary /24", "10.6.0.0/24", true},
		{"a /30 has room for one device", "10.6.0.0/30", true},
		{"a /31 has room for nobody", "10.6.0.0/31", false},
		{"loopback", "127.0.0.0/24", false},
		{"IPv6", "fd00::/64", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := enabledConfig()
			c.WireGuard.Subnet = netip.MustParsePrefix(tc.subnet)
			res := Validate(c, nil)
			if ok := !hasPath(res, "wireguard.subnet"); ok != tc.want {
				t.Errorf("subnet %s accepted = %v, want %v (%v)", tc.subnet, ok, tc.want, errorPaths(res))
			}
		})
	}
}

func TestPeerRules(t *testing.T) {
	base := enabledConfig()
	addr := netip.MustParseAddr("10.6.0.2")

	for _, tc := range []struct {
		name string
		peer Peer
		path string
	}{
		{"no name", Peer{PublicKey: testKey.Public, Address: addr}, "wireguard.peers[0].name"},
		{"a name that cannot be a DNS label", Peer{Name: "my phone", PublicKey: testKey.Public, Address: addr},
			"wireguard.peers[0].name"},
		{"no key", Peer{Name: "phone", Address: addr}, "wireguard.peers[0].public_key"},
		{"a truncated key", Peer{Name: "phone", PublicKey: "abc", Address: addr}, "wireguard.peers[0].public_key"},
		{"no address", Peer{Name: "phone", PublicKey: testKey.Public}, "wireguard.peers[0].address"},
		{"an address outside the network", Peer{Name: "phone", PublicKey: testKey.Public,
			Address: netip.MustParseAddr("192.168.1.9")}, "wireguard.peers[0].address"},
		{"this box's own address", Peer{Name: "phone", PublicKey: testKey.Public,
			Address: netip.MustParseAddr("10.6.0.1")}, "wireguard.peers[0].address"},
		{"an unknown route scope", Peer{Name: "phone", PublicKey: testKey.Public, Address: addr,
			Routes: "some"}, "wireguard.peers[0].routes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base.Clone()
			c.WireGuard.Peers = []Peer{tc.peer}
			res := Validate(c, testNetworks)
			if !hasPath(res, tc.path) {
				t.Errorf("want an error at %s, got %v", tc.path, errorPaths(res))
			}
		})
	}
}

// One key is one device. Two names for one key is the detectable half of
// docs/remote-access.md §2.1: only one of them can be connected at a time, and
// which one is decided by whoever handshaked last.
func TestTwoPeersMayNotShareAKey(t *testing.T) {
	c := enabledConfig()
	shared := mustKey().Public
	c.WireGuard.Peers = []Peer{
		{Name: "laptop", PublicKey: shared, Address: netip.MustParseAddr("10.6.0.2")},
		{Name: "phone", PublicKey: shared, Address: netip.MustParseAddr("10.6.0.3")},
	}

	res := Validate(c, testNetworks)
	if !hasPath(res, "wireguard.peers[1].public_key") {
		t.Fatalf("a shared key was accepted: %v", errorPaths(res))
	}
	if !strings.Contains(res.Errors[0].Message, "laptop") {
		t.Errorf("the error does not name the other holder: %q", res.Errors[0].Message)
	}
}

func TestTwoPeersMayNotShareAnAddress(t *testing.T) {
	c := enabledConfig()
	addr := netip.MustParseAddr("10.6.0.2")
	c.WireGuard.Peers = []Peer{
		{Name: "laptop", PublicKey: mustKey().Public, Address: addr},
		{Name: "phone", PublicKey: mustKey().Public, Address: addr},
	}

	if res := Validate(c, testNetworks); !hasPath(res, "wireguard.peers[1].address") {
		t.Fatalf("a shared address was accepted: %v", errorPaths(res))
	}
}

// Both are warnings rather than refusals: neither is wrong, and both describe
// something outside this module's control (docs/remote-access.md §8).
func TestTheTwoThingsThatOnlyWarn(t *testing.T) {
	t.Run("home with no networks", func(t *testing.T) {
		c, _ := withPeer(enabledConfig(), "phone", RouteHome)
		res := Validate(c, nil)
		if !res.OK() {
			t.Fatalf("refused rather than warned: %v", errorPaths(res))
		}
		if len(res.Warnings) == 0 {
			t.Fatal("no warning about a tunnel that reaches only this box")
		}
	})

	t.Run("everything with no egress translation", func(t *testing.T) {
		c, _ := withPeer(enabledConfig(), "laptop", RouteEverything)
		res := Validate(c, testNetworks)
		if !res.OK() {
			t.Fatalf("refused rather than warned: %v", errorPaths(res))
		}
		var found bool
		for _, w := range res.Warnings {
			if strings.Contains(w.Message, "translation") {
				found = true
			}
		}
		if !found {
			t.Errorf("no warning that a full tunnel reaches no internet yet: %v", res.Warnings)
		}
	})
}

func TestEscapeHatchMayNotRestateWhatWeRender(t *testing.T) {
	c := enabledConfig()
	c.WireGuard.ExtraConf = "ListenPort = 9999"

	if res := Validate(c, testNetworks); !hasPath(res, "wireguard.raw_wireguard_conf") {
		t.Fatalf("the hatch was allowed to contradict the config: %v", errorPaths(res))
	}

	// A hand-written peer block is the main thing the hatch is for, so the keys
	// a `[Peer]` needs must stay allowed.
	c.WireGuard.ExtraConf = "[Peer]\nPublicKey = " + mustKey().Public + "\nAllowedIPs = 10.6.0.200/32"
	if res := Validate(c, testNetworks); !res.OK() {
		t.Errorf("a hand-written peer block was refused: %v", errorPaths(res))
	}
}

func TestInterfaceNameRules(t *testing.T) {
	c := enabledConfig()
	c.WireGuard.Interface = strings.Repeat("w", MaxInterfaceNameLen+1)
	if res := Validate(c, testNetworks); !hasPath(res, "wireguard.interface") {
		t.Errorf("an unusable interface name was accepted: %v", errorPaths(res))
	}

	c.WireGuard.Interface = "lo"
	if res := Validate(c, testNetworks); !hasPath(res, "wireguard.interface") {
		t.Errorf("loopback was accepted as the tunnel's name: %v", errorPaths(res))
	}
}
