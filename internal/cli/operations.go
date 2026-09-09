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

	// `adopt` and `release` are not here. They are real commands now, and they
	// live in internal/link beside the module they write to — this package
	// deliberately knows no modules, so cmd/olr mounts them into this group the
	// same way it mounts each module's tree.
	//
	// `status` is not here either, and for a different reason: it stopped being
	// a stub. It used to promise "aggregate drift and daemon liveness across
	// modules" and do neither, while `olr daemon status` quietly did the
	// liveness half. Flattening the daemon group merged the two, so the real
	// one lives in daemon.go beside the systemd query it is built on.
	return []*cobra.Command{
		op("diff", "Show drifted or pending configuration, per module", cobra.NoArgs),
		op("history", "List configuration revisions, per module", cobra.NoArgs),
		op("rollback", "Roll a module back to an earlier revision", cobra.NoArgs),
	}
}
