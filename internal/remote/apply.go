package remote

import (
	"context"
	"errors"
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier turns intent into a running tunnel.
//
// There is no rollback (design.md §5.2/§5.3.2). If a multi-step change fails
// halfway, the steps that landed stay landed and are reported; re-running
// finishes the job. That is why Steps records outcomes rather than being
// discarded on error — and it matters more here than in a file-rendering
// module, because a half-applied tunnel is one an operator may be looking at
// from the outside, unable to get in to fix it.
type Applier struct {
	// Kernel is the window onto what this module programs. Nil means
	// NewKernel — the real one on Linux, a refusal everywhere else.
	Kernel Kernel

	// Networks is the window onto `link`'s networks: what `routes: home`
	// expands to, and what the dial-in subnet must not overlap.
	Networks NetworkView

	// Store is core's configuration document.
	Store *core.Store

	// PortInUse reports whether something already holds the tunnel's UDP port.
	// Nil means core.UDPPortInUse. Injectable so the refusal path is testable
	// without arranging a real conflict on the build machine.
	PortInUse func(port uint64) (bool, error)
}

func (a Applier) kernel() Kernel {
	if a.Kernel == nil {
		return NewKernel()
	}
	return a.Kernel
}

func (a Applier) portInUse() func(uint64) (bool, error) {
	if a.PortInUse != nil {
		return a.PortInUse
	}
	return core.UDPPortInUse
}

// Load reads stored intent out of the configuration document.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Save stores intent without programming anything.
//
// Read-modify-write on the shared document, so a save here cannot drop another
// module's configuration. It is safe without further locking because every
// config write in this process holds the one global apply lock (§3.6) — the
// caller takes it.
func (a Applier) Save(c Config) error {
	doc, err := a.Store.Load()
	if err != nil {
		return err
	}
	data, err := MarshalConfig(c)
	if err != nil {
		return err
	}
	doc.Set(ModuleName, data)
	if err := a.Store.Save(doc); err != nil {
		return fmt.Errorf("storing configuration in %s: %w", a.Store.Path(), err)
	}
	return nil
}

// Observe reads the actual state of the tunnel, fresh.
//
// The interface it looks at comes from stored intent, which is the one place
// this module reads config in order to read the system. That is unavoidable:
// "what does the kernel hold" is not a question that can be asked without
// saying which interface, and an operator who renamed it would otherwise have
// olr observing a device nobody configured.
func (a Applier) Observe(ctx context.Context) (Observed, error) {
	cfg, err := a.Load()
	if err != nil {
		return Observed{}, err
	}
	return a.ObserveFor(ctx, cfg)
}

// ObserveFor reads the state of the interface a given config names.
func (a Applier) ObserveFor(ctx context.Context, c Config) (Observed, error) {
	return a.kernel().Observe(ctx, c.WireGuard.InterfaceOrDefault())
}

// Plan reports what applying desired would do, without doing it.
//
// The stored config is read as well as the proposed one, because a device that
// is being removed is in the kernel and in the store and nowhere else — see
// BuildPlan. A store that cannot be read is not a reason to refuse to plan: the
// worst that follows is a revoked device named by its key.
func (a Applier) Plan(ctx context.Context, desired Config) (Plan, Desired, error) {
	obs, err := a.ObserveFor(ctx, desired)
	if err != nil {
		return Plan{}, Desired{}, err
	}
	stored, _ := a.Load()
	return BuildPlan(desired, stored, a.Networks, obs)
}

// Drift reports what has changed underneath stored intent — design.md §5.4 in
// one line. A non-empty plan is drift.
func (a Applier) Drift(ctx context.Context) (Plan, error) {
	current, err := a.Load()
	if err != nil {
		return Plan{}, err
	}
	plan, _, err := a.Plan(ctx, current)
	return plan, err
}

// Apply stores the config and brings the tunnel into line with it. It applies
// immediately and is in effect on return (design.md §5.1).
func (a Applier) Apply(ctx context.Context, desired Config) (ApplyResult, error) {
	plan, d, err := a.Plan(ctx, desired)
	if err != nil {
		return ApplyResult{Plan: plan}, err
	}
	return a.ApplyPlanned(ctx, desired, plan, d)
}

// ApplyPlanned applies a plan that has already been built and looked at.
//
// Split out of Apply for the HTTP surface, which has to do something between
// the two halves: a disruptive plan is *held* and returned for confirmation
// rather than applied (http.go). Re-planning after that decision would be a
// second observation, so the plan the operator agreed to would not be the plan
// that lands — and "this device loses its way in" is exactly the kind of thing
// that must not change shape between being shown and being done.
func (a Applier) ApplyPlanned(ctx context.Context, desired Config, plan Plan, d Desired) (ApplyResult, error) {
	result := ApplyResult{Plan: plan}
	run := func(description string, fn func() error) error {
		err := fn()
		step := Step{Description: description, Done: err == nil}
		if err != nil {
			step.Error = err.Error()
		}
		result.Steps = append(result.Steps, step)
		return err
	}

	if plan.Blocked != "" {
		return result, errors.New(plan.Blocked)
	}

	// Intent is stored first, before anything it describes, so that a failed
	// later step leaves a re-run something to finish from (§5.3.2). It matters
	// especially here: a key generated and not stored is a key every client
	// configuration already names and nothing can reproduce.
	if err := run("store configuration in "+a.Store.Path(), func() error {
		return a.Save(desired)
	}); err != nil {
		return result, err
	}

	if plan.Empty() {
		return result, nil
	}

	// Only when the interface does not exist yet. Once it does, the port is
	// held by our own tunnel, and a check here would report the box conflicting
	// with itself on every subsequent apply.
	if d.Enabled {
		obs, err := a.ObserveFor(ctx, desired)
		if err == nil && !obs.Present {
			if err := run(fmt.Sprintf("check nothing else holds UDP/%d", d.ListenPort), func() error {
				held, err := a.portInUse()(uint64(d.ListenPort))
				if err != nil || !held {
					return err
				}
				return ErrPortInUse(d.ListenPort)
			}); err != nil {
				return result, err
			}
		}
	}

	steps, err := a.kernel().Apply(ctx, d)
	result.Steps = append(result.Steps, steps...)
	if err != nil {
		return result, fmt.Errorf("configuring %s: %w", d.Interface, err)
	}
	return result, nil
}

// Blockers reports what stands between this module and working.
//
// Published from status on every read, including while remote access is
// switched off — that is when it matters, because an operator turning it on
// should meet the missing package before the failure rather than after it.
func Blockers() []core.Blocker { return core.DependencyBlockers(Dependencies()) }

// ErrPortInUse explains a refused start, naming the command that finds the
// incumbent.
//
// Deliberately not behind a build tag, even though the detection is: finding a
// held port needs procfs, explaining one is string formatting, and splitting
// them keeps the message identical on every platform — which matters because
// the refusal text is worth testing and the alternative is a test that only
// passes on Linux.
func ErrPortInUse(port uint16) error {
	return fmt.Errorf(
		"UDP/%d is already in use, so something else on this box is listening where the tunnel "+
			"would.\nolr will not take a port from another daemon. Find the holder with "+
			"`ss -lunp 'sport = :%d'` and stop it, or give the tunnel a different port with "+
			"`olr remote set --port <port>`",
		port, port)
}

// GenerateFor fills in the key a tunnel cannot work without.
//
// Called on the write path rather than at startup, and returning the config
// rather than mutating the store, so that generating a key is part of the one
// edit the apply lock covers. A box that generated a key and then failed to
// store it would hand out client configurations naming a public key it no
// longer has.
func GenerateFor(c Config) (Config, error) {
	if c.WireGuard.PrivateKey != "" {
		return c, nil
	}
	pair, err := GenerateKey()
	if err != nil {
		return c, err
	}
	out := c.Clone()
	out.WireGuard.PrivateKey = pair.Private
	return out, nil
}
