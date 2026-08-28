package core

import (
	"testing"
	"time"
)

func TestPublishReachesEverySubscriber(t *testing.T) {
	e := NewEvents()

	a, cancelA := e.Subscribe()
	defer cancelA()
	b, cancelB := e.Subscribe()
	defer cancelB()

	e.Publish(Event{Type: EventApplied, Module: "dhcp"})

	for name, ch := range map[string]<-chan Event{"a": a, "b": b} {
		select {
		case got := <-ch:
			if got.Module != "dhcp" {
				t.Errorf("%s: module = %q", name, got.Module)
			}
			if got.At.IsZero() {
				t.Errorf("%s: event carries no timestamp", name)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: no event delivered", name)
		}
	}
}

// An apply must not be slowed by a browser tab. A subscriber that has fallen
// behind loses events rather than blocking the publisher.
func TestPublishDoesNotBlockOnASlowSubscriber(t *testing.T) {
	e := NewEvents()
	_, cancel := e.Subscribe()
	defer cancel()

	done := make(chan struct{})
	go func() {
		for range subscriberBuffer * 4 {
			e.Publish(Event{Type: EventApplied, Module: "dhcp"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked behind a subscriber that never read")
	}
}

func TestUnsubscribeStopsDeliveryAndIsIdempotent(t *testing.T) {
	e := NewEvents()
	ch, cancel := e.Subscribe()

	if got := e.subscribers(); got != 1 {
		t.Fatalf("subscribers = %d, want 1", got)
	}

	cancel()
	cancel() // must not panic on a double close

	if got := e.subscribers(); got != 0 {
		t.Errorf("subscribers = %d after cancel, want 0", got)
	}

	// Publishing after everyone has gone must not panic on a closed channel.
	e.Publish(Event{Type: EventApplied})

	if _, open := <-ch; open {
		t.Error("the channel should be closed after cancelling")
	}
}
