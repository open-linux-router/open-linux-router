package dnsrelay

import (
	"net/netip"
	"sort"
	"sync"
	"time"
)

// The domain→IP map: the one thing here that genuinely requires reading
// responses, and the reason the invariant in message.go is phrased as it is.
//
// What it buys is attribution. A device connecting to 151.101.1.140 means
// nothing on its own; that it asked for news.example.com to get there is the
// whole story. IP→ASN stays the fallback for what this can never cover —
// clients that resolved before the relay started, connected by address, or
// asked somewhere we cannot see.
//
// The many-to-many below applies to every flow, with no exception for traffic
// that happens to be proxied. An earlier design had one — it routed by domain
// name using a proxy's fake IPs, and those are 1:1 with names, so anything
// routed that way attributed exactly. docs/gateway.md §4 removed that mechanism,
// and the map is the weaker, uniform story that remains. Better one account of
// how accurate this is than two.

// MaxNames bounds the map, and is the only thing that ever removes an entry.
//
// It is keyed by (client, address), so the natural size is devices times the
// addresses each has resolved, which for a house is thousands. The cap is what
// stops one device enumerating a subdomain wildcard from growing this without
// limit — the same class of defence as dnsmasq's lease ceiling.
//
// An earlier design expired entries at the answer's TTL plus a grace period, on
// the theory that a TTL says how long the pairing is true. It does not: a TTL
// says how long a resolver may cache the answer, while a connection routinely
// outlives it and the name stays right. Tying attribution to the resolver's
// clock made retention unpredictable — the same map held an entry for five
// minutes or for ten hours depending on who answered — and could drop a name
// out from under a live connection, which is exactly when the chart needs it.
//
// So this is a bounded set of facts ordered by recency, not a cache. When it
// fills, the pairing nobody has confirmed for longest goes first. That bounds
// retention by how much the network resolves rather than by a timer, and it
// bounds the damage of a reassigned address the same way: the stale pairing is
// the one no answer has refreshed, so it is the first to be evicted.
const MaxNames = 20000

type nameKey struct {
	client netip.Addr
	addr   netip.Addr
}

// NameMap is the observed domain→address map.
//
// Keyed by (device, address), not by address alone. The source address is free
// — every observation already carries it — and global keying cross-attributes
// the moment two devices reach one CDN address by different names, which on a
// real network is immediately.
type NameMap struct {
	mu      sync.Mutex
	entries map[nameKey]Name
}

// NewNameMap returns an empty map.
func NewNameMap() *NameMap { return &NameMap{entries: map[nameKey]Name{}} }

// Record folds one observation into the map.
//
// It is many-to-many by construction: one name resolves to several addresses,
// and one CDN address serves dozens of names. Nothing here tries to reduce that
// to a single owner, because a per-domain byte split on a shared address would
// be fabricated — and a fabricated number is worse than an absent one.
func (m *NameMap) Record(client netip.Addr, obs Observation, now time.Time) {
	if m == nil || len(obs.Addrs) == 0 {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, addr := range obs.Addrs {
		// Checked per address rather than once per answer: one answer routinely
		// carries a dozen addresses, and a check before the loop would let the
		// map overshoot its cap by the size of the answer.
		if len(m.entries) >= MaxNames {
			m.evictLocked()
		}
		// Later evidence wins. When the same device reaches one address by two
		// names both are true, and the query log still holds the earlier one —
		// but keeping a list here would imply we know how that device's traffic
		// divides between them, and we do not. Refusing to fabricate the split
		// is the same rule that governs per-domain byte accounting on a shared
		// CDN address.
		key := nameKey{client: client.Unmap(), addr: addr.Unmap()}
		m.entries[key] = Name{
			Client:   key.client,
			Name:     obs.Name,
			Addr:     key.addr,
			Chain:    obs.Chain,
			LastSeen: now,
		}
	}
}

// Snapshot returns up to limit entries, newest first. A negative limit —
// Unbounded — returns everything.
//
// The sort runs outside the lock, which is why this takes and releases it by
// hand rather than deferring. Ordering up to MaxNames entries while holding it
// puts the observer's own bookkeeping behind a reader, and the observer is what
// drains the tee: a slow read would surface as Stats.Dropped — observations
// thrown away — rather than as a slow read. Copying first is what makes that
// safe. The slice is local, and Record replaces a map entry wholesale rather
// than mutating one in place, so nothing the sort touches is shared with a
// writer.
//
// Unlike the query log's, this limit cannot narrow the work before the sort:
// "the newest N" is only known once everything is ordered. What it saves is
// encoding, sending and parsing entries nobody asked for, which on a full map
// is all but a fraction of them.
func (m *NameMap) Snapshot(limit int) []Name {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	out := make([]Name, 0, len(m.entries))
	for _, n := range m.entries {
		out = append(out, n)
	}
	m.mu.Unlock()

	sort.Slice(out, func(i, j int) bool {
		if !out[i].LastSeen.Equal(out[j].LastSeen) {
			return out[i].LastSeen.After(out[j].LastSeen)
		}
		// A total order, so the API does not reshuffle rows between two reads
		// that observed nothing new.
		if out[i].Client != out[j].Client {
			return out[i].Client.String() < out[j].Client.String()
		}
		return out[i].Addr.String() < out[j].Addr.String()
	})

	if limit >= 0 && limit < len(out) {
		out = out[:limit]
	}
	return out
}

// Len is how many entries are held.
func (m *NameMap) Len() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

// evictLocked drops the tenth of the map that no answer has confirmed for
// longest. The caller holds the lock.
func (m *NameMap) evictLocked() {
	if len(m.entries) < MaxNames {
		return
	}

	type aged struct {
		key  nameKey
		seen time.Time
	}
	all := make([]aged, 0, len(m.entries))
	for k, n := range m.entries {
		all = append(all, aged{key: k, seen: n.LastSeen})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].seen.Before(all[j].seen) })

	// A tenth at a time, so a full map does not pay for a sort on every
	// subsequent observation.
	drop := len(all) / 10
	if drop == 0 {
		drop = 1
	}
	for _, a := range all[:drop] {
		delete(m.entries, a.key)
	}
}
