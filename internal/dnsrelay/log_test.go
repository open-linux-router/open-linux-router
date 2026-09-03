package dnsrelay

import (
	"net/netip"
	"testing"
	"time"
)

func TestQueryLogDisabledCostsNothing(t *testing.T) {
	l := NewQueryLog(0)
	if l.Enabled() {
		t.Error("a zero capacity log reports itself enabled")
	}
	l.Add(Query{Name: "example.com"})
	if got := l.Snapshot(); len(got) != 0 {
		t.Errorf("a disabled log recorded %d entries", len(got))
	}
	if l.Held() != 0 || l.Capacity() != 0 {
		t.Errorf("Held/Capacity = %d/%d, want 0/0", l.Held(), l.Capacity())
	}
}

// The ring is what bounds the memory. An unbounded log is a slow leak that
// presents as the resolver being OOM-killed some days later, which reads to
// everyone in the building as the internet breaking for no reason.
func TestQueryLogRingIsBounded(t *testing.T) {
	l := NewQueryLog(3)
	for i := range 10 {
		l.Add(Query{Name: string(rune('a'+i)) + ".example", At: time.Now()})
	}

	got := l.Snapshot()
	if len(got) != 3 {
		t.Fatalf("held %d entries, want 3", len(got))
	}
	if l.Held() != 3 {
		t.Errorf("Held() = %d, want 3", l.Held())
	}
	// Newest first, because every consumer wants the recent end.
	if got[0].Name != "j.example" {
		t.Errorf("newest entry = %q, want j.example", got[0].Name)
	}
	if got[2].Name != "h.example" {
		t.Errorf("oldest retained entry = %q, want h.example", got[2].Name)
	}
}

func TestQueryLogPartiallyFilled(t *testing.T) {
	l := NewQueryLog(5)
	l.Add(Query{Name: "a.example"})
	l.Add(Query{Name: "b.example"})

	got := l.Snapshot()
	if len(got) != 2 {
		t.Fatalf("held %d entries, want 2", len(got))
	}
	if got[0].Name != "b.example" || got[1].Name != "a.example" {
		t.Errorf("order = %q, %q; want newest first", got[0].Name, got[1].Name)
	}
	if l.Held() != 2 || l.Capacity() != 5 {
		t.Errorf("Held/Capacity = %d/%d, want 2/5", l.Held(), l.Capacity())
	}
}

func TestQueryLogIsConcurrencySafe(t *testing.T) {
	// The tee's consumer writes while the observation socket reads, so this is
	// the real access pattern rather than a hypothetical one.
	l := NewQueryLog(64)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 500 {
			l.Add(Query{Name: "example.com", Client: netip.MustParseAddr("192.168.1.10")})
		}
	}()
	for range 500 {
		_ = l.Snapshot()
		_ = l.Held()
	}
	<-done
}
