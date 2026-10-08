package socksout

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

func Command() *cobra.Command {
	base := core.APIPrefix + "/" + ModuleName
	show := cli.Verb("show", "Show outbound SOCKS5 configuration")
	show.Args = cobra.NoArgs
	show.RunE = func(c *cobra.Command, _ []string) error {
		var v Config
		if err := cli.ClientFor(c).Get(c.Context(), base+"/config", &v); err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), v)
	}
	status := cli.Verb("status", "Show outbound SOCKS5 backend")
	status.Args = cobra.NoArgs
	status.RunE = func(c *cobra.Command, _ []string) error {
		var v any
		if err := cli.ClientFor(c).Get(c.Context(), base+"/status", &v); err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), v)
	}
	set := cli.Verb("set", "Configure outbound SOCKS5")
	var endpoint string
	set.Flags().StringVar(&endpoint, "proxy", "", "socks5://IP:port (no credentials yet)")
	set.Args = cobra.NoArgs
	set.RunE = func(c *cobra.Command, _ []string) error {
		if !c.Flags().Changed("proxy") {
			return fmt.Errorf("--proxy is required")
		}
		cfg := Config{Enabled: true, Proxy: endpoint}
		if err := cfg.Validate(); err != nil {
			return err
		}
		var v any
		if cli.DryRun(c) {
			if err := cli.ClientFor(c).Post(c.Context(), base+"/plan", cfg, &v); err != nil {
				return err
			}
		} else if err := cli.ClientFor(c).Put(c.Context(), base+"/config", cfg, &v); err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), v)
	}
	off := cli.Verb("disable", "Stop outbound SOCKS5")
	off.Args = cobra.NoArgs
	off.RunE = func(c *cobra.Command, _ []string) error {
		var cfg Config
		if err := cli.ClientFor(c).Get(c.Context(), base+"/config", &cfg); err != nil {
			return err
		}
		cfg.Enabled = false
		var v any
		if cli.DryRun(c) {
			if err := cli.ClientFor(c).Post(c.Context(), base+"/plan", cfg, &v); err != nil {
				return err
			}
		} else if err := cli.ClientFor(c).Put(c.Context(), base+"/config", cfg, &v); err != nil {
			return err
		}
		return cli.JSON(c.OutOrStdout(), v)
	}
	return cli.NewModule("socks-out", "Transparent outbound SOCKS5 for selected networks and devices", show, set, status, off)
}
