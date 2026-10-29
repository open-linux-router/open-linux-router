package remote

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// ProxyApplier turns the proxy's intent into a running daemon.
//
// internal/ingress's shape, minus the parts that were about certificates: write
// the file, check the unit exists, move it. There is no rollback (design.md
// §5.2/§5.3.2) — if a multi-step change fails halfway the steps that landed
// stay landed and are reported, and re-running finishes the job.
type ProxyApplier struct {
	// Store is the module's document, shared with the tunnel's applier.
	Store

	// Paths is the on-disk layout.
	Paths Paths

	// Unit supervises the backend.
	Unit core.Unit

	Locate func() (string, error)

	// Settle is how long apply watches a started backend before believing it.
	// Zero means DefaultSettle; negative means check once and return.
	Settle time.Duration

	Object ProxyObject
}

const DefaultSettle = 1500 * time.Millisecond

const settleInterval = 250 * time.Millisecond

func (a ProxyApplier) settleWindow() time.Duration {
	switch {
	case a.Settle == 0:
		return DefaultSettle
	case a.Settle < 0:
		return 0
	default:
		return a.Settle
	}
}

func (a ProxyApplier) locate() func() (string, error) {
	if a.Locate != nil {
		return a.Locate
	}
	return a.Object.kind().Locate
}

// Observe reads the actual state of the system, every field fresh.
func (a ProxyApplier) Observe(ctx context.Context) (ProxyObserved, error) {
	obs := ProxyObserved{Files: map[string][]byte{}}

	path := a.Object.kind().Conf(a.Paths)
	data, err := os.ReadFile(path)
	if err == nil {
		obs.Files[path] = data
	} else if !os.IsNotExist(err) {
		return ProxyObserved{}, fmt.Errorf("reading %s: %w", path, err)
	}

	// A missing service manager is reported as "not running" rather than
	// failing the observation: the file half is still useful, and status
	// surfaces the service error on its own line.
	if a.Unit != nil {
		if status, err := a.Unit.Status(ctx); err == nil {
			obs.ServiceKnown = true
			obs.Running = status.Active
			obs.EnabledAtBoot = status.Enabled
			obs.Installed = status.Installed
		}
	}

	return obs, nil
}

// Plan reports what applying desired would do, without doing it.
func (a ProxyApplier) Plan(ctx context.Context, desired Config) (ProxyPlan, Rendered, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return ProxyPlan{}, Rendered{}, err
	}
	stored, _ := a.Load()
	return a.Object.kind().Build(desired, stored, a.Paths, obs)
}

// Apply writes the config and brings the proxy into line with it. It applies
// immediately and is in effect on return (design.md §5.1).
func (a ProxyApplier) Apply(ctx context.Context, desired Config) (ProxyApplyResult, error) {
	plan, rendered, err := a.Plan(ctx, desired)
	if err != nil {
		return ProxyApplyResult{Plan: plan}, err
	}
	return a.ApplyPlanned(ctx, desired, plan, rendered)
}

// ProxyApplyResult is what an apply actually did.
type ProxyApplyResult struct {
	Plan  ProxyPlan `json:"plan"`
	Steps []Step    `json:"steps"`
}

