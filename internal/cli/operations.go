package cli

import (
	"github.com/spf13/cobra"
)

// operationCommands are the hub-level commands from design.md §6.1. They fan
// out across modules rather than belonging to any one of them.
func operationCommands() []*cobra.Command {
	// Args is declared even though every one of these is a stub. A command that
	// advertises `<interface>` and accepts any number of arguments is lying
	// about its shape now, and would still be lying on the day someone fills in
	// the body — docs/cli.md R2 makes the Use string and the validator one
	// statement, so they are written together or not at all.
	op := func(use, short string, args cobra.PositionalArgs) *cobra.Command {
		return &cobra.Command{
			Use:     use,
			Short:   short,
			GroupID: GroupOperations,
			Args:    args,
			RunE:    NotImplemented,
		}
	}

	return []*cobra.Command{
		op("status", "Aggregate drift and daemon liveness across modules", cobra.NoArgs),
		op("diff", "Show drifted or pending configuration, per module", cobra.NoArgs),
		op("history", "List configuration revisions, per module", cobra.NoArgs),
		op("rollback", "Roll a module back to an earlier revision", cobra.NoArgs),
		op("adopt <interface>", "Take ownership of an interface", cobra.ExactArgs(1)),
		op("release <interface>", "Hand an interface back to the system", cobra.ExactArgs(1)),
	}
}
