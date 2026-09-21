package link

import (
	"context"
	"fmt"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Applier is the read and write path onto adoption and networks.
//
// The two halves behave differently on purpose, and the difference is the
// module's whole shape:
//
//   - **Adoption stores consent and touches nothing.** Adopting an interface
//     renders no file, reloads no daemon and changes no link, which is exactly
//     what §7's promise that "install alone changes nothing" needs. Its visible
//     effect is that a form somewhere else stops rejecting you.
//   - **A network is configuration and reaches the kernel.** Applying one puts
//     this router's address on its member interface. That is the half this
//     module did not have, and its absence is why `dhcp` could only validate a
//     range against an address configured outside olr — an error with no page
//     behind it.
//
// The asymmetry runs the other way too: removing a name, or a network, can make
// another module's stored config invalid. That is reported by that module,
// against its own field, which is why the UI warns before a release rather than
// after.
type Applier struct {
	// Store is core's configuration document, which owns this module's intent
	// alongside every other module's.
	Store *core.Store

	// Source reads the observed half. Nil means Kernel.
	Source Source

	// Writer programs addresses. Nil means NewWriter — the real kernel on
	// Linux, a refusal everywhere else.
	Writer Writer
}

// writer resolves the zero value.
func (a Applier) writer() Writer {
	if a.Writer == nil {
		return NewWriter()
	}
	return a.Writer
}

// Load reads stored intent out of the configuration document.
func (a Applier) Load() (Config, error) {
	doc, err := a.Store.Load()
	if err != nil {
		return Config{}, err
	}
	return FromDocument(doc)
}

// Observe reads the kernel's interfaces.
func (a Applier) Observe() ([]Interface, error) {
	if a.Source == nil {
		return Kernel()
	}
	return a.Source()
}

// observeQuietly reads the interfaces for validation, tolerating failure.
//
// A machine whose interface list cannot be read is not a reason to refuse to
// store consent: every rule that needs the list produces warnings, not errors,
// and the alternative is an operator locked out of adopting anything by a
// transient netlink failure. The name rules still apply, because they are the
// ones that can actually make the document meaningless.
func (a Applier) observeQuietly() []Interface {
	observed, err := a.Observe()
	if err != nil {
		return nil
	}
	return observed
}

// Save validates and stores intent, returning the stored form.
//
// Read-modify-write on the shared document, so a save here cannot drop another
// module's configuration. It is safe without further locking because every
// config write in the process holds the one global apply lock (§3.6) — the
// caller takes it.
func (a Applier) Save(cfg Config) (Config, error) {
	cfg.Normalize()
	if res := Validate(cfg, a.observeQuietly()); !res.OK() {
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
//
// The kernel half is read fresh every time and never from a cache of what we
// last wrote. That is what makes drift mean something (design.md §5.4, §4.5):
// somebody who runs `ip addr del` by hand has to show up here the same way a
// hand-edited config file does in the file-rendering modules.
func (a Applier) Plan(desired Config) (planView, error) {
	stored, err := a.Load()
	if err != nil {
		return planView{}, err
	}
	return buildPlan(stored, desired, a.observeQuietly()), nil
}

// Drift reports what the kernel would need in order to match what is stored.
//
// Planning the stored config against itself: the document diff is empty by
// construction, so anything left is the kernel disagreeing with intent.
func (a Applier) Drift() (planView, error) {
	stored, err := a.Load()
	if err != nil {
		return planView{}, err
	}
	return buildPlan(stored, stored, a.observeQuietly()), nil
}

// ApplyResult is what an apply actually did.
type ApplyResult struct {
	Plan  planView `json:"plan"`
	Steps []Step   `json:"steps"`
}

// Apply stores intent and then makes the kernel match it.
//
// Store first, program second, and the order is not arbitrary. An address
// change can take away the operator's own route to this box (§5.5's scenario,
// and its guard is not built yet) — so if exactly one of the two is going to
// survive, it must be the stored intent. A box whose config says 172.16.1.1 and
// whose kernel says otherwise is drift, visible on every surface and fixable by
// re-applying. A kernel that was renumbered against a config that was not is a
// box nobody can explain.
//
// Steps come back alongside any error rather than instead of it. There is no
// rollback (§5.2): a revert here would itself be an address change, attempted
// at the exact moment the evidence says address changes are failing.
func (a Applier) Apply(ctx context.Context, desired Config) (ApplyResult, error) {
	plan, err := a.Plan(desired)
	if err != nil {
		return ApplyResult{}, err
	}

	stored, err := a.Save(desired)
	if err != nil {
		return ApplyResult{Plan: plan}, err
	}

	// No networks means nothing to program, and the writer is not called at
	// all. This is what keeps adoption's promise intact: `olr adopt eth0` on a
	// box with no networks still touches nothing, still needs no kernel, and
	// still cannot fail for a reason that has nothing to do with what was asked
	// (§7 — "install alone changes nothing").
	want := DesiredFor(stored)
	if len(want) == 0 {
		return ApplyResult{Plan: plan}, nil
	}

	steps, err := a.writer().Apply(ctx, want)
	result := ApplyResult{Plan: plan, Steps: steps}
	if err != nil {
		return result, fmt.Errorf("configuring interfaces: %w", err)
	}
	return result, nil
}

// Restore puts stored addressing back on the interfaces after a reboot.
//
// # Why this exists at all
//
// An address is kernel state, and the kernel forgets it. Every other thing
// this product programs into the kernel is put back when olrd starts —
// nftables tables, RPDB entries, route tables, a WireGuard interface — and
// `link` was simply never added to that list. The result was a box that came
// back from a reboot with its configuration intact and its router address
// gone: dnsmasq with no address inside the range it serves, a `dns` render
// deriving allow_from from an interface that no longer carried the LAN, and a
// gateway whose policy pointed at a network the box was no longer on. One
// missing line, three modules visibly broken, and nothing in olr saying why.
//
// # Why it is not just Apply
//
// Two differences, and both matter.
//
// It does not save. Apply is the operator's path and writes the document
// because the operator said something new; startup has been told nothing, and
// a restore that rewrote olr.json on every boot would put a modification time
// on a file nobody edited.
//
// It is additive (Desired.AddOnly). Apply enforces PlanAddrs' ownership claim,
// which is correct when a human said what an interface's addressing is, and
// is a race at boot against every other address source on the box. AddOnly's
// comment has the argument and the concrete way it bites.
//
// Failure is reported and never fatal, like every other start step: a box
// whose addresses cannot be programmed is exactly the box whose API has to
// come up, because the API is how it gets fixed.
func (a Applier) Restore(ctx context.Context) ([]Step, error) {
	cfg, err := a.Load()
	if err != nil {
		return nil, err
	}

	want := DesiredFor(cfg)
	if len(want) == 0 {
		// Same promise Apply keeps: a box with no networks is one olr has not
		// been asked to address, and startup touches nothing on it (§7).
		return nil, nil
	}
	for i := range want {
		want[i].AddOnly = true
	}

	steps, err := a.writer().Apply(ctx, want)
	if err != nil {
		return steps, fmt.Errorf("restoring interface addressing: %w", err)
	}
	return steps, nil
}
