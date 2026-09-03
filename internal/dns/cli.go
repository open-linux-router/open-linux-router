// Package dns is the DNS module.
//
// It owns unbound and olr's own relay, and nothing else: DHCP stays with the
// dhcp module and dnsmasq (design.md §4.2), which is why that module renders
// `port=0` and leaves :53 free for this one to claim.
package dns

import (
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the config document, the rendered files or systemd directly, and that is the
// point rather than an implementation detail: a CLI that wrote the system on
// its own would be a second writer, holding a lock olrd cannot see, running its
// own copy of the validation rules, and publishing none of the change events
// the UI listens for.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	return cli.NewModule("dns", "DNS resolver and policy (olr-dnsd + unbound)",
		showCommand(),
		setCommand(),
		addCommand(),
		rmCommand(),
		statusCommand(),
		logsCommand(),
		enableCommand(),
		disableCommand(),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	configEndpoint  = core.APIPrefix + "/" + ModuleName + "/config"
	planEndpoint    = core.APIPrefix + "/" + ModuleName + "/plan"
	statusEndpoint  = core.APIPrefix + "/" + ModuleName + "/status"
	queriesEndpoint = core.APIPrefix + "/" + ModuleName + "/queries"
	namesEndpoint   = core.APIPrefix + "/" + ModuleName + "/names"
)

// ctxOf is the request context, falling back to Background for a command
// invoked outside cobra's execution (which the tests do).
func ctxOf(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// loadConfig fetches stored intent.
func loadConfig(c *cobra.Command) (Config, error) {
	var cfg Config
	if err := cli.ClientFor(c).Get(ctxOf(c), configEndpoint, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// verb wraps cli.Verb so the shared vocabulary check still runs (design.md
// §6.1: a verb outside the vocabulary panics at startup).
func verb(name, short string, build func(*cobra.Command)) *cobra.Command {
	c := cli.Verb(name, short)
	c.RunE = nil
	build(c)
	return c
}

// ---------------------------------------------------------------- show

func showCommand() *cobra.Command {
	c := verb("show", "Show DNS configuration", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			// --dry-run on `show` is the drift question: plan stored intent
			// against reality and print what does not match (design.md §5.4).
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

	c.AddCommand(
		showPoliciesCommand(),
		showQueriesCommand(),
		showNamesCommand(),
	)
	return c
}

func showPoliciesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "policies [name]",
		Short: "List policies, or show one in full",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			if len(args) == 0 {
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), cfg.Policies)
				}
				return writePoliciesText(c.OutOrStdout(), cfg.Policies)
			}

			p, ok := cfg.Policy(args[0])
			if !ok {
				return fmt.Errorf("no policy named %q", args[0])
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), p)
			}
			return writeNamesListText(c.OutOrStdout(), p)
		},
	}
}

func showQueriesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "queries",
		Short: "List recently answered queries",
		Long: "List the queries the relay has answered.\n\n" +
			"Queries are observed, not configured: they are read from the relay on\n" +
			"every request and are never stored or revisioned by olr (design.md §6.2).\n" +
			"The log lives in the relay's memory, so it starts empty after a restart —\n" +
			"the `since` line in `olr dns status` says when that was.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var resp queriesResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), queriesEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp)
			}
			return writeQueriesText(c.OutOrStdout(), resp)
		},
	}
}

func showNamesCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "names",
		Short: "List the addresses each device resolved, and for what",
		Long: "List the domain-to-address map the relay built from the answers it saw.\n\n" +
			"This is what makes traffic attributable: a device connecting to a CDN\n" +
			"address is meaningless on its own, and this says which name it asked for\n" +
			"to get there. Entries expire at the record's TTL plus a grace period.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var resp namesResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), namesEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp)
			}
			return writeNamesText(c.OutOrStdout(), resp)
		},
	}
}

// ---------------------------------------------------------------- set

func setCommand() *cobra.Command {
	var f configFlags

	c := verb("set", "Change DNS settings", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { return f.apply(c, cfg) })
		}
	})
	f.register(c)
	return c
}

type configFlags struct {
	listen     []string
	allowFrom  []string
	mode       string
	servers    []string
	tls        bool
	tlsName    string
	queryLog   bool
	logEntries int
	hijack     bool
	interfaces []string
	blockDoT   bool
}

