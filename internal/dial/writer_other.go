//go:build !linux

package dial

import "context"

// NewWriter returns a writer that refuses, so the module builds and its
// decision layer stays testable on a developer's laptop.
//
// Apply fails rather than pretending to succeed: a silent no-op here would let
// a test assert that an uplink was configured on a machine where nothing was.
// Observe answers "nothing is there" instead of failing, because it is a read —
// a plan on a non-Linux machine should render, showing every step as still to
// do, rather than refusing to be computed.
func NewWriter() Writer { return unsupportedWriter{} }

type unsupportedWriter struct{}

func (unsupportedWriter) Apply(context.Context, Desired) ([]Step, error) {
	return nil, ErrNoKernel
}

func (unsupportedWriter) Observe(context.Context, string) (Observed, error) {
	return Observed{}, nil
}
