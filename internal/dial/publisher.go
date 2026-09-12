package dial

import (
	"context"
	"log/slog"
	"net/netip"
	"sync"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/dial/provider"
)

// The mechanism (docs/ddns.md §6). Ours, not upstream's.
//
// ddns-go's RunTimer is `for { RunOnce(); sleep }`, which is the right shape for
// a program with one job and the wrong one for a module that has to answer
// `status` and take part in design.md §5's apply semantics. Three rules come out
// of that and every one of them is visible below:
//
//   - **A check is not an update.** The address is read on a schedule; the
//     provider is called only when it changed. The cache is ours and it starts
//     empty after a restart, so the first check after a restart always
//     publishes — which is also the repair path for a record somebody changed by
//     hand at the provider.
//   - **Backoff is on the provider, not on the address.** A rejected update
//     backs off, because providers rate-limit and some of them ban. A failed
//     address read does not, because it costs a stranger nothing and the next
//     tick is the whole of the retry that case needs.
//   - **Three separate facts in status.** When the address was last read, what
//     it was, and whether the last publish attempted with it succeeded.
//     Collapsing them into one "OK" is how the failure this module exists to
//     make visible becomes invisible again.
//
// It lives inside olrd by design.md §3.5's test — *does it have to keep running
// while olrd is stopped?* No: with olrd down the name stops being updated, which
// is a loss of freshness rather than of service, exactly as you lose live UI
// updates and not packets.

// MaxBackoff caps the wait after repeated provider failures.
//
// An hour rather than something longer, because the common cause is a credential
// that was just rotated and is about to be fixed, and an operator who corrects it
// should not wait a day to find out. Every config edit restarts the record's
// loop anyway, so a correction through the API takes effect at once — this cap
// is for the box nobody is watching.
const MaxBackoff = time.Hour

// RecordState is what status knows about one record.
//
// Three groups of fields, matching the three questions docs/ddns.md §6 insists
// stay separate. A record can be in the state where Checked is a minute ago,
// Address is correct, and Published is four days ago with PublishError set —
// and that state has to be describable, because it is the failure this whole
// module was arranged around.
type RecordState struct {
	// Checked is when the address was last read, successfully or not. It is
	// stamped on every attempt, so "the reflector has been unreachable for two
	// hours" is visible as a fresh Checked with CheckError set — not as a stale
	// timestamp that could equally mean olrd is wedged.
	Checked time.Time `json:"checked,omitempty"`

	// CheckError is why the last read failed, empty when it did not. This is
	// the first row of docs/ddns.md §7's table and the failure upstream's return
	// value cannot express — the reason the provider code is ported rather than
	// imported.
	CheckError string `json:"check_error,omitempty"`

	// Address is the last address successfully read. It survives a failed read,
	// on purpose: it is the last thing we knew, and CheckError beside it says
	// it may be old.
	Address string `json:"address,omitempty"`

	// CGNAT reports that Address is inside 100.64.0.0/10, so the name resolves
	// to something nobody outside the ISP can reach (docs/ddns.md §3.2).
	CGNAT bool `json:"cgnat,omitempty"`

	// Published is when the provider last accepted this record, and
	// PublishedAddress is what it accepted. The pair is the cache: an address
	// equal to PublishedAddress is not sent again.
	Published        time.Time `json:"published,omitempty"`
	PublishedAddress string    `json:"published_address,omitempty"`

	// PublishError is the provider's own words about the last refusal, empty
	// when the last attempt succeeded.
	PublishError string `json:"publish_error,omitempty"`

	// Failures counts consecutive refusals, and Retry is when the next attempt
	// is due. Reported rather than kept private because "it will try again in
	// forty minutes" is the difference between an operator waiting and an
	// operator restarting things.
	Failures int       `json:"failures,omitempty"`
	Retry    time.Time `json:"retry,omitempty"`
}

