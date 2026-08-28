package core

import "context"

// Lock is the one global apply lock (design.md §3.6).
//
// Every config write across every module takes it. Not per-module: the
// simplicity argument is obvious, but the correctness one is better. A global
// lock is what makes cross-module validation sound (§5.3.1) — validate dhcp's
// pool against link's subnet, then apply, with nothing able to move link in
// between. Per-module locks reintroduce exactly the TOCTOU window that
// validation exists to close.
//
// **Reads never take it.** §5.4 already requires reads to hit actual system
// state — netlink, the lease file, D-Bus — so there is nothing to serialise,
// and a read that blocked behind a slow apply would make the UI feel broken
// during the one operation the operator is watching.
//
// The contention budget is a human clicking a toggle. There is no throughput
// requirement to trade against.
type Lock struct {
	// ch is a semaphore rather than a sync.Mutex so that Do can respect a
	// request's context. A client that gave up waiting should stop waiting;
	// sync.Mutex cannot be cancelled.
	ch chan struct{}
}

// NewLock returns an unlocked Lock.
func NewLock() *Lock {
	l := &Lock{ch: make(chan struct{}, 1)}
	l.ch <- struct{}{}
	return l
}

// Do runs fn while holding the lock, releasing it even if fn panics.
//
// It returns ctx.Err() without running fn if the context is cancelled while
// waiting. Apply itself must stay bounded — render, reload, return, never
// waiting for convergence (§3.6) — so a caller should not be here long.
func (l *Lock) Do(ctx context.Context, fn func() error) error {
	select {
	case <-l.ch:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { l.ch <- struct{}{} }()

	return fn()
}

// TryDo runs fn only if the lock is free, reporting whether it ran.
//
// This is for the surfaces that would rather say "an apply is in progress" than
// queue behind one.
func (l *Lock) TryDo(fn func() error) (bool, error) {
	select {
	case <-l.ch:
	default:
		return false, nil
	}
	defer func() { l.ch <- struct{}{} }()

	return true, fn()
}

// held reports whether the lock is currently taken. Test support only — a
// caller cannot act on this without racing.
func (l *Lock) held() bool { return len(l.ch) == 0 }
