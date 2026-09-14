package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The bug: dnsmasq, unbound and nft live in /usr/sbin on Debian, and /usr/sbin
// is not on an ordinary user's PATH there. `olr enable` looked only at $PATH,
// so it reported "dnsmasq is not on PATH" on a box where dnsmasq was installed
// and running, and told the operator to install it again — into the full
// dnsmasq package, which is the one olr deliberately does not want.
func TestLookToolFindsABackendInSbin(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dnsmasq-for-test")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	restore := sbinDirs
	sbinDirs = []string{dir}
	t.Cleanup(func() { sbinDirs = restore })

	if got := lookTool("dnsmasq-for-test"); got != path {
		t.Errorf("lookTool() = %q; want %q — a daemon off $PATH must still be found", got, path)
	}
}

// A file that is there but not executable is not a backend. Returning it would
// put an ExecStart in a unit that systemd then fails on, which is a worse
// failure than saying the tool is missing.
func TestLookToolIgnoresWhatItCannotRun(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "unbound-for-test"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "nft-for-test"), 0o755); err != nil {
		t.Fatal(err)
	}

	restore := sbinDirs
	sbinDirs = []string{dir}
	t.Cleanup(func() { sbinDirs = restore })

	if got := lookTool("unbound-for-test"); got != "" {
		t.Errorf("lookTool() = %q for a non-executable file; want \"\"", got)
	}
	if got := lookTool("nft-for-test"); got != "" {
		t.Errorf("lookTool() = %q for a directory; want \"\"", got)
	}
}

func TestLookToolReportsNothingWhenThereIsNothing(t *testing.T) {
	restore := sbinDirs
	sbinDirs = []string{t.TempDir()}
	t.Cleanup(func() { sbinDirs = restore })

	if got := lookTool("olr-no-such-backend"); got != "" {
		t.Errorf("lookTool() = %q; want \"\"", got)
	}
}

// `olr enable` no longer refuses over a missing backend, so the message this
// used to assert is gone with the function that built it. What it protected —
// naming dnsmasq-base and saying why — is now internal/dhcp's declaration, and
// core.TestInstallAdviceCarriesTheDebianWarning holds it there.

// The table this used to assert against moved to internal/core, where every
// surface can read it; its test moved with it (core.TestDistroBackendsCarryAFix
// AndSpareTheResolver).

// fakeUnit records which systemd verb it was sent.
type fakeUnit struct {
	active    bool
	statusErr error
	calls     []string
	core.Unit // nil: any method this test does not exercise panics rather than lying
}

func (f *fakeUnit) Status(context.Context) (core.UnitStatus, error) {
	f.calls = append(f.calls, "status")
	return core.UnitStatus{Active: f.active}, f.statusErr
}
func (f *fakeUnit) Start(context.Context) error   { f.calls = append(f.calls, "start"); return nil }
func (f *fakeUnit) Restart(context.Context) error { f.calls = append(f.calls, "restart"); return nil }

// The upgrade bug, as an operator met it: `olr enable` copied a new binary in,
// rewrote the units, printed "started olrd.service" — and left the old process
// running, because Start on a running unit is a no-op. The new CLI then talked
// to the old daemon and reported `no such endpoint` for a route that had just
// been added, and a browser was still answered by the code the upgrade replaced.
func TestEnableRestartsADaemonThatIsAlreadyRunning(t *testing.T) {
	unit := &fakeUnit{active: true}
	var out bytes.Buffer

	if err := startOrRestart(context.Background(), unit, &out); err != nil {
		t.Fatal(err)
	}

	if !slices.Contains(unit.calls, "restart") {
		t.Errorf("calls = %v; a running olrd must be restarted or the upgrade does not take effect",
			unit.calls)
	}
	if slices.Contains(unit.calls, "start") {
		t.Errorf("calls = %v; Start on a running unit is the no-op that hid this", unit.calls)
	}
	if !strings.Contains(out.String(), "restarted") {
		t.Errorf("output does not say what happened:\n%s", out.String())
	}
}

func TestEnableStartsADaemonThatIsNotRunning(t *testing.T) {
	unit := &fakeUnit{active: false}
	var out bytes.Buffer

	if err := startOrRestart(context.Background(), unit, &out); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(unit.calls, "start") {
		t.Errorf("calls = %v; a stopped olrd must be started", unit.calls)
	}
	if slices.Contains(unit.calls, "restart") {
		t.Errorf("calls = %v; restarting a stopped unit is not what was asked", unit.calls)
	}
}

// A box with no systemd to ask still has to end up with a running daemon.
func TestEnableStartsWhenTheStatusCannotBeRead(t *testing.T) {
	unit := &fakeUnit{active: true, statusErr: errors.New("no service manager")}
	var out bytes.Buffer

	if err := startOrRestart(context.Background(), unit, &out); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(unit.calls, "start") {
		t.Errorf("calls = %v; an unreadable status must still bring the unit up", unit.calls)
	}
}
