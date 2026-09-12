package dial

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

// The publisher is driven through Check with an injected reader, updater and
// clock, the way internal/gateway's prober is driven through record. That is
// the whole reason those three fields are injectable: the rules in
// docs/ddns.md §6 are about *when* a request is made, and a test that had to
// wait for a ticker to prove it could not assert on the interesting cases at
// all.

// fake is a scripted address reader and provider.
type fake struct {
	mu sync.Mutex

	addr    string
	readErr error

	updateErr error
	updates   []string
	reads     int
}

func (f *fake) read(context.Context, Record) (netip.Addr, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.readErr != nil {
		return netip.Addr{}, f.readErr
	}
	return netip.MustParseAddr(f.addr), nil
}

func (f *fake) update(_ context.Context, _ Record, address string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.updateErr != nil {
		return f.updateErr
	}
	f.updates = append(f.updates, address)
	return nil
}

func (f *fake) set(fn func(*fake)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

// clock is a hand-wound time source, so backoff can be asserted without waiting.
type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func testRecord() Record {
	return Record{
		Name:         "home.example.net",
		Provider:     "cloudflare",
		Token:        "t",
		Source:       SourceReflector,
		ReflectorURL: "https://reflector.example/",
		Interval:     Duration(time.Minute),
	}
}

func newTestPublisher(f *fake, c *clock) *Publisher {
	p := NewPublisher()
	p.Read = f.read
	p.Update = f.update
	p.Now = c.Now
	return p
}

func newClock() *clock {
	return &clock{now: time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC)}
}

// docs/ddns.md §6: a check is not an update.
func TestAnUnchangedAddressIsNotRepublished(t *testing.T) {
	f := &fake{addr: "203.0.113.9"}
	c := newClock()
	p := newTestPublisher(f, c)
	rec := testRecord()

	for range 5 {
		p.Check(context.Background(), rec)
		c.advance(time.Minute)
	}

	if len(f.updates) != 1 {
		t.Fatalf("made %d updates for an address that never moved, want 1", len(f.updates))
	}
	if f.reads != 5 {
		t.Errorf("read the address %d times, want 5; the schedule is checking, not publishing", f.reads)
	}
}

// The first check after a restart always publishes, because the cache is ours
// and it starts empty — which is also the repair path for a record somebody
// changed by hand at the provider.
func TestTheFirstCheckAfterAStartAlwaysPublishes(t *testing.T) {
	f := &fake{addr: "203.0.113.9"}
	p := newTestPublisher(f, newClock())

	p.Check(context.Background(), testRecord())
	if len(f.updates) != 1 {
		t.Fatalf("made %d updates on the first check, want 1", len(f.updates))
	}
}

func TestAChangedAddressIsPublished(t *testing.T) {
	f := &fake{addr: "203.0.113.9"}
	c := newClock()
	p := newTestPublisher(f, c)
	rec := testRecord()

	p.Check(context.Background(), rec)
	f.set(func(f *fake) { f.addr = "203.0.113.10" })
	c.advance(time.Minute)
	p.Check(context.Background(), rec)

	if len(f.updates) != 2 || f.updates[1] != "203.0.113.10" {
		t.Fatalf("updates = %v", f.updates)
	}
}

// docs/ddns.md §6: backoff is on the provider, not on the address.
func TestARejectedUpdateBacksOff(t *testing.T) {
	f := &fake{addr: "203.0.113.9", updateErr: errors.New("badauth")}
	c := newClock()
	p := newTestPublisher(f, c)
	rec := testRecord()

	p.Check(context.Background(), rec)

	state := p.States()[rec.Name]
	if state.PublishError == "" {
		t.Fatal("a refused update should be visible in status")
	}
	if state.Retry.IsZero() {
		t.Fatal("a refused update should set a retry time")
	}
	if !state.Retry.After(c.Now()) {
		t.Errorf("Retry = %s, which is not in the future", state.Retry)
	}

	// A check inside the backoff window still reads the address — status must
	// stay fresh — but does not go back to the provider.
	before := len(f.updates)
	readsBefore := f.reads
	c.advance(time.Second)
	p.Check(context.Background(), rec)
	if len(f.updates) != before {
		t.Error("attempted an update while backing off")
	}
	if f.reads != readsBefore+1 {
		t.Error("stopped reading the address while backing off; status would go stale")
	}

	// Past the window, it tries again.
	c.advance(MaxBackoff)
	f.set(func(f *fake) { f.updateErr = nil })
	p.Check(context.Background(), rec)
	if len(f.updates) != before+1 {
		t.Error("did not retry after the backoff window")
	}
	if got := p.States()[rec.Name]; got.PublishError != "" || got.Failures != 0 {
		t.Errorf("a success should clear the failure state, got %+v", got)
	}
}

