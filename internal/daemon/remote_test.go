package daemon

import (
	"net/netip"
	"path/filepath"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/remote"
)

// The address the resolver is told to answer on is the one the client
// configuration names — read through the same derivation, not recomputed.
func TestDNSDialInReadsTheTunnelAddress(t *testing.T) {
	store := remote.Store{Store: core.NewStore(filepath.Join(t.TempDir(), "olr.json"), remote.ModuleName)}
	view := dnsDialIn{store: store}

	// Nothing turned on: no tunnel, so no address to answer on.
	if info, err := view.DialIn(); err != nil {
		t.Fatal(err)
	} else if info.Enabled || info.Addr.IsValid() {
		t.Errorf("DialIn() = %+v with the tunnel off, want nothing", info)
	}

	wg := remote.WireGuard{Enabled: true, Subnet: netip.MustParsePrefix("10.6.0.0/24")}
	if err := store.Save(remote.Config{WireGuard: wg}); err != nil {
		t.Fatal(err)
	}

	info, err := view.DialIn()
	if err != nil {
		t.Fatal(err)
	}
	if !info.Enabled {
		t.Fatal("a tunnel that is on reported itself off")
	}
	// RouterAddr, not a copy of the rule: .1 on the ordinary prefix, and the
	// exact value wg_render writes into `DNS =`.
	if want := netip.MustParseAddr("10.6.0.1"); info.Addr != want {
		t.Errorf("DialIn() addr = %v, want %v", info.Addr, want)
	}
}
