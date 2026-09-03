package core

import (
	"strings"
	"testing"
)

func TestDiffOps(t *testing.T) {
	tests := []struct {
		name          string
		before, after []string
		want          []string
	}{
		{"identical", []string{"a", "b"}, []string{"a", "b"}, []string{" a", " b"}},
		{"insert", []string{"a", "c"}, []string{"a", "b", "c"}, []string{" a", "+b", " c"}},
		{"delete", []string{"a", "b", "c"}, []string{"a", "c"}, []string{" a", "-b", " c"}},
		{"replace", []string{"a"}, []string{"b"}, []string{"-a", "+b"}},
		{"empty before", nil, []string{"a"}, []string{"+a"}},
		{"empty after", []string{"a"}, nil, []string{"-a"}},
		{"both empty", nil, nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := diffOps(tc.before, tc.after)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("diffOps = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLineDiffMarksTheChangedLine(t *testing.T) {
	got := strings.Join(LineDiff(
		[]byte("alpha\nbravo\ncharlie\n"),
		[]byte("alpha\nbravo-changed\ncharlie\n"),
	), "\n")

	for _, want := range []string{"-bravo", "+bravo-changed", " alpha", " charlie"} {
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
func TestLineDiffCollapsesUnchangedRuns(t *testing.T) {
	var before []string
	for i := range 40 {
		before = append(before, "line "+string(rune('a'+i%26))+"-"+strings.Repeat("x", i%3))
	}
	after := append([]string{}, before...)
	after[20] = "changed"

	got := LineDiff(
		[]byte(strings.Join(before, "\n")+"\n"),
		[]byte(strings.Join(after, "\n")+"\n"),
	)
	joined := strings.Join(got, "\n")

	if !strings.Contains(joined, "unchanged lines @@") {
		t.Errorf("long unchanged runs were not collapsed:\n%s", joined)
	}
	if len(got) > 12 {
		t.Errorf("a one-line edit produced a %d line diff:\n%s", len(got), joined)
	}
}

// Empty content is no lines at all, not one empty line: a created file must
// diff as pure additions.
func TestSplitLinesTreatsEmptyAsNoLines(t *testing.T) {
	if got := SplitLines(nil); got != nil {
		t.Errorf("SplitLines(nil) = %v, want nil", got)
	}
	if got := SplitLines([]byte("one\ntwo\n")); len(got) != 2 {
		t.Errorf("SplitLines = %v, want 2 lines", got)
	}
	// A trailing newline is a terminator, not a separator introducing a third
	// line — otherwise every rendered file would diff against itself.
	if got := SplitLines([]byte("one\n")); len(got) != 1 || got[0] != "one" {
		t.Errorf("SplitLines(%q) = %v", "one\n", got)
	}
}

func TestPlural(t *testing.T) {
	if got := Plural(1, "line"); got != "1 line" {
		t.Errorf("Plural(1) = %q", got)
	}
	if got := Plural(3, "line"); got != "3 lines" {
		t.Errorf("Plural(3) = %q", got)
	}
	if got := Plural(0, "line"); got != "0 lines" {
		t.Errorf("Plural(0) = %q", got)
	}
}
