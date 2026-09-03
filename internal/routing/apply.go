package routing

import (
	"context"
	"fmt"
	"net/netip"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier turns intent into kernel state.
//
// Much of the shape is internal/dhcp's, minus everything to do with a backend:
// there is no config file to render, no unit to reload and no port to check,
// because what this module drives is the kernel itself. Apply is therefore
// bounded by construction, which is what design.md §3.6 requires of anything
// holding the global apply lock — a handful of netlink calls, returning when
// they are acknowledged rather than when traffic has converged.
type Applier struct {
	// Kernel is the window onto what we program.
	Kernel Kernel

	// Links is the window onto the link module.
	Links LinkView

	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store

	// Probes reports what the health checker currently believes. Nil means
	// every exit is treated as up, which is the right answer when nothing is
	// probing — an exit we are not watching must not be assumed dead.
	Probes HealthSource
}

// HealthSource is where exit health comes from.
//
// An interface rather than the prober itself, so that planning and applying can
// be tested without one running, and so that the prober can be absent entirely
// on a build with no network.
type HealthSource interface {
	// Health reports up/down per exit name. Exits it has no opinion about are
	// absent from the map and are treated as up.
	Health() Health
}

func (a Applier) health() Health {
	if a.Probes == nil {
		return nil
	}
	return a.Probes.Health()
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
// currently be applied — because the tunnel interface has not come up yet, or
// because another tool is holding the routing table — is still a config the
// operator asked for, and losing it on the way to reporting the problem would
// make the problem worse.
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
//
// admin is the address the request arrived from, or the zero value for a local
// caller over the unix socket. It is what lets the plan warn that a change
// would route the operator's own connection somewhere else — §5.1's lockout
// row, and the one mistake here that cannot be undone by clicking again.
func (a Applier) Plan(ctx context.Context, cfg Config, admin netip.Addr) (Plan, Desired, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return Plan{}, Desired{}, err
	}
	return BuildPlan(cfg, a.Links, a.health(), obs, admin)
}

// Apply stores intent and programs it.
//
// The order is store-then-program, and it matters for recovery: if programming
// fails, the intent is on disk and a later `olr routing apply` — or the next
// boot — finishes the job, which is design.md §5.3.2's idempotent re-apply
// rather than a rollback.
func (a Applier) Apply(ctx context.Context, cfg Config, admin netip.Addr) (ApplyResult, Config, error) {
	plan, desired, err := a.Plan(ctx, cfg, admin)
	if err != nil {
		return ApplyResult{Plan: plan}, cfg, err
	}
	if plan.Blocked != "" {
		// §6: detect and refuse. We do not rewrite somebody else's config file
		// and we do not silently work around them, because the failure mode of
		// sharing the routing table is that it works until a version bump moves
		// a priority number and then some traffic quietly takes the wrong path.
		return ApplyResult{Plan: plan}, cfg, fmt.Errorf("%s", plan.Blocked)
	}

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
// effective, what is running, and what it cannot account for.
type Status struct {
	// Enabled is intent; Known is whether the kernel could be read at all.
	Enabled bool
	Known   bool

	// Exits is one row per configured exit, with its effective state.
	Exits []ExitStatus

	// Assignments is §2.2's effective value with its source, per network.
	Assignments []AssignmentStatus

	// Drifted reports whether the kernel disagrees with intent, and Plan
	// carries the detail. design.md §5.4: drift is not separate machinery, it
	// is the plan against unchanged intent.
	Drifted bool
	Plan    Plan

	// Foreign is §7.3's residual rule at the routing layer: someone else's
	// rules, reported rather than hidden, because a hand-rolled setup that is
	// visible is one somebody can reason about.
	Foreign []ForeignRule
}

// ExitStatus is one exit and what is true of it right now.
type ExitStatus struct {
	Exit Exit

	// Up is the prober's verdict. Probed says whether anybody asked.
	Up     bool
	Probed bool

	// UsedBy lists the networks whose traffic goes through it.
	UsedBy []string

	// Mark, Table and Priority are the kernel resources it holds, surfaced
	// because §3.2's whole argument for documenting the ranges is that somebody
	// can plan around them — which they cannot do without being told the
	// numbers this box actually used.
	Mark     uint32
	Table    int
	Priority int
}

// AssignmentStatus is one network's effective exit and where it came from.
type AssignmentStatus struct {
	Interface string
	Exit      string
	Source    Source

	// Reason explains a state that is not simply "via this exit" — an exit that
	// is down, or a network that falls back to the box's own route. This is
	// §2.2's *"no internet — Clash is down"*, which is the sentence that makes
	// a failure diagnosable in the place the operator is already looking.
	Reason string
}

// GetStatus assembles the status, reading everything fresh.
func (a Applier) GetStatus(ctx context.Context) (Status, error) {
	cfg, err := a.Load()
	if err != nil {
		return Status{}, err
	}

	health := a.health()
	st := Status{Enabled: cfg.Enabled}

	obs, obsErr := a.Observe(ctx)
	if obsErr == nil {
		st.Known = obs.Known
		st.Foreign = obs.Foreign
		// The zero address: status is a read, so there is no caller whose
		// connection could be moved by it.
		plan, _, err := BuildPlan(cfg, a.Links, health, obs, netip.Addr{})
		if err == nil {
			st.Plan = plan
			st.Drifted = obs.Known && !plan.Empty()
		}
	}

	for _, e := range cfg.Exits {
		up, probed := health[e.Name]
		st.Exits = append(st.Exits, ExitStatus{
			Exit:     e,
			Up:       up || !probed,
			Probed:   probed,
			UsedBy:   cfg.UsedBy(e.Name),
			Mark:     e.Mark(),
			Table:    e.Table(),
			Priority: e.Priority(),
		})
	}

	for _, as := range cfg.Interfaces {
		name, source := cfg.Assigned(as.Interface)
		row := AssignmentStatus{Interface: as.Interface, Exit: name, Source: source}
		switch {
		case name == "":
			row.Reason = "uses the box's own connection"
		case health.Down(name):
			e, _ := cfg.Find(name)
			if e.OnFailure.OrDefault() == FailDirect {
				row.Reason = fmt.Sprintf("%s is not responding, so traffic is using the box's own connection", name)
			} else {
				row.Reason = fmt.Sprintf("no internet — %s is not responding", name)
			}
		}
		st.Assignments = append(st.Assignments, row)
	}

	return st, obsErr
}
