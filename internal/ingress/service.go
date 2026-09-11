package ingress

import (
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The module's window onto its supervised backend.
//
// The systemd driving itself lives in core, which design.md §3.3 makes
// responsible for "systemd unit management for backends". What stays here is the
// module's vocabulary — and here that vocabulary collides, in a way worth
// recording because the resolution is a rule and not a preference.
//
// `dhcp` and `dns` both call the supervised daemon a *Service*. This module
// cannot: `Service` is already the name of the thing an **operator** creates —
// a published service. When the daemon's word and the operator's word are the
// same word, design.md §1 says which one wins: the default surface speaks the
// operator's vocabulary, not the daemon's. So the operator keeps `Service` and
// the supervised process is a `ProxyUnit`.
//
// Aliases rather than wrappers, so a fake in a test satisfies both names and
// there is no adapter layer to keep in step.

// ProxyUnit is the interface the Applier drives. See core.Unit.
//
// One unit, unlike `dns`, which drives two. The plan reflects that: there is no
// "which of them does this file oblige us to signal" question here, so Change
// carries no unit field.
type ProxyUnit = core.Unit

// ProxyStatus is the backend-liveness half of `olr ingress status`
// (design.md §5.4). See core.UnitStatus.
type ProxyStatus = core.UnitStatus

// ErrNoServiceManager is returned where systemd is not available — a non-Linux
// build, or a container with no system bus. Reported rather than papered over,
// because "we could not tell" and "it is not running" are different answers and
// only one of them is honest.
var ErrNoServiceManager = core.ErrNoServiceManager

// NewProxyUnit returns a ProxyUnit for the module's backend unit.
func NewProxyUnit(unit string) (ProxyUnit, error) { return core.NewUnit(unit) }
