package routing

import (
	"fmt"
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
// They are hand-written on the TypeScript side too, which is the gap
// design.md §6.2 names: core reflects config structs and not response shapes,
// so this file and web/src/lib/api-types.ts have to change together.

// planView is a Plan with the diff included.
type planView struct {
	Changes []changeView  `json:"changes"`
	Impact  Impact        `json:"impact"`
	Foreign []ForeignRule `json:"foreign,omitempty"`
	Reasons []string      `json:"reasons,omitempty"`

	// Blocked is §6's refusal. A string rather than a boolean because the
	// operator has to go and find another program's configuration file, and
	// "blocked: true" does not tell them where to look.
	Blocked string `json:"blocked,omitempty"`

	// Empty is the drift answer (§5.4), precomputed so a client does not have
	// to reimplement what counts as "no change".
	Empty bool `json:"empty"`

	// Known reports whether the kernel could be read at all. Without it a
	// client cannot tell "nothing to do" from "we could not look", and those
	// need very different words on screen.
	Known bool `json:"known"`

	// Diff is the whole change as unified text, which is what makes an impact
	// actionable — "disruptive" with no diff is a scarier spinner, not an
	// explanation.
	Diff string `json:"diff,omitempty"`

	// Warnings are findings that did not block the change. Plan carries these
	// with `json:"-"` internally; they are the whole point of a preview, so
	// they are surfaced here.
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
		Blocked:  p.Blocked,
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

// statusView is `olr routing status`.
type statusView struct {
	Enabled bool `json:"enabled"`

	// Known reports whether the kernel answered. False on a machine that is not
	// a router, and on one where olrd lacks CAP_NET_ADMIN — two situations a
	// client should describe differently from "nothing is configured".
	Known bool `json:"known"`

	Exits       []exitStatusView       `json:"exits"`
	Assignments []assignmentStatusView `json:"assignments"`

	// Drifted is design.md §5.4: the plan against unchanged intent is not
	// empty, so the kernel disagrees with what was asked for.
	Drifted bool `json:"drifted"`

	// Foreign is §7.3's residual rule at the routing layer — somebody else's
	// rules, reported rather than hidden, because a hand-rolled setup that is
	// visible is one an operator can reason about.
	Foreign []ForeignRule `json:"foreign,omitempty"`

	// Problems carries the validation warnings for the stored config, so the
	// status screen is where a half-finished setup gets noticed.
	Problems []core.Problem `json:"problems,omitempty"`

	AsOf time.Time `json:"as_of"`
}

type exitStatusView struct {
	Name string  `json:"name"`
	Via  ViaKind `json:"via"`

	// Up and Probed together: an exit nobody probes is reported up, and saying
	// so without saying it was never checked would be claiming knowledge we do
	// not have (§5.6's third obligation — faults must not hide inside a
	// default).
	Up     bool `json:"up"`
	Probed bool `json:"probed"`

	UsedBy []string `json:"used_by,omitempty"`

	// The kernel resources this exit holds. Published because §3.2's whole
	// argument for documenting the ranges is that somebody can plan around
	// them, which they cannot do without the numbers this box actually used.
	Mark     string `json:"mark"`
	Table    int    `json:"table"`
	Priority int    `json:"priority"`
}

type assignmentStatusView struct {
	Interface string `json:"interface"`

	// Exit is the effective value and Source is which rung of the ladder set
	// it — §2.2, without which inheritance is unusable.
	Exit   string `json:"exit"`
	Source Source `json:"source"`

	Reason string `json:"reason,omitempty"`
}

func viewStatus(s Status, warnings []Problem, now time.Time) statusView {
	v := statusView{
		Enabled:     s.Enabled,
		Known:       s.Known,
		Drifted:     s.Drifted,
		Foreign:     s.Foreign,
		Problems:    problems(warnings),
		Exits:       make([]exitStatusView, 0, len(s.Exits)),
		Assignments: make([]assignmentStatusView, 0, len(s.Assignments)),
		AsOf:        now,
	}
	for _, e := range s.Exits {
		v.Exits = append(v.Exits, exitStatusView{
			Name:     e.Exit.Name,
			Via:      e.Exit.Via.Kind,
			Up:       e.Up,
			Probed:   e.Probed,
			UsedBy:   e.UsedBy,
			Mark:     markString(e.Mark),
			Table:    e.Table,
			Priority: e.Priority,
		})
	}
	for _, a := range s.Assignments {
		v.Assignments = append(v.Assignments, assignmentStatusView{
			Interface: a.Interface,
			Exit:      a.Exit,
			Source:    a.Source,
			Reason:    a.Reason,
		})
	}
	return v
}

// markString renders a mark the way it is written everywhere else — hex, eight
// digits — rather than as a JSON number. A reader comparing it against
// `nft list ruleset` or `ip rule` output should not have to convert.
func markString(m uint32) string { return fmt.Sprintf("%#08x", m) }
