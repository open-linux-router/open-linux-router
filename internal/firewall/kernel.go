package firewall

import (
	"context"
	"errors"
	"maps"
)

// ErrUnsupported is returned by a kernel that cannot do this at all.
var ErrUnsupported = errors.New("the firewall needs a Linux kernel with nftables")

// Kernel is the seam between the decision layer and nftables (design.md §10).
// No policy below this line: everything is decided against values above it.
type Kernel interface {
	// Observe reads our table back, fresh.
	Observe(ctx context.Context) (Observed, error)

	// Apply replaces our table with d, or removes it when d is disabled, in
	// one atomic batch.
	Apply(ctx context.Context, d Desired) error
}

// StaticKernel is a Kernel backed by lines. What the tests use.
type StaticKernel struct {
	State      []string
	Blocked    map[string]uint64
	Unknown    bool
	ObserveErr error
	ApplyErr   error
}

// Observe implements Kernel.
func (k *StaticKernel) Observe(context.Context) (Observed, error) {
	if k.ObserveErr != nil {
		return Observed{}, k.ObserveErr
	}
	return Observed{
		Known:   !k.Unknown,
		Lines:   append([]string(nil), k.State...),
		Blocked: maps.Clone(k.Blocked),
	}, nil
}

// Apply implements Kernel.
func (k *StaticKernel) Apply(_ context.Context, d Desired) error {
	if k.ApplyErr != nil {
		return k.ApplyErr
	}
	k.State = d.Lines()
	k.Blocked = nil
	return nil
}
