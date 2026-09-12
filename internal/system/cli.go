package system

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
)

const (
	accessEndpoint = "/api/" + ModuleName + "/access"
	claimEndpoint  = accessEndpoint + "/claim"
)

func ctxOf(c *cobra.Command) context.Context { return c.Context() }

// verb wraps cli.Verb so the shared vocabulary check still runs.
func verb(name, short string, build func(*cobra.Command)) *cobra.Command {
	c := cli.Verb(name, short)
	build(c)
	return c
}

// Command is `olr system`, the module surface.
func Command() *cobra.Command {
	c := &cobra.Command{
		Use:     ModuleName,
		Short:   "Show how this router may be reached",
		GroupID: cli.GroupModules,
		Long: "How this router is reached over the network.\n\n" +
			"A box nobody has set up refuses to be configured over the network at\n" +
			"all: the web UI is served, and every other request is turned away.\n" +
			"That is what lets olr open the UI the moment it is installed without\n" +
			"leaving an unprotected admin API on your LAN.\n\n" +
			"Setting it up is `olr claim`, once. There is no user model here — one\n" +
			"box, one optional password, no accounts.",
	}
	c.AddCommand(showCommand())
	return c
}

// OperationCommands returns `claim`.
//
// Top-level rather than `olr system claim`, following `adopt` and `release`:
// **consent is a top-level verb** while the configuration it unlocks lives in a
// module. Claiming a box is the same kind of act as adopting an interface, one
// level up — and it is the first thing anybody types, which is the other reason
// it is not three words deep.
func OperationCommands() []*cobra.Command {
	return []*cobra.Command{claimCommand()}
}

func showCommand() *cobra.Command {
	return verb("show", "Show how this router may be reached", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			var view AccessView
			if err := cli.ClientFor(c).Get(ctxOf(c), accessEndpoint, &view); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), view)
			}
			printAccess(c.OutOrStdout(), view)
			return nil
		}
	})
}

func claimCommand() *cobra.Command {
	// Both halves of the pair exist even though only one works, and that is
	// cli.md R4 doing its job rather than a workaround for it: a `--no-x` with
	// no `--x` to undo is a flag the operator cannot reason about. `--password`
	// being present and explicit about not being ready is strictly better than
	// it being an unknown flag, which is what "coming later" would otherwise
	// look like from the terminal.
	var withPassword, noPassword bool

	c := &cobra.Command{
		Use:     "claim",
		Short:   "Set this router up for the first time",
		GroupID: cli.GroupOperations,
		Long: "Record how this router may be reached over the network.\n\n" +
			"Until this is done the box serves its web UI and refuses everything\n" +
			"else over the network, so this is the step that makes it usable from\n" +
			"another machine. Normally it happens in the browser — the UI asks on\n" +
			"the first visit — and this exists for a box you only ever reach by SSH.\n\n" +
			"--no-password is required, and is the only choice there is today:\n" +
			"anyone who can reach this box on the network will be able to\n" +
			"configure it. Requiring a password instead needs a login screen,\n" +
			"which is the next release.\n\n" +
			"Claiming works once.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.RejectDryRun(c); err != nil {
				return err
			}
			// Required rather than defaulted. The flag is the operator saying
			// out loud what they are choosing, which is the entire difference
			// between this and v0.1.6's silent open listener — and when
			// --password arrives it joins an interface that already asks.
			if withPassword {
				return fmt.Errorf("passwords are not supported yet: enforcing one needs a " +
					"login screen, which is the next release.\n" +
					"Claim with --no-password for now, and set a password when it lands")
			}
			if !noPassword {
				return fmt.Errorf("pass --no-password to confirm that anyone on this " +
					"network may configure this router")
			}

			var view AccessView
			body := ClaimRequest{NoPassword: true}
			if err := cli.ClientFor(c).Post(ctxOf(c), claimEndpoint, body, &view); err != nil {
				return err
			}

			fmt.Fprintln(c.OutOrStdout(), "This router is set up.")
			printAccess(c.OutOrStdout(), view)
			return nil
		},
	}
	c.Flags().BoolVar(&withPassword, "password", false,
		"require a password (not supported yet; needs the login screen)")
	c.Flags().BoolVar(&noPassword, "no-password", false,
		"require no password; anyone on the network can configure this router")
	return c
}
