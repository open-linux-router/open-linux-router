// Package dhcp is the DHCP module.
//
// It owns dnsmasq and nothing else: DNS stays with the dns module and unbound
// (design.md §4.2). Empty for now — the command tree is here so the shape is
// reviewable before there is behaviour behind it.
package dhcp

import (
	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
)

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	return cli.NewModule("dhcp", "DHCP server (dnsmasq)",
		cli.Verb("show", "Show DHCP configuration"),
		cli.Verb("set", "Change DHCP configuration"),
		cli.Verb("add", "Add a reservation or pool"),
		cli.Verb("rm", "Remove a reservation or pool"),
		cli.Verb("status", "Show service state and lease count"),
		cli.Verb("logs", "Show dnsmasq log output"),
		cli.Verb("enable", "Enable the DHCP service"),
		cli.Verb("disable", "Disable the DHCP service"),
	)
}
