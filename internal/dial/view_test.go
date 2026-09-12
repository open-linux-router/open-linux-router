package dial

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func now() time.Time { return time.Date(2026, 9, 12, 10, 0, 0, 0, time.UTC) }

// docs/ddns.md §6: the three questions stay separate. The state this asserts on
// is the one the whole module is arranged around — the address is fresh, and the
// record at the provider has been stale for four days.
func TestStatusSaysAllThreeThingsWhenPublishingIsBroken(t *testing.T) {
	published := now().Add(-96 * time.Hour)
	checked := now().Add(-30 * time.Second)

	view := recordView{
		Name: "home.example.net", Provider: "cloudflare",
		Source: SourceReflector, From: "https://reflector.example/", Watched: true,
	}
	viewState(&view, RecordState{
		Checked:          checked,
		Address:          "203.0.113.9",
		Published:        published,
		PublishedAddress: "198.51.100.1",
		PublishError:     "Invalid API Token (code 9109)",
		Failures:         3,
		Retry:            now().Add(time.Hour),
	})

	var out bytes.Buffer
	if err := writeStatusText(&out, statusResponse{Records: []recordView{view}, AsOf: now()}); err != nil {
		t.Fatal(err)
	}
	got := out.String()

	for _, want := range []string{
		"203.0.113.9",                  // what the address is
		"30s ago",                      // when it was read
		"4 days ago at 198.51.100.1",   // when it was last published, and as what
		"Invalid API Token",            // the provider's own words
		"backing off after 3 refusals", // and what happens next
	} {
		if !strings.Contains(got, want) {
			t.Errorf("status does not say %q:\n%s", want, got)
		}
	}
}

// The row that must not read as "pending".
func TestStatusDistinguishesNeverPublishedFromNotYet(t *testing.T) {
	refused := recordView{Name: "a.example.net", Watched: true}
	viewState(&refused, RecordState{Checked: now(), Address: "203.0.113.9", PublishError: "badauth"})

	var out bytes.Buffer
	if err := writeStatusText(&out, statusResponse{Records: []recordView{refused}, AsOf: now()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "never — the provider refused: badauth") {
		t.Errorf("a record that has never published and is being refused reads as pending:\n%s", out.String())
	}

	pending := recordView{Name: "b.example.net", Watched: true}
	viewState(&pending, RecordState{})
	out.Reset()
	if err := writeStatusText(&out, statusResponse{Records: []recordView{pending}, AsOf: now()}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "refused") {
		t.Errorf("a record that has not been checked yet reads as a failure:\n%s", out.String())
	}
}

// §3.2's diagnosis has to be a sentence, not a boolean somebody has to interpret.
func TestStatusExplainsCGNAT(t *testing.T) {
	view := recordView{Name: "home.example.net", Watched: true}
	viewState(&view, RecordState{
		Checked: now(), Address: "100.72.14.3", CGNAT: true,
		Published: now(), PublishedAddress: "100.72.14.3",
	})

	var out bytes.Buffer
	if err := writeStatusText(&out, statusResponse{Records: []recordView{view}, AsOf: now()}); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	if !strings.Contains(got, "carrier-grade NAT") || !strings.Contains(got, "port forward") {
		t.Errorf("the CGNAT verdict does not explain itself:\n%s", got)
	}
}

// `omitempty` does nothing for a time.Time, so a record that has never been
// published would otherwise report the year 1 — a value every client has to
// know to special-case.
func TestStatusOmitsTimestampsThatNeverHappened(t *testing.T) {
	view := recordView{Name: "home.example.net", Watched: true}
	viewState(&view, RecordState{})

	data, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "0001-01-01") {
		t.Errorf("a zero timestamp reached the wire: %s", data)
	}
	for _, absent := range []string{"checked", "published", "retry"} {
		if strings.Contains(string(data), `"`+absent+`"`) {
			t.Errorf("%q is present for a record nothing has happened to: %s", absent, data)
		}
	}
}

// The credential cannot reach a diff an operator prints and pastes into a bug
// report.
func TestThePlanDiffIsRedacted(t *testing.T) {
	desired := Config{Records: []Record{{
		Name: "home.example.net", Provider: "cloudflare", Token: "cf-secret",
		Source: SourceReflector, ReflectorURL: "https://reflector.example/",
	}}}

	plan := buildPlan(Config{}, desired, testLinks())
	if len(plan.Changes) != 1 {
		t.Fatalf("plan = %+v", plan)
	}
	if strings.Contains(plan.Changes[0].Diff, "cf-secret") {
		t.Fatalf("the token reached the diff:\n%s", plan.Changes[0].Diff)
	}
	if !strings.Contains(plan.Changes[0].Diff, RedactedToken) {
		t.Errorf("the diff does not show that a credential was set:\n%s", plan.Changes[0].Diff)
	}
}

// Adding a record does nothing to the box and starts this box talking to a
// third party on a timer. The plan is where an operator finds that out.
func TestThePlanSaysWhatAddingStarts(t *testing.T) {
	desired := Config{Records: []Record{{
		Name: "home.example.net", Provider: "cloudflare", Token: "t",
		Source: SourceReflector, ReflectorURL: "https://reflector.example/",
	}}}

	plan := buildPlan(Config{}, desired, testLinks())
	var joined string
	for _, w := range plan.Warnings {
		joined += w.Message + "\n"
	}
	if !strings.Contains(joined, "cloudflare") || !strings.Contains(joined, "reflector.example") {
		t.Errorf("the plan does not say who this box starts talking to:\n%s", joined)
	}
	if plan.Impact != impactNone {
		t.Errorf("Impact = %q; nothing on the box changes", plan.Impact)
	}
}

// Removing a record stops the updating; it does not remove the name. An
// operator who thinks otherwise leaves a stale public record behind.
func TestThePlanSaysRemovalLeavesTheNameBehind(t *testing.T) {
	stored := Config{Records: []Record{{
		Name: "home.example.net", Provider: "cloudflare", Token: "t",
		Source: SourceReflector, ReflectorURL: "https://reflector.example/",
	}}}

	plan := buildPlan(stored, Config{}, testLinks())
	if len(plan.Changes) != 1 || plan.Changes[0].Kind != kindDelete {
		t.Fatalf("plan = %+v", plan)
	}
	var joined string
	for _, w := range plan.Warnings {
		joined += w.Message + "\n"
	}
	if !strings.Contains(joined, "stays at") {
		t.Errorf("the plan does not say the name survives:\n%s", joined)
	}
}

func TestAnEmptyPlanIsEmpty(t *testing.T) {
	stored := Config{Records: []Record{{
		Name: "home.example.net", Provider: "cloudflare", Token: "t",
		Source: SourceReflector, ReflectorURL: "https://reflector.example/",
	}}}
	if plan := buildPlan(stored, stored, testLinks()); !plan.Empty {
		t.Errorf("planning a config against itself found changes: %+v", plan.Changes)
	}
}
