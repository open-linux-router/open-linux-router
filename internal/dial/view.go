package dial

import (
	"fmt"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// The API's own shapes, for the reason internal/dhcp/view.go gives: what the
// HTTP API returns should be a deliberate decision rather than a side effect of
// which fields happened to be exported.

// recordView is one row of the status list.
//
// Flat rather than nested, and the flatness is load-bearing: docs/ddns.md §6
// says status answers three separate questions, and a nested `health` object
// with one verdict in it is exactly the collapse that makes the interesting
// failure — read fine, published days ago, provider refusing since — invisible.
type recordView struct {
	Name     string `json:"name"`
	Provider string `json:"provider"`

	// Source and From say where the address comes from, in the two halves an
	// operator reads separately: which mechanism, and which interface or
	// endpoint.
	Source Source `json:"source"`
	From   string `json:"from,omitempty"`

	// The three facts (docs/ddns.md §6). Spelled out rather than embedding
	// RecordState, for one reason: `omitempty` does nothing for a time.Time, so
	// an embedded struct would publish `"published": "0001-01-01T00:00:00Z"` for
	// a record that has never been published — a timestamp in the year 1 that a
	// client has to know to special-case. A pointer is absent instead, which is
	// what "it has not happened" should look like on the wire.
	Checked    *time.Time `json:"checked,omitempty"`
	CheckError string     `json:"check_error,omitempty"`

	Address string `json:"address,omitempty"`
	CGNAT   bool   `json:"cgnat,omitempty"`

	Published        *time.Time `json:"published,omitempty"`
	PublishedAddress string     `json:"published_address,omitempty"`
	PublishError     string     `json:"publish_error,omitempty"`

	Failures int        `json:"failures,omitempty"`
	Retry    *time.Time `json:"retry,omitempty"`

	// Problems are this record's findings from the validator, so a row that is
	// misconfigured says so beside itself rather than only in a list at the
	// bottom.
	Problems []core.Problem `json:"problems,omitempty"`

	// Watched reports whether the publisher currently has a loop for this
	// record. False with no problems means olrd has not picked the config up
	// yet, which is a real transient state right after a write.
	Watched bool `json:"watched"`
}

// viewState projects the publisher's state onto the wire shape.
func viewState(v *recordView, s RecordState) {
	v.Checked = stamp(s.Checked)
	v.CheckError = s.CheckError
	v.Address = s.Address
	v.CGNAT = s.CGNAT
	v.Published = stamp(s.Published)
	v.PublishedAddress = s.PublishedAddress
	v.PublishError = s.PublishError
	v.Failures = s.Failures
	v.Retry = stamp(s.Retry)
}

// stamp turns "never happened" into an absent field rather than a zero time.
func stamp(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// statusResponse is the whole module's observed state.
type statusResponse struct {
	Records []recordView `json:"records"`

	// AsOf stamps the reply: nothing here is stored, so it carries its
	// freshness (§4.5).
	AsOf time.Time `json:"as_of"`
}

// From renders the source's parameter for a status row.
func recordFrom(r Record) string {
	switch r.Source {
	case SourceInterface:
		return r.Interface
	case SourceReflector:
		if r.Interface != "" {
			return r.ReflectorURL + " (via " + r.Interface + ")"
		}
		return r.ReflectorURL
	default:
		return ""
	}
}

// --- plan -------------------------------------------------------------------

// planView mirrors internal/link's plan shape on purpose.
//
// The field names are identical so that a client's Plan type and its plan
// preview render any module's answer without a second implementation. The
// values are what they honestly are for a module that writes only a document:
// no backend, no service action, and an impact of none — storing a record
// cannot drop a client, because by itself it does nothing at all.
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

// buildPlan diffs stored records against a proposal.
func buildPlan(stored, desired Config, links LinkView) planView {
	stored.Normalize()
	desired.Normalize()

	res := Validate(desired, links)
	plan := planView{
		Backend:  "",
		Changes:  []changeView{},
		Action:   actionNone,
		Impact:   impactNone,
		Warnings: problems(res.Warnings),
	}

	for _, want := range desired.Records {
		have, existed := stored.Find(want.Name)
		switch {
		case !existed:
			plan.Changes = append(plan.Changes, changeView{
				Path:   itemPath(want.Name),
				Kind:   kindCreate,
				Impact: impactNone,
				Diff:   diffRecord(Record{}, want),
			})
			// The one consequence the machine's own state does not show, said
			// at the moment the operator is looking at the preview: from here
			// on this box talks to somebody it did not talk to before, on a
			// timer, whether or not anything else changes.
			plan.Warnings = append(plan.Warnings, core.Problem{
				Path: itemPath(want.Name),
				Message: fmt.Sprintf(
					"olr will tell %s this box's address every %s from now on%s",
					want.Provider, Duration(want.ResolvedInterval()), reflectorNote(want)),
			})
		case have != want:
			plan.Changes = append(plan.Changes, changeView{
				Path:   itemPath(want.Name),
				Kind:   kindUpdate,
				Impact: impactNone,
				Diff:   diffRecord(have, want),
			})
		}
	}

	for _, have := range stored.Records {
		if _, kept := desired.Find(have.Name); kept {
			continue
		}
		plan.Changes = append(plan.Changes, changeView{
			Path:   itemPath(have.Name),
			Kind:   kindDelete,
			Impact: impactNone,
			Diff:   diffRecord(have, Record{}),
		})
		// Removing a record stops olr updating the name. It does not remove the
		// name: the last address published stays at the provider, answering,
		// until somebody deletes it there. Saying so here is the difference
		// between an operator who knows the name is now frozen and one who
		// believes it is gone.
		plan.Warnings = append(plan.Warnings, core.Problem{
			Path: itemPath(have.Name),
			Message: fmt.Sprintf("%s stops being kept current, but it stays at %s "+
				"pointing at the address last published; delete it there if you want it gone",
				have.Name, have.Provider),
		})
	}

	plan.Empty = len(plan.Changes) == 0
	return plan
}

func reflectorNote(r Record) string {
	if r.Source != SourceReflector || r.ReflectorURL == "" {
		return ""
	}
	return ", and ask " + r.ReflectorURL + " what that address is"
}

func itemPath(name string) string { return "records[" + name + "]" }

// diffRecord renders one record's change, redacted.
//
// Both credential fields go through Redacted before they reach here, so the
// diff an operator prints — and pastes into a bug report — carries the shape of
// the change and none of the secret.
func diffRecord(before, after Record) string {
	var out string
	for _, f := range recordFields(before, after) {
		switch {
		case f.before == f.after:
			continue
		case f.before == "":
			out += fmt.Sprintf("+ %s %s\n", f.name, f.after)
		case f.after == "":
			out += fmt.Sprintf("- %s %s\n", f.name, f.before)
		default:
			out += fmt.Sprintf("- %s %s\n+ %s %s\n", f.name, f.before, f.name, f.after)
		}
	}
	return out
}

type fieldDiff struct{ name, before, after string }

func recordFields(before, after Record) []fieldDiff {
	b, a := redactRecord(before), redactRecord(after)
	return []fieldDiff{
		{"name", b.Name, a.Name},
		{"provider", b.Provider.String(), a.Provider.String()},
		{"zone", b.Zone, a.Zone},
		{"key-id", b.KeyID, a.KeyID},
		{"token", b.Token, a.Token},
		{"callback-url", b.CallbackURL, a.CallbackURL},
		{"source", string(b.Source), string(a.Source)},
		{"interface", b.Interface, a.Interface},
		{"reflector", b.ReflectorURL, a.ReflectorURL},
		{"interval", durationField(b.Interval), durationField(a.Interval)},
		{"ttl", intField(b.TTL), intField(a.TTL)},
	}
}

func redactRecord(r Record) Record {
	c := Config{Records: []Record{r}}.Redacted()
	return c.Records[0]
}

func durationField(d Duration) string {
	if d == 0 {
		return ""
	}
	return d.String()
}

func intField(n int) string {
	if n == 0 {
		return ""
	}
	return fmt.Sprint(n)
}
