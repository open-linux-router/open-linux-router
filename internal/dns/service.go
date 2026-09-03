package dns

import (
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's window onto its supervised daemons.
//
// The systemd driving itself lives in core, which design.md §3.3 makes
// responsible for "systemd unit management for backends". What stays here is
// the module's vocabulary: inside dns the supervised things are *services*, and
// §4.2 is emphatic that a module is not its backend.
//
// Aliases rather than wrappers, so a fake in a test satisfies both names and
// there is no adapter layer to keep in step.

// Service is the interface the Applier drives. See core.Unit.
type Service = core.Unit

// ServiceStatus is the daemon liveness half of `olr status` (design.md §5.4).
// See core.UnitStatus.
type ServiceStatus = core.UnitStatus

// ErrNoServiceManager is returned where systemd is not available — a non-Linux
// build, or a container with no system bus. Reported rather than papered over,
// because "we could not tell" and "it is not running" are different answers and
// only one of them is honest.
var ErrNoServiceManager = core.ErrNoServiceManager

// NewService returns a Service for one of the module's backend units.
func NewService(unit string) (Service, error) { return core.NewUnit(unit) }
