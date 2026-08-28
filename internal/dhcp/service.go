package dhcp

import (
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's window onto the supervised daemon.
//
// The systemd driving itself lives in core, which design.md §3.3 makes
// responsible for "systemd unit management for backends". `olr daemon` needs
// exactly the same mechanism for olrd's own unit, and two copies of it would
// drift. What stays here is the module's vocabulary: inside dhcp the supervised
// thing is a *service*, and §4.2 is emphatic that a module is not its backend.
//
// Aliases rather than wrappers, so a fake in a test satisfies both names and
// there is no adapter layer to keep in step.

// Service is the interface the Applier drives. See core.Unit.
type Service = core.Unit

// ServiceStatus is the daemon liveness half of `olr status` (design.md §5.4) —
// the half that is not derived from drift. See core.UnitStatus.
type ServiceStatus = core.UnitStatus

// ErrNoServiceManager is returned where systemd is not available — a non-Linux
// build, or a container with no system bus. Reported rather than papered over,
// because "we could not tell" and "it is not running" are different answers and
// only one of them is honest.
var ErrNoServiceManager = core.ErrNoServiceManager

// NewService returns a Service for the module's backend unit.
func NewService(unit string) (Service, error) { return core.NewUnit(unit) }
