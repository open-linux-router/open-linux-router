package devices

import (
	"fmt"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes, for the reason internal/dhcp/view.go gives: what the
// HTTP API returns should be a deliberate decision rather than a side effect of
// which fields happened to be exported.

// deviceView is one row of the device list.
type deviceView struct {
	MAC string `json:"mac"`

	// Name is already resolved — stored name, else observed hostname, else
	// empty. NameOrigin says which, because "we call it this" and "it calls
	// itself this" are different claims.
	Name       string `json:"name"`
	NameOrigin Origin `json:"name_origin,omitempty"`

	Category       Category `json:"category"`
	CategoryOrigin Origin   `json:"category_origin,omitempty"`

	// DetectedCategory and DetectReason are what inference produced regardless
	// of whether it won, so a UI can offer the guess for confirmation on an
	// untouched device and can show an override *as* an override.
	DetectedCategory Category `json:"detected_category,omitempty"`
	DetectReason     string   `json:"detect_reason,omitempty"`
	Vendor           string   `json:"vendor,omitempty"`

	Model string `json:"model,omitempty"`
	Notes string `json:"notes,omitempty"`

	// Stored reports whether a human has described this device. False means it
	// is listed purely because it was seen, which is what the UI uses to offer
	// "name this device".
	Stored bool `json:"stored"`

	// Online is presence, computed against the response's as_of so a client
	// never has to guess which clock to compare against.
	Online bool `json:"online"`

	// Seen is false for a stored device that no source has observed — the
	// powered-off printer. Distinguished from Online because "we have never
	// seen this" and "we saw this and it is away" are different facts, and
	// collapsing them would make a typo'd MAC look like an offline device.
	Seen bool `json:"seen"`

	IPs      []string `json:"ips,omitempty"`
	Hostname string   `json:"hostname,omitempty"`
	Sources  []Source `json:"sources,omitempty"`

	// Expires is null for a device with no lease, and for a lease that never
	// expires. A pointer rather than a zero time, for the reason leaseView
	// gives in internal/dhcp/view.go.
	Expires *time.Time `json:"expires"`

	// FixedIP is the reserved address. Owned by dhcp; joined here so that no
	// client has to reimplement the join (§11.1).
	FixedIP string `json:"fixed_ip,omitempty"`
}

func viewDevice(r Resolved) deviceView {
	v := deviceView{
		MAC:              r.MAC,
		Name:             r.Name,
		NameOrigin:       r.NameOrigin,
		Category:         r.Category,
		CategoryOrigin:   r.CategoryOrigin,
		DetectedCategory: r.Detected.Category,
		DetectReason:     r.Detected.Reason,
		Vendor:           r.Detected.Vendor,
		Model:            r.Model,
		Notes:            r.Notes,
		Stored:           r.Stored,
		Online:           r.Online(),
		Seen:             r.Presence != nil,
		FixedIP:          r.FixedIP,
	}
	if r.Presence != nil {
		v.IPs = r.Presence.IPs
		v.Hostname = r.Presence.Hostname
		v.Sources = r.Presence.Sources
		v.Expires = r.Presence.Expires
	}
	return v
}

type listResponse struct {
	Devices []deviceView `json:"devices"`

	// Problems are the things that went wrong without taking the answer down:
	// an unreadable ARP table, a garbled lease line. Reported rather than
	// dropped so a partial list is visible as partial.
	Problems []core.Problem `json:"problems,omitempty"`

	// AsOf stamps the whole reply, because every observed object carries its
	// freshness (§4.5).
	AsOf time.Time `json:"as_of"`
}

// --- plan ------------------------------------------------------------------

// planView mirrors internal/dhcp's plan shape on purpose.
//
// The field names are identical so that a client's Plan type and its plan
// preview render either module's answer without a second implementation. The
// values are what they honestly are for a module with no backend: no service
// action, and an impact of none — storing a name cannot drop a client.
type planView struct {
	Backend  string         `json:"backend"`
	Changes  []changeView   `json:"changes"`
	Action   string         `json:"action"`
	Impact   string         `json:"impact"`
	Empty    bool           `json:"empty"`
	Warnings []core.Problem `json:"warnings,omitempty"`
}

type changeView struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Impact string `json:"impact"`
	Diff   string `json:"diff"`
}

const (
	impactNone = "none"
	actionNone = "none"

	kindCreate = "create"
	kindUpdate = "update"
	kindDelete = "delete"
)

// buildPlan diffs stored intent against a proposal.
//
// A plan for this module is not about the system — nothing on the box changes —
// it is about the document. It exists so `olr --dry-run` and an agent proposing
// a change for a human to review (§6.4) work here the same way they work
// everywhere else, and so the UI can skip a write that would do nothing.
func buildPlan(stored, desired Config) planView {
	stored.Normalize()
	desired.Normalize()

	res := Validate(desired)
	plan := planView{
		Backend:  "",
		Changes:  []changeView{},
		Action:   actionNone,
		Impact:   impactNone,
		Warnings: problems(res.Warnings),
	}

	before := map[string]Device{}
	for _, d := range stored.Devices {
		before[d.MAC] = d
	}
	after := map[string]Device{}
	for _, d := range desired.Devices {
		after[d.MAC] = d
	}

	// Iterated over the sorted desired list, then the sorted stored list, so
	// the change order is deterministic rather than map-order.
	for _, d := range desired.Devices {
		old, existed := before[d.MAC]
		switch {
		case !existed:
			plan.Changes = append(plan.Changes, changeView{
				Path:   path(d.MAC),
				Kind:   kindCreate,
				Impact: impactNone,
				Diff:   describe(d, "+"),
			})
		case old != d:
			plan.Changes = append(plan.Changes, changeView{
				Path:   path(d.MAC),
				Kind:   kindUpdate,
				Impact: impactNone,
				Diff:   describe(old, "-") + describe(d, "+"),
			})
		}
	}
	for _, d := range stored.Devices {
		if _, kept := after[d.MAC]; !kept {
			plan.Changes = append(plan.Changes, changeView{
				Path:   path(d.MAC),
				Kind:   kindDelete,
				Impact: impactNone,
				Diff:   describe(d, "-"),
			})
		}
	}

	plan.Empty = len(plan.Changes) == 0
	return plan
}

func path(mac string) string { return fmt.Sprintf("devices[%s]", mac) }

// describe renders a device as prefixed lines, omitting fields that are unset
// so a diff shows what was said rather than a form full of blanks.
func describe(d Device, prefix string) string {
	var b strings.Builder
	line := func(k, v string) {
		if v != "" {
			fmt.Fprintf(&b, "%s %s: %s\n", prefix, k, v)
		}
	}
	line("mac", d.MAC)
	line("name", d.Name)
	line("category", string(d.Category))
	line("model", d.Model)
	line("notes", d.Notes)
	return b.String()
}
