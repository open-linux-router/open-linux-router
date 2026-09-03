package dhcp

import (
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Diff renders the change as a unified-style diff.
//
// The line diffing is core's (core/diff.go), because every module that renders
// a backend config wants exactly the same thing from it. What stays here is the
// header: only this module knows that a change under hosts.d costs a SIGHUP and
// a change to dnsmasq.conf costs a restart, so the impact annotation is not
// core's to write.
func (c Change) Diff() string {
	var b strings.Builder
	b.WriteString("--- " + c.Path + "\n")
	b.WriteString("+++ " + c.Path + " (" + string(c.Kind) + ", " + c.Impact.String() + ")\n")
	for _, l := range core.LineDiff(c.Before, c.After) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}
