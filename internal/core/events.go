package core

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// Live data streams to the WebUI over SSE (design.md §6.3). SSE rather than
// WebSockets because the traffic is one-directional — the browser already has
// the REST API for anything it wants to say — and because SSE reconnects on its
// own and survives a proxy without a protocol upgrade.
//
// This bus is a notification channel, not a data channel. An event says
// *something changed*; the client re-reads the affected endpoint. That keeps
// §10's rule intact — olrd caches no state, so there is nothing here that could
// drift from the system and become a second, staler source of truth.

// EventType names a kind of event. Deliberately few.
type EventType string

const (
	// EventApplied is published after a module's config has been applied.
	EventApplied EventType = "applied"
	// EventHeartbeat keeps intermediaries from closing an idle stream.
	EventHeartbeat EventType = "heartbeat"
)

// Event is one notification.
type Event struct {
	Type   EventType `json:"type"`
	Module string    `json:"module,omitempty"`

	// At is when the event was published. Every observed thing on the API
	// carries its own freshness (§4.5); an event is no exception.
	At time.Time `json:"at"`

	// Data is an optional small payload. It must stay a hint, not a copy of
	// state — a client that acts on it instead of re-reading would be trusting
	// a cache we promised not to keep.
	Data any `json:"data,omitempty"`
}

// subscriberBuffer is how many events a slow client may fall behind before we
// start dropping. Small on purpose: events are hints, so a client that missed
// some re-reads and is immediately correct again.
const subscriberBuffer = 16

// heartbeatInterval is under the 60s that proxies and load balancers commonly
// use to reap idle connections.
const heartbeatInterval = 25 * time.Second

// Events is the fan-out bus.
type Events struct {
	mu   sync.Mutex
	subs map[chan Event]struct{}
}

// NewEvents returns an empty bus.
func NewEvents() *Events {
	return &Events{subs: map[chan Event]struct{}{}}
}

// Publish delivers ev to every current subscriber.
//
// It never blocks. A subscriber that has fallen behind by more than
// subscriberBuffer misses the event rather than stalling the applier that
// published it — an apply must not be slowed by a browser tab.
func (e *Events) Publish(ev Event) {
	if ev.At.IsZero() {
		ev.At = time.Now()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	for ch := range e.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Subscribe returns a channel of events and a function that unsubscribes it.
// The cancel function is idempotent and must be called.
func (e *Events) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, subscriberBuffer)

	e.mu.Lock()
	e.subs[ch] = struct{}{}
	e.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			e.mu.Lock()
			delete(e.subs, ch)
			e.mu.Unlock()
			close(ch)
		})
	}
}

// subscribers reports the current subscriber count. Test support.
func (e *Events) subscribers() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.subs)
}

// Handler streams events as text/event-stream.
func (e *Events) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			WriteError(w, http.StatusInternalServerError,
				"this server cannot stream events")
			return
		}

		h := w.Header()
		h.Set("Content-Type", "text/event-stream")
		h.Set("Cache-Control", "no-cache")
		h.Set("Connection", "keep-alive")
		// Defeats proxy buffering, which otherwise holds events until a buffer
		// fills and makes a live UI look frozen.
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		ch, cancel := e.Subscribe()
		defer cancel()

		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-r.Context().Done():
				return

			case ev := <-ch:
				if !writeEvent(w, ev) {
					return
				}
				flusher.Flush()

			case <-ticker.C:
				if !writeEvent(w, Event{Type: EventHeartbeat, At: time.Now()}) {
					return
				}
				flusher.Flush()
			}
		}
	})
}

// writeEvent emits one SSE frame, reporting whether the connection is still
// usable.
func writeEvent(w http.ResponseWriter, ev Event) bool {
	data, err := json.Marshal(ev)
	if err != nil {
		// Dropping one malformed event is better than tearing down a stream
		// the rest of the UI depends on.
		return true
	}
	_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
	return err == nil
}
