package main

import (
	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dns"
	"github.com/open-linux-router/open-linux-router/internal/routing"
)

// newRoot builds the command tree an operator actually gets.
//
// This exists as a function, rather than inline in main, for one reason: the
// conformance tests in this package have to walk the *whole* tree. They used to
// walk cli.NewRoot(), which mounts the operations stubs, `daemon` and
// `version` — and not one module. So "every command has a Short", "every
// top-level command is grouped" and the rest had never once looked at dhcp,
// dns or routing, and passed for that reason while the module surfaces drifted
// apart in every direction docs/cli.md now catalogues.
//
// main and the tests call the same function so the two cannot diverge again.
func newRoot() *cobra.Command {
	root := cli.NewRoot()

	// Modules are mounted explicitly. The list is bounded, so it is a literal
	// list rather than a registry (design.md §3.2).
	root.AddCommand(
		dhcp.Command(),
		dns.Command(),
		routing.Command(),
	)
	return root
}
