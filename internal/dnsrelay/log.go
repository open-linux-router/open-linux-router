package dnsrelay

import "sync"

// QueryLog is a fixed-size ring of answered queries.
//
// In memory, and lost on restart. Where the query log should eventually live is
// genuinely open (docs/dns.md §7.5) — it is one of two workloads voting in
// design.md §10's storage decision — and keeping it here pre-decides nothing.
// It also avoids the alternative's real cost: a line per query appended to disk
// is continuous flash wear on the many olr boxes that will boot from an SD card.
//
// The ring is what bounds the memory. A busy house does tens of queries a
// second, so an unbounded log is a slow leak that presents as the resolver
// being OOM-killed some days later — which reads to everyone in the building as
// the internet breaking for no reason.
type QueryLog struct {
	mu       sync.Mutex
	entries  []Query
	next     int
	full     bool
	capacity int
}

// NewQueryLog returns a log holding capacity entries. A capacity of zero or
// less disables logging entirely, and Add then costs one comparison.
func NewQueryLog(capacity int) *QueryLog {
	if capacity <= 0 {
		return &QueryLog{}
	}
	return &QueryLog{entries: make([]Query, capacity), capacity: capacity}
}

// Enabled reports whether anything is being recorded.
func (l *QueryLog) Enabled() bool { return l != nil && l.capacity > 0 }

// Capacity is how many entries the log can hold.
func (l *QueryLog) Capacity() int {
	if l == nil {
		return 0
	}
	return l.capacity
}

// Add records one answered query, overwriting the oldest when full.
func (l *QueryLog) Add(q Query) {
	if !l.Enabled() {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	l.entries[l.next] = q
	l.next++
	if l.next == l.capacity {
		l.next = 0
		l.full = true
	}
}

// Held is how many entries the log currently holds.
func (l *QueryLog) Held() int {
	if !l.Enabled() {
		return 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.full {
		return l.capacity
	}
	return l.next
}

// Snapshot returns the log newest first.
//
// Newest first because every consumer of this — the CLI table, the API, a
// person looking for what just happened — wants the recent end. Returning it
// oldest-first and making each caller reverse it is how one of them forgets.
func (l *QueryLog) Snapshot() []Query {
	if !l.Enabled() {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()

	held := l.next
	if l.full {
		held = l.capacity
	}
	out := make([]Query, 0, held)
	// Walk backwards from the most recently written slot.
	for i := 0; i < held; i++ {
		idx := l.next - 1 - i
		if idx < 0 {
			idx += l.capacity
		}
		out = append(out, l.entries[idx])
	}
	return out
}
