package remote

import (
	"context"
	"errors"
	"net/netip"
	"sort"
	"time"
)

// The seam between the decision layer and the kernel.
//
// design.md §10 requires netlink and anything like it to sit behind an
// interface so that everything else is unit-testable off Linux, and §3.6 notes
// that this testability seam is also the privilege-separation seam if that
// split is ever taken. This module needs it as badly as internal/gateway does
// and for the same reason: what it produces *is* kernel state, so without a
// seam there would be nothing to test on a machine that is not a router.
//
// It is also the seam behind which the one subprocess lives. Everything `wg` is
// used for is on the other side of these two methods, so replacing it with
// wgctrl later is a change to one file (docs/remote-access.md §5.2).

// ErrUnsupported is returned by a kernel that cannot do this at all.
var ErrUnsupported = errors.New("remote access needs a Linux kernel with WireGuard")

// Kernel is the window onto what this module programs.
//
// Two methods and no more. Every decision — which address a peer holds, what a
// peer may send, whether the interface should exist — is made above this line
// against values, so an implementation has no policy in it.
type Kernel interface {
	// Observe reads the live state of one interface, fresh. Planning against
	// observation rather than a cached copy of what we last wrote is what makes
	// drift detection free (design.md §5.4).
	Observe(ctx context.Context, iface string) (Observed, error)

	// Apply programs the desired state, returning what it managed to do.
	//
	// Steps come back alongside an error rather than instead of one. There is
	// no rollback here on purpose (design.md §5.2/§5.3.2): if a multi-step
	// change fails halfway the steps that landed stay landed and are reported,
	// and re-running finishes the job.
	Apply(ctx context.Context, d Desired) ([]Step, error)
}

// Observed is the actual state of the tunnel, read fresh.
type Observed struct {
	// Known reports whether the kernel answered at all.
	//
	// The distinction internal/gateway draws with the same field: "we could not
	// tell" and "there is nothing there" are different answers, and treating
	// the first as the second would make every box we cannot read — a
	// developer's laptop, a container with no CAP_NET_ADMIN — report permanent
	// drift and offer to fix it.
	Known bool

	// Present reports whether the interface exists.
	Present bool

	// Foreign reports that the interface exists and is **not** WireGuard.
	//
	// The one refusal this module makes, and it is design.md §3.4's
	// adopt-only rule in the one place it can be violated by accident: `wg0` is
	// a conventional name, so a box already running a tunnel somebody else
	// configured is exactly the box where olr must not assume the name is free.
	// Reported rather than resolved — there is nothing here olr could safely do
	// to somebody else's interface.
	Foreign bool

	// Up reports whether the interface is administratively up.
	Up bool

	// Lines is the live state in the same canonical form Desired produces, so
	// comparing them is a string comparison.
	Lines []string

	// Peers is the liveness half, and the only honest answer to "is this
	// working": WireGuard has no connection state to query, so the last
	// handshake is what distinguishes a peer that is in use from one that has
	// never connected at all.
	Peers []PeerState
}

// PeerState is what the kernel knows about one peer right now.
//
// Never stored (design.md §4.5) and always reported with the `as_of` its
// response carries.
type PeerState struct {
	// PublicKey identifies the peer. Names are olr's and the kernel has none,
	// so the join back to a peer happens above this layer.
	PublicKey string `json:"public_key"`

	// Endpoint is where the peer was last heard from — its address out in the
	// world, which is the field that answers "is my phone on the hotel Wi-Fi or
	// on mobile data".
	Endpoint string `json:"endpoint,omitempty"`

	// LastHandshake is zero for a peer that has never connected. That zero is
	// load-bearing: "never" and "an hour ago" need different answers from the
	// operator, and flattening them would make a peer whose configuration was
	// never imported look like one that is merely idle.
	LastHandshake time.Time `json:"last_handshake,omitzero"`

	// RxBytes and TxBytes are from this box's point of view: received from the
	// peer, sent to it.
	RxBytes uint64 `json:"rx_bytes,omitempty"`
	TxBytes uint64 `json:"tx_bytes,omitempty"`
}

// Step is one unit of work and how it went. Same shape as internal/gateway's
// and internal/link's, so a client's plan-and-apply rendering works against any
// module's answer without a second implementation.
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

// ApplyOrder is the sequence Apply must follow, written down rather than left
// to the implementation because the reason is not obvious from either end.
//
//	create  →  address  →  wg setconf  →  up          (installing)
//	down    →  delete                                 (removing)
//
// Up is last. An interface that is up with no peers configured is a socket
// accepting handshakes it will then reject, and a client that retried in that
// window reports a failure at the exact moment olr is making things work —
// which gets reported as a bug in the wrong place.
const ApplyOrder = "create, then address, then wg setconf, then up"

// StaticKernel is a Kernel backed by values.
//
// It is what the tests use, and it is deliberately not a mock: it holds the
// same canonical lines a real kernel would report, so a test asserts on the
// plan an operator would see rather than on which methods were called.
type StaticKernel struct {
	// State is the live state in canonical form. Apply replaces it.
	State []string

	// Present, Foreign and Up fill out the rest of Observed.
	Present bool
	Foreign bool
	Up      bool

	// PeerStates is what Observe reports as the liveness half.
	PeerStates []PeerState

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

	// Applied is the last Desired handed to Apply, for the tests that care what
	// was asked for rather than what was left behind.
	Applied Desired
}

// Observe implements Kernel.
func (k *StaticKernel) Observe(context.Context, string) (Observed, error) {
	if k.ObserveErr != nil {
		return Observed{}, k.ObserveErr
	}
	obs := Observed{
		Known:   !k.Unknown,
		Present: k.Present,
		Foreign: k.Foreign,
		Up:      k.Up,
		Lines:   append([]string(nil), k.State...),
		Peers:   append([]PeerState(nil), k.PeerStates...),
	}
	sort.Strings(obs.Lines)
	return obs, nil
}

// Apply implements Kernel.
func (k *StaticKernel) Apply(_ context.Context, d Desired) ([]Step, error) {
	k.Applied = d

	steps := []Step{
		{Description: "create " + d.Interface, Done: true},
		{Description: "address " + d.Interface, Done: true},
		{Description: "load the peer configuration", Done: true},
		{Description: "bring " + d.Interface + " up", Done: true},
	}
	if !d.Enabled {
		steps = []Step{{Description: "remove " + d.Interface, Done: true}}
	}

	if k.ApplyErr != nil {
		at := k.FailAfter
		if at < 0 || at >= len(steps) {
			at = 0
		}
		steps = steps[:at+1]
		steps[at] = Step{Description: steps[at].Description, Error: k.ApplyErr.Error()}
		return steps, k.ApplyErr
	}

	k.State = d.Lines()
	k.Present = d.Enabled
	k.Up = d.Enabled
	sort.Strings(k.State)
	return steps, nil
}

// parsePrefixes turns the kernel's comma-separated allowed-ips into values.
//
// Shared by the real kernel and by tests that build an Observed by hand, so the
// two cannot disagree about whether a space after the comma matters.
func parsePrefixes(in []string) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(in))
	for _, s := range in {
		if p, err := netip.ParsePrefix(s); err == nil {
			out = append(out, p)
		}
	}
	return out
}
