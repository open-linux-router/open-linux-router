package dhcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Applier turns intent into a running DHCP server.
//
// There is no rollback here on purpose (design.md §5.2/§5.3.2). If a multi-step
// change fails halfway, the steps that landed stay landed and are reported;
// re-running finishes the job. That is more honest and more debuggable than a
// silent revert, and it is why Steps below records outcomes rather than being
// discarded on error.
type Applier struct {
	// Backend renders the daemon's files.
	Backend Backend
	// Links is the window onto the link module.
	Links LinkView
	// Service supervises the daemon.
	Service Service
	// Paths is the on-disk layout.
	Paths Paths
	// ConfigPath is where intent is stored. Core will own this file — with
	// revision history and `olr dhcp rollback` — once it exists; until then the
	// module reads and writes it directly.
	ConfigPath string

	// PortCheck reports whether something already holds the DHCP server port.
	// Nil means PortConflict. Injectable so the refusal path is testable
	// without arranging for a real port conflict on the build machine.
	PortCheck func() (bool, error)
}

func (a Applier) portCheck() func() (bool, error) {
	if a.PortCheck != nil {
		return a.PortCheck
	}
	return PortConflict
}

// NewApplier wires up the default dnsmasq backend against a link view.
func NewApplier(links LinkView) (Applier, error) {
	paths := DefaultPaths()
	backend := NewDnsmasq(paths)
	service, err := NewService(backend.Unit())
	if err != nil {
		return Applier{}, err
	}
	return Applier{
		Backend:    backend,
		Links:      links,
		Service:    service,
		Paths:      paths,
		ConfigPath: ConfigPath,
	}, nil
}

// Step is one unit of work and how it went.
type Step struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// ApplyResult is what an apply actually did.
type ApplyResult struct {
	Plan  Plan   `json:"plan"`
	Steps []Step `json:"steps"`
}

// Load reads stored intent.
func (a Applier) Load() (Config, error) { return LoadConfig(a.ConfigPath) }

// Observe reads the actual state of the system: what is on disk, whether the
// daemon is running, and which leases are held.
//
// Every field is read fresh. Planning against observation rather than against a
// cached copy of what we last wrote is what makes drift detection free
// (design.md §5.4).
func (a Applier) Observe(ctx context.Context) (Observed, error) {
	obs := Observed{Files: map[string][]byte{}}

	// The main config, plus everything in the directories we own. Files we did
	// not render must be included or a stale reservation would never be
	// noticed.
	paths := []string{a.Paths.Conf}
	for _, dir := range []string{a.Paths.HostsDir, a.Paths.OptsDir} {
		entries, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Observed{}, fmt.Errorf("reading %s: %w", dir, err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				paths = append(paths, filepath.Join(dir, e.Name()))
			}
		}
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return Observed{}, fmt.Errorf("reading %s: %w", p, err)
		}
		obs.Files[p] = data
	}

	leases, _, err := LoadLeases(a.Paths.LeaseFile)
	if err != nil {
		return Observed{}, err
	}
	obs.Leases = leases

	// A missing service manager is reported as "not running" rather than
	// failing the whole observation: the file half of the answer is still
	// useful, and `olr dhcp status` surfaces the service error separately.
	if status, err := a.Service.Status(ctx); err == nil {
		obs.Running = status.Active
	}

	return obs, nil
}

// Plan reports what applying desired would do, without doing it.
func (a Applier) Plan(ctx context.Context, desired Config) (Plan, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return Plan{}, err
	}
	return BuildPlan(a.Backend, desired, a.Links, obs, time.Now())
}

// Drift reports what has changed underneath stored intent.
//
// This is §5.4 in one line: plan the intent we already have against the system
// as it actually is. A non-empty plan is drift.
func (a Applier) Drift(ctx context.Context) (Plan, error) {
	current, err := a.Load()
	if err != nil {
		return Plan{}, err
	}
	return a.Plan(ctx, current)
}

// Apply writes the config and brings the daemon into line with it.
//
// It applies immediately and is in effect on return (design.md §5.1) — there is
// no staged commit to follow up with.
func (a Applier) Apply(ctx context.Context, desired Config) (ApplyResult, error) {
	obs, err := a.Observe(ctx)
	if err != nil {
		return ApplyResult{}, err
	}
	plan, err := BuildPlan(a.Backend, desired, a.Links, obs, time.Now())
	if err != nil {
		return ApplyResult{Plan: plan}, err
	}

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

	// Intent is stored first, before anything it describes. If a later step
	// fails, a re-run has something to finish the job from — which is the whole
	// premise of idempotent re-apply instead of rollback (§5.3.2).
	if err := run("store configuration in "+a.ConfigPath, func() error {
		data, err := MarshalConfig(desired)
		if err != nil {
			return err
		}
		return writeFileAtomic(a.ConfigPath, data, 0o644)
	}); err != nil {
		return result, err
	}

	if plan.Empty() {
		return result, nil
	}

	for _, dir := range []string{
		filepath.Dir(a.Paths.Conf), a.Paths.HostsDir, a.Paths.OptsDir,
		filepath.Dir(a.Paths.LeaseFile),
	} {
		if err := run("create "+dir, func() error { return os.MkdirAll(dir, 0o755) }); err != nil {
			return result, err
		}
	}

	rendered, err := a.Backend.Render(desired, a.Links)
	if err != nil {
		return result, err
	}
	for _, change := range plan.Changes {
		switch change.Kind {
		case ChangeDelete:
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
		default:
			file, ok := rendered.Get(change.Path)
			if !ok {
				return result, fmt.Errorf("internal: planned %s but rendered nothing for it", change.Path)
			}
			if err := run("write "+file.Path, func() error {
				return writeFileAtomic(file.Path, file.Data, file.Mode)
			}); err != nil {
				return result, err
			}
		}
	}

	// Only checked when we are about to start: a reload or restart of a daemon
	// that is already bound would trip over its own socket.
	if plan.Action == ActionStart {
		if err := run("check nothing else holds UDP/67", func() error {
			inUse, err := a.portCheck()()
			if err != nil {
				return err
			}
			if inUse {
				return ErrPortInUse()
			}
			return nil
		}); err != nil {
			return result, err
		}
	}

	if action := plan.Action; action != ActionNone {
		err := run(string(action)+" "+a.Backend.Unit(), func() error {
			switch action {
			case ActionStart:
				return a.Service.Start(ctx)
			case ActionStop:
				return a.Service.Stop(ctx)
			case ActionReload:
				return a.Service.Reload(ctx)
			case ActionRestart:
				return a.Service.Restart(ctx)
			}
			return nil
		})
		if err != nil {
			return result, err
		}
	}

	return result, nil
}

// Leases reads the current lease database.
func (a Applier) Leases() ([]Lease, []Problem, error) { return LoadLeases(a.Paths.LeaseFile) }

// Usage summarises each pool's occupancy.
func (a Applier) Usage(c Config, leases []Lease) []Usage {
	out := make([]Usage, 0, len(c.Pools))
	for _, p := range c.Pools {
		out = append(out, UsageOf(p, leases, time.Now()))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Interface < out[j].Interface })
	return out
}

// writeFileAtomic replaces a file in one step, so a reader — including the
// daemon re-reading a hosts directory — never sees a half-written config
// (design.md §3.3, atomic file replacement).
func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// Durability before visibility: a rename that survives a crash pointing at
	// unflushed content would leave an empty config where a valid one was.
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
