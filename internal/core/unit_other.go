//go:build !linux

package core

import "context"

// This build exists so the rest of the package compiles and tests on a
// developer's machine (design.md §10, test hardware). It is not a simulation of
// systemd: every call fails, loudly, rather than returning a plausible-looking
// zero value that a test could mistake for a working service.

type noUnit struct{ unit string }

// NewUnit reports that there is no service manager on this platform.
func NewUnit(unit string) (Unit, error) { return noUnit{unit: unit}, nil }

func (n noUnit) Status(context.Context) (UnitStatus, error) {
	return UnitStatus{Unit: n.unit}, ErrNoServiceManager
}

func (n noUnit) Start(context.Context) error   { return ErrNoServiceManager }
func (n noUnit) Stop(context.Context) error    { return ErrNoServiceManager }
func (n noUnit) Restart(context.Context) error { return ErrNoServiceManager }
func (n noUnit) Reload(context.Context) error  { return ErrNoServiceManager }
