package remote

import (
	"net/netip"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes for things the module models internally.
//
// design.md §4.5: the model and the query interface are ours, always. These
// types exist so that "what the HTTP API returns" is a deliberate decision
// rather than a side effect of which fields happened to be exported — which
// matters more here than usual, because one of the fields that must never be
// exported is the box's private key.

// planView is a Plan with the diff rendered.
//
// The canonical lines are safe to render in full, unlike the rendered files of
// internal/ingress: the private key is not among them by construction
// (Desired.PrivateKey), so there is no secret to withhold and no rule for a
// future caller to forget.
type planView struct {
	Changes []Change `json:"changes"`
	Impact  Impact   `json:"impact"`

	// Reasons explains the impact in the operator's terms.
	Reasons []string `json:"reasons,omitempty"`

	// Blocked is set when the interface name belongs to something else.
	Blocked string `json:"blocked,omitempty"`

	// Empty is the drift answer (§5.4), precomputed so a client does not have
	// to reimplement what counts as "no change".
	Empty bool `json:"empty"`

	// Known reports whether the kernel could be read. Empty means opposite
	// things with and without it — "nothing to do" against "we could not look"
	// — and a screen has to say them differently.
	Known bool `json:"known"`

	// Diff is the unified rendering, for a UI that shows the lines next to the
	// impact that classified them.
	Diff string `json:"diff,omitempty"`

	// Warnings are findings that did not block the change.
	Warnings []core.Problem `json:"warnings,omitempty"`
}

func viewPlan(p Plan, before, after []string) planView {
	v := planView{
		Changes:  p.Changes,
		Impact:   p.Impact,
		Reasons:  p.Reasons,
		Blocked:  p.Blocked,
		Empty:    p.Empty(),
		Known:    p.Known,
		Warnings: problems(p.Validation.Warnings),
	}
	if len(p.Changes) > 0 {
		v.Diff = p.Diff(before, after)
	}
	return v
}

// peerView is one peer as the API reports it: what the operator stored, joined
// to what the kernel knows right now.
//
// The join is the point. "phone, 10.6.0.2, home" is the configuration and
// answers nothing about whether remote access is working; "last handshake 12
// seconds ago" is the only signal WireGuard offers, and it is not in the
// config.
type peerView struct {
	Name    string     `json:"name"`
	Address netip.Addr `json:"address,omitempty"`
	Routes  RouteScope `json:"routes"`

	// PublicKey is reported. It is not a secret — it is in every client
	// configuration and in `wg show` — and it is the only way to match a peer
	// against output from the tool.
	PublicKey string `json:"public_key,omitempty"`

	// LastHandshake is zero for a peer that has never connected, and that zero
	// is load-bearing: "never" means the configuration was probably never
	// imported, while "an hour ago" means it works and the device is asleep.
	LastHandshake time.Time `json:"last_handshake,omitzero"`

	// Online is LastHandshake read against the clock, so that no surface has to
	// know what interval counts. WireGuard rekeys about every two minutes while
	// traffic is flowing, so a peer that handshaked inside OnlineWindow is
	// connected now.
	Online bool `json:"online"`

	// Endpoint is where the peer was last heard from — its address out in the
	// world.
	Endpoint string `json:"endpoint,omitempty"`

	RxBytes uint64 `json:"rx_bytes,omitempty"`
	TxBytes uint64 `json:"tx_bytes,omitempty"`

	// Unknown reports that the kernel could not be read, so every observed
	// field above is absent rather than zero. Without it a developer's laptop
	// would report every peer as having never connected, which is a claim
	// rather than an absence of one.
	Unknown bool `json:"unknown,omitempty"`
}

// OnlineWindow is how recently a peer must have handshaked to count as
// connected.
//
// Three minutes, against WireGuard's rekey interval of about two: the extra
// minute is slack for a peer that is idle between rekeys, and the alternative —
// a tighter window — would show an operator's own phone as offline while they
// are using it.
const OnlineWindow = 3 * time.Minute

func viewPeers(c Config, obs Observed, now time.Time) []peerView {
	live := make(map[string]PeerState, len(obs.Peers))
	for _, p := range obs.Peers {
		live[p.PublicKey] = p
	}

	out := make([]peerView, 0, len(c.WireGuard.Peers))
	for _, p := range c.WireGuard.Peers {
		v := peerView{
			Name:      p.Name,
			Address:   p.Address,
			Routes:    p.Routes.OrDefault(),
			PublicKey: p.PublicKey,
			Unknown:   !obs.Known,
		}
		if state, ok := live[p.PublicKey]; ok && obs.Known {
			v.LastHandshake = state.LastHandshake
			v.Endpoint = state.Endpoint
			v.RxBytes, v.TxBytes = state.RxBytes, state.TxBytes
			v.Online = !state.LastHandshake.IsZero() &&
				now.Sub(state.LastHandshake) < OnlineWindow
		}
		out = append(out, v)
	}
	return out
}

// peerResult is what comes back from writing a peer, and it exists because
// this module has an effect the kernel does not hold: **a file on somebody's
// phone.**
//
// Two shapes reach it, and both are things an operator has to be told exactly
// once, at the moment they are true:
//
//   - a new peer, with the configuration to import. Returned once, in the
//     response to the request that created it, because the private key it
//     contains is stored nowhere (docs/remote-access.md §4.1).
//   - an edit that only the device can apply. `routes` lives in the file rather
//     than in the tunnel, so changing it is a line for the operator to edit —
//     and a response that said nothing would leave them believing olr had done
//     it.
type peerResult struct {
	Name    string     `json:"name"`
	Address netip.Addr `json:"address,omitempty"`

	// ClientConfig is the file a device imports. Absent on an edit, and absent
	// when the operator supplied the public key themselves, because olr cannot
	// write an `[Interface]` section for a private key it never had.
	ClientConfig string `json:"client_config,omitempty"`

	// Note says what to do with what is here, or why what is expected is not. A
	// field rather than something each surface words for itself.
	Note string `json:"note,omitempty"`

	// NextSteps are commands in *other* modules that this one deliberately does
	// not run.
	//
	// One today, and it is the whole of docs/remote-access.md §3's naming story.
	// A local name is `dns`'s stored intent; writing one from here would be
	// inferring a change to another module's configuration, which design.md
	// §5.6 forbids, and would leave `dns` drifted until somebody applied it.
	// What is left is to say the command with the address already in it, at the
	// one moment the operator is looking at that address.
	NextSteps []string `json:"next_steps,omitempty"`
}

func problems(in []Problem) []core.Problem {
	if len(in) == 0 {
		return nil
	}
	out := make([]core.Problem, 0, len(in))
	for _, p := range in {
		out = append(out, core.Problem{Path: p.Path, Message: p.Message})
	}
	return out
}

// stamp is the freshness every observed reply carries (§4.5), so no surface can
// imply a currency it does not have.
func stamp() time.Time { return time.Now() }
