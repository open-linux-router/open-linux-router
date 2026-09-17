package remote

import (
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Shared fixtures.
//
// Everything in this package that decides anything is pure — key generation,
// validation, rendering, planning — so the whole rule set runs on a laptop with
// no kernel, no `wg` and no root (design.md §10, test hardware). The kernel
// seam is the only thing stubbed, and StaticKernel holds the same canonical
// lines a real one reports rather than recording method calls.

// testNetworks is the home network every fixture below assumes.
var testNetworks = StaticNetworks{
	{Name: "lan", Subnet: netip.MustParsePrefix("192.168.1.0/24")},
}

// testKey is one key pair, generated once so that a test can assert on a
// public key without hardcoding bytes that would have to be regenerated if the
// clamping ever changed.
var testKey = mustKey()

func mustKey() KeyPair {
	pair, err := GenerateKey()
	if err != nil {
		panic(err)
	}
	return pair
}

// enabledConfig is the smallest config that passes validation with the tunnel
// on: an endpoint, a key, and nothing else typed.
func enabledConfig() Config {
	return Config{
		// Module level now: the address clients dial is a fact about the box,
		// and both ways in need exactly this value.
		Endpoint: "home.example.net",
		WireGuard: WireGuard{
			Enabled:    true,
			PrivateKey: testKey.Private,
		},
	}
}

// validateAll runs both objects' rules plus the shared endpoint's, which is
// what the HTTP surface does before it plans anything.
func validateAll(c Config, networks NetworkView) Result {
	r := ValidateWireGuard(c, networks)
	r.merge(ValidateShadowsocks(c))
	validateEndpoint(&r, c)
	return r
}

// withPeer returns the config plus one device at the next free address.
func withPeer(c Config, name string, routes RouteScope) (Config, Peer) {
	addr, ok := c.WireGuard.NextAddress()
	if !ok {
		panic("no free address in " + c.WireGuard.SubnetOrDefault().String())
	}
	peer := Peer{Name: name, Address: addr, Routes: routes, PublicKey: mustKey().Public}
	c.WireGuard.SetPeer(peer)
	return c, peer
}

func testApplier(t *testing.T) (TunnelApplier, *StaticKernel) {
	t.Helper()
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	kernel := &StaticKernel{}
	return TunnelApplier{
		Store:     Store{Store: store},
		Kernel:    kernel,
		Networks:  testNetworks,
		PortInUse: func(uint64) (bool, error) { return false, nil },
	}, kernel
}