func (f *configFlags) register(c *cobra.Command) {
	c.Flags().StringSliceVar(&f.listen, "listen", nil,
		"addresses to answer queries on, e.g. 192.168.1.1:53 (repeatable)")
	c.Flags().StringSliceVar(&f.allowFrom, "allow-from", nil,
		"source networks permitted to query, e.g. 192.168.1.0/24 (repeatable; empty means the networks listened on)")
	c.Flags().StringVar(&f.mode, "mode", "",
		"how names are resolved: "+joinUpstreamModes())
	c.Flags().StringSliceVar(&f.servers, "upstream", nil,
		"forwarders, used with --mode forward, e.g. 1.1.1.1 (repeatable)")
	c.Flags().BoolVar(&f.tls, "tls", false, "forward over DNS-over-TLS")
	c.Flags().StringVar(&f.tlsName, "tls-name", "",
		"certificate name the forwarder must present, e.g. cloudflare-dns.com")
	c.Flags().BoolVar(&f.queryLog, "query-log", false, "record answered queries")
	c.Flags().IntVar(&f.logEntries, "query-log-entries", 0,
		"how many answered queries to keep in memory")
	c.Flags().BoolVar(&f.hijack, "redirect", false,
		"redirect clients that use another resolver back to this one")
	c.Flags().StringSliceVar(&f.interfaces, "redirect-on", nil,
		"interfaces whose forwarded DNS is redirected (repeatable)")
	c.Flags().BoolVar(&f.blockDoT, "block-dot", false,
		"drop DNS-over-TLS on 853, so clients cannot route around the redirect")
}

func (f *configFlags) apply(c *cobra.Command, cfg *Config) error {
	changed := c.Flags().Changed

	if changed("listen") {
		listen, err := parseAddrPorts("--listen", f.listen, DNSPort)
		if err != nil {
			return err
		}
		cfg.Listen = listen
	}
	if changed("allow-from") {
		prefixes, err := parsePrefixes("--allow-from", f.allowFrom)
		if err != nil {
			return err
		}
		cfg.AllowFrom = prefixes
	}
	if changed("mode") {
		mode := UpstreamMode(f.mode)
		if !mode.Valid() {
			return fmt.Errorf("--mode: unknown mode %q (want %s)", f.mode, joinUpstreamModes())
		}
		cfg.Upstream.Mode = mode
	}
	if changed("upstream") {
		// A bare address means the right port for the transport, which is the
		// difference between `--upstream 1.1.1.1 --tls` working and silently
		// trying DoT against plaintext 53.
		port := uint16(53)
		if f.tls || cfg.Upstream.TLS {
			port = DefaultDoTPort
		}
		servers, err := parseAddrPorts("--upstream", f.servers, port)
		if err != nil {
			return err
		}
		cfg.Upstream.Servers = servers
		// Listing forwarders and leaving the mode at its default would be a
		// config that quietly ignores them, so the obvious reading wins.
		if !changed("mode") && len(servers) > 0 {
			cfg.Upstream.Mode = ModeForward
		}
	}
	if changed("tls") {
		cfg.Upstream.TLS = f.tls
	}
	if changed("tls-name") {
		cfg.Upstream.TLSName = f.tlsName
	}
	if changed("query-log") {
		cfg.QueryLog.Enabled = f.queryLog
	}
	if changed("query-log-entries") {
		cfg.QueryLog.Entries = f.logEntries
	}
	if changed("redirect") {
		cfg.Hijack.Enabled = f.hijack
	}
	if changed("redirect-on") {
		cfg.Hijack.Interfaces = f.interfaces
	}
	if changed("block-dot") {
		cfg.Hijack.BlockDoT = f.blockDoT
	}
	return nil
}

// ---------------------------------------------------------------- add / rm

func addCommand() *cobra.Command {
	c := verb("add", "Add a policy or a blocked name", func(c *cobra.Command) {})
	c.AddCommand(policyCommand("add"), blockCommand("add"), allowCommand("add"))
	return c
}

func rmCommand() *cobra.Command {
	c := verb("rm", "Remove a policy or a blocked name", func(c *cobra.Command) {})
	c.AddCommand(rmPolicyCommand(), blockCommand("rm"), allowCommand("rm"))
	return c
}

