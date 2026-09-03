package core

import (
	"strconv"
	"strings"
)

// Line diffing for rendered backend configuration.
//
// `olr diff` is a first-class operation (design.md §6.1), and "3 files will
// change" is not an answer an operator can act on. Showing the lines is what
// lets a human — or an agent proposing a change for review (§6.4) — see that a
// pool moved by one address rather than that something moved.
//
// It lives in core rather than in a module because every module that renders a
// backend config needs exactly this, and the second copy is where the two
// quietly stop agreeing about what a diff looks like. What stays with the
// module is the header: only the module knows what a change to one of its files
// costs, so the impact annotation is not core's to write.

// DiffContext is how many unchanged lines are kept either side of a change.
const DiffContext = 2

// LineDiff produces "-", "+" and " " prefixed lines, with unchanged runs
// collapsed so a two-line edit in a 60-line config reads as a two-line edit.
func LineDiff(before, after []byte) []string {
	ops := diffOps(SplitLines(before), SplitLines(after))

	// Mark which entries to keep: every change, plus DiffContext lines either
	// side of one.
	keep := make([]bool, len(ops))
	for i, op := range ops {
		if op[0] == ' ' {
			continue
		}
		for j := max(0, i-DiffContext); j < min(len(ops), i+DiffContext+1); j++ {
			keep[j] = true
		}
	}

	var out []string
	skipped := 0
	flush := func() {
		if skipped > 0 {
			out = append(out, "@@ "+Plural(skipped, "unchanged line")+" @@")
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

// SplitLines splits file contents into lines, treating empty content as no
// lines at all rather than as one empty line.
func SplitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
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

// Plural renders a count and its noun, pluralising the naive way.
//
// Exported because module text output is full of "3 files will change" and
// "1 line could not be read", and a module having its own copy is how two
// surfaces end up disagreeing about whether one of something gets an "s".
func Plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(n) + " " + noun + "s"
}
