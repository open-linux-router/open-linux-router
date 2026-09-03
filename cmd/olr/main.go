// Command olr is the hub CLI for open-linux-router.
package main

import (
	"fmt"
	"os"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/dhcp"
	"github.com/open-linux-router/open-linux-router/internal/dns"
)

func main() {
	root := cli.NewRoot()

	// Modules are mounted explicitly. The list is bounded, so it is a literal
	// list rather than a registry (design.md §3.2).
	root.AddCommand(
		dhcp.Command(),
		dns.Command(),
	)

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "olr:", err)
		os.Exit(1)
	}
}