// Publisher keeps every configured name pointing at the address it names.
//
// Modelled on internal/gateway's Prober, down to the reconcile-on-every-apply
// shape of Watch, because the two solve the same problem: a set of long-running
// per-object goroutines that has to follow a config the operator keeps editing.
type Publisher struct {
	// Read reads one record's address. Nil means a Discoverer over Links, which
	// is the real one. Injectable so the schedule can be tested without a
	// network — the same reason Prober.Dial is.
	Read func(ctx context.Context, rec Record) (netip.Addr, error)

	// Update publishes one record. Nil means the provider package. Injectable
	// for the same reason, and separately from Read so a test can fail one
	// without the other — which is the distinction the backoff rule turns on.
	Update func(ctx context.Context, rec Record, address string) error

	// Links is what the default Read uses. Ignored when Read is set.
	Links LinkView

	// Now is the clock. Nil means time.Now.
	Now func() time.Time

	// Log is where publishes and failures go. docs/ddns.md §7 makes this the
	// record of what happened while nobody was watching, which is the only form
	// of it that exists — there is no history here.
	Log *slog.Logger

	mu      sync.Mutex
	states  map[string]RecordState
	running map[string]Record
	cancels map[string]context.CancelFunc
}

// NewPublisher returns a publisher with nothing being published yet.
func NewPublisher() *Publisher {
	return &Publisher{
		states:  map[string]RecordState{},
		running: map[string]Record{},
		cancels: map[string]context.CancelFunc{},
	}
}

// States returns a copy of what is known about every record.
func (p *Publisher) States() map[string]RecordState {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]RecordState, len(p.states))
	for k, v := range p.states {
		out[k] = v
	}
	return out
}

// Watch reconciles the running loops against a config.
//
// Called after every apply. A record whose settings are unchanged keeps its
// loop, its state and — the part that matters — its published-address cache:
// restarting it on an unrelated edit elsewhere in the document would empty that
// cache and send a redundant update to the provider on the next tick. On a box
// whose config is edited a few times in an afternoon, that is the difference
// between four provider calls and none.
func (p *Publisher) Watch(ctx context.Context, c Config) {
	want := make(map[string]Record, len(c.Records))
	for _, rec := range c.Records {
		want[rec.Name] = rec
	}

	p.mu.Lock()
	var stop []context.CancelFunc
	for name, cancel := range p.cancels {
		if next, keep := want[name]; keep && next == p.running[name] {
			continue
		}
		stop = append(stop, cancel)
		delete(p.cancels, name)
		delete(p.running, name)
		// The state goes with it. A record whose provider or source changed has
		// a published address that was published somewhere else, and keeping it
		// would suppress the first update to the new place.
		delete(p.states, name)
	}

	var start []Record
	for name, rec := range want {
		if _, running := p.cancels[name]; running {
			continue
		}
		p.states[name] = RecordState{}
		p.running[name] = rec
		start = append(start, rec)
	}
	p.mu.Unlock()

	for _, cancel := range stop {
		cancel()
	}
	for _, rec := range start {
		child, cancel := context.WithCancel(ctx)
		p.mu.Lock()
		p.cancels[rec.Name] = cancel
		p.mu.Unlock()
		go p.run(child, rec)
	}
}

// Stop ends every loop.
func (p *Publisher) Stop() {
	p.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(p.cancels))
	for name, cancel := range p.cancels {
		cancels = append(cancels, cancel)
		delete(p.cancels, name)
		delete(p.running, name)
	}
	p.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// run is one record's loop.
