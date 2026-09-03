package routing

import (
	"context"
	"log/slog"
	"net"
	"sync"
	"time"
)

// Exit health (docs/gateway.md §5.5).
//
// **A through-path probe, not a ping.** A crashed mihomo on a live Debian box
// answers ARP and ICMP indefinitely while forwarding nothing — and worse, loops
// our traffic back at us, because its own default gateway is us. So the only
// check worth running is one that traverses the exit and reaches something on
// the far side.
//
// This is not a later refinement. docs/dns.md §1.2 rests the whole topology
// rule — DHCP hands out olr, and only olr — on olr being able to re-point a
// dead exit, because IPv4 has no default-gateway failover at the device layer.
// Take this out and every device on the network is a single point of failure
// away from being offline until a human intervenes.
//
// It lives inside olrd by design.md §3.5's test: *does it have to keep running
// while olrd is stopped?* No — if olrd dies you lose failover, not
// connectivity, exactly as you lose live UI updates and not packets.

// Prober watches every exit that asks to be watched.
//
// Its output is a Health map, which is an *input* to Render rather than
// something it writes to the kernel itself. That indirection is what keeps
// design.md §5.4 honest: a tripped exit's unreachable route is then the
// expected state for the current config, so drift keeps meaning "the kernel
// disagrees with intent" instead of also meaning "an exit is down".
type Prober struct {
	// Dial opens a connection through a given exit. Nil means DialThrough,
	// which is the real one. Injectable so the hysteresis can be tested without
	// a network.
	Dial func(ctx context.Context, mark uint32, target string) error

	// OnChange is called whenever an exit's verdict flips, after the map has
	// been updated. It is how olrd re-applies the routing state and publishes
	// an event; nil means nobody is listening.
	//
	// Called without the prober's lock held, so the callback is free to read
	// Health() and to take the global apply lock without deadlocking against
	// the next probe tick.
	OnChange func(exit string, up bool)

	// Log is where transitions go. §5.6 obliges us to log every one of them:
	// an exit that flapped at 3am and recovered is the whole explanation for a
	// complaint the next morning, and it is invisible without this.
	Log *slog.Logger

	mu      sync.Mutex
	health  Health
	streaks map[string]int
	cancels map[string]context.CancelFunc
}

// NewProber returns a prober with nothing being watched yet.
func NewProber() *Prober {
	return &Prober{
		health:  Health{},
		streaks: map[string]int{},
		cancels: map[string]context.CancelFunc{},
	}
}

// Health implements HealthSource.
func (p *Prober) Health() Health {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(Health, len(p.health))
	for k, v := range p.health {
		out[k] = v
	}
	return out
}

// Watch reconciles the running probes against a config.
//
// Called after every apply. Exits that gained a probe start being watched,
// exits that lost one or were deleted stop, and an exit whose probe settings
// are unchanged keeps its current verdict and its streak — restarting a healthy
// probe on every unrelated config edit would reset the hysteresis that exists
// to stop it flapping.
func (p *Prober) Watch(ctx context.Context, c Config) {
	want := map[string]Exit{}
	for _, e := range c.Exits {
		// A blocked exit is never down, and an exit nobody routes through is
		// not worth opening connections about.
		if e.Probe == nil || e.Via.Kind == ViaBlocked || !c.InUse(e.Name) {
			continue
		}
		want[e.Name] = e
	}

	p.mu.Lock()
	var stop []context.CancelFunc
	for name, cancel := range p.cancels {
		if _, keep := want[name]; keep {
			continue
		}
		stop = append(stop, cancel)
		delete(p.cancels, name)
		delete(p.health, name)
		delete(p.streaks, name)
	}

	var start []Exit
	for name, e := range want {
		if _, running := p.cancels[name]; running {
			continue
		}
		// A new exit starts believed-up. Starting it down would mean every
		// added exit blackholes its traffic for the first few probe intervals,
		// which is a self-inflicted outage on the happy path.
		p.health[name] = true
		p.streaks[name] = 0
		start = append(start, e)
	}
	p.mu.Unlock()

	for _, cancel := range stop {
		cancel()
	}
	for _, e := range start {
		child, cancel := context.WithCancel(ctx)
		p.mu.Lock()
		p.cancels[e.Name] = cancel
		p.mu.Unlock()
		go p.run(child, e)
	}
}

// Stop ends every probe.
func (p *Prober) Stop() {
	p.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(p.cancels))
	for name, cancel := range p.cancels {
		cancels = append(cancels, cancel)
		delete(p.cancels, name)
	}
	p.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// run is one exit's probe loop.
func (p *Prober) run(ctx context.Context, e Exit) {
	cfg := e.Probe.Resolved()
	ticker := time.NewTicker(cfg.Interval.Duration())
	defer ticker.Stop()

	for {
		p.check(ctx, e, cfg)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// check runs one attempt and folds it into the streak.
func (p *Prober) check(ctx context.Context, e Exit, cfg Probe) {
	attempt, cancel := context.WithTimeout(ctx, cfg.Timeout.Duration())
	err := p.dial()(attempt, e.Mark(), cfg.Target.String())
	cancel()

	if ctx.Err() != nil {
		// Shutting down. A cancelled attempt is not evidence about the exit,
		// and counting it would mark everything down on the way out.
		return
	}

	changed, up := p.record(e.Name, err == nil, cfg)
	if !changed {
		return
	}

	if p.Log != nil {
		if up {
			p.Log.Info("exit recovered", "exit", e.Name, "target", cfg.Target)
		} else {
			p.Log.Warn("exit is not forwarding traffic",
				"exit", e.Name, "target", cfg.Target, "error", err,
				"behaviour", string(e.OnFailure.OrDefault()))
		}
	}
	if p.OnChange != nil {
		p.OnChange(e.Name, up)
	}
}

// record folds one result into the streak and reports whether the verdict
// flipped.
//
// Hysteresis in both directions (§5.5), and the two thresholds are deliberately
// different. Going down takes Failures consecutive misses so a single dropped
// packet cannot blackhole the network; coming back takes Successes so a proxy
// that is half-recovered and flapping does not drag the whole house with it.
// Flapping is worse than either steady state.
func (p *Prober) record(name string, ok bool, cfg Probe) (changed, up bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	was, known := p.health[name]
	if !known {
		was = true
	}

	// streaks counts consecutive results *against* the current verdict, so it
	// resets the moment one agrees with it.
	switch {
	case ok == was:
		p.streaks[name] = 0
		return false, was
	default:
		p.streaks[name]++
	}

	need := cfg.Failures
	if ok {
		need = cfg.Successes
	}
	if p.streaks[name] < need {
		return false, was
	}

	p.health[name] = ok
	p.streaks[name] = 0
	return true, ok
}

func (p *Prober) dial() func(context.Context, uint32, string) error {
	if p.Dial != nil {
		return p.Dial
	}
	return DialThrough
}

// dialTimeout bounds the dial itself, under the attempt's own context.
var dialer = net.Dialer{}
