//go:build !linux

package link

import "context"

// NewWriter returns a writer that refuses, so the module builds and its
// decision layer stays testable on a developer's laptop.
//
// It fails rather than pretending to succeed: a silent no-op here would let a
// test assert that a network was configured on a machine where nothing was.
func NewWriter() Writer { return unsupportedWriter{} }

type unsupportedWriter struct{}

func (unsupportedWriter) Apply(context.Context, []Desired) ([]Step, error) {
	return nil, ErrUnsupported
}
