package routing

import (
	"context"
	"errors"
	"net/netip"
	"sync"
	"testing"
	"time"
)

var errBoom = errors.New("boom")

// A prober wired to a scripted dialer, so the hysteresis can be exercised
// without a network — which is the whole reason Prober.Dial is injectable.
type scriptedDialer struct {
	mu sync.Mutex
	ok bool
}

func (s *scriptedDialer) set(ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ok = ok
}

func (s *scriptedDialer) dial(context.Context, uint32, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ok {
		return nil
	}
	return errBoom
}

func probeCfg() Probe {
	return Probe{
		Target:    netip.MustParseAddrPort("1.1.1.1:443"),
		Failures:  3,
		Successes: 2,
	}.Resolved()
}

// §5.5: going down takes several consecutive misses, so one dropped packet
// cannot blackhole the network.
func TestGoingDownNeedsConsecutiveFailures(t *testing.T) {
	p := NewProber()
	p.health["Clash"] = true
	cfg := probeCfg()

	for i := 1; i < cfg.Failures; i++ {
		if changed, _ := p.record("Clash", false, cfg); changed {
			t.Fatalf("flipped after %d failures, want %d", i, cfg.Failures)
		}
	}
	changed, up := p.record("Clash", false, cfg)
	if !changed || up {
		t.Fatalf("should be down after %d failures, got changed=%v up=%v", cfg.Failures, changed, up)
	}
}

// A single success in the middle of a bad run resets the streak, which is what
// stops a flapping exit from being reported down on a technicality.
func TestOneSuccessResetsTheFailureStreak(t *testing.T) {
	p := NewProber()
	p.health["Clash"] = true
	cfg := probeCfg()

	p.record("Clash", false, cfg)
	p.record("Clash", false, cfg)
	p.record("Clash", true, cfg) // agrees with the current verdict; resets
	p.record("Clash", false, cfg)
	p.record("Clash", false, cfg)

	if changed, _ := p.record("Clash", false, cfg); !changed {
		t.Fatal("the third consecutive failure after the reset should flip it")
	}
}

func TestComingBackNeedsConsecutiveSuccesses(t *testing.T) {
	p := NewProber()
	p.health["Clash"] = false
	cfg := probeCfg()

	if changed, _ := p.record("Clash", true, cfg); changed {
		t.Fatal("one success should not be enough to bring an exit back")
	}
	changed, up := p.record("Clash", true, cfg)
	if !changed || !up {
		t.Fatalf("should be up after %d successes, got changed=%v up=%v", cfg.Successes, changed, up)
	}
}

// Not symmetric by accident: a half-recovered proxy that flaps drags the whole
// house with it, so coming back is deliberately slower than going down.
func TestRecoveryIsSlowerThanFailure(t *testing.T) {
	cfg := probeCfg()
	if cfg.Successes >= cfg.Failures {
		return // a legitimate configuration, just not the default we ship
	}
	if DefaultProbeSuccesses > DefaultProbeFailures {
		t.Errorf("recovery (%d) should not need more evidence than failure (%d)",
			DefaultProbeSuccesses, DefaultProbeFailures)
	}
}

func TestWatchStartsAndStopsWithTheConfig(t *testing.T) {
	dialer := &scriptedDialer{ok: true}
	p := NewProber()
	p.Dial = dialer.dial

	c := testConfig()
	setExit(&c, "Clash", func(e *Exit) {
		e.Probe = &Probe{Target: netip.MustParseAddrPort("1.1.1.1:443")}
	})
	c.Normalize()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p.Watch(ctx, c)
	defer p.Stop()

	if _, watched := p.Health()["Clash"]; !watched {
		t.Fatal("an exit with a probe should be watched")
	}
	// A blocked exit is never down, so probing one does nothing.
	if _, watched := p.Health()["Blocked"]; watched {
		t.Error("a blocked exit should not be probed")
	}

	// Removing the probe stops the watch and forgets the verdict, rather than
	// leaving a stale "down" behind that nothing will ever clear.
	setExit(&c, "Clash", func(e *Exit) { e.Probe = nil })
	p.Watch(ctx, c)
	if _, watched := p.Health()["Clash"]; watched {
		t.Error("an exit with no probe should not be watched")
	}
}

func TestANewExitStartsBelievedUp(t *testing.T) {
	// Starting it down would blackhole its traffic for the first few intervals,
	// which is a self-inflicted outage on the happy path.
	p := NewProber()
	p.Dial = func(context.Context, uint32, string) error { return errBoom }

	c := testConfig()
	setExit(&c, "Clash", func(e *Exit) {
		e.Probe = &Probe{Target: netip.MustParseAddrPort("1.1.1.1:443"), Interval: Duration(time.Hour)}
	})
	c.Normalize()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p.Watch(ctx, c)
	defer p.Stop()

	if p.Health().Down("Clash") {
		t.Error("a freshly-watched exit should not start down")
	}
}

func TestHealthTreatsAnUnknownExitAsUp(t *testing.T) {
	// An exit nobody is probing must not be assumed dead.
	var h Health
	if h.Down("Clash") {
		t.Error("an unprobed exit is not down")
	}
	if (Health{"Clash": true}).Down("Clash") {
		t.Error("an exit reported up is not down")
	}
	if !(Health{"Clash": false}).Down("Clash") {
		t.Error("an exit reported down is down")
	}
}

func TestProbeDefaultsAreFilledIn(t *testing.T) {
	got := Probe{Target: netip.MustParseAddrPort("1.1.1.1:443")}.Resolved()
	if got.Interval != DefaultProbeInterval || got.Timeout != DefaultProbeTimeout {
		t.Errorf("timings not resolved: %+v", got)
	}
	if got.Failures != DefaultProbeFailures || got.Successes != DefaultProbeSuccesses {
		t.Errorf("thresholds not resolved: %+v", got)
	}
	if got.Timeout >= got.Interval {
		t.Errorf("the defaults must not overlap: timeout %s, interval %s", got.Timeout, got.Interval)
	}
}
