package remote

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Pointing the unit at the 3proxy that is actually on the box.
//
// The shipped unit names one path in its ExecStart — SocksUnitPath, the place an
// operator who builds 3proxy by hand is told to put it. The package 3proxy
// publishes installs to /usr/bin, and the ordinary order of events on a new box
// is: install olr, run `olr enable`, decide to switch SOCKS5 on, *then* install
// 3proxy. So `olr enable` had nothing to point a drop-in at when it ran, and the
// unit keeps naming a path no package will ever write to.
//
// The failure that produces is the worst shape a failure can take: the rendered
// file is correct, `olr remote status` says the config matches, and systemd
// reports 203/EXEC for a binary that is installed, executable, and three
// directories away. On the screen it reads as "socks5 does not work", which is
// true and points at nothing.
//
// So the apply — the moment olr learns where the binary is — writes the drop-in
// that makes systemd run *that* binary. A drop-in rather than an edited unit:
// `olr enable` rewrites the units on every upgrade, and an edit there would be
// lost the next time (design.md §7, and packaging/dropin.go for the other
// four backends).
//
// None of this is rendered configuration. The drop-in is not under rendered/,
// drift does not compare it, and it holds no secret — it names a binary and the
// config path the unit already names. It is a step in the apply, reported like
// one, and idempotent: an apply that finds the file already right writes
// nothing and reloads nothing.

// SocksDropInPath is where that drop-in goes.
const SocksDropInPath = "/etc/systemd/system/" + SocksUnitName + ".d/10-path.conf"

// PointSocksUnit makes olr-socks5.service run the binary FindSocks returned.
//
// Reports whether the box changed, because that is the only case in which
// systemd has to re-read the unit. A daemon-reload on every apply would be a
// reload for a file that is already correct.
func PointSocksUnit(ctx context.Context, binary string, paths Paths) (bool, error) {
	changed, err := writeSocksDropIn(binary, paths.SocksConf, paths.SocksDropIn)
	if err != nil || !changed {
		return changed, err
	}
	// A box with no service manager has no unit to point and no reader of the
	// file; ErrNoServiceManager is the answer to "was it reloaded", not a
	// failure to report. Everywhere else a failed reload is fatal, because the
	// unit about to be started would still be the one naming the wrong path.
	if err := core.DaemonReload(ctx); err != nil && !errors.Is(err, core.ErrNoServiceManager) {
		return true, err
	}
	return true, nil
}

// writeSocksDropIn puts a drop-in naming binary at path, or removes the file
// when the unit's own path turned out to be the right one after all. Idempotent,
// and reports whether it changed anything.
func writeSocksDropIn(binary, conf, path string) (bool, error) {
	want := socksDropIn(binary, conf)

	current, err := os.ReadFile(path)
	switch {
	case err == nil && want == nil:
		// The proxy moved back to the path the unit names. Leaving the drop-in
		// behind would keep the unit pointed at a binary that is no longer
		// there — the same 203/EXEC from the other direction.
		if err := os.Remove(path); err != nil {
			return false, fmt.Errorf("removing %s: %w", path, err)
		}
		return true, nil
	case err == nil && bytes.Equal(current, want):
		return false, nil
	case err == nil:
		return true, core.WriteFileAtomic(path, want, 0o644)
	case !os.IsNotExist(err):
		return false, fmt.Errorf("reading %s: %w", path, err)
	case want == nil:
		return false, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return false, fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	return true, core.WriteFileAtomic(path, want, 0o644)
}

// socksDropIn renders the drop-in, or nil when the unit's own path is the one
// that was found and there is nothing to override.
func socksDropIn(binary, conf string) []byte {
	if binary == SocksUnitPath {
		return nil
	}
	return []byte(fmt.Sprintf(`# Written by open-linux-router on apply. 3proxy is not at the path
# olr-socks5.service names, so this replaces ExecStart instead of editing the
# unit — an edited unit is overwritten by the next upgrade.
[Service]
ExecStart=
ExecStart=%s %s
`, binary, conf))
}
