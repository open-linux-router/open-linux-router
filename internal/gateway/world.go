package gateway

import (
	"context"
	"net/http"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/open-linux-router/open-linux-router/internal/geoip"
)

// worldPeers bounds both the ranked snapshot and the GeoIP lookups it
// triggers. Forty distinct arcs read as a traffic map on a busy router; a
// handful read as loose threads.
const worldPeers = 40

type worldFlow struct {
	Key      string
	IP       netip.Addr
	Up, Down uint64
}
type worldBytes struct{ Up, Down uint64 }
type worldBucket struct {
	At     time.Time
	Values map[netip.Addr]worldBytes
}
type WorldPeer struct {
	IP       string      `json:"ip"`
	Upload   float64     `json:"upload"`
	Download float64     `json:"download"`
	Place    geoip.Place `json:"place"`
}
type WorldSnapshot struct {
	State     string       `json:"state"`
	Error     string       `json:"error,omitempty"`
	AsOf      time.Time    `json:"as_of"`
	Seconds   float64      `json:"seconds"`
	Total     float64      `json:"total"`
	Peers     []WorldPeer  `json:"peers"`
	Locations geoip.Status `json:"locations"`
}

// WorldLocator provides cached locations without waiting for network lookups.
type WorldLocator interface {
	Locate([]netip.Addr) (map[string]geoip.Place, geoip.Status)
}

// WorldTraffic retains only a 30-second window in memory. Sampling is shared
// by all viewers; GeoIP is consulted only after the IP ranking is complete.
type WorldTraffic struct {
	mu       sync.Mutex
	read     func(context.Context) ([]worldFlow, bool, error)
	Enabled  func() bool
	places   WorldLocator
	previous map[string]worldFlow
	buckets  []worldBucket
	last     time.Time
	snapshot WorldSnapshot
}

func NewWorldTraffic(places WorldLocator) *WorldTraffic {
	return &WorldTraffic{read: readWorldFlows, places: places, snapshot: WorldSnapshot{State: "warming", Peers: []WorldPeer{}}}
}

func (m *WorldTraffic) Run(ctx context.Context) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		var flows []worldFlow
		var enabled bool
		var err error
		if m.Enabled == nil || m.Enabled() {
			flows, enabled, err = m.read(ctx)
		}
		m.sample(time.Now(), flows, enabled, err)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (m *WorldTraffic) sample(now time.Time, flows []worldFlow, enabled bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := WorldSnapshot{State: "warming", AsOf: now, Peers: []WorldPeer{}}
	if err != nil || !enabled {
		s.State = "disabled"
		if err != nil {
			s.State, s.Error = "unavailable", err.Error()
		}
		m.previous, m.buckets, m.last = nil, nil, time.Time{}
		m.snapshot = s
		return
	}
	current := make(map[string]worldFlow, len(flows))
	values := map[netip.Addr]worldBytes{}
	for _, f := range flows {
		current[f.Key] = f
		if m.last.IsZero() {
			continue
		}
		old, exists := m.previous[f.Key]
		// A newly observed connection starts at zero; existing counters that
		// decrease establish a new baseline rather than wrapping uint64.
		v := values[f.IP]
		if !exists || f.Up >= old.Up {
			v.Up += f.Up - old.Up
		}
		if !exists || f.Down >= old.Down {
			v.Down += f.Down - old.Down
		}
		values[f.IP] = v
	}
	if !m.last.IsZero() {
		m.buckets = append(m.buckets, worldBucket{At: m.last, Values: values})
	}
	m.previous, m.last = current, now
	for len(m.buckets) > 0 && m.buckets[0].At.Before(now.Add(-30*time.Second)) {
		m.buckets = m.buckets[1:]
	}
	if len(m.buckets) == 0 {
		m.snapshot = s
		return
	}
	s.State, s.Seconds = "ready", now.Sub(m.buckets[0].At).Seconds()
	if s.Seconds <= 0 {
		m.snapshot = s
		return
	}
	sums := map[netip.Addr]worldBytes{}
	for _, bucket := range m.buckets {
		for ip, v := range bucket.Values {
			sum := sums[ip]
			sum.Up += v.Up
			sum.Down += v.Down
			sums[ip] = sum
		}
	}
	for ip, v := range sums {
		if v.Up == 0 && v.Down == 0 {
			continue
		}
		up, down := float64(v.Up)/s.Seconds, float64(v.Down)/s.Seconds
		s.Total += up + down
		s.Peers = append(s.Peers, WorldPeer{IP: ip.String(), Upload: up, Download: down})
	}
	// The map draws one arc per location; forty keeps a busy router legible
	// without locating every remote IP. Sorting once is cheaper than the
	// insertion loop it replaces, and the denominator stays the full set.
	sort.Slice(s.Peers, func(i, j int) bool {
		a, b := s.Peers[i], s.Peers[j]
		if a.Upload+a.Download == b.Upload+b.Download {
			return a.IP < b.IP
		}
		return a.Upload+a.Download > b.Upload+b.Download
	})
	if len(s.Peers) > worldPeers {
		s.Peers = s.Peers[:worldPeers]
	}
	if m.places != nil {
		ips := make([]netip.Addr, len(s.Peers))
		for i, p := range s.Peers {
			ips[i] = netip.MustParseAddr(p.IP)
		}
		places, status := m.places.Locate(ips)
		s.Locations = status
		for i := range s.Peers {
			s.Peers[i].Place = places[s.Peers[i].IP]
		}
	}
	m.snapshot = s
}

func (h HTTP) getWorldTraffic(w http.ResponseWriter, r *http.Request) {
	if h.World == nil {
		core.WriteJSON(w, http.StatusOK, WorldSnapshot{State: "unavailable", Peers: []WorldPeer{}})
		return
	}
	h.World.mu.Lock()
	s := h.World.snapshot
	h.World.mu.Unlock()
	core.WriteJSON(w, http.StatusOK, s)
}
