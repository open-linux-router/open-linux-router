package dial

import (
	"context"
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier is the read and write path onto the uplink and the records.
//
// The two halves behave differently, and the difference is the module's shape —
// the same asymmetry internal/link/apply.go opens with:
//
//   - **A record stores intent and touches nothing.** Storing one changes
//     nothing on the box. What it changes is that olrd starts telling a third
//     party this box's address on a timer, which is a consequence of the
//     *publisher* picking the new config up (see HTTP.Watch) rather than of the
//     write. Its plan impact is honestly none, and the one consequence the
//     machine's state does not show — that this box starts talking to somebody
//     it never talked to before — reaches the operator as a plan warning.
//   - **The uplink is configuration and reaches the kernel.** Applying one puts
//     an address on the interface, brings it up, and replaces the default route
//     in the main table. That is the half this module did not have, and its
//     absence is why an operator with a modem-facing NIC had nowhere in olr to
//     put a gateway.
type Applier struct {
	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store

	// Links is the window onto adoption, addresses and networks, used by
	// validation and by the plan. Nil is legal and skips the rules that need
	// the box, which is what keeps the module testable without one.
	Links LinkView

	// Writer programs the uplink. Nil means NewWriter — the real kernel on
	// Linux, a refusal everywhere else.
	Writer Writer

	// Host takes the uplink's interface, and the box's own resolvers, from the
	// distribution's network configuration after every apply (internal/host).
	// A function for the reason link.Applier.Host is one; nil outside olrd.
	Host func(context.Context) ([]core.Step, error)
}

// writer resolves the zero value.
func (a Applier) writer() Writer {
	if a.Writer == nil {
		return NewWriter()
	}
	return a.Writer
}

// Observe reads back what the kernel has for the stored uplink's interface.
//
// Read fresh every time and never from a cache of what we last wrote. That is
// what makes drift mean something (design.md §5.4, §4.5): somebody who runs
// `ip route del default` by hand shows up here the same way a hand-edited
// config file does in the file-rendering modules.
func (a Applier) Observe(ctx context.Context, iface string) Observed {
	if iface == "" {
		return Observed{}
	}
	obs, err := a.writer().Observe(ctx, iface)
	if err != nil {
		// Tolerated rather than fatal, matching link.Applier.observeQuietly: a
		// machine whose kernel state cannot be read is not a reason to refuse
		// to store intent, and every rule that needs it produces a plan line
		// rather than an error.
		return Observed{}
	}
	return obs
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

// Plan compares a proposal against the stored config and the live kernel.
func (a Applier) Plan(ctx context.Context, desired Config) (planView, error) {
	stored, err := a.Load()
	if err != nil {
		return planView{}, err
	}
	return buildPlan(stored, desired, a.Links, a.Observe(ctx, desired.uplinkInterface())), nil
}

// Drift reports what the kernel would need in order to match what is stored.
//
// Planning the stored config against itself: the document diff is empty by
// construction, so anything left is the kernel disagreeing with intent. Before
// the uplink existed this was always empty, because the document was the only
// thing this module owned.
func (a Applier) Drift(ctx context.Context) (planView, error) {
	stored, err := a.Load()
	if err != nil {
		return planView{}, err
	}
	return buildPlan(stored, stored, a.Links, a.Observe(ctx, stored.uplinkInterface())), nil
}

// ApplyResult is what an apply actually did.
type ApplyResult struct {
	Plan  planView `json:"plan"`
	Steps []Step   `json:"steps"`
}

// Apply stores intent and then makes the kernel match it.
//
// Store first, program second, and the order is not arbitrary. Replacing the
// default route can take away the operator's own route to this box (§5.5's
// scenario, and its guard is not built yet) — so if exactly one of the two is
// going to survive, it must be the stored intent. A box whose config names a
// gateway and whose kernel does not is drift, visible on every surface and
// fixable by re-applying. A kernel that was re-routed against a config that was
// not is a box nobody can explain.
//
// Steps come back alongside any error rather than instead of it. There is no
// rollback (§5.2) — see the Writer interface for why a revert here is worse
// than the failure it would be recovering from.
func (a Applier) Apply(ctx context.Context, desired Config) (ApplyResult, error) {
	previous, err := a.Load()
	if err != nil {
		return ApplyResult{}, err
	}
	plan, err := a.Plan(ctx, desired)
	if err != nil {
		return ApplyResult{}, err
	}

	stored, err := a.Save(desired)
	if err != nil {
		return ApplyResult{Plan: plan}, err
	}

	// No uplink means nothing to program, and the writer is not called at all.
	// This is what keeps §7's promise intact for the DDNS half: adding a record
	// on a box with no uplink still touches nothing, still needs no kernel, and
	// still cannot fail for a reason that has nothing to do with what was
	// asked.
	want := DesiredFor(stored)
	want.RetireFrom, want.Retire = Retiring(previous, stored)
	result := ApplyResult{Plan: plan}
	if !want.Empty() {
		steps, err := a.writer().Apply(ctx, want)
		result.Steps = steps
		if err != nil {
			return result, fmt.Errorf("configuring the uplink: %w", err)
		}
	}

	// After the kernel, so the address and route are in place before the
	// distribution's DHCP client is told to stop providing its own — and run
	// even with no kernel work, because handing the uplink back is exactly
	// when the distribution gets the interface back.
	if a.Host != nil {
		steps, err := a.Host(ctx)
		for _, s := range steps {
			result.Steps = append(result.Steps, Step{Description: s.Description, Done: s.Done, Error: s.Error})
		}
		if err != nil {
			return result, fmt.Errorf("handing the uplink over from the distribution: %w", err)
		}
	}
	return result, nil
}

// Restore puts the uplink back on the box after a reboot.
//
// # Why this exists at all
//
// An address and a route are kernel state, and the kernel forgets both. Every
// other thing this product programs into the kernel is put back when olrd
// starts — nftables tables, RPDB entries, route tables, a WireGuard interface,
// and since link.Applier.Restore the router's own addresses — and an uplink
// olr owns has to be on that list for the same reason, only more so: the thing
// it would come back without is the box's way out.
//
// # Why it is not just Apply
//
// It does not save. Apply is the operator's path and writes the document
// because the operator said something new; startup has been told nothing, and a
// restore that rewrote olr.json on every boot would put a modification time on
// a file nobody edited. link.Applier.Restore makes the same distinction in the
// same words.
//
// It needs no AddOnly flag, unlike link's, because this writer is additive
// already — the uplink is the interface a distribution's DHCP client is most
// likely to also be acting on, so PlanUplink never claims the other addresses
// at any time, not just at boot.
//
// Failure is reported and never fatal, like every other start step: a box whose
// uplink cannot be programmed is exactly the box whose API has to come up,
// because the API is how it gets fixed.
func (a Applier) Restore(ctx context.Context) ([]Step, error) {
	cfg, err := a.Load()
	if err != nil {
		return nil, err
	}

	want := DesiredFor(cfg)
	if want.Empty() {
		// Same promise Apply keeps: a box with no uplink is one olr has not
		// been asked to connect, and startup touches nothing on it (§7).
		return nil, nil
	}

	steps, err := a.writer().Apply(ctx, want)
	if err != nil {
		return steps, fmt.Errorf("restoring the uplink: %w", err)
	}
	return steps, nil
}

// uplinkInterface is the interface to read the kernel for, or "".
func (c Config) uplinkInterface() string {
	if c.Uplink == nil {
		return ""
	}
	return c.Uplink.Interface
}
