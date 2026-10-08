package iptv

import (
	"fmt"
	"net/netip"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

func Command() *cobra.Command {
	endpoint := core.APIPrefix + "/" + ModuleName

	show := cli.Verb("show", "Show IPTV configuration")
	show.Args = cobra.NoArgs
	show.RunE = func(cmd *cobra.Command, _ []string) error {
		var cfg Config
		if err := cli.ClientFor(cmd).Get(cmd.Context(), endpoint+"/config", &cfg); err != nil {
			return err
		}
		return cli.JSON(cmd.OutOrStdout(), cfg)
	}
	status := cli.Verb("status", "Show IPTV backend state")
	status.Args = cobra.NoArgs
	status.RunE = func(cmd *cobra.Command, _ []string) error {
		var v any
		if err := cli.ClientFor(cmd).Get(cmd.Context(), endpoint+"/status", &v); err != nil {
			return err
		}
		return cli.JSON(cmd.OutOrStdout(), v)
	}
	set := cli.Verb("set", "Configure IPTV upstream and LAN networks")
	var sources []string
	var upstream string
	var networks []string
	set.Flags().StringArrayVar(&sources, "source", nil, "upstream multicast source IPv4 prefix (repeatable)")
	set.Flags().StringVar(&upstream, "upstream", "", "adopted IPTV interface")
	set.Flags().StringArrayVar(&networks, "network", nil, "downstream network (repeatable)")
	set.Args = cobra.NoArgs
	set.RunE = func(cmd *cobra.Command, _ []string) error {
		cfg := Config{Enabled: true, Upstream: upstream, Networks: networks}
		for _, raw := range sources {
			prefix, err := netip.ParsePrefix(raw)
			if err != nil {
				return err
			}
			cfg.Sources = append(cfg.Sources, prefix)
		}
		if !cmd.Flags().Changed("upstream") || len(networks) == 0 {
			return fmt.Errorf("--upstream and at least one --network are required")
		}
		if cli.DryRun(cmd) {
			var v any
			if err := cli.ClientFor(cmd).Post(cmd.Context(), endpoint+"/plan", cfg, &v); err != nil {
				return err
			}
			return cli.JSON(cmd.OutOrStdout(), v)
		}
		var v any
		if err := cli.ClientFor(cmd).Put(cmd.Context(), endpoint+"/config", cfg, &v); err != nil {
			return err
		}
		return cli.JSON(cmd.OutOrStdout(), v)
	}
	off := cli.Verb("disable", "Stop IPTV multicast forwarding")
	off.Args = cobra.NoArgs
	off.RunE = func(cmd *cobra.Command, _ []string) error {
		var cfg Config
		if err := cli.ClientFor(cmd).Get(cmd.Context(), endpoint+"/config", &cfg); err != nil {
			return err
		}
		cfg.Enabled = false
		var v any
		if cli.DryRun(cmd) {
			if err := cli.ClientFor(cmd).Post(cmd.Context(), endpoint+"/plan", cfg, &v); err != nil {
				return err
			}
		} else if err := cli.ClientFor(cmd).Put(cmd.Context(), endpoint+"/config", cfg, &v); err != nil {
			return err
		}
		return cli.JSON(cmd.OutOrStdout(), v)
	}
	return cli.NewModule("iptv", "Routed IPTV multicast (IGMP proxy)", show, status, set, off)
}
