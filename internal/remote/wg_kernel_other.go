//go:build !linux

package remote

import "context"

// The non-Linux build, which exists so that everything above the kernel seam —
// key generation, validation, rendering, planning, the HTTP surface — compiles
// and tests on a developer's machine (design.md §10, test hardware).
//
// It is deliberately not a simulator. Reporting an empty kernel that accepted
// everything would let a test pass against behaviour that has never run, which
// is worse than no test; what it reports instead is that it does not know.

// NewKernel returns the kernel implementation for this platform.
func NewKernel() Kernel { return unsupportedKernel{} }

type unsupportedKernel struct{}

// Observe reports that nothing could be read.
//
// Known:false rather than an empty state. "We could not tell" and "there is
// nothing there" are different answers, and treating the first as the second
// would make every plan on a machine like this claim the tunnel had been torn
// down behind olr's back and offer to rebuild it.
func (unsupportedKernel) Observe(context.Context, string) (Observed, error) {
	return Observed{Known: false}, nil
}

// Apply refuses, rather than silently succeeding.
func (unsupportedKernel) Apply(context.Context, Desired) ([]Step, error) {
	return nil, ErrUnsupported
}