// policyCommand creates or replaces a policy.
func policyCommand(string) *cobra.Command {
	var (
		clients  []string
		block    []string
		allow    []string
		response string
	)

	c := &cobra.Command{
		Use:   "policy NAME",
		Short: "Add or replace a policy",
		Long: "Add or replace a policy: a set of clients and what they may look up.\n\n" +
			"A policy with no --client is the default one, applying to every device no\n" +
			"other policy claims. The most specific matching network wins, so a policy\n" +
			"naming one address beats one naming the whole subnet.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				p, existing := cfg.Policy(args[0])
				p.Name = args[0]
				if !existing {
					p.Block, p.Allow = nil, nil
				}

				if c.Flags().Changed("client") {
					prefixes, err := parsePrefixes("--client", clients)
					if err != nil {
						return err
					}
					p.Clients = prefixes
				}
				if c.Flags().Changed("block") {
					p.Block = block
				}
				if c.Flags().Changed("allow") {
					p.Allow = allow
				}
				if c.Flags().Changed("response") {
					r := BlockResponse(response)
					if !r.Valid() {
						return fmt.Errorf("--response: unknown response %q (want %s)",
							response, joinBlockResponses())
					}
					p.Response = r
				}
				cfg.SetPolicy(p)
				return nil
			})
		},
	}

	c.Flags().StringSliceVar(&clients, "client", nil,
		"a network this policy governs, e.g. 192.168.1.50/32 (repeatable; omit for the default policy)")
	c.Flags().StringSliceVar(&block, "block", nil,
		"a name to block, covering it and everything under it (repeatable)")
	c.Flags().StringSliceVar(&allow, "allow", nil,
		"an exception that beats --block (repeatable)")
	c.Flags().StringVar(&response, "response", "",
		"what a blocked name answers with: "+joinBlockResponses())
	return c
}

func rmPolicyCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "policy NAME",
		Short: "Remove a policy",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				if !cfg.RemovePolicy(args[0]) {
					return fmt.Errorf("no policy named %q", args[0])
				}
				return nil
			})
		},
	}
}

// blockCommand and allowCommand edit one list inside a policy, which is the
// common operation and the one an operator should not have to restate a whole
// policy to perform.
func blockCommand(mode string) *cobra.Command { return nameListCommand(mode, "block") }
func allowCommand(mode string) *cobra.Command { return nameListCommand(mode, "allow") }

func nameListCommand(mode, list string) *cobra.Command {
	var policy string

	short := map[string]string{
		"block": "names this policy refuses",
		"allow": "exceptions that beat the block list",
	}[list]

	c := &cobra.Command{
		Use:   list + " NAME...",
		Short: map[string]string{"add": "Add to the ", "rm": "Remove from the "}[mode] + short,
		Args:  cobra.MinimumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				p, err := targetPolicy(cfg, policy)
				if err != nil {
					return err
				}
				current := &p.Block
				if list == "allow" {
					current = &p.Allow
				}
				for _, raw := range args {
					name := NormalizeName(raw)
					if err := checkName(name); err != nil {
						return fmt.Errorf("%q: %v", raw, err)
					}
					if mode == "add" {
						*current = append(*current, name)
						continue
					}
					if !removeName(current, name) {
						return fmt.Errorf("%q is not in %s's %s list", name, p.Name, list)
					}
				}
				cfg.SetPolicy(p)
				return nil
			})
		},
	}

	c.Flags().StringVar(&policy, "policy", "",
		"which policy to edit (defaults to the only one, or the default policy)")
	return c
}

// targetPolicy resolves which policy an edit applies to.
//
// Naming one is only necessary once there is more than one, which keeps the
// simple case simple: a house with a single blocklist never types --policy.
func targetPolicy(cfg *Config, name string) (Policy, error) {
	if name != "" {
		p, ok := cfg.Policy(name)
		if !ok {
			return Policy{}, fmt.Errorf("no policy named %q", name)
		}
		return p, nil
	}
	switch len(cfg.Policies) {
	case 0:
		// Created rather than refused: `olr dns add block ads.example` on a
		// fresh box should work, and inventing the obvious default is better
		// than an error telling them to run a different command first.
		return Policy{Name: "default"}, nil
	case 1:
		return cfg.Policies[0], nil
	}
	if p, ok := cfg.DefaultPolicy(); ok {
		return p, nil
	}
	names := make([]string, 0, len(cfg.Policies))
	for _, p := range cfg.Policies {
		names = append(names, p.Name)
	}
	return Policy{}, fmt.Errorf(
		"there are %d policies and none of them is the default, so --policy is required (have %s)",
		len(cfg.Policies), strings.Join(names, ", "))
}

func removeName(list *[]string, name string) bool {
	for i, n := range *list {
		if n == name {
			*list = append((*list)[:i], (*list)[i+1:]...)
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------- lifecycle

func enableCommand() *cobra.Command {
	return verb("enable", "Enable DNS", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { cfg.Enabled = true; return nil })
		}
	})
}

func disableCommand() *cobra.Command {
	return verb("disable", "Disable DNS", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { cfg.Enabled = false; return nil })
		}
	})
}

