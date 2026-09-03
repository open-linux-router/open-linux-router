//go:build !linux

package routing

import "context"

// The non-Linux build, which exists so that everything above the kernel seam —
// config parsing, validation, rendering, planning, the HTTP surface — compiles
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
// Known:false rather than an empty state, and the distinction is the one
// internal/dhcp draws with ServiceKnown: "we could not tell" and "there is
// nothing there" are different answers. Treating the first as the second would
// make every plan on a machine like this claim the box had drifted and offer to
// fix it.
func (unsupportedKernel) Observe(context.Context) (Observed, error) {
	return Observed{Known: false}, nil
}

// Apply refuses, rather than silently succeeding.
func (unsupportedKernel) Apply(context.Context, Desired) ([]Step, error) {
	return nil, ErrUnsupported
}
