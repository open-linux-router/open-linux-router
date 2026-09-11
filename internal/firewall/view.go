package firewall

import (
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes for things the module models internally.
//
// design.md §4.5: the model and the query interface are ours, always —
// including for observed things. These types exist so that "what the HTTP API
// returns" is a deliberate decision rather than a side effect of which fields
// happened to be exported, and so that a change to an internal struct does not
// silently become a breaking API change.
//
// They are hand-written on the TypeScript side too, which is the gap design.md
// §6.2 names: core reflects config structs and not response shapes, so this file
// and web/src/lib/api-types.ts have to change together.

// planView is a Plan with the diff included.
type planView struct {
	Changes []changeView    `json:"changes"`
	Impact  Impact          `json:"impact"`
	Foreign []ForeignFilter `json:"foreign,omitempty"`
	Reasons []string        `json:"reasons,omitempty"`

	// Empty is the drift answer (§5.4), precomputed so a client does not have to
	// reimplement what counts as "no change".
	Empty bool `json:"empty"`

	// Known reports whether the kernel could be read at all. Without it a client
	// cannot tell "nothing to do" from "we could not look", and those need very
	// different words on screen.
	Known bool `json:"known"`

	// Diff is the whole change as unified text, which is what makes an impact
	// actionable — "disruptive" with no diff is a scarier spinner, not an
	// explanation.
	Diff string `json:"diff,omitempty"`

	// Warnings are findings that did not block the change. Plan carries these
	// with `json:"-"` internally; they are the whole point of a preview, so they
	// are surfaced here.
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Kind ChangeKind `json:"kind"`
	Line string     `json:"line"`
}

func viewPlan(p Plan, obs Observed, desired Desired) planView {
	v := planView{
		Changes:  make([]changeView, 0, len(p.Changes)),
		Impact:   p.Impact,
		Foreign:  p.Foreign,
		Reasons:  p.Reasons,
		Empty:    p.Empty(),
		Known:    obs.Known,
		Warnings: problems(p.Validation.Warnings),
	}
	for _, c := range p.Changes {
		v.Changes = append(v.Changes, changeView{Kind: c.Kind, Line: c.Line})
	}
	if obs.Known && len(p.Changes) > 0 {
		v.Diff = p.Diff(obs.Lines, desired.Lines())
	}
	return v
}

// applyResponse carries the plan and the steps alongside the stored config.
//
// Steps are present whether the apply succeeded or not: design.md §5.3.2 has no
// rollback, so a half-finished change stays half-finished and saying exactly
// which parts landed is the whole substitute for unwinding them.
type applyResponse struct {
	Plan   planView        `json:"plan"`
	Steps  []Step          `json:"steps,omitempty"`
	Config Config          `json:"config"`
	Error  *core.ErrorBody `json:"error,omitempty"`
}

// statusView is `olr firewall status`.
type statusView struct {
	Enabled bool `json:"enabled"`

	// Known reports whether the kernel answered. False on a machine that is not
	// a router, and on one where olrd lacks CAP_NET_ADMIN — two situations a
	// client should describe differently from "nothing is configured".
	Known bool `json:"known"`

	Forwards []forwardStatusView `json:"forwards"`

	// Drifted is design.md §5.4: the plan against unchanged intent is not empty,
	// so the kernel disagrees with what was asked for.
	Drifted bool `json:"drifted"`

	// Foreign is somebody else's filtering of forwarded traffic, reported rather
	// than hidden, because it is the reason a correct-looking forward may not
	// reach.
	Foreign []ForeignFilter `json:"foreign,omitempty"`

	// Problems carries the validation warnings for the stored config, so the
	// status screen is where a half-finished setup gets noticed.
	Problems []core.Problem `json:"problems,omitempty"`

	AsOf time.Time `json:"as_of"`
}

type forwardStatusView struct {
	Name     string   `json:"name"`
	In       string   `json:"in"`
	Protocol Protocol `json:"protocol"`
	Port     string   `json:"port"`
	To       string   `json:"to"`
	Hairpin  bool     `json:"hairpin"`

	// Counted and the two totals together: a forward nobody has counted is
	// reported at zero, and saying so without saying it was never measured would
	// be claiming knowledge we do not have (§5.6's third obligation — faults
	// must not hide inside a default).
	Counted bool   `json:"counted"`
	Packets uint64 `json:"packets"`
	Bytes   uint64 `json:"bytes"`
}

func viewStatus(s Status, warnings []Problem, now time.Time) statusView {
	v := statusView{
		Enabled:  s.Enabled,
		Known:    s.Known,
		Drifted:  s.Drifted,
		Foreign:  s.Foreign,
		Problems: problems(warnings),
		Forwards: make([]forwardStatusView, 0, len(s.Forwards)),
		AsOf:     now,
	}
	for _, f := range s.Forwards {
		v.Forwards = append(v.Forwards, forwardStatusView{
			Name:     f.Forward.Name,
			In:       f.Forward.In,
			Protocol: f.Forward.ProtocolOrDefault(),
			Port:     f.Forward.Port.String(),
			To:       f.Forward.To.String(),
			Hairpin:  f.Forward.HairpinOrDefault(),
			Counted:  f.Counted,
			Packets:  f.Packets,
			Bytes:    f.Bytes,
		})
	}
	return v
}
