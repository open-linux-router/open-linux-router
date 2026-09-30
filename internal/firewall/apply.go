package firewall

import (
	"context"
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier turns intent into kernel state. One netlink transaction per apply,
// so it is bounded the way design.md §3.6 requires of anything holding the
// global apply lock.
type Applier struct {
	Store    *core.Store
	Kernel   Kernel
	Boundary Boundary
}

// Load reads stored intent.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// save stores intent.
func (a Applier) save(c Config) error {
	doc, err := a.Store.Load()
	if err != nil {
		return err
	}
	raw, err := MarshalConfig(c)
	if err != nil {
		return err
	}
	doc.Set(ModuleName, raw)
	if err := a.Store.Save(doc); err != nil {
		return fmt.Errorf("storing configuration in %s: %w", a.Store.Path(), err)
	}
	return nil
}

// Plan answers "what would applying this do?" without doing it.
func (a Applier) Plan(ctx context.Context, c Config, via string) (Plan, Desired, error) {
	obs, err := a.Kernel.Observe(ctx)
	if err != nil {
		return Plan{}, Desired{}, err
	}
	return BuildPlan(c, a.Boundary, obs, via)
}

// ApplyPlanned stores intent, then programs what was planned.
//
// Store first, for design.md §5.3.2's reason: if programming fails the intent
// is on disk, and the next apply — or olrd's next start — finishes the job.
func (a Applier) ApplyPlanned(ctx context.Context, c Config, plan Plan, d Desired) ([]core.Step, error) {
	if err := a.save(c); err != nil {
		return nil, err
	}
	if plan.Empty {
		return nil, nil
	}
	const desc = "write nftables"
	if err := a.Kernel.Apply(ctx, d); err != nil {
		return []core.Step{{Description: desc, Error: err.Error()}}, err
	}
	return []core.Step{{Description: desc, Done: true}}, nil
}

// Reapply programs stored intent again. What olrd runs at startup and after
// another module changes something this one is built from: a network, a remote
// access server, a published service.
func (a Applier) Reapply(ctx context.Context) (Plan, []core.Step, error) {
	c, err := a.Load()
	if err != nil {
		return Plan{}, nil, err
	}
	plan, d, err := a.Plan(ctx, c, "")
	if err != nil {
		return plan, nil, err
	}
	steps, err := a.ApplyPlanned(ctx, c, plan, d)
	return plan, steps, err
}

// Status is the module's account of itself.
type Status struct {
	Enabled bool `json:"enabled"`

	// Known is whether the kernel could be read at all.
	Known bool `json:"known"`

	// Drifted is the plan against unchanged intent being non-empty.
	Drifted bool `json:"drifted"`
	Plan    Plan `json:"plan"`

	// Inside and Openings are what the ruleset is built from right now. The
	// openings list is the answer to "what can the internet reach here?", and
	// it is shown whether or not the firewall is on — with it off, the honest
	// answer is "these, and everything else".
	Inside   []string  `json:"inside"`
	Openings []Opening `json:"openings"`

	// BlockedInput and BlockedForward count packets turned away since the
	// table was last built, which every apply does.
	BlockedInput   uint64 `json:"blocked_input"`
	BlockedForward uint64 `json:"blocked_forward"`

	// Problem is why a plan could not be built — today only "no networks".
	Problem string `json:"problem,omitempty"`
}

// GetStatus reads everything fresh.
func (a Applier) GetStatus(ctx context.Context) (Status, error) {
	c, err := a.Load()
	if err != nil {
		return Status{}, err
	}
	st := Status{Enabled: c.Enabled, Inside: []string{}, Openings: []Opening{}}

	if inside, err := a.Boundary.Inside(); err == nil {
		st.Inside = normalizeInside(inside)
	}
	if openings, err := a.Boundary.Openings(); err == nil {
		st.Openings = normalizeOpenings(openings)
	}

	obs, err := a.Kernel.Observe(ctx)
	if err != nil {
		return st, err
	}
	st.Known = obs.Known
	st.BlockedInput = obs.Blocked[InputCounter]
	st.BlockedForward = obs.Blocked[ForwardCounter]

	plan, _, err := BuildPlan(c, a.Boundary, obs, "")
	if err != nil {
		st.Problem = err.Error()
		return st, nil
	}
	st.Plan = plan
	st.Drifted = obs.Known && !plan.Empty
	return st, nil
}
