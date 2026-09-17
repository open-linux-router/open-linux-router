package remote

import (
	"slices"
	"strings"
	"testing"
)

func TestLinesDescribeTheTunnel(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "phone", RouteHome)
	lines := Render(c).Lines()

	for _, want := range []string{
		"interface wg0",
		"listen-port 51820",
		"public-key " + testKey.Public,
		"address 10.6.0.1/24",
		"peer " + peer.PublicKey + " allowed-ips 10.6.0.2/32",
	} {
		if !slices.Contains(lines, want) {
			t.Errorf("missing line %q in:\n  %s", want, strings.Join(lines, "\n  "))
		}
	}
}

// The canonical form is printed in plans, diffs and status. A private key in it
// would be a credential in a terminal, in scrollback, and then in a bug report.
func TestLinesNeverCarryThePrivateKey(t *testing.T) {
	c, _ := withPeer(enabledConfig(), "phone", RouteHome)
	for _, line := range Render(c).Lines() {
		if strings.Contains(line, testKey.Private) {
			t.Fatalf("the private key is in a canonical line: %q", line)
		}
	}
}

// A disabled tunnel renders nothing rather than rendering its peers, so the
// plan against an absent interface reads as "off" rather than as every device
// being revoked at once.
func TestDisabledRendersNoLines(t *testing.T) {
	c, _ := withPeer(enabledConfig(), "phone", RouteHome)
	c.WireGuard.Enabled = false

	if lines := Render(c).Lines(); len(lines) != 0 {
		t.Errorf("a disabled tunnel rendered %d lines: %v", len(lines), lines)
	}
}

func TestServerConfIsWhatWgSetconfWants(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "phone", RouteHome)
	conf := Render(c).ServerConf()

	for _, want := range []string{
		"[Interface]",
		"PrivateKey = " + testKey.Private,
		"ListenPort = 51820",
		"[Peer]",
		"# phone",
		"PublicKey = " + peer.PublicKey,
		// The filter, and it must be the peer's address alone: anything wider
		// would let one key claim traffic from any device on the network.
		"AllowedIPs = 10.6.0.2/32",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q in:\n%s", want, conf)
		}
	}

	// `wg setconf` configures keys and peers. An address or a route here would
	// mean two owners of the interface's addressing.
	for _, forbidden := range []string{"Address", "DNS", "Table", "PostUp"} {
		if strings.Contains(conf, forbidden+" ") {
			t.Errorf("the server config names %q, which is netlink's job", forbidden)
		}
	}
}

func TestServerConfCarriesTheEscapeHatch(t *testing.T) {
	c := enabledConfig()
	c.WireGuard.ExtraConf = "[Peer]\nPublicKey = " + mustKey().Public

	conf := Render(c).ServerConf()
	if !strings.Contains(conf, "raw_wireguard_conf") {
		t.Error("the appended section is not labelled, so nobody reading the file knows where it came from")
	}
	if !strings.Contains(conf, c.WireGuard.ExtraConf) {
		t.Error("the escape hatch was not appended verbatim")
	}
}

func TestClientConfigForHome(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "phone", RouteHome)

	conf, err := ClientConfig(c, peer, "PRIVATE", testNetworks)
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"PrivateKey = PRIVATE",
		// A /32 on the device: it holds one address inside the tunnel and has
		// no business claiming a route to the rest of the dial-in network.
		"Address = 10.6.0.2/32",
		"DNS = 10.6.0.1",
		"PublicKey = " + testKey.Public,
		"Endpoint = home.example.net:51820",
		"AllowedIPs = 10.6.0.0/24, 192.168.1.0/24",
		"PersistentKeepalive = 25",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("missing %q in:\n%s", want, conf)
		}
	}
}

func TestClientConfigForEverythingIsIPv4Only(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "laptop", RouteEverything)

	conf, err := ClientConfig(c, peer, "PRIVATE", testNetworks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conf, "AllowedIPs = 0.0.0.0/0") {
		t.Errorf("full tunnel did not route everything:\n%s", conf)
	}
	// ::/0 would send the device's IPv6 traffic into a tunnel that carries no
	// IPv6, and it would hang rather than falling back.
	if strings.Contains(conf, "::/0") {
		t.Errorf("the client config claims IPv6 the tunnel cannot carry:\n%s", conf)
	}
}

// Nothing on the box knows the public address, so a configuration without one
// is a file that cannot do anything. Refused rather than written.
func TestClientConfigNeedsAnEndpoint(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "phone", RouteHome)
	c.Endpoint = ""

	if _, err := ClientConfig(c, peer, "PRIVATE", testNetworks); err == nil {
		t.Fatal("a client config was written with nowhere to dial")
	}
}

// The operator generated the pair on the device, so olr has no private key to
// write. The file is still worth producing — every other line is one they would
// otherwise have to assemble by hand.
func TestClientConfigWithoutAPrivateKeySaysSo(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "work", RouteHome)

	conf, err := ClientConfig(c, peer, "", testNetworks)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conf, "PrivateKey = <") {
		t.Errorf("no placeholder for the key the operator holds:\n%s", conf)
	}
}

// A box with no networks yet: the device reaches the router and nothing behind
// it, which the validator warns about. The rendered file still has to be
// coherent.
func TestHomeWithNoNetworksIsJustTheTunnel(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "phone", RouteHome)

	conf, err := ClientConfig(c, peer, "PRIVATE", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(conf, "AllowedIPs = 10.6.0.0/24\n") {
		t.Errorf("want only the dial-in network:\n%s", conf)
	}
}
