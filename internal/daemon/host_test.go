package daemon

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/dial"
	"github.com/open-linux-router/open-linux-router/internal/link"
)

func hostStore(t *testing.T, document string) *core.Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "olr.json")
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		t.Fatal(err)
	}
	return core.NewStore(path, link.ModuleName, dial.ModuleName)
}

// The join internal/host cannot make: IPv4 on every interface olr writes an
// address to, and the box's resolvers only from a static uplink.
func TestHostDesiredJoinsNetworksAndTheUplink(t *testing.T) {
	store := hostStore(t, `{
		"link": {"adopted": ["ens18", "ens19", "ens20"], "groups": [
			{"name": "lan", "members": ["ens19"], "ipv4": {"subnet": "172.16.1.0/24"}},
			{"name": "bare", "members": ["ens20"]}
		]},
		"dial": {"uplink": {"interface": "ens18",
			"ipv4": {"address": "192.168.1.2/24", "gateway": "192.168.1.1"},
			"dns": ["192.168.1.1"]}}
	}`)

	d, err := hostDesired(store)
	if err != nil {
		t.Fatal(err)
	}
	if got := slices.Sorted(slices.Values(d.IPv4)); !slices.Equal(got, []string{"ens18", "ens19"}) {
		t.Errorf("IPv4 = %v; a network with no subnet has no IPv4 olr writes", got)
	}
	if !slices.Equal(d.Resolvers, []netip.Addr{netip.MustParseAddr("192.168.1.1")}) {
		t.Errorf("Resolvers = %v", d.Resolvers)
	}
}

// An uplink olr does not address takes nothing: whatever the distribution runs
// on it is still the only thing providing an address and resolvers.
func TestAnUplinkWithoutAStaticAddressTakesNothing(t *testing.T) {
	store := hostStore(t, `{"dial": {"uplink": {"interface": "ens18", "dns": ["192.168.1.1"]}}}`)
	d, err := hostDesired(store)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.IPv4) != 0 || len(d.Resolvers) != 0 {
		t.Errorf("took %+v from an uplink olr does not address", d)
	}
}
