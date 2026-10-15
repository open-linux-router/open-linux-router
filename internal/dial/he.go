package dial

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	probing "github.com/prometheus-community/pro-bing"
)

var heCheckRunning atomic.Bool

type heServer struct {
	Region  string `json:"region"`
	Name    string `json:"name"`
	Address string `json:"address"`
}

// HE's creation-page catalog, supplied 2026-10-15. Empty address means full.
var heCatalog = []heServer{
	{"North America", "Ashburn, VA, US", "216.66.22.2"},
	{"North America", "Calgary, AB, CA", "216.218.200.58"},
	{"North America", "Chicago, IL, US", "184.105.253.14"},
	{"North America", "Dallas, TX, US", "184.105.253.10"},
	{"North America", "Denver, CO, US", "184.105.250.46"},
	{"North America", "Fremont, CA, US", "72.52.104.74"},
	{"North America", "Fremont, CA, US", "64.62.134.130"},
	{"North America", "Honolulu, HI, US", "64.71.156.86"},
	{"North America", "Kansas City, MO, US", "216.66.77.230"},
	{"North America", "Los Angeles, CA, US", "66.220.18.42"},
	{"North America", "Miami, FL, US", "209.51.161.58"},
	{"North America", "New York, NY, US", "209.51.161.14"},
	{"North America", "Phoenix, AZ, US", "66.220.7.82"},
	{"North America", "Seattle, WA, US", "216.218.226.238"},
	{"North America", "Toronto, ON, CA", "216.66.38.58"},
	{"North America", "Winnipeg, MB, CA", "184.105.255.26"},
	{"Europe", "Amsterdam, NL", "216.66.84.46"},
	{"Europe", "Berlin, DE", "216.66.86.114"},
	{"Europe", "Budapest, HU", "216.66.87.14"},
	{"Europe", "Frankfurt, DE", "216.66.80.30"},
	{"Europe", "Lisbon, PT", "216.66.87.102"},
	{"Europe", "London, UK", "216.66.80.26"},
	{"Europe", "London, UK", "216.66.88.98"},
	{"Europe", "Paris, FR", "216.66.84.42"},
	{"Europe", "Prague, CZ", "216.66.86.122"},
	{"Europe", "Stockholm, SE", "216.66.80.90"},
	{"Europe", "Warsaw, PL", ""},
	{"Europe", "Zurich, CH", "216.66.80.98"},
	{"Asia", "Hong Kong, HK", "216.218.221.6"},
	{"Asia", "Singapore, SG", "216.218.221.42"},
	{"Asia", "Tokyo, JP", "74.82.46.6"},
	{"Africa", "Djibouti City, DJ", "216.66.87.98"},
	{"Africa", "Johannesburg, ZA", "216.66.87.134"},
	{"South America", "Bogota, CO", "216.66.64.154"},
	{"Oceania", "Sydney, NSW, AU", "216.218.142.50"},
	{"Middle East", "Dubai, AE", "216.66.90.30"},
}

func (h HTTP) heServers(w http.ResponseWriter, r *http.Request) {
	core.WriteJSON(w, http.StatusOK, heCatalog)
}

func (h HTTP) tunnelBrokerStatus(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, 500, err.Error())
		return
	}
	obs := h.Applier.Observe(r.Context(), cfg.uplinkInterface())
	want := DesiredFor(cfg).Tunnel
	ready := want != nil && PlanTunnel(*want, obs) == nil
	core.WriteJSON(w, 200, map[string]any{"present": obs.Tunnel.Present, "up": obs.Tunnel.Up, "matches": ready, "native_default_dev": obs.V6DefaultDev, "as_of": time.Now()})
}

func (h HTTP) heCheck(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Target string `json:"target"`
	}
	if err := core.DecodeJSON(w, r, &req); err != nil {
		core.WriteError(w, 400, err.Error())
		return
	}
	cfg, err := h.Applier.Load()
	if err != nil {
		core.WriteError(w, 500, err.Error())
		return
	}
	if cfg.Uplink == nil {
		core.WriteError(w, 422, "Set up an uplink first.")
		return
	}
	if !heCheckRunning.CompareAndSwap(false, true) {
		core.WriteError(w, http.StatusConflict, "An HE check is already running. Retry when it finishes.")
		return
	}
	defer heCheckRunning.Store(false)
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	if req.Target == "" {
		core.WriteError(w, http.StatusBadRequest, "Use /api/system/public-addresses for the shared ip.sb lookup; target is required for HE probes.")
		return
	}
	iface := cfg.Uplink.Interface
	allowed := false
	for _, s := range heCatalog {
		if s.Address != "" && s.Address == req.Target {
			allowed = true
		}
	}
	// IPv6 verification is restricted to the configured tunnel peer and public test targets.
	if tunnel := cfg.ActiveTunnel(); tunnel != nil {
		prefix := tunnel.Address
		if prefix.IsValid() {
			peer := prefix.Masked().Addr().Next().String()
			if cfg.TunnelBroker != nil && cfg.TunnelBroker.ServerIPv6.IsValid() {
				peer = cfg.TunnelBroker.ServerIPv6.Addr().String()
			}
			if req.Target == peer || req.Target == "2606:4700:4700::1111" || req.Target == "2001:4860:4860::8888" {
				allowed = true
				iface = TunnelInterface
			}
		}
	}
	if !allowed {
		core.WriteError(w, 400, "Choose a built-in HE server or a configured tunnel test target.")
		return
	}
	p := probing.New(req.Target)
	p.InterfaceName = iface
	if iface == TunnelInterface {
		p.Source = cfg.ActiveTunnel().Address.Addr().String()
	}
	p.Count = 8
	p.Interval = 250 * time.Millisecond
	p.Timeout = 4 * time.Second
	if err := p.RunWithContext(ctx); err != nil {
		core.WriteError(w, 503, err.Error())
		return
	}
	st := p.Statistics()
	core.WriteJSON(w, 200, map[string]any{"sent": st.PacketsSent, "received": st.PacketsRecv, "loss": st.PacketLoss, "avg_ms": float64(st.AvgRtt) / float64(time.Millisecond), "variation_ms": float64(st.StdDevRtt) / float64(time.Millisecond), "as_of": time.Now()})
}
