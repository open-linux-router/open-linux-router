package remote

import (
	"cmp"
	"context"
	"net/http"
	"net/netip"
	"slices"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

// Who is using the ways in right now, and from where.
//
// The tunnel knows its devices by key, so it can say which device is where.
// The two proxies cannot: everybody shares one password, and nothing in either
// daemon says who a connection belongs to. What the box can say is which
// addresses have connections open to a proxy's port — the kernel's connection
// table has every one — and where each address is. For the case this is for,
// an operator and their own phone and laptop, that is one or two addresses,
// and "China Telecom, China" beside one of them is the operator recognising
// themselves.
//
// **An address is a place, not a device.** Two devices behind one hotel NAT
// share one; a phone moving from Wi-Fi to its carrier becomes a second. So
// this reports addresses and never claims a device count.
//
// **Scanners knock on every open port.** A connection that completed its
// handshake and was then dropped for not knowing the password looks, to the
// connection table, like any other. When the kernel counts bytes per
// connection (nf_conntrack_acct), an address that has sent less than
// minBytes across all of its connections is left out — a real client moves
// more than that in its first request, a probe rarely does. When it does not
// count, nothing is left out, and `counted: false` says so.

// Flow is one tracked connection to this box, as the connection table has it.
type Flow struct {
	Src     netip.Addr
	DstPort uint16
	Proto   string
	// Established is false for a TCP connection still opening or closing. UDP
	// has no such state and is always true.
	Established bool
	// Bytes is what the client has sent, zero when the kernel does not count.
	Bytes uint64
}

// ConnTable reads the connection table for flows addressed to this box.
type ConnTable interface {
	// Flows returns every flow whose destination is one of this box's own
	// addresses, and whether the kernel is counting bytes.
	Flows(ctx context.Context) (flows []Flow, counted bool, err error)
}

// Locator places addresses. *geoip.Locator is the real one.
type Locator interface {
	Locate(addrs []netip.Addr) (map[string]geoip.Place, geoip.Status)
}

// minBytes is how much an address has to have sent to count as a client when
// bytes are counted.
const minBytes = 1024

type clientView struct {
	Address     string `json:"address"`
	Connections int    `json:"connections"`
	// Bytes is what the address has sent over its open connections. Absent
	// when the kernel does not count.
	Bytes uint64 `json:"bytes,omitempty"`
}

type proxyClientsView struct {
	// Proxy is the object: "shadowsocks" or "socks5".
	Proxy   string       `json:"proxy"`
	Port    uint16       `json:"port"`
	Clients []clientView `json:"clients"`
}

type clientsResponse struct {
	// Proxies has one entry per proxy that is switched on, clients or not.
	Proxies []proxyClientsView `json:"proxies"`

	// Places is where every address in this reply is — each proxy client and
	// each tunnel device's last endpoint — keyed by the bare address. An
	// address the databases cannot place is absent.
	Places map[string]geoip.Place `json:"places"`

	// Locations says how complete Places is, and carries the attribution the
	// databases' licence asks every surface showing a place to carry.
	Locations geoip.Status `json:"locations"`

	// Counted is whether the kernel counts bytes per connection. Without it,
	// scanners cannot be told apart from clients and every address is listed.
	Counted bool `json:"counted"`

	// Error is a connection table that could not be read. Places for the
	// tunnel's devices are still answered.
	Error string `json:"error,omitempty"`

	AsOf time.Time `json:"as_of"`
}

// proxyClients folds flows into the addresses connected to one port.
func proxyClients(flows []Flow, port uint16, udp, counted bool) []clientView {
	by := map[netip.Addr]*clientView{}
	for _, f := range flows {
		if f.DstPort != port || !f.Established {
			continue
		}
		if f.Proto != "tcp" && !(udp && f.Proto == "udp") {
			continue
		}
		c := by[f.Src]
		if c == nil {
			c = &clientView{Address: f.Src.String()}
			by[f.Src] = c
		}
		c.Connections++
		c.Bytes += f.Bytes
	}
	out := make([]clientView, 0, len(by))
	for _, c := range by {
		if counted && c.Bytes < minBytes {
			continue
		}
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b clientView) int {
		return cmp.Or(cmp.Compare(b.Bytes, a.Bytes), cmp.Compare(b.Connections, a.Connections), cmp.Compare(a.Address, b.Address))
	})
	return out
}

func (h HTTP) conns() ConnTable {
	if h.Conns == nil {
		return NewConnTable()
	}
	return h.Conns
}

func (h HTTP) getClients(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Tunnel.Load()
	if err != nil {
		core.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	resp := clientsResponse{Proxies: []proxyClientsView{}, Places: map[string]geoip.Place{}, AsOf: stamp()}

	var addrs []netip.Addr
	type proxy struct {
		name string
		on   bool
		port uint16
		udp  bool
	}
	proxies := []proxy{
		{"shadowsocks", cfg.Shadowsocks.Enabled, cfg.Shadowsocks.PortOrDefault(), cfg.Shadowsocks.UDPEnabled()},
		{string(ObjectSocks), cfg.Socks.Enabled, cfg.Socks.PortOrDefault(), false},
	}
	var flows []Flow
	if slices.ContainsFunc(proxies, func(p proxy) bool { return p.on }) {
		flows, resp.Counted, err = h.conns().Flows(r.Context())
		if err != nil {
			resp.Error = err.Error()
		}
	}
	for _, p := range proxies {
		if !p.on {
			continue
		}
		v := proxyClientsView{Proxy: p.name, Port: p.port, Clients: proxyClients(flows, p.port, p.udp, resp.Counted)}
		for _, c := range v.Clients {
			addrs = append(addrs, netip.MustParseAddr(c.Address))
		}
		resp.Proxies = append(resp.Proxies, v)
	}

	if cfg.WireGuard.Enabled && len(cfg.WireGuard.Peers) > 0 {
		if obs, err := h.Tunnel.ObserveFor(r.Context(), cfg); err == nil {
			for _, p := range viewPeers(cfg, obs, resp.AsOf) {
				if ap, err := netip.ParseAddrPort(p.Endpoint); err == nil {
					addrs = append(addrs, ap.Addr())
				}
			}
		}
	}

	if h.Places != nil {
		resp.Places, resp.Locations = h.Places.Locate(addrs)
	} else {
		resp.Locations = geoip.Status{State: geoip.Unavailable, Source: geoip.Source, SourceURL: geoip.SourceURL}
	}
	core.WriteJSON(w, http.StatusOK, resp)
}
