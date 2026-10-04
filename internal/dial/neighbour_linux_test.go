//go:build linux

package dial

import (
	"net"
	"net/netip"
	"testing"

	"github.com/vishvananda/netlink"
)

func TestIPv6GatewayMACRequiresResolvedNeighbourOnRouteInterface(t *testing.T) {
	gw := netip.MustParseAddr("fe80::1")
	mac, _ := net.ParseMAC("aa:bb:cc:dd:ee:ff")
	entry := netlink.Neigh{LinkIndex: 7, IP: net.ParseIP("fe80::1"), HardwareAddr: mac, State: netlink.NUD_STALE}
	if got := neighbourMAC([]netlink.Neigh{entry}, gw, 7); got != mac.String() {
		t.Fatalf("resolved neighbour = %q, want %q", got, mac)
	}
	for _, tc := range []struct {
		name string
		n    netlink.Neigh
	}{
		{"other interface", netlink.Neigh{LinkIndex: 8, IP: entry.IP, HardwareAddr: mac, State: netlink.NUD_REACHABLE}},
		{"other address", netlink.Neigh{LinkIndex: 7, IP: net.ParseIP("fe80::2"), HardwareAddr: mac, State: netlink.NUD_REACHABLE}},
		{"incomplete", netlink.Neigh{LinkIndex: 7, IP: entry.IP, State: netlink.NUD_INCOMPLETE}},
		{"failed with stale hardware", netlink.Neigh{LinkIndex: 7, IP: entry.IP, HardwareAddr: mac, State: netlink.NUD_FAILED}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := neighbourMAC([]netlink.Neigh{tc.n}, gw, 7); got != "" {
				t.Errorf("unverified neighbour = %q", got)
			}
		})
	}
}
