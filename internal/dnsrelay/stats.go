package dnsrelay

import (
	"net/netip"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// The relay's account of itself, including — deliberately, prominently — the
// parts it cannot account for.
//
// "Always show what you cannot account for" is the rule the whole observability
// story rests on. A tee that silently dropped observations under load would
// break it in the least visible way possible: the query log would look complete
// and would be missing exactly the traffic that arrived when the box was busy.

// MaxTrackedClients bounds the per-client table.
//
// Generous for a house, and a cap rather than no cap because the key is a
// source address: one spoofed-source flood would otherwise grow this without
// limit. Overflow is counted rather than silently ignored, for the same reason
// everything else here is.
const MaxTrackedClients = 4096

// Counters is the relay's running tally. Every field is touched on the hot
// path, so they are atomics rather than anything needing a lock.
type Counters struct {
	Queries  atomic.Uint64
	Blocked  atomic.Uint64
	Refused  atomic.Uint64
	Failed   atomic.Uint64
	Dropped  atomic.Uint64
	Unparsed atomic.Uint64
}

// ClientTable records who has been asking.
//
// It is what lets olrd answer "would this change cut anybody off" with a fact
// rather than a guess — the direct analogue of internal/dhcp consulting the
// live lease database before calling a change disruptive.
type ClientTable struct {
	mu       sync.Mutex
	clients  map[netip.Addr]*clientCount
	overflow uint64
}

type clientCount struct {
	queries  uint64
	lastSeen time.Time
}

// NewClientTable returns an empty table.
func NewClientTable() *ClientTable {
	return &ClientTable{clients: map[netip.Addr]*clientCount{}}
}

// Seen records one query from a client.
func (t *ClientTable) Seen(addr netip.Addr, now time.Time) {
	if t == nil {
		return
	}
	addr = addr.Unmap()

	t.mu.Lock()
	defer t.mu.Unlock()

	if c, ok := t.clients[addr]; ok {
		c.queries++
		c.lastSeen = now
		return
	}
	if len(t.clients) >= MaxTrackedClients {
		t.overflow++
		return
	}
	t.clients[addr] = &clientCount{queries: 1, lastSeen: now}
}

// Snapshot returns the table, busiest first.
func (t *ClientTable) Snapshot() []Client {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()

	out := make([]Client, 0, len(t.clients))
	for addr, c := range t.clients {
		out = append(out, Client{Addr: addr, Queries: c.queries, LastSeen: c.lastSeen})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Queries != out[j].Queries {
			return out[i].Queries > out[j].Queries
		}
		// A total order, so two reads with no traffic between them agree.
		return out[i].Addr.String() < out[j].Addr.String()
	})
	return out
}

// Snapshot builds the wire form of everything the relay knows about itself.
func (r *Relay) Snapshot() Stats {
	return Stats{
		Since:    r.started,
		Queries:  r.counters.Queries.Load(),
		Blocked:  r.counters.Blocked.Load(),
		Refused:  r.counters.Refused.Load(),
		Failed:   r.counters.Failed.Load(),
		Dropped:  r.counters.Dropped.Load(),
		Unparsed: r.counters.Unparsed.Load(),
		Held:     r.log.Held(),
		Capacity: r.log.Capacity(),
		Clients:  r.clients.Snapshot(),
	}
}
