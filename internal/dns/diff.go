package dns

import (
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Diff renders the change as a unified-style diff.
//
// The line diffing is core's (core/diff.go). What stays here is the header, and
// it carries one field internal/dhcp's does not: the unit. This module drives
// two daemons, so "which of them does this file oblige us to signal" is part of
// reading the change rather than something to work out afterwards.
func (c Change) Diff() string {
	annotation := string(c.Kind) + ", " + c.Impact.String()
	if c.Unit != "" && c.Impact > ImpactNone {
		annotation += " " + c.Unit
	}

	var b strings.Builder
	b.WriteString("--- " + c.Path + "\n")
	b.WriteString("+++ " + c.Path + " (" + annotation + ")\n")
	for _, l := range core.LineDiff(c.Before, c.After) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}
