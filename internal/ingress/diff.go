package ingress

import (
	"strings"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Diff renders the change as a unified-style diff.
//
// The line diffing is core's. What stays here is the header and one rule the
// other modules do not need: **a secret file shows its existence and never its
// contents.**
//
// That rule is load-bearing rather than tidy. `olr ingress plan` is the command
// an operator runs when something is wrong, in a terminal with scrollback, and
// its output is what gets pasted into an issue. A credential printed there is a
// credential disclosed, and nothing about the disclosure is visible at the
// moment it happens. The file is still planned and still compared byte for
// byte — the operator is told that the token changed, which is the fact they
// need — so withholding the value costs them nothing they could act on.
func (c Change) Diff() string {
	annotation := string(c.Kind) + ", " + c.Impact.String()

	var b strings.Builder
	b.WriteString("--- " + c.Path + "\n")
	b.WriteString("+++ " + c.Path + " (" + annotation + ")\n")

	if c.Secret {
		b.WriteString(secretPlaceholder(c))
		return b.String()
	}

	for _, l := range core.LineDiff(c.Before, c.After) {
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}

// secretPlaceholder says what happened without saying what it was.
//
// Which of the three it is matters to the operator — "the credential changed"
// and "the credential file is being removed" are different events — and none of
// the three requires showing a byte of it.
func secretPlaceholder(c Change) string {
	switch c.Kind {
	case ChangeCreate:
		return "@@ contents withheld: this file holds a credential @@\n+ (created)\n"
	case ChangeDelete:
		return "@@ contents withheld: this file holds a credential @@\n- (removed)\n"
	default:
		return "@@ contents withheld: this file holds a credential @@\n~ (changed)\n"
	}
}