// The other half of the same rule: a reflector that did not answer costs the
// stranger nothing, so the next tick is the whole retry it needs.
func TestAFailedAddressReadDoesNotBackOff(t *testing.T) {
	f := &fake{readErr: errors.New("connection refused")}
	c := newClock()
	p := newTestPublisher(f, c)
	rec := testRecord()

	p.Check(context.Background(), rec)

	state := p.States()[rec.Name]
	if !state.Retry.IsZero() {
		t.Errorf("a failed read set a backoff (%s); only a refused update should", state.Retry)
	}
	if state.Failures != 0 {
		t.Errorf("Failures = %d; a failed read is not a provider failure", state.Failures)
	}
}

// The failure ddns-go's return value cannot express, which is why the provider
// code is ported rather than imported (docs/ddns.md §4.2).
func TestAFailedAddressReadIsVisibleInStatus(t *testing.T) {
	f := &fake{addr: "203.0.113.9"}
	c := newClock()
	p := newTestPublisher(f, c)
	rec := testRecord()

	p.Check(context.Background(), rec)
	c.advance(time.Minute)
	f.set(func(f *fake) { f.readErr = errors.New("dial tcp: i/o timeout") })
	p.Check(context.Background(), rec)

	state := p.States()[rec.Name]
	switch {
	case state.CheckError == "":
		t.Fatal("a failed read must be reported, not silently ignored")
	case state.Checked != c.Now():
		t.Errorf("Checked = %s, want the attempt's time %s; a stale timestamp is "+
			"indistinguishable from a wedged daemon", state.Checked, c.Now())
	case state.Address != "203.0.113.9":
		t.Errorf("Address = %q; the last thing we knew should survive beside the error", state.Address)
	case state.PublishedAddress != "203.0.113.9":
		t.Error("a failed read discarded the published cache, so the reflector coming " +
			"back would cause a redundant update")
	}
}

// §3.2's diagnosis has to reach status, because it is the one nothing else in
// the product can offer.
func TestCGNATReachesStatus(t *testing.T) {
	f := &fake{addr: "100.72.14.3"}
	p := newTestPublisher(f, newClock())

	p.Check(context.Background(), testRecord())

	if !p.States()["home.example.net"].CGNAT {
		t.Fatal("an address inside 100.64.0.0/10 should be flagged")
	}
}

// A record whose settings did not change keeps its loop and its cache: emptying
// it on an unrelated edit elsewhere in the document would send a redundant
// update on the next tick.
func TestWatchKeepsAnUnchangedRecordsCache(t *testing.T) {
	f := &fake{addr: "203.0.113.9"}
	p := newTestPublisher(f, newClock())
	// No goroutines: Watch is given a cancelled context so the loops exit at
	// once, and the state map is what this asserts on.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	cfg := Config{Records: []Record{testRecord()}}
	p.Watch(ctx, cfg)
	p.Check(context.Background(), cfg.Records[0])
	p.Watch(ctx, cfg)

	if got := p.States()["home.example.net"].PublishedAddress; got != "203.0.113.9" {
		t.Errorf("PublishedAddress = %q after a no-op Watch; the cache was reset", got)
	}

	// Changing the record does reset it: the address it published was published
	// somewhere else, and keeping it would suppress the first update to the new
	// place.
	changed := cfg.Clone()
	changed.Records[0].Provider = "alidns"
	p.Watch(ctx, changed)
	if got := p.States()["home.example.net"].PublishedAddress; got != "" {
		t.Errorf("PublishedAddress = %q after the provider changed; want it forgotten", got)
	}
}

func TestWatchStopsWatchingARemovedRecord(t *testing.T) {
	p := newTestPublisher(&fake{addr: "203.0.113.9"}, newClock())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	p.Watch(ctx, Config{Records: []Record{testRecord()}})
	if _, watched := p.States()["home.example.net"]; !watched {
		t.Fatal("a configured record should be watched")
	}

	p.Watch(ctx, Config{})
	if _, watched := p.States()["home.example.net"]; watched {
		t.Error("a removed record should stop being watched, not leave a stale state behind")
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	interval := time.Minute
	if got := backoff(interval, 1); got != interval {
		t.Errorf("backoff after one refusal = %s, want the check interval %s", got, interval)
	}
	if got := backoff(interval, 2); got != 2*interval {
		t.Errorf("backoff after two refusals = %s, want %s", got, 2*interval)
	}
	if got := backoff(interval, 40); got != MaxBackoff {
		t.Errorf("backoff after forty refusals = %s, want the cap %s", got, MaxBackoff)
	}
	if got := backoff(2*MaxBackoff, 1); got != MaxBackoff {
		t.Errorf("backoff from an interval above the cap = %s, want %s", got, MaxBackoff)
	}
}
