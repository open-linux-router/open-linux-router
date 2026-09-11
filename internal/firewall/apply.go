package firewall

import (
	"context"
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier turns intent into kernel state.
//
// Much of the shape is internal/gateway's, minus the health prober: there is
// nothing to probe, because a forward has no far end whose liveness we could
// measure. Apply is bounded by construction, which is what design.md §3.6
// requires of anything holding the global apply lock — one netlink transaction,
// returning when it is acknowledged.
type Applier struct {
	// Kernel is the window onto what we program.
	Kernel Kernel

	// Links is the window onto the link module.
	Links LinkView

	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store
}

// Load reads stored intent out of the configuration document.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Save validates and stores intent, returning the stored form.
//
// Read-modify-write on the shared document, so a save here cannot drop another
// module's configuration. It is safe without further locking because every
// config write in the process holds the one global apply lock (§3.6) — the
// caller takes it.
//
// Storing is deliberately separate from programming: a config that cannot
// currently be applied — because the interface has not come up yet — is still a
// config the operator asked for, and losing it on the way to reporting the
// problem would make the problem worse.
func (a Applier) Save(cfg Config) (Config, error) {
	cfg.Normalize()
	if res := Validate(cfg, a.Links); !res.OK() {
		return cfg, res.Err()
	}

	doc, err := a.Store.Load()
	if err != nil {
		return cfg, err
	}
	data, err := MarshalConfig(cfg)
	if err != nil {
		return cfg, err
	}
	doc.Set(ModuleName, data)
	if err := a.Store.Save(doc); err != nil {
		return cfg, fmt.Errorf("storing configuration in %s: %w", a.Store.Path(), err)
	}
	return cfg, nil
}

// Observe reads the actual state of the system, fresh.
func (a Applier) Observe(ctx context.Context) (Observed, error) {
	return a.Kernel.Observe(ctx)
}

// Plan answers "what would applying this do?" without doing it.
func (a Applier) Plan(ctx context.Context, cfg Config) (Plan, Desired, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return Plan{}, Desired{}, err
	}
	return BuildPlan(cfg, a.Links, obs)
}

// Apply stores intent and programs it.
//
// The order is store-then-program, and it matters for recovery: if programming
// fails, the intent is on disk and a later `olr firewall apply` — or the next
// boot — finishes the job, which is design.md §5.3.2's idempotent re-apply
// rather than a rollback.
func (a Applier) Apply(ctx context.Context, cfg Config) (ApplyResult, Config, error) {
	plan, desired, err := a.Plan(ctx, cfg)
	if err != nil {
		return ApplyResult{Plan: plan}, cfg, err
	}
	return a.ApplyPlanned(ctx, cfg, plan, desired)
}

// ApplyPlanned programs a plan that has already been built.
//
// Split out of Apply for the caller that has to look at the plan before deciding
// whether to act on it — the HTTP layer refuses a `disruptive` change that was
// not confirmed (§5.3.3), and it can only know that by planning first. Without
// this split, acting on that decision would mean reading the kernel a second
// time, and the second read could disagree with the one the operator was shown.
func (a Applier) ApplyPlanned(ctx context.Context, cfg Config, plan Plan, desired Desired) (ApplyResult, Config, error) {
	stored, err := a.Save(cfg)
	if err != nil {
		return ApplyResult{Plan: plan}, stored, err
	}

	if plan.Empty() {
		// Nothing to program. Distinguished from "programmed successfully with
		// no steps" by there being no steps at all, which is what lets a caller
		// tell a no-op apply from a real one without re-deriving the plan.
		return ApplyResult{Plan: plan}, stored, nil
	}

	steps, err := a.Kernel.Apply(ctx, desired)
	return ApplyResult{Plan: plan, Steps: steps}, stored, err
}

// Status is the module's account of itself: what is configured, what is
// running, and what it cannot account for.
type Status struct {
	// Enabled is intent; Known is whether the kernel could be read at all.
	Enabled bool
	Known   bool

	// Forwards is one row per configured forward, with its counter.
	Forwards []ForwardStatus

	// Drifted reports whether the kernel disagrees with intent, and Plan carries
	// the detail. design.md §5.4: drift is not separate machinery, it is the
	// plan against unchanged intent.
	Drifted bool
	Plan    Plan

	// Foreign is somebody else's filtering, reported rather than hidden, because
	// a box where forwarded traffic is being dropped elsewhere is one whose
	// correct-looking forwards need explaining.
	Foreign []ForeignFilter
}

// ForwardStatus is one forward and what is true of it right now.
type ForwardStatus struct {
	Forward Forward

	// Counted reports whether the counter could be read at all, which is what
	// separates "nothing has arrived" from "we could not look". The two need
	// very different words on screen: the first is a diagnosis and the second
	// is an admission.
	Counted bool

	// Packets and Bytes are what has arrived through this forward since the
	// table was last built.
	//
	// Since the table was built, not since boot and not for all time: applying
	// any change to this module rebuilds the table and resets these
	// (docs/firewall.md §3.6). That is worth knowing before reading a small
	// number as evidence of nothing happening.
	Packets uint64
	Bytes   uint64
}

// GetStatus assembles the status, reading everything fresh.
func (a Applier) GetStatus(ctx context.Context) (Status, error) {
	cfg, err := a.Load()
	if err != nil {
		return Status{}, err
	}

	st := Status{Enabled: cfg.Enabled}

	obs, obsErr := a.Observe(ctx)
	if obsErr == nil {
		st.Known = obs.Known
		st.Foreign = obs.Foreign
		plan, _, err := BuildPlan(cfg, a.Links, obs)
		if err == nil {
			st.Plan = plan
			st.Drifted = obs.Known && !plan.Empty()
		}
	}

	for _, f := range cfg.Forwards {
		row := ForwardStatus{Forward: f}
		if c, ok := obs.Counters[f.Counter()]; ok {
			row.Counted = true
			row.Packets, row.Bytes = c.Packets, c.Bytes
		}
		st.Forwards = append(st.Forwards, row)
	}

	return st, obsErr
}
