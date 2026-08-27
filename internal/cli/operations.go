package cli

import (
	"github.com/spf13/cobra"
)

// operationCommands are the hub-level commands from design.md §6.1. They fan
// out across modules rather than belonging to any one of them.
func operationCommands() []*cobra.Command {
	op := func(use, short string) *cobra.Command {
		return &cobra.Command{
			Use:     use,
			Short:   short,
			GroupID: GroupOperations,
			RunE:    NotImplemented,
		}
	}

	return []*cobra.Command{
		op("status", "Aggregate drift and daemon liveness across modules"),
		op("diff", "Show drifted or pending configuration, per module"),
		op("history", "List configuration revisions, per module"),
		op("rollback", "Roll a module back to an earlier revision"),
		op("adopt <iface>", "Take ownership of an interface"),
		op("release <iface>", "Hand an interface back to the system"),
	}
}
