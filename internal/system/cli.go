package system

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

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
	c.AddCommand(showCommand(), passwordCommand())
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
			"Use --password to read a password from redirected stdin, or\n" +
			"--no-password to allow anyone who can reach this box to configure it.\n" +
			"Passwords need at least 12 characters; do not put them in command arguments.\n\n" +
			"Claiming works once.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.RejectDryRun(c); err != nil {
				return err
			}
			if withPassword == noPassword {
				return fmt.Errorf("choose exactly one of --password or --no-password")
			}

			var view AccessView
			body := ClaimRequest{NoPassword: noPassword}
			if withPassword {
				value, err := passwordInput(c)
				if err != nil {
					return err
				}
				body.Password = value
			}
			if err := cli.ClientFor(c).Post(ctxOf(c), claimEndpoint, body, &view); err != nil {
				return err
			}

			fmt.Fprintln(c.OutOrStdout(), "This router is set up.")
			printAccess(c.OutOrStdout(), view)
			return nil
		},
	}
	c.Flags().BoolVar(&withPassword, "password", false,
		"require a password read from redirected stdin")
	c.Flags().BoolVar(&noPassword, "no-password", false,
		"require no password; anyone on the network can configure this router")
	return c
}

func passwordInput(c *cobra.Command) (string, error) {
	reader := c.InOrStdin()
	if file, ok := reader.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeCharDevice != 0 {
			return "", fmt.Errorf("read the password from a protected file or pipe on stdin; interactive input would echo it")
		}
	}
	data, err := io.ReadAll(io.LimitReader(reader, 1026))
	if err != nil {
		return "", err
	}
	value := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if len(value) > 1024 || strings.ContainsAny(value, "\r\n") {
		return "", fmt.Errorf("stdin must contain one password of at most 1024 bytes")
	}
	return value, nil
}

func passwordCommand() *cobra.Command {
	var noPassword, withPassword bool
	set := verb("set", "Change administrator access", func(c *cobra.Command) {})
	set.RunE = nil
	password := &cobra.Command{
		Use: "password", Short: "Set a password from stdin, or remove it for local recovery",
		Long: "Change the administrator password through the local control socket.\n\nRead one password from a protected file or pipe on stdin. It needs at least\n12 characters. --no-password removes it. Every browser session is revoked.\nThe API token is separate and is not changed.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.RejectDryRun(c); err != nil {
				return err
			}
			if withPassword && noPassword {
				return fmt.Errorf("choose either --password or --no-password")
			}
			req := ClaimRequest{NoPassword: noPassword}
			if !noPassword {
				value, err := passwordInput(c)
				if err != nil {
					return err
				}
				req.Password = value
			}
			var view AccessView
			if err := cli.ClientFor(c).Put(ctxOf(c), accessEndpoint+"/password", req, &view); err != nil {
				return err
			}
			printAccess(c.OutOrStdout(), view)
			return nil
		},
	}
	password.Flags().BoolVar(&noPassword, "no-password", false, "remove the password; anyone who can reach this router may configure it")
	password.Flags().BoolVar(&withPassword, "password", false, "set the password from redirected stdin (the default)")
	set.AddCommand(password)
	return set
}