func statusCommand() *cobra.Command {
	return verb("status", "Show service state, query counts and drift", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var status statusResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), statusEndpoint, &status); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), status)
			}
			return writeStatusText(c.OutOrStdout(), status)
		}
	})
}

func logsCommand() *cobra.Command {
	var (
		lines  int
		follow bool
		which  string
	)

	c := verb("logs", "Show resolver and relay log output", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Show the DNS backends' log output.\n\n" +
			"olr stores no logs of its own — journald already does that\n" +
			"(design.md §3.4), so this streams from the journal rather than\n" +
			"reimplementing it. Both units are shown interleaved by default, which\n" +
			"is usually what you want: a relay failing because its upstream died\n" +
			"reads as one story across the two."
		c.RunE = func(c *cobra.Command, _ []string) error {
			// The one command here that does not go through olrd, and it does
			// not because there is nothing for olrd to add: the logs are
			// journald's, the unit names are constants, and streaming a follow
			// through the API would be reimplementing journalctl over HTTP.
			b := Backend{}
			units := b.Units()
			switch which {
			case "":
			case "resolver":
				units = []string{b.ResolverUnit()}
			case "relay":
				units = []string{b.RelayUnit()}
			default:
				return fmt.Errorf("--unit: want resolver or relay, got %q", which)
			}

			journalctl, err := exec.LookPath("journalctl")
			if err != nil {
				return fmt.Errorf(
					"journalctl not found; this module keeps its logs in the journal, under the units %s",
					strings.Join(units, " and "))
			}

			args := []string{"-n", strconv.Itoa(lines)}
			for _, u := range units {
				args = append(args, "-u", u)
			}
			if follow {
				args = append(args, "-f")
			}

			cmd := exec.CommandContext(ctxOf(c), journalctl, args...)
			cmd.Stdout = c.OutOrStdout()
			cmd.Stderr = c.ErrOrStderr()
			return cmd.Run()
		}
	})

	c.Flags().IntVarP(&lines, "lines", "n", 50, "number of lines to show")
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new entries")
	c.Flags().StringVar(&which, "unit", "", "show one backend only: resolver or relay")
	return c
}

// ---------------------------------------------------------------- plumbing

// mutate is the shape every change shares: load, edit, plan, then apply unless
// asked not to.
//
// Applying happens on return, with no staged commit (design.md §5.1), so the
// diff and the impact are printed either way — the operator sees what happened
// rather than only that something did.
func mutate(c *cobra.Command, edit func(*Config) error) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	ctx, client := ctxOf(c), cli.ClientFor(c)

	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	if err := edit(&cfg); err != nil {
		return err
	}

	// A dry run asks what would happen and stops. This is the HTTP spelling of
	// the same question the WebUI asks before every edit (§5.1/§5.3.3), and on
	// this module it matters more than most: the difference between "reload"
	// and "the whole house loses name resolution" is one field.
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
		// Report what landed before returning the failure: there is no
		// rollback, so which steps completed is the operator's starting point
		// (design.md §5.3.2).
		writeStepsText(c.ErrOrStderr(), result.Steps)
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), result)
	}
	return writePlanText(c.OutOrStdout(), result.Plan, false)
}

// parseAddrPorts reads addresses that may or may not carry a port, supplying
// the default where they do not.
func parseAddrPorts(flag string, values []string, defaultPort uint16) ([]netip.AddrPort, error) {
	out := make([]netip.AddrPort, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if ap, err := netip.ParseAddrPort(v); err == nil {
			out = append(out, ap)
			continue
		}
		addr, err := netip.ParseAddr(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not an address or address:port", flag, v)
		}
		out = append(out, netip.AddrPortFrom(addr, defaultPort))
	}
	return out, nil
}

// parsePrefixes reads networks, accepting a bare address as a host route so
// that naming one device does not require typing /32.
func parsePrefixes(flag string, values []string) ([]netip.Prefix, error) {
	out := make([]netip.Prefix, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		if p, err := netip.ParsePrefix(v); err == nil {
			out = append(out, p)
			continue
		}
		addr, err := netip.ParseAddr(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a network or an address", flag, v)
		}
		out = append(out, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return out, nil
}

func joinUpstreamModes() string {
	parts := make([]string, 0, len(UpstreamModes()))
	for _, m := range UpstreamModes() {
		parts = append(parts, string(m))
	}
	return strings.Join(parts, "|")
}

func joinBlockResponses() string {
	parts := make([]string, 0, len(BlockResponses()))
	for _, r := range BlockResponses() {
		parts = append(parts, string(r))
	}
	return strings.Join(parts, "|")
}
