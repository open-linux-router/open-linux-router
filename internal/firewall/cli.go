package firewall

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
)

const (
	configEndpoint = "/api/" + ModuleName + "/config"
	statusEndpoint = "/api/" + ModuleName + "/status"
)

// verb wraps cli.Verb so the shared vocabulary check still runs.
func verb(name, short string, build func(*cobra.Command)) *cobra.Command {
	c := cli.Verb(name, short)
	build(c)
	return c
}

// Command is `olr firewall`.
func Command() *cobra.Command {
	c := &cobra.Command{
		Use:     ModuleName,
		Short:   "Block connections from outside that nothing here asked for",
		GroupID: cli.GroupModules,
		Long: "A default stance, not a rule list.\n\n" +
			"When it is on, nothing from outside your networks may start a connection\n" +
			"to this router or to anything behind it. Replies to connections from\n" +
			"inside still arrive, and so does whatever olr is already serving to the\n" +
			"outside — port forwards, remote access, published services. Those\n" +
			"openings come from the objects themselves, so removing one closes its\n" +
			"port too.\n\n" +
			"Inside means the members of your networks and the remote access tunnel;\n" +
			"every other interface is outside.",
	}
	c.AddCommand(showCommand(), statusCommand(), enableCommand(), disableCommand())
	return c
}

func showCommand() *cobra.Command {
	return verb("show", "Show whether the firewall is on", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var cfg Config
			if err := cli.ClientFor(c).Get(c.Context(), configEndpoint, &cfg); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), cfg)
			}
			writeConfigText(c.OutOrStdout(), cfg)
			return nil
		}
	})
}

func statusCommand() *cobra.Command {
	return verb("status", "Show what the firewall lets in and what it has blocked", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var st Status
			if err := cli.ClientFor(c).Get(c.Context(), statusEndpoint, &st); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), st)
			}
			writeStatusText(c.OutOrStdout(), st)
			return nil
		}
	})
}

func enableCommand() *cobra.Command {
	return verb("enable", "Block connections from outside", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error { return send(c, true) }
	})
}

func disableCommand() *cobra.Command {
	return verb("disable", "Stop blocking connections from outside", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error { return send(c, false) }
	})
}

// send switches the firewall. confirm=true because the disruptive gate is for
// the WebUI, which can ask and wait; over the socket there is no session to
// lose. --dry-run is how to look first.
func send(c *cobra.Command, enabled bool) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	body := map[string]any{"enabled": enabled}
	client := cli.ClientFor(c)

	if cli.DryRun(c) {
		var plan Plan
		if err := client.Do(c.Context(), "PATCH", configEndpoint+"?dry_run=true", body, &plan); err != nil {
			return err
		}
		if cli.IsJSON(c) {
			return cli.JSON(c.OutOrStdout(), plan)
		}
		writePlanText(c.OutOrStdout(), plan, true)
		return nil
	}

	var resp applyResponse
	if err := client.Do(c.Context(), "PATCH", configEndpoint+"?confirm=true", body, &resp); err != nil {
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), resp)
	}
	writePlanText(c.OutOrStdout(), resp.Plan, false)
	if enabled {
		fmt.Fprintln(c.OutOrStdout(), "The firewall is on.")
	} else {
		fmt.Fprintln(c.OutOrStdout(), "The firewall is off.")
	}
	return nil
}
