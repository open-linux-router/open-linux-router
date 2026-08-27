package dhcp

import (
	"context"
	"errors"
	"time"
)

// Service is the module's window onto the supervised daemon.
//
// This is the only interface in the package that touches the operating system,
// and it exists for exactly the reason design.md §10 gives under test hardware:
// keep renderers, lease parsing and validation free of Linux-only imports so
// they are unit-testable anywhere, and isolate systemd behind a seam. Everything
// above this line runs on a laptop.
type Service interface {
	// Status reports what systemd knows about the unit.
	Status(ctx context.Context) (ServiceStatus, error)
	// Start the unit.
	Start(ctx context.Context) error
	// Stop the unit.
	Stop(ctx context.Context) error
	// Restart the unit. Leases survive: they live in a file, not in memory.
	Restart(ctx context.Context) error
	// Reload signals the daemon to re-read what it can without restarting.
	Reload(ctx context.Context) error
}

// ServiceStatus is the daemon liveness half of `olr status` (design.md §5.4) —
// the half that is not derived from drift.
type ServiceStatus struct {
	// Unit is the systemd unit name.
	Unit string `json:"unit"`
	// Active reports whether it is running right now.
	Active bool `json:"active"`
	// Enabled reports whether it starts at boot.
	Enabled bool `json:"enabled"`
	// State is systemd's ActiveState, verbatim, so an unusual one is reported
	// rather than flattened into a boolean.
	State string `json:"state"`
	// SubState is systemd's finer-grained state ("running", "dead", "failed").
	SubState string `json:"sub_state,omitempty"`
	// Since is when it entered that state.
	Since time.Time `json:"since,omitzero"`
	// MainPID is the daemon's process id, or 0.
	//
	// Worth surfacing: an unchanged pid across a reservation change is the
	// visible proof that the reload path did not restart the daemon.
	MainPID int `json:"main_pid,omitempty"`
}

// ErrNoServiceManager is returned where systemd is not available — a non-Linux
// build, or a container with no system bus. Reported rather than papered over,
// because "we could not tell" and "it is not running" are different answers and
// only one of them is honest.
var ErrNoServiceManager = errors.New("no systemd service manager available")
