package cli

import (
	"github.com/spf13/cobra"
)

// daemonCommand groups the commands that manage olrd itself.
//
// This is the one place the CLI is not an API client. `olr daemon start` cannot
// be an HTTP call to the thing it is starting, and `olr daemon status` has to
// answer truthfully when olrd is wedged. These talk to systemd directly.
func daemonCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "daemon",
		Short:   "Manage the olrd service itself",
		Long:    "Commands that manage olrd. Unlike the rest of olr, these do not go\nthrough olrd's API — they work when it is stopped or unresponsive.",
		GroupID: GroupLocal,
	}

	sub := func(use, short string) *cobra.Command {
		return &cobra.Command{Use: use, Short: short, RunE: NotImplemented}
	}

	c.AddCommand(
		sub("start", "Start olrd"),
		sub("stop", "Stop olrd"),
		sub("restart", "Restart olrd"),
		sub("status", "Report whether olrd is running"),
	)

	return c
}
