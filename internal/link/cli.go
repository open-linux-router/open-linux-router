package link

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Every command here is a client of olrd, for the reason internal/dhcp/cli.go
// states at length: a CLI that wrote the document itself would be a second
// writer holding a lock olrd cannot see.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
//
// Read-only, and deliberately so. Adoption is written through `olr adopt` and
// `olr release` — hub-level operations in design.md §6.1's own vocabulary,
// which is where an operator will look for them, and which read better than
// `olr link add adopted eth0` for what is a one-word decision about one
// interface. See OperationCommands.
func Command() *cobra.Command {
	return cli.NewModule("link", "Network interfaces olr has been given",
		showCommand(),
	)
}

// OperationCommands returns `adopt` and `release`.
//
// They live in this package rather than internal/cli because that package
// deliberately knows no modules — cmd/olr mounts them, exactly as it mounts
// each module's tree — and they are grouped as operations rather than under
// `olr link` because design.md §6.1 lists them that way: they fan out across
// modules in effect, since adopting is what lets dhcp, dns and routing use an
// interface at all.
func OperationCommands() []*cobra.Command {
	return []*cobra.Command{adoptCommand(), releaseCommand()}
}

// Endpoints this module's commands call.
const (
	configEndpoint     = core.APIPrefix + "/" + ModuleName + "/config"
	planEndpoint       = core.APIPrefix + "/" + ModuleName + "/plan"
	interfacesEndpoint = core.APIPrefix + "/" + ModuleName + "/interfaces"
)

// ctxOf is the request context, falling back to Background for a command
// invoked outside cobra's execution (which the tests do).
func ctxOf(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func loadConfig(c *cobra.Command) (Config, error) {
	var cfg Config
	if err := cli.ClientFor(c).Get(ctxOf(c), configEndpoint, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadInterfaces(c *cobra.Command) (listResponse, error) {
	var resp listResponse
	err := cli.ClientFor(c).Get(ctxOf(c), interfacesEndpoint, &resp)
	return resp, err
}

// verb wraps cli.Verb so the shared vocabulary check still runs.
func verb(name, short string, build func(*cobra.Command)) *cobra.Command {
	c := cli.Verb(name, short)
	c.RunE = nil
	build(c)
	return c
}

// ---------------------------------------------------------------- show

func showCommand() *cobra.Command {
	c := verb("show", "Show which interfaces olr has been given", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			if cli.DryRun(c) {
				var plan planView
				if err := cli.ClientFor(c).Post(ctxOf(c), planEndpoint, nil, &plan); err != nil {
					return err
				}
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), plan)
				}
				return writePlanText(c.OutOrStdout(), plan, true)
			}

			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), cfg)
			}
			return writeConfigText(c.OutOrStdout(), cfg)
		}
	})

	c.AddCommand(showInterfacesCommand())
	return c
}

func showInterfacesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "interfaces",
		Short: "List this machine's network interfaces",
		Long: "List every network interface on this machine: its addresses, whether it\n" +
			"is up, and whether it has been given to olr.\n\n" +
			"Interfaces are observed, not configured. They are read from the kernel on\n" +
			"every request and never stored, so this is what is true now rather than\n" +
			"what was last written (design.md §4.5).",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			resp, err := loadInterfaces(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp)
			}
			return writeInterfacesText(c.OutOrStdout(), resp)
		},
	}
}

// ---------------------------------------------------------------- adopt / release

func adoptCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "adopt <interface>",
		Short:   "Take ownership of an interface",
		GroupID: cli.GroupOperations,
		Long: "Hand an interface to olr, so that it may serve addresses, answer DNS or\n" +
			"route traffic on it.\n\n" +
			"Adopting by itself changes nothing on the machine: no address is set, no\n" +
			"daemon is started, and the interface keeps whatever configuration it\n" +
			"already had. What it changes is permission — olr refuses to touch an\n" +
			"interface nobody handed it (design.md §3.4), so this is the step that\n" +
			"makes `olr dhcp add pool` stop refusing.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, args[0], func(cfg *Config) (bool, string) {
				if !cfg.Adopt(args[0]) {
					return false, fmt.Sprintf("%s is already adopted.", args[0])
				}
				return true, ""
			}, fmt.Sprintf(
				"Add an address range with `olr dhcp add pool %s --range START-END`.", args[0]))
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(unadoptedInterfaces)
	return c
}

func releaseCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "release <interface>",
		Short:   "Hand an interface back to the system",
		GroupID: cli.GroupOperations,
		Long: "Take an interface back from olr.\n\n" +
			"Nothing on the interface is undone — releasing does not remove an address\n" +
			"or stop a daemon. What it removes is permission, so any address range,\n" +
			"resolver or exit still naming this interface will be refused the next\n" +
			"time it is applied. Remove those first if you mean to stop serving on it.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, args[0], func(cfg *Config) (bool, string) {
				if !cfg.Release(args[0]) {
					return false, fmt.Sprintf("%s is not adopted.", args[0])
				}
				return true, ""
			}, "Anything still configured on it will be refused until it is adopted again.")
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(adoptedInterfaces)
	return c
}

// mutate is the shared read-modify-write path for adopt and release.
//
// `edit` reports whether it changed anything, and if not, what to say. A no-op
// is not an error: `olr adopt eth0` run twice should print a calm sentence, not
// a failure the operator has to read carefully to discover was harmless.
func mutate(c *cobra.Command, iface string, edit func(*Config) (bool, string), hint string) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	ctx, client := ctxOf(c), cli.ClientFor(c)

	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	changed, unchanged := edit(&cfg)
	if !changed {
		if cli.IsJSON(c) {
			return cli.JSON(c.OutOrStdout(), cfg)
		}
		_, err := fmt.Fprintln(c.OutOrStdout(), unchanged)
		return err
	}

	if cli.DryRun(c) {
		var plan planView
		if err := client.Post(ctx, planEndpoint, cfg, &plan); err != nil {
			return err
		}
		if cli.IsJSON(c) {
			return cli.JSON(c.OutOrStdout(), plan)
		}
		return writePlanText(c.OutOrStdout(), plan, true)
	}

	var result applyResponse
	if err := client.Put(ctx, configEndpoint, cfg, &result); err != nil {
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), result)
	}
	if err := writePlanText(c.OutOrStdout(), result.Plan, false); err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.OutOrStdout(), "\n%s\n", hint)
	return err
}

// ---------------------------------------------------------------- completion

// adoptedInterfaces completes `release` from what is actually adopted.
func adoptedInterfaces(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	return cfg.Adopted, nil
}

// unadoptedInterfaces completes `adopt` from the interfaces that exist and have
// not been adopted.
//
// The kernel's list rather than nothing, which is the difference between this
// and the `add` exemption in docs/cli.md R7: an interface name is not a new
// name the operator is inventing, it is an existing object they have to spell
// exactly right, and `enp0s31f6` is precisely the string nobody types twice.
func unadoptedInterfaces(c *cobra.Command) ([]string, error) {
	resp, err := loadInterfaces(c)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, iface := range resp.Interfaces {
		if !iface.Adopted && iface.Present && !iface.Loopback {
			out = append(out, iface.Name)
		}
	}
	return out, nil
}
