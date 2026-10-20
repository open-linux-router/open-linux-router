package remote

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

func TestNextAddressSkipsTheRouterAndWhatIsTaken(t *testing.T) {
	c := enabledConfig()

	first, ok := c.WireGuard.NextAddress()
	if !ok {
		t.Fatal("no address available in an empty network")
	}
	// .1 is this box, so the first device is .2. Asserted because the client
	// configuration carries this number to a phone, where getting it wrong is
	// invisible until nothing works.
	if want := netip.MustParseAddr("10.6.0.2"); first != want {
		t.Fatalf("first address = %s, want %s", first, want)
	}

	c, _ = withPeer(c, "phone", "")
	second, _ := c.WireGuard.NextAddress()
	if want := netip.MustParseAddr("10.6.0.3"); second != want {
		t.Fatalf("second address = %s, want %s", second, want)
	}
}

// The property that keeps a small network usable: removing a device returns its
// address rather than leaving a hole the allocator walks past forever.
func TestNextAddressReusesAHole(t *testing.T) {
	c := enabledConfig()
	c, _ = withPeer(c, "phone", "")
	c, _ = withPeer(c, "laptop", "")
	c.WireGuard.RemovePeer("phone")

	got, _ := c.WireGuard.NextAddress()
	if want := netip.MustParseAddr("10.6.0.2"); got != want {
		t.Fatalf("address = %s, want the freed %s", got, want)
	}
}

func TestNextAddressRunsOut(t *testing.T) {
	c := Config{WireGuard: WireGuard{
		// /30: two host addresses, one of which is this box.
		Subnet: netip.MustParsePrefix("10.9.9.0/30"),
	}}
	if _, ok := c.WireGuard.NextAddress(); !ok {
		t.Fatal("a /30 has room for one device")
	}
	c, _ = withPeer(c, "phone", "")
	if addr, ok := c.WireGuard.NextAddress(); ok {
		t.Fatalf("a /30 with one device has no room left, got %s", addr)
	}
}

func TestNormalizeIsCanonical(t *testing.T) {
	addr := netip.MustParseAddr("10.6.0.1")
	c := Config{WireGuard: WireGuard{
		// Typed as a host address inside the network, which is what a form
		// produces when somebody pastes the router's address into the subnet
		// field.
		Subnet:  netip.MustParsePrefix("10.6.0.5/24"),
		Address: &addr,
		Peers: []Peer{
			{Name: "Laptop", PublicKey: " " + testKey.Public + " "},
			{Name: "phone"},
			{Name: "phone"},
		},
	}}
	c.Normalize()

	if got := c.WireGuard.Subnet.String(); got != "10.6.0.0/24" {
		t.Errorf("subnet = %s, want it masked", got)
	}
	// An address equal to the derived one is dropped, so `.1` is written once —
	// in the subnet — and a form that helpfully filled the field in has not
	// turned a default into a pin.
	if c.WireGuard.Address != nil {
		t.Errorf("address = %v, want it dropped as redundant", c.WireGuard.Address)
	}
	if len(c.WireGuard.Peers) != 2 {
		t.Fatalf("peers = %d, want the duplicate dropped", len(c.WireGuard.Peers))
	}
	if c.WireGuard.Peers[0].Name != "laptop" {
		t.Errorf("peers[0] = %q, want lower-cased and sorted", c.WireGuard.Peers[0].Name)
	}
	if c.WireGuard.Peers[0].PublicKey != testKey.Public {
		t.Errorf("public key = %q, want it trimmed", c.WireGuard.Peers[0].PublicKey)
	}
}

// Editing a device must not renumber it: the address is in a file on a phone,
// and nothing on the phone would notice it changing.
func TestSetPeerKeepsTheAddressAndKeyWhenTheEditOmitsThem(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "phone", RouteHome)

	c.WireGuard.SetPeer(Peer{Name: "phone", Routes: RouteEverything})

	got, ok := c.WireGuard.Peer("phone")
	if !ok {
		t.Fatal("the peer disappeared")
	}
	if got.Address != peer.Address {
		t.Errorf("address = %s, want the original %s", got.Address, peer.Address)
	}
	if got.PublicKey != peer.PublicKey {
		t.Errorf("public key changed on an edit that did not name one")
	}
	if got.Routes != RouteEverything {
		t.Errorf("routes = %q, want the edit to have landed", got.Routes)
	}
}

// Renaming is the one edit to a device that changes nothing on the box, and the
// test says so in the terms that matter: the kernel's view of the tunnel —
// `Render`'s lines — is identical before and after.
func TestRenamePeerKeepsEverythingButTheName(t *testing.T) {
	c, peer := withPeer(enabledConfig(), "phone", RouteEverything)
	c, _ = withPeer(c, "laptop", RouteHome)
	before := slices.Clone(Render(c).Lines())

	// Cased, because a name typed into a form is lower-cased like every other
	// spelling of it (normalizeName).
	renamed, err := c.WireGuard.RenamePeer("Phone", "handset")
	if err != nil {
		t.Fatalf("rename: %v", err)
	}
	if renamed.Name != "handset" {
		t.Errorf("name = %q, want the new one", renamed.Name)
	}
	if renamed.Address != peer.Address {
		t.Errorf("address = %s, want the original %s — it is in the file on the device",
			renamed.Address, peer.Address)
	}
	if renamed.PublicKey != peer.PublicKey {
		t.Error("the key changed, so every configuration naming the old one stopped working")
	}

	if _, ok := c.WireGuard.Peer("phone"); ok {
		t.Error("the old name still resolves to a device")
	}
	got, ok := c.WireGuard.Peer("handset")
	if !ok {
		t.Fatal("the new name holds nothing")
	}
	if got.Routes != RouteEverything {
		t.Errorf("routes = %q, want what the device sends to have survived", got.Routes)
	}

	if after := Render(c).Lines(); !slices.Equal(before, after) {
		t.Errorf("a rename changed kernel state:\nbefore %v\nafter  %v", before, after)
	}
}