func (p *Publisher) run(ctx context.Context, rec Record) {
	ticker := time.NewTicker(rec.ResolvedInterval())
	defer ticker.Stop()

	for {
		p.Check(ctx, rec)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check runs one cycle: read the address, and publish it if it moved.
//
// Exported so the tests can drive the schedule without goroutines or a clock,
// the way internal/gateway's do with Prober.record.
func (p *Publisher) Check(ctx context.Context, rec Record) {
	now := p.now()

	addr, err := p.read()(ctx, rec)
	if ctx.Err() != nil {
		// Shutting down. A cancelled read is not evidence about anything, and
		// recording it would leave every record wearing an error on the way out.
		return
	}
	if err != nil {
		p.failedCheck(rec.Name, now, err)
		if p.Log != nil {
			p.Log.Warn("could not read the address to publish",
				"record", rec.Name, "source", string(rec.Source), "error", err)
		}
		return
	}

	state := p.readCheck(rec.Name, now, addr)

	if state.CGNAT && p.Log != nil {
		// Said once per check rather than once ever, because it is a condition
		// rather than an event and the journal is where somebody looks after
		// the fact. §3.2: this is the diagnosis nothing else in the product can
		// offer.
		p.Log.Warn("the address to publish is behind carrier-grade NAT, so the name will not be reachable",
			"record", rec.Name, "address", addr.String())
	}

	switch {
	case state.PublishedAddress == addr.String() && state.PublishError == "":
		// A check is not an update.
		return
	case !state.Retry.IsZero() && now.Before(state.Retry):
		// Backing off from a refusal. The address has still been read and
		// status still says so — only the request to the provider waits.
		return
	}

	err = p.update()(ctx, rec, addr.String())
	if ctx.Err() != nil {
		return
	}
	p.publishResult(rec, p.now(), addr, err)
}

// failedCheck records a read that did not answer.
//
// It sets no backoff, which is the rule rather than an omission: reading an
// address costs a stranger nothing, so the next tick is the whole retry this
// case needs (docs/ddns.md §6). It also leaves Address and the published cache
// alone — the last thing we knew stays visible beside the error saying it may
// be old, and a reflector outage does not cause a redundant republish when it
// ends.
func (p *Publisher) failedCheck(name string, now time.Time, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.states[name]
	state.Checked = now
	state.CheckError = err.Error()
	p.states[name] = state
}

// readCheck records a successful read and returns the state to act on.
func (p *Publisher) readCheck(name string, now time.Time, addr netip.Addr) RecordState {
	p.mu.Lock()
	defer p.mu.Unlock()
	state := p.states[name]
	state.Checked = now
	state.CheckError = ""
	state.Address = addr.String()
	state.CGNAT = IsCGNAT(addr)
	p.states[name] = state
	return state
}

// publishResult folds the provider's answer into the state.
func (p *Publisher) publishResult(rec Record, now time.Time, addr netip.Addr, err error) {
	p.mu.Lock()
	state := p.states[rec.Name]
	if err == nil {
		state.Published = now
		state.PublishedAddress = addr.String()
		state.PublishError = ""
		state.Failures = 0
		state.Retry = time.Time{}
	} else {
		state.PublishError = err.Error()
		state.Failures++
		state.Retry = now.Add(backoff(rec.ResolvedInterval(), state.Failures))
	}
	p.states[rec.Name] = state
	p.mu.Unlock()

	if p.Log == nil {
		return
	}
	if err == nil {
		p.Log.Info("published", "record", rec.Name,
			"address", addr.String(), "provider", rec.Provider)
		return
	}
	p.Log.Error("the provider refused the update, so the name is now out of date",
		"record", rec.Name, "address", addr.String(), "provider", rec.Provider,
		"error", err, "failures", state.Failures, "retry", state.Retry)
}

// backoff is the wait after n consecutive refusals.
//
// Doubling from the check interval, capped. It starts at the interval rather
// than at a second because the first retry of a rejected update is almost never
// going to be accepted — the causes are a wrong credential, a revoked token or a
// rate limit, and none of them clears in a second.
func backoff(interval time.Duration, failures int) time.Duration {
	if failures < 1 {
		failures = 1
	}
	wait := interval
	for i := 1; i < failures; i++ {
		wait *= 2
		if wait >= MaxBackoff {
			return MaxBackoff
		}
	}
	if wait > MaxBackoff {
		return MaxBackoff
	}
	return wait
}

func (p *Publisher) now() time.Time {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now()
}

func (p *Publisher) read() func(context.Context, Record) (netip.Addr, error) {
	if p.Read != nil {
		return p.Read
	}
	d := Discoverer{Links: p.Links}
	return d.Address
}

// update is the default publish path: pick the provider by name, hand it the
// record. The client is nil, so the provider uses http.DefaultClient — the
// binding that matters is on the *address read*, not on the update, because
// which way out a provider's API is reached does not change what gets written.
func (p *Publisher) update() func(context.Context, Record, string) error {
	if p.Update != nil {
		return p.Update
	}
	return func(ctx context.Context, rec Record, address string) error {
		impl, err := provider.For(string(rec.Provider), nil)
		if err != nil {
			return err
		}
		return impl.Update(ctx, rec.ProviderRecord(address))
	}
}
