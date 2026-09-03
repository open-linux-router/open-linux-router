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

// NameGrace is how long an entry outlives the TTL that created it.
//
// A real trade in both directions, so the number is a judgement rather than a
// derivation. Conntrack flows routinely outlive the TTL that created them, and
// dropping the entry first loses attribution for a connection that is still
// open. Holding it too long misattributes silently once a CDN address is
// reassigned to another tenant. Half an hour covers the first without being
// long enough for the second to matter much.
const NameGrace = 30 * time.Minute

// MinNameLifetime is the floor on how long an entry is kept.
//
// CDNs answer with 30-second TTLs as a matter of course. Without a floor the
// map would be almost empty almost always, and the feature would look broken
// rather than short-lived.
const MinNameLifetime = 5 * time.Minute

// MaxNames bounds the map.
//
// It is keyed by (client, address), so the natural size is devices times the
// addresses each has resolved, which for a house is thousands. The cap is what
// stops one device enumerating a subdomain wildcard from growing this without
// limit — the same class of defence as dnsmasq's lease ceiling.
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

	lifetime := obs.TTL + NameGrace
	if lifetime < MinNameLifetime {
		lifetime = MinNameLifetime
	}
	expires := now.Add(lifetime)

	m.mu.Lock()
	defer m.mu.Unlock()

	if len(m.entries) >= MaxNames {
		m.evictLocked(now)
	}

	for _, addr := range obs.Addrs {
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
			Expires:  expires,
			LastSeen: now,
		}
	}
}

// Snapshot returns the live entries, newest first, dropping anything expired.
func (m *NameMap) Snapshot(now time.Time) []Name {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.expireLocked(now)

	out := make([]Name, 0, len(m.entries))
	for _, n := range m.entries {
		out = append(out, n)
	}
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
	return out
}

// Len is how many live entries are held, expired ones included until the next
// sweep.
func (m *NameMap) Len() int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

func (m *NameMap) expireLocked(now time.Time) {
	for k, n := range m.entries {
		if now.After(n.Expires) {
			delete(m.entries, k)
		}
	}
}

// evictLocked makes room when the map is full: expired entries first, and if
// that was not enough, the least recently seen.
func (m *NameMap) evictLocked(now time.Time) {
	m.expireLocked(now)
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