// The three ways a rename is refused. Each leaves the document exactly as it
// was, which is the half that matters: `Normalize` drops a peer whose name
// collides or is empty, so a careless rename would not fail — it would delete a
// device.
func TestRenameRefusesWhatItCannotDo(t *testing.T) {
	c, _ := withPeer(enabledConfig(), "phone", RouteHome)
	c, _ = withPeer(c, "laptop", RouteHome)

	if _, err := c.WireGuard.RenamePeer("phone", "laptop"); !errors.Is(err, ErrPeerNameTaken) {
		t.Errorf("err = %v, want ErrPeerNameTaken", err)
	}
	if _, err := c.WireGuard.RenamePeer("nobody", "handset"); !errors.Is(err, ErrNoSuchPeer) {
		t.Errorf("err = %v, want ErrNoSuchPeer", err)
	}
	if _, err := c.WireGuard.RenamePeer("phone", "   "); !errors.Is(err, ErrPeerNameNeeded) {
		t.Errorf("err = %v, want ErrPeerNameNeeded", err)
	}
	// The name it already has is not a refusal: a form posted back unchanged
	// must succeed.
	if _, err := c.WireGuard.RenamePeer("phone", "phone"); err != nil {
		t.Errorf("renaming to the current name: %v", err)
	}

	for _, name := range []string{"phone", "laptop"} {
		if _, ok := c.WireGuard.Peer(name); !ok {
			t.Fatalf("%q was lost by a refused rename", name)
		}
	}
	if n := len(c.WireGuard.Peers); n != 2 {
		t.Errorf("peers = %d, want the two that were there", n)
	}
}

// The endpoint is the module's, not the tunnel's, so the port a client dials
// comes from whichever way in it is dialling — and `public_port` is what says
// so when a router in front forwards a different one.
func TestDialAddress(t *testing.T) {
	for _, tc := range []struct {
		name     string
		endpoint string
		listen   uint16
		public   uint16
		want     string
	}{
		{"a name gets the listen port", "home.example.net", 0, 0, "home.example.net:51820"},
		{"public_port wins", "home.example.net", 0, 51821, "home.example.net:51821"},
		{"the configured port is used", "home.example.net", 3000, 0, "home.example.net:3000"},
		{"IPv4", "203.0.113.5", 0, 0, "203.0.113.5:51820"},
		// The one worth a case: a bare v6 address has colons of its own, so
		// appending `:port` would produce something no client can parse.
		{"IPv6 is bracketed", "2001:db8::1", 0, 0, "[2001:db8::1]:51820"},
		{"nothing set", "", 0, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{
				Endpoint:  tc.endpoint,
				WireGuard: WireGuard{ListenPort: tc.listen, PublicPort: tc.public},
			}
			if got := c.DialAddress(c.WireGuard.DialPort()); got != tc.want {
				t.Errorf("DialAddress() = %q, want %q", got, tc.want)
			}
		})
	}
}

// A port in the shared endpoint could only ever be right for one of the two
// ways in, so it is refused rather than guessed at.
func TestEndpointMayNotCarryAPort(t *testing.T) {
	c := enabledConfig()
	c.Endpoint = "home.example.net:51820"

	res := validateAll(c, testNetworks)
	if res.OK() {
		t.Fatal("an endpoint with a port was accepted")
	}
	if !strings.Contains(res.Errors[0].Message, "public_port") {
		t.Errorf("the refusal does not name the field that replaces it: %q", res.Errors[0].Message)
	}
}

func TestRedactedHidesTheKeyAndLeavesTheOriginal(t *testing.T) {
	c := enabledConfig()

	red := c.Redacted()
	if red.WireGuard.PrivateKey != RedactedSecret {
		t.Errorf("private key = %q, want it masked", red.WireGuard.PrivateKey)
	}
	if c.WireGuard.PrivateKey != testKey.Private {
		t.Error("Redacted mutated the config it was given")
	}
}

func TestConfigRoundTrips(t *testing.T) {
	c, _ := withPeer(enabledConfig(), "phone", RouteEverything)

	data, err := MarshalConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	back, err := UnmarshalConfig(data)
	if err != nil {
		t.Fatal(err)
	}

	if back.WireGuard.PrivateKey != c.WireGuard.PrivateKey {
		t.Error("the key did not survive the round trip")
	}
	if len(back.WireGuard.Peers) != 1 || back.WireGuard.Peers[0].Routes != RouteEverything {
		t.Errorf("peers did not survive: %+v", back.WireGuard.Peers)
	}

	// Stable bytes, so that two identical edits do not diff against each other.
	again, err := MarshalConfig(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(data) {
		t.Errorf("marshalling is not stable:\n  %s\n  %s", data, again)
	}
}

// A typo'd key that is silently ignored produces a box that is quietly not
// doing what its config says — here, a device whose routes were never narrowed.
func TestUnmarshalRejectsUnknownFields(t *testing.T) {
	_, err := UnmarshalConfig([]byte(`{"wireguard":{"enabled":true,"rutes":"home"}}`))
	if err == nil {
		t.Fatal("an unknown field was accepted")
	}
	if !strings.Contains(err.Error(), "rutes") {
		t.Errorf("error %q does not name the offending key", err)
	}
}

func TestEmptyIsOnlyTrueBeforeAnythingIsSet(t *testing.T) {
	if !(Config{}).Empty() {
		t.Error("a zero config is not empty")
	}
	if enabledConfig().Empty() {
		t.Error("a configured tunnel reports itself empty, so olrd would not restore it at boot")
	}
}