// ApplyPlanned applies a plan that has already been built and looked at.
//
// Split out of Apply for the HTTP surface, which has to do something between
// the two halves: a disruptive plan is held and returned for confirmation
// rather than applied. Re-planning after that decision would be a second
// observation, so the plan the operator agreed to would not be the plan that
// lands.
func (a ProxyApplier) ApplyPlanned(ctx context.Context, desired Config, plan ProxyPlan, rendered Rendered) (ProxyApplyResult, error) {
	kind := a.Object.kind()
	result := ProxyApplyResult{Plan: plan}
	run := func(description string, fn func() error) error {
		err := fn()
		step := Step{Description: description, Done: err == nil}
		if err != nil {
			step.Error = err.Error()
		}
		result.Steps = append(result.Steps, step)
		return err
	}

	// Intent is stored first, before anything it describes, so that a failed
	// later step leaves a re-run something to finish from (§5.3.2). It matters
	// especially here: a password generated and not stored is a password every
	// link already names and nothing can reproduce.
	if err := run("store configuration in "+a.Path(), func() error {
		return a.Save(desired)
	}); err != nil {
		return result, err
	}

	if plan.Empty() {
		return result, nil
	}

	if len(rendered.Files) > 0 {
		dir := filepath.Dir(a.Object.kind().Conf(a.Paths))
		if err := run("create "+dir, func() error { return os.MkdirAll(dir, 0o700) }); err != nil {
			return result, err
		}
	}

	for _, change := range plan.Changes {
		if change.Kind == FileDelete {
			path := change.Path
			if err := run("remove "+path, func() error {
				err := os.Remove(path)
				if os.IsNotExist(err) {
					return nil
				}
				return err
			}); err != nil {
				return result, err
			}
			continue
		}
		file, ok := rendered.Get(change.Path)
		if !ok {
			return result, fmt.Errorf("internal: planned %s but rendered nothing for it", change.Path)
		}
		if err := run("write "+file.Path, func() error {
			return core.WriteFileAtomic(file.Path, file.Data, file.Mode)
		}); err != nil {
			return result, err
		}
	}

	var binary string
	if plan.Action != ActionNone || plan.Enable != nil {
		// The binary before the unit, because it is the more likely of the two
		// to be missing and the one whose absence olr can explain. A unit that
		// exists with no binary behind it fails at start with an exec error
		// naming a path and nothing else.
		if kind.Enabled(desired) {
			if err := run("check a "+kind.Object+" server is available", func() error {
				path, err := a.locate()()
				if err != nil {
					return kind.Missing()
				}
				binary = path
				return nil
			}); err != nil {
				return result, err
			}
		}
		if err := run("check "+kind.Unit+" is installed", func() error {
			return a.checkInstalled(ctx)
		}); err != nil {
			return result, err
		}
	}

	// A box with no service manager gets the files and nothing else, said out
	// loud rather than crashed on or silently skipped. That is a developer
	// machine or a container — "we could not tell" is a legitimate answer
	// (design.md §3.4), and it must not look like a successful start.
	if a.Unit == nil {
		if plan.Action != ActionNone || plan.Enable != nil {
			result.Steps = append(result.Steps, Step{
				Description: "no service manager on this box, so " + kind.Unit + " was not touched",
				Done:        true,
			})
		}
		return result, nil
	}

	// The unit named a path when it shipped, and this is where that path and the
	// binary that was actually found are allowed to disagree — see
	// socks_unit.go for the box this happened on and the 203/EXEC it produced.
	// Before the enable and the start, because both act on the unit descriptor
	// systemd holds, not on the file on disk.
	if binary != "" && kind.Point != nil {
		changed, err := kind.Point(ctx, binary, a.Paths)
		if err != nil {
			result.Steps = append(result.Steps, Step{
				Description: "point " + kind.Unit + " at " + binary,
				Error:       err.Error(),
			})
			return result, err
		}
		if changed {
			result.Steps = append(result.Steps, Step{
				Description: "point " + kind.Unit + " at " + binary,
				Done:        true,
			})
		}
	}

	// Boot-time state before the service action, so a backend that then fails
	// to start is at least configured the way the operator asked.
	if plan.Enable != nil {
		enable := *plan.Enable
		verb := map[bool]string{true: "enable", false: "disable"}[enable]
		if err := run(verb+" "+kind.Unit+" at boot", func() error {
			if enable {
				return a.Unit.Enable(ctx)
			}
			return a.Unit.Disable(ctx)
		}); err != nil {
			return result, err
		}
	}

	if action := plan.Action; action != ActionNone {
		if err := run(string(action)+" "+kind.Unit, func() error {
			switch action {
			case ActionStart:
				return a.Unit.Start(ctx)
			case ActionStop:
				return a.Unit.Stop(ctx)
			case ActionRestart:
				return a.Unit.Restart(ctx)
			}
			return nil
		}); err != nil {
			return result, err
		}

		if kind.Enabled(desired) && action != ActionStop {
			if err := run("verify "+kind.Unit+" stayed up", func() error {
				return a.verifyServing(ctx)
			}); err != nil {
				return result, err
			}
		}
	}

	return result, nil
}

// checkInstalled refuses to drive a unit whose file is not on the box.
//
// Not an error where the service manager itself is missing: that is a developer
// machine, and "we cannot tell" must not be reported as "it is broken".
func (a ProxyApplier) checkInstalled(ctx context.Context) error {
	if a.Unit == nil {
		return nil
	}
	status, err := a.Unit.Status(ctx)
	switch {
	case errors.Is(err, core.ErrNoServiceManager):
		return nil
	case err != nil:
		return err
	case !status.Installed:
		return fmt.Errorf(
			"%s is not installed, so there is no proxy for olr to drive.\n"+
				"The unit ships inside olr and is written out by `olr enable`; a missing one "+
				"means the binary was copied into place without it. Run `sudo olr enable`",
			status.Unit)
	}
	return nil
}

// verifyServing watches the unit for the settle window and fails if it does not
// stay active.
//
// Staying up is a stronger signal than it looks, and on this object it is the
// one that catches the failure the operator could least diagnose: a password
// that does not match its cipher is refused at startup, not at connect time, so
// without this the first symptom would be a phone that will not connect to a
// proxy olr has just reported as applied.
func (a ProxyApplier) verifyServing(ctx context.Context) error {
	if a.Unit == nil {
		return nil
	}
	deadline := time.Now().Add(a.settleWindow())
	for {
		status, err := a.Unit.Status(ctx)
		switch {
		case errors.Is(err, core.ErrNoServiceManager):
			return nil
		case err != nil:
			return err
		case !status.Active:
			// Where it stopped, when systemd has an answer. Without it the
			// operator gets "did not stay running" and no way in: the reason
			// (203/EXEC, a rejected config, a port already taken) is only in the
			// journal, and the difference between those is the whole diagnosis.
			return fmt.Errorf(
				"%s did not stay running%s. The server exits rather than serving a configuration "+
					"it cannot load, so check `olr remote status` and `journalctl -u %s`",
				status.Unit, whereItStopped(status), status.Unit)
		}
		if !time.Now().Before(deadline) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(settleInterval):
		}
	}
}

// whereItStopped is systemd's own answer to "what happened", when it has one.
//
// State and SubState rather than an exit status: systemd reports the latter on
// the service interface, which core's shared read does not touch, and
// "activating/auto-restart" is enough to say "it is crash-looping" — the exit
// status it is looping on is in the journal the message points at.
func whereItStopped(status core.UnitStatus) string {
	switch {
	case status.State == "":
		return ""
	case status.SubState == "":
		return " (" + status.State + ")"
	default:
		return " (" + status.State + "/" + status.SubState + ")"
	}
}
