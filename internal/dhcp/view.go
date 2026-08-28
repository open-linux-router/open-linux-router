package dhcp

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

// planView is a Plan with the diffs included.
//
// Plan omits file contents from JSON because a rendered dnsmasq config is long
// and the CLI does not always want it. The UI does: §5.3.3's impact
// classification is only actionable next to the lines that caused it —
// "disruptive" with no diff is a scarier spinner, not an explanation.
type planView struct {
	Backend string        `json:"backend"`
	Changes []changeView  `json:"changes"`
	Action  ServiceAction `json:"action"`
	Impact  Impact        `json:"impact"`

	// Enable, when non-nil, is the boot-time state the unit will be moved to.
	// Surfaced because "running now" and "running after a reboot" are different
	// promises, and only one of them is visible without asking.
	Enable *bool `json:"enable,omitempty"`

	// Reasons explains the impact in the operator's terms — above all, which
	// clients a disruptive change would drop.
	Reasons []string `json:"reasons,omitempty"`

	// Empty is the drift answer (§5.4), precomputed so a client does not have
	// to reimplement what counts as "no change".
	Empty bool `json:"empty"`

	// Warnings are findings that did not block the change. Plan carries these
	// with `json:"-"` internally; they are the whole point of a preview, so
	// they are surfaced here.
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Path   string     `json:"path"`
	Kind   ChangeKind `json:"kind"`
	Impact Impact     `json:"impact"`
	Diff   string     `json:"diff"`
}

func viewPlan(p Plan) planView {
	v := planView{
		Backend:  p.Backend,
		Changes:  make([]changeView, 0, len(p.Changes)),
		Action:   p.Action,
		Impact:   p.Impact,
		Enable:   p.Enable,
		Reasons:  p.Reasons,
		Empty:    p.Empty(),
		Warnings: problems(p.Validation.Warnings),
	}
	for _, c := range p.Changes {
		v.Changes = append(v.Changes, changeView{
			Path:   c.Path,
			Kind:   c.Kind,
			Impact: c.Impact,
			Diff:   c.Diff(),
		})
	}
	// An action of "" from a zero Plan would be an odd thing to publish; the
	// vocabulary has a word for nothing-to-do.
	if v.Action == "" {
		v.Action = ActionNone
	}
	return v
}

// leaseView is one lease as the API presents it.
type leaseView struct {
	IP       string `json:"ip"`
	MAC      string `json:"mac,omitempty"`
	IAID     string `json:"iaid,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	ClientID string `json:"client_id,omitempty"`

	// Expires is null for a lease that never expires. A pointer rather than a
	// zero time because "0001-01-01T00:00:00Z means infinite" is exactly the
	// kind of encoding trap every client would have to be told about
	// separately, and one of them would not be.
	Expires *time.Time `json:"expires"`

	// Active is computed against the same instant as the response's as_of, so
	// a client never has to guess which clock to compare against.
	Active bool `json:"active"`
}

func viewLease(l Lease, now time.Time) leaseView {
	v := leaseView{
		IP:       l.IP.String(),
		MAC:      l.MAC,
		IAID:     l.IAID,
		Hostname: l.Hostname,
		ClientID: l.ClientID,
		Active:   l.Active(now),
	}
	if !l.Expires.IsZero() {
		expires := l.Expires
		v.Expires = &expires
	}
	return v
}

// usageView is pool occupancy, with the derived numbers spelled out.
//
// Free and Percent are methods on Usage, so they would vanish from JSON. A
// client recomputing them would be a second implementation of "how full is this
// pool" that could disagree with ours.
type usageView struct {
	Interface string `json:"interface"`
	Size      int    `json:"size"`
	Active    int    `json:"active"`
	Expired   int    `json:"expired"`
	Free      int    `json:"free"`
	Percent   int    `json:"percent"`
}

func viewUsage(u Usage) usageView {
	return usageView{
		Interface: u.Interface,
		Size:      u.Size,
		Active:    u.Active,
		Expired:   u.Expired,
		Free:      u.Free(),
		Percent:   u.Percent(),
	}
}

// problems converts the module's validation findings into core's wire shape, so
// that every module reports a bad field the same way and a UI needs one
// renderer rather than one per module.
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
