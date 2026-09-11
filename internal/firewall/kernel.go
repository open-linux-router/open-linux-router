package firewall

import (
	"context"
	"errors"
	"sort"
)

// The seam between the decision layer and the kernel.
//
// design.md §10 requires netlink, nftables and systemd to sit behind an
// interface so that everything else is unit-testable off Linux, and §3.6 notes
// that this testability seam is also the privilege-separation seam if that split
// is ever taken. This module leans on it as hard as internal/gateway does: dhcp
// and dns render files that can be asserted on anywhere, while what this one
// produces *is* kernel state, so without a seam there would be nothing to test
// on a machine that is not a router.

// ErrUnsupported is returned by a kernel that cannot do this at all.
var ErrUnsupported = errors.New("port forwarding needs a Linux kernel with nftables")

// Kernel is the window onto what this module programs.
//
// Two methods and no more. Every decision — which rules a forward becomes, which
// source ranges are masqueraded, whether a change is disruptive — is made above
// this line against values, so an implementation of it has no policy in it at
// all.
type Kernel interface {
	// Observe reads the live state, fresh. Planning against observation rather
	// than against a cached copy of what we last wrote is what makes drift
	// detection free (design.md §5.4).
	Observe(ctx context.Context) (Observed, error)

	// Apply programs the desired state, returning what it managed to do.
	//
	// It returns steps alongside an error rather than instead of one. There is
	// no rollback here on purpose (design.md §5.2/§5.3.2): if a change fails
	// halfway the steps that landed stay landed and are reported, and re-running
	// finishes the job. That is more honest and more debuggable than a silent
	// revert.
	Apply(ctx context.Context, d Desired) ([]Step, error)
}

// Step is one unit of work and how it went.
type Step struct {
	Description string `json:"description"`
	Done        bool   `json:"done"`
	Error       string `json:"error,omitempty"`
}

// ApplyResult is what an apply actually did.
type ApplyResult struct {
	Plan  Plan   `json:"plan"`
	Steps []Step `json:"steps"`
}

// StaticKernel is a Kernel backed by a slice of lines.
//
// It is what the tests use, and it is deliberately not a mock: it holds the same
// canonical lines a real kernel would report, so a test asserts on the plan an
// operator would see rather than on which methods were called.
type StaticKernel struct {
	// State is the live state, in canonical form. Apply replaces it.
	State []string

	// Counters is each named counter's totals, which is what makes a removal
	// disruptive or not (plan.go's everUsed).
	Counters map[string]Counter

	// Foreign and Listening fill out the rest of Observed.
	Foreign   []ForeignFilter
	Listening []ListeningPort

	// Unknown makes Observe report that the kernel could not be read, which is
	// what a non-Linux build and an unprivileged container both look like.
	Unknown bool

	// ObserveErr and ApplyErr force the failure paths.
	ObserveErr error
	ApplyErr   error

	// FailAfter stops Apply after this many steps when ApplyErr is set, so a
	// test can assert on the half-finished state design.md §5.3.2 promises to
	// report rather than unwind.
	FailAfter int
}

// Observe implements Kernel.
func (k *StaticKernel) Observe(context.Context) (Observed, error) {
	if k.ObserveErr != nil {
		return Observed{}, k.ObserveErr
	}
	obs := Observed{
		Known:     !k.Unknown,
		Lines:     append([]string(nil), k.State...),
		Counters:  map[string]Counter{},
		Foreign:   append([]ForeignFilter(nil), k.Foreign...),
		Listening: append([]ListeningPort(nil), k.Listening...),
	}
	for name, n := range k.Counters {
		obs.Counters[name] = n
	}
	sort.Strings(obs.Lines)
	return obs, nil
}

// Apply implements Kernel.
func (k *StaticKernel) Apply(_ context.Context, d Desired) ([]Step, error) {
	steps := []Step{
		{Description: "write nftables", Done: true},
	}
	if k.ApplyErr != nil {
		at := k.FailAfter
		if at < 0 || at > len(steps) {
			at = 0
		}
		steps = steps[:at+1]
		steps[at] = Step{Description: steps[at].Description, Error: k.ApplyErr.Error()}
		return steps, k.ApplyErr
	}
	k.State = d.objectLines()
	sort.Strings(k.State)
	return steps, nil
}
