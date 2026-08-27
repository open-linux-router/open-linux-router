//go:build !linux

package dhcp

import "context"

// This build exists so the rest of the package compiles and tests on a
// developer's machine (design.md §10, test hardware). It is not a simulation of
// systemd: every call fails, loudly, rather than returning a plausible-looking
// zero value that a test could mistake for a working service.

type noService struct{ unit string }

// NewService reports that there is no service manager on this platform.
func NewService(unit string) (Service, error) { return noService{unit: unit}, nil }

func (n noService) Status(context.Context) (ServiceStatus, error) {
	return ServiceStatus{Unit: n.unit}, ErrNoServiceManager
}

func (n noService) Start(context.Context) error   { return ErrNoServiceManager }
func (n noService) Stop(context.Context) error    { return ErrNoServiceManager }
func (n noService) Restart(context.Context) error { return ErrNoServiceManager }
func (n noService) Reload(context.Context) error  { return ErrNoServiceManager }
