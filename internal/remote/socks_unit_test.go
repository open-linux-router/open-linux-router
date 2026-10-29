package remote

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The unit ships naming one path for 3proxy. 3proxy's own package installs to
// another, and on an ordinary box it is installed *after* `olr enable` ran and
// found nothing to point a drop-in at — so the apply is the only place that
// knows both facts, and these tests hold it to using them.

// fakeUnit is a core.Unit that records what the applier asked of it. systemd is
// not what these tests are about; the steps are.
type fakeUnit struct {
	active    bool
	enabled   bool
	installed bool

	started  int
	restarts int
}

func (u *fakeUnit) Status(context.Context) (core.UnitStatus, error) {
	state := "inactive"
	if u.active {
		state = "active"
	}
	return core.UnitStatus{
		Unit:      SocksUnitName,
		Active:    u.active,
		Enabled:   u.enabled,
		Installed: u.installed,
		State:     state,
	}, nil
}

func (u *fakeUnit) Start(context.Context) error   { u.started++; u.active = true; return nil }
func (u *fakeUnit) Stop(context.Context) error    { u.active = false; return nil }
func (u *fakeUnit) Restart(context.Context) error { u.restarts++; u.active = true; return nil }
func (u *fakeUnit) Reload(context.Context) error  { return nil }
func (u *fakeUnit) Enable(context.Context) error  { u.enabled = true; return nil }
func (u *fakeUnit) Disable(context.Context) error { u.enabled = false; return nil }

// socksApplier is a proxy applier whose unit, config file and drop-in all live
// in one temporary directory, and whose 3proxy is wherever the test says.
func socksApplier(t *testing.T, binary string) (ProxyApplier, *fakeUnit, Paths) {
	t.Helper()
	dir := t.TempDir()
	paths := Paths{
		SocksConf:   filepath.Join(dir, "rendered", "3proxy.cfg"),
		SocksDropIn: filepath.Join(dir, "systemd", "olr-socks5.service.d", "10-path.conf"),
	}
	unit := &fakeUnit{installed: true}
	return ProxyApplier{
		Store:  Store{Store: core.NewStore(filepath.Join(dir, "olr.json"), ModuleName)},
		Paths:  paths,
		Unit:   unit,
		Locate: func() (string, error) { return binary, nil },
		Settle: -1,
		Object: ObjectSocks,
	}, unit, paths
}

// enabledSocks is the smallest config that renders a proxy: a way in, and a
// credential olr would otherwise have generated.
func enabledSocks() Config {
	c := enabledConfig()
	c.Socks = Socks5{Enabled: true, Password: "secret"}
	return c
}

func hasStep(steps []Step, description string) bool {
	for _, s := range steps {
		if s.Description == description && s.Done {
			return true
		}
	}
	return false
}

// The bug this whole file is about, end to end: apt installed 3proxy in
// /usr/bin, the unit names /usr/lib, and the apply has to close the gap — or
// systemd answers 203/EXEC and the proxy never runs.
func TestApplyPointsTheUnitAtTheBinaryItFound(t *testing.T) {
	a, unit, paths := socksApplier(t, "/usr/bin/3proxy")

	result, err := a.Apply(context.Background(), enabledSocks())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if unit.started != 1 {
		t.Fatalf("the proxy was not started after being pointed at its binary: %+v", result.Steps)
	}

	data, err := os.ReadFile(paths.SocksDropIn)
	if err != nil {
		t.Fatalf("no drop-in was written, so the unit still names a binary that is not there: %v", err)
	}
	if !strings.Contains(string(data), "ExecStart=/usr/bin/3proxy "+paths.SocksConf) {
		t.Errorf("the drop-in does not run the binary that was found:\n%s", data)
	}
	// The clearing assignment has to come first: assigning to an Exec*
	// directive appends, so without it systemd would run both paths.
	if !strings.Contains(string(data), "ExecStart=\n") {
		t.Errorf("the drop-in does not clear the unit's ExecStart before setting it:\n%s", data)
	}

	want := "point " + SocksUnitName + " at /usr/bin/3proxy"
	if !hasStep(result.Steps, want) {
		t.Errorf("no %q step, so the operator is not told the unit was moved:\n%+v", want, result.Steps)
	}
}

// A box that put the binary where the unit looks needs no drop-in, and must not
// collect one: an explicit ExecStart that agrees with the unit is a file nobody
// would ever read, and it would survive the day the binary moved back.
func TestApplyLeavesTheUnitAloneWhenTheBinaryIsWhereItBelongs(t *testing.T) {
	a, _, paths := socksApplier(t, SocksUnitPath)

	result, err := a.Apply(context.Background(), enabledSocks())
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(paths.SocksDropIn); !os.IsNotExist(err) {
		t.Errorf("a drop-in was written for a binary the unit already names: %v", err)
	}
	if hasStep(result.Steps, "point "+SocksUnitName+" at "+SocksUnitPath) {
		t.Errorf("an apply that changed nothing reported pointing the unit:\n%+v", result.Steps)
	}
}

// And the other direction: a drop-in left over from when 3proxy was elsewhere
// points at a binary that is now gone, which is the same 203/EXEC with the
// paths swapped.
func TestApplyRemovesADropInTheUnitNoLongerNeeds(t *testing.T) {
	a, _, paths := socksApplier(t, SocksUnitPath)

	if err := os.MkdirAll(filepath.Dir(paths.SocksDropIn), 0o755); err != nil {
		t.Fatal(err)
	}
	stale := socksDropIn("/usr/bin/3proxy", paths.SocksConf)
	if err := os.WriteFile(paths.SocksDropIn, stale, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := a.Apply(context.Background(), enabledSocks()); err != nil {
		t.Fatalf("apply: %v", err)
	}
	if _, err := os.Stat(paths.SocksDropIn); !os.IsNotExist(err) {
		t.Errorf("the stale drop-in is still there, pointing the unit at %s: %v", "/usr/bin/3proxy", err)
	}
}

// Every apply runs this, so it has to be quiet when there is nothing to do: a
// daemon-reload on each one would be a reload for a file that is already right.
func TestWritingTheDropInIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "systemd", "10-path.conf")

	changed, err := writeSocksDropIn("/usr/bin/3proxy", "/etc/x/3proxy.cfg", path)
	if err != nil || !changed {
		t.Fatalf("first write = (%v, %v), want (true, nil)", changed, err)
	}
	changed, err = writeSocksDropIn("/usr/bin/3proxy", "/etc/x/3proxy.cfg", path)
	if err != nil || changed {
		t.Fatalf("second write = (%v, %v), want (false, nil)", changed, err)
	}
}
