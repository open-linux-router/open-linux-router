//go:build !linux

package firewall

import "context"

// NewKernel returns the kernel implementation for this platform: one that says
// it cannot tell, rather than one that pretends to have applied anything.
func NewKernel() Kernel { return unsupportedKernel{} }

type unsupportedKernel struct{}

func (unsupportedKernel) Observe(context.Context) (Observed, error) {
	return Observed{Known: false}, nil
}

func (unsupportedKernel) Apply(context.Context, Desired) error { return ErrUnsupported }
