package core

import (
	"context"
	"errors"
	"time"
)

// systemd unit management, which design.md §3.3 lists among the things core
// offers modules rather than each module building its own.
//
// Two callers want it for different reasons and the distinction is §4.2's:
// a *module* drives its backend's unit (dhcp → olr-dhcp.service), while
// `olr daemon` drives olrd's own. Same mechanism, and the vocabulary is worth
// keeping straight — "daemon" is olrd, "backend" is what it drives.

// Unit is a window onto one systemd unit.
//
// This interface is also the seam design.md §10 requires under test hardware:
// isolating systemd here is what lets renderers, planning and validation be
// unit-tested on a laptop. It is the same seam privilege separation would use
// if §3.6's deferred split is ever taken, which is why keeping it clean is
// worth more than the tests alone.
type Unit interface {
	// Status reports what systemd knows about the unit.
	Status(ctx context.Context) (UnitStatus, error)
	// Start the unit.
	Start(ctx context.Context) error
	// Stop the unit.
	Stop(ctx context.Context) error
	// Restart the unit.
	Restart(ctx context.Context) error
	// Reload signals the unit to re-read what it can without restarting.
	Reload(ctx context.Context) error

	// Enable makes the unit start at boot, and Disable stops it doing so.
	//
	// These are separate from Start and Stop because they answer a different
	// question, and conflating them is how a router comes back from a power cut
	// with no DHCP. Start is "run now"; Enable is "run after the next reboot
	// too". A module whose config says the service is on means both — the
	// operator did not ask for a server that survives until the next power cut.
	Enable(ctx context.Context) error
	Disable(ctx context.Context) error
}

// UnitStatus is the daemon-liveness half of `olr status` (design.md §5.4) —
// the half that is not derived from drift.
type UnitStatus struct {
	// Unit is the systemd unit name.
	Unit string `json:"unit"`
	// Active reports whether it is running right now.
	Active bool `json:"active"`
	// Enabled reports whether it starts at boot.
	Enabled bool `json:"enabled"`
	// Installed reports whether the unit file exists at all.
	//
	// Distinguished from Enabled because the two failures need different
	// answers: a unit that is installed but disabled is a config problem olr
	// can fix on the next apply, while a unit that was never installed means
	// the package is broken or absent and no amount of applying will help.
	// Without this the operator gets D-Bus's bare "Unit ... not found".
	Installed bool `json:"installed"`
	// State is systemd's ActiveState, verbatim, so an unusual one is reported
	// rather than flattened into a boolean.
	State string `json:"state"`
	// SubState is systemd's finer-grained state ("running", "dead", "failed").
	SubState string `json:"sub_state,omitempty"`
	// Since is when it entered that state.
	Since time.Time `json:"since,omitzero"`
	// MainPID is the process id, or 0.
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
