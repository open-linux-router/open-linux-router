package remote

import (
	"context"
	"net/http"
	"net/netip"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

func flow(src string, port uint16, proto string, bytes uint64) Flow {
	return Flow{Src: netip.MustParseAddr(src), DstPort: port, Proto: proto, Established: true, Bytes: bytes}
}

// One address, many connections: a client is an address, and the count says
// how busy it is rather than how many of them there are.
func TestProxyClientsAreAddressesBusiestFirst(t *testing.T) {
	flows := []Flow{
		flow("203.0.113.7", 8388, "tcp", 4000),
		flow("203.0.113.7", 8388, "tcp", 9000),
		flow("198.51.100.20", 8388, "tcp", 2000),
		flow("198.51.100.20", 8388, "udp", 500),
		flow("192.0.2.1", 22, "tcp", 90000),                                               // another port
		{Src: netip.MustParseAddr("192.0.2.9"), DstPort: 8388, Proto: "tcp", Bytes: 5000}, // still opening
	}
	got := proxyClients(flows, 8388, true, true)
	want := []clientView{
		{Address: "203.0.113.7", Connections: 2, Bytes: 13000},
		{Address: "198.51.100.20", Connections: 2, Bytes: 2500},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v\nwant %+v", got, want)
	}

	// SOCKS5 relays UDP on ports of its own; a UDP flow to its TCP port is not a client.
	if got := proxyClients(flows, 8388, false, true); got[1].Connections != 1 {
		t.Errorf("a UDP flow was counted for a TCP-only proxy: %+v", got)
	}
}

// With bytes counted, an address that has sent next to nothing is a knock on
// the door, not a client. Without them there is no telling, so nobody is left
// out.
func TestScannersAreLeftOutOnlyWhenBytesAreCounted(t *testing.T) {
	flows := []Flow{flow("203.0.113.7", 8388, "tcp", 50000), flow("192.0.2.66", 8388, "tcp", 300)}
	if got := proxyClients(flows, 8388, true, true); len(got) != 1 || got[0].Address != "203.0.113.7" {
		t.Errorf("counted: %+v", got)
	}
	flows = []Flow{flow("203.0.113.7", 8388, "tcp", 0), flow("192.0.2.66", 8388, "tcp", 0)}
	if got := proxyClients(flows, 8388, true, false); len(got) != 2 {
		t.Errorf("uncounted: %+v", got)
	}
}

type fakeConns struct {
	flows   []Flow
	counted bool
}

func (f fakeConns) Flows(context.Context) ([]Flow, bool, error) { return f.flows, f.counted, nil }

type fakePlaces map[string]geoip.Place

func (f fakePlaces) Locate(addrs []netip.Addr) (map[string]geoip.Place, geoip.Status) {
	out := map[string]geoip.Place{}
	for _, a := range addrs {
		if p, ok := f[a.String()]; ok {
			out[a.String()] = p
		}
	}
	return out, geoip.Status{State: geoip.Ready, Source: geoip.Source}
}

func TestClientsAnswersEachProxyThatIsOnWithPlaces(t *testing.T) {
	tunnel, _ := testApplier(t)
	proxy := ProxyApplier{
		Store:  tunnel.Store,
		Paths:  Paths{Conf: filepath.Join(t.TempDir(), "rendered", "shadowsocks.json")},
		Locate: func() (string, error) { return "ssserver", nil },
		Settle: -1,
	}
	h := HTTP{
		Tunnel: tunnel, Proxy: proxy, Lock: core.NewLock(), Events: core.NewEvents(),
		Conns:  fakeConns{flows: []Flow{flow("203.0.113.7", DefaultShadowsocksPort, "tcp", 0)}},
		Places: fakePlaces{"203.0.113.7": {Country: "CN", Org: "Chinanet"}},
	}.Handler()

	// Nothing switched on: no proxies, and nothing to place.
	got := decode[clientsResponse](t, do(t, h, http.MethodGet, "/clients", ""))
	if len(got.Proxies) != 0 || len(got.Places) != 0 {
		t.Fatalf("with nothing on: %+v", got)
	}

	if w := do(t, h, http.MethodPatch, "/config?confirm=true", `{"endpoint":"home.example.net"}`); w.Code != http.StatusOK {
		t.Fatalf("endpoint = %d: %s", w.Code, w.Body)
	}
	if w := do(t, h, http.MethodPatch, "/shadowsocks/config?confirm=true", `{"enabled":true}`); w.Code != http.StatusOK {
		t.Fatalf("enable = %d: %s", w.Code, w.Body)
	}
	got = decode[clientsResponse](t, do(t, h, http.MethodGet, "/clients", ""))
	if len(got.Proxies) != 1 || got.Proxies[0].Proxy != "shadowsocks" || len(got.Proxies[0].Clients) != 1 {
		t.Fatalf("proxies = %+v", got.Proxies)
	}
	if got.Places["203.0.113.7"].Org != "Chinanet" || got.Locations.Source != geoip.Source {
		t.Errorf("places = %+v, locations = %+v", got.Places, got.Locations)
	}
}
