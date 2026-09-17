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
	return Config{WireGuard: WireGuard{
		Enabled:    true,
		Endpoint:   "home.example.net",
		PrivateKey: testKey.Private,
	}}
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

func testApplier(t *testing.T) (Applier, *StaticKernel) {
	t.Helper()
	store := core.NewStore(filepath.Join(t.TempDir(), "olr.json"), ModuleName)
	kernel := &StaticKernel{}
	return Applier{
		Kernel:    kernel,
		Networks:  testNetworks,
		Store:     store,
		PortInUse: func(uint64) (bool, error) { return false, nil },
	}, kernel
}
