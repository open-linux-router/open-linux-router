package dhcp

import (
	"strings"
	"testing"
)

func TestDiffShowsChangedLines(t *testing.T) {
	before := "alpha\nbravo\ncharlie\n"
	after := "alpha\nbravo-changed\ncharlie\n"

	got := Change{Path: "/etc/x.conf", Kind: ChangeUpdate, Impact: ImpactRestart,
		Before: []byte(before), After: []byte(after)}.Diff()

	for _, want := range []string{"-bravo", "+bravo-changed", " alpha", " charlie", "/etc/x.conf", "restart"} {
		if !strings.Contains(got, want) {
			t.Errorf("diff missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "-alpha") {
		t.Errorf("unchanged line reported as removed:\n%s", got)
	}
}

// A two-line edit in a long config must read as a two-line edit, or `olr diff`
// is no more use than "something changed".
func TestDiffCollapsesUnchangedRuns(t *testing.T) {
	var before, after []string
	for i := range 40 {
		before = append(before, "line "+string(rune('a'+i%26))+"-"+strings.Repeat("x", i%3))
	}
	after = append([]string{}, before...)
	after[20] = "changed"

	got := Change{Path: "f", Kind: ChangeUpdate,
		Before: []byte(strings.Join(before, "\n") + "\n"),
		After:  []byte(strings.Join(after, "\n") + "\n")}.Diff()

	if !strings.Contains(got, "unchanged lines @@") {
		t.Errorf("long unchanged runs were not collapsed:\n%s", got)
	}
	body := strings.Count(got, "\n")
	if body > 15 {
		t.Errorf("a one-line edit produced a %d line diff:\n%s", body, got)
	}
}

func TestDiffOfACreatedFileIsAllAdditions(t *testing.T) {
	got := Change{Path: "f", Kind: ChangeCreate, After: []byte("one\ntwo\n")}.Diff()
	if !strings.Contains(got, "+one") || !strings.Contains(got, "+two") {
		t.Errorf("created file not shown as additions:\n%s", got)
	}
	if strings.Contains(got, "-") && strings.Contains(got, "\n-") {
		t.Errorf("created file shows removals:\n%s", got)
	}
}

func TestDiffOfADeletedFileIsAllRemovals(t *testing.T) {
	got := Change{Path: "f", Kind: ChangeDelete, Before: []byte("one\ntwo\n")}.Diff()
	if !strings.Contains(got, "-one") || !strings.Contains(got, "-two") {
		t.Errorf("deleted file not shown as removals:\n%s", got)
	}
}

// The line-level diffing — diffOps, the context collapsing, the pluralisation —
// moved to core when the dns module needed the same thing, and its table tests
// went with it. What stays here is what is this module's: that a Change renders
// with a header naming the path and what applying it costs.
