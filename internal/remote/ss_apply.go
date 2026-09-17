package remote

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

	// Locate finds the proxy binary. Nil means the finder for Object.
	//
	// Injectable for the reason internal/ingress injects its own: left to the
	// real thing, every test in this package would pass or fail depending on
	// whether the build machine happens to have an `ssserver` on its PATH.
	Locate func() (string, error)

	// Settle is how long apply watches a started backend before believing it.
	// Zero means DefaultSettle; negative means check once and return.
	Settle time.Duration

	// Object selects which of the two file-backed proxies this applier drives.
	// The zero value is Shadowsocks, so a construction that predates the second
	// object keeps its meaning (socks_apply.go).
	Object ProxyObject
}

// DefaultSettle is the post-apply observation window.
//
// Not a guess at how long the proxy takes to start: it is how long we watch the
// unit *after* systemd says the job is done. A configuration the server rejects
// at runtime looks identical to a healthy start for the first fraction of a
// second — and the rejection this exists to catch is the cipher/password one
// (Cipher.KeyLen), which is exactly the failure an operator would otherwise
// discover from a client that will not connect.
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

	root := filepath.Dir(a.Object.kind().Conf(a.Paths))
	err := filepath.WalkDir(root, func(path string, e fs.DirEntry, err error) error {
		switch {
		case os.IsNotExist(err):
			return nil
		case err != nil:
			return err
		case e.IsDir():
			return nil
		case strings.HasPrefix(e.Name(), "."):
			// core.WriteFileAtomic's temporaries. Skipping them keeps a crashed
			// apply's leftovers out of the plan.
			return nil
		}
		data, readErr := os.ReadFile(path)
		if os.IsNotExist(readErr) {
			return nil
		}
		if readErr != nil {
			return fmt.Errorf("reading %s: %w", path, readErr)
		}
		obs.Files[path] = data
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return ProxyObserved{}, fmt.Errorf("reading %s: %w", root, err)
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

	if plan.Action != ActionNone || plan.Enable != nil {
		// The binary before the unit, because it is the more likely of the two
		// to be missing and the one whose absence olr can explain. A unit that
		// exists with no binary behind it fails at start with an exec error
		// naming a path and nothing else.
		if kind.Enabled(desired) {
			if err := run("check a "+kind.Object+" server is available", func() error {
				if _, err := a.locate()(); err != nil {
					return kind.Missing()
				}
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
			return fmt.Errorf(
				"%s did not stay running. The server exits rather than serving a configuration "+
					"it cannot load, so check `olr remote logs shadowsocks`", status.Unit)
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

// ShadowsocksBlockers reports what stands between this object and working.
//
// One thing, and it is not a package: olr ships no Shadowsocks server and no
// distribution carries shadowsocks-rust, so this is the internal/ingress
// situation rather than the internal/dhcp one — a binary the operator fetches,
// not a `core.Dependency` with a package name per distribution. Declaring one
// would produce "install shadowsocks-rust with your package manager", which is
// advice naming a package that does not exist anywhere.
func ShadowsocksBlockers() []core.Blocker {
	if _, err := FindShadowsocks(); err == nil {
		return nil
	}
	return []core.Blocker{{
		Kind:    core.BlockerMissingDependency,
		Summary: "No Shadowsocks server is installed on this box.",
		Detail: "olr does not implement Shadowsocks and does not ship a server. " +
			"No distribution packages the one this drives, so it is a download rather than " +
			"an apt line — which also means security updates are yours to apply.",
		Fix: shadowsocksInstallHint(),
	}}
}
