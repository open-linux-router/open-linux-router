package dhcp

import (
	"strconv"
	"strings"
)

// Diff renders the change as a unified-style diff.
//
// `olr diff` is a first-class operation (design.md §6.1), and "3 files will
// change" is not an answer an operator can act on. Showing the lines is what
// lets a human — or an agent proposing a change for review (§6.4) — see that a
// pool moved by one address rather than that something moved.
func (c Change) Diff() string {
	var b strings.Builder
	b.WriteString("--- " + c.Path + "\n")
	b.WriteString("+++ " + c.Path + " (" + string(c.Kind) + ", " + c.Impact.String() + ")\n")
	for _, l := range lineDiff(splitLines(c.Before), splitLines(c.After)) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// lineDiff produces "-", "+" and " " prefixed lines, with unchanged runs
// collapsed so a two-line edit in a 60-line config reads as a two-line edit.
func lineDiff(before, after []string) []string {
	const context = 2

	ops := diffOps(before, after)

	// Mark which entries to keep: every change, plus `context` lines either
	// side of one.
	keep := make([]bool, len(ops))
	for i, op := range ops {
		if op[0] == ' ' {
			continue
		}
		for j := max(0, i-context); j < min(len(ops), i+context+1); j++ {
			keep[j] = true
		}
	}

	var out []string
	skipped := 0
	flush := func() {
		if skipped > 0 {
			out = append(out, "@@ "+plural(skipped, "unchanged line")+" @@")
			skipped = 0
		}
	}
	for i, op := range ops {
		if !keep[i] {
			skipped++
			continue
		}
		flush()
		out = append(out, op)
	}
	flush()
	return out
}

// diffOps is a longest-common-subsequence diff over lines. The inputs are
// rendered config files — tens of lines — so the quadratic table is free and
// not worth trading for a more elaborate algorithm.
func diffOps(before, after []string) []string {
	n, m := len(before), len(after)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if before[i] == after[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}

	var out []string
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case before[i] == after[j]:
			out = append(out, " "+before[i])
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "-"+before[i])
			i++
		default:
			out = append(out, "+"+after[j])
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, "-"+before[i])
	}
	for ; j < m; j++ {
		out = append(out, "+"+after[j])
	}
	return out
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
