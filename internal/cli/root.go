// Package cli assembles the olr command tree.
//
// The tree is static. Every command is registered in Go, so `olr --help` works
// with olrd stopped and there is nothing to fetch, cache, or invalidate.
package cli

import (
	"github.com/spf13/cobra"
)

// Command groups.
//
// The split matters: most of olr is a client of olrd's HTTP API, on equal
// footing with the WebUI and the MCP server (design.md §1). But a few commands
// manage olrd itself and so cannot go through it — they must work when the
// daemon is down. Those live in GroupLocal.
const (
	GroupModules    = "modules"
	GroupOperations = "operations"
	GroupLocal      = "local"
)

// DefaultSocket is olrd's control socket.
const DefaultSocket = "/run/olr/olrd.sock"

type globalOptions struct {
	socket string
	output string
	dryRun bool
}

// NewRoot returns the root command with the hub-level commands attached.
// Modules are mounted by the caller, not here — see cmd/olr/main.go.
func NewRoot() *cobra.Command {
	opts := &globalOptions{}

	root := &cobra.Command{
		Use:   "olr",
		Short: "Control an open-linux-router box",
		Long: "olr controls a Linux box running open-linux-router.\n\n" +
			"Most commands are clients of olrd over its control socket. The commands\n" +
			"under `olr daemon` manage olrd itself and work without it running.",

		// Runtime failures should not dump usage; a bad invocation still does.
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	root.PersistentFlags().StringVar(&opts.socket, FlagSocket, DefaultSocket,
		"olrd control socket")
	root.PersistentFlags().StringVarP(&opts.output, FlagOutput, "o", OutputText,
		"output format: text|json")

	// Changes apply on return (design.md §5.1), so the only way to look before
	// leaping is to ask. Global rather than per-module: "show me what this
	// would do" should mean the same thing everywhere in the tool.
	root.PersistentFlags().BoolVar(&opts.dryRun, FlagDryRun, false,
		"show what would change and exit without applying it")

	root.AddGroup(
		&cobra.Group{ID: GroupModules, Title: "Modules:"},
		&cobra.Group{ID: GroupOperations, Title: "Operations:"},
		&cobra.Group{ID: GroupLocal, Title: "Local (work with olrd stopped):"},
	)

	// Keep cobra's built-ins out of an untitled "Additional Commands" bucket.
	root.SetHelpCommandGroupID(GroupLocal)
	root.SetCompletionCommandGroupID(GroupLocal)

	root.AddCommand(operationCommands()...)
	root.AddCommand(daemonCommand(), versionCommand())

	return root
}
