package routing

import (
	"context"
	"fmt"
	"net/netip"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the kernel directly, and on this module that is not merely a convention: a
// CLI that programmed nftables and the RPDB itself would be a second writer of
// the one thing this module exists to own, holding a lock olrd cannot see and
// racing the health prober.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	return cli.NewModule("routing", "Exits and which networks use them",
		showCommand(),
		setCommand(),
		addCommand(),
		rmCommand(),
		statusCommand(),
		enableCommand(),
		disableCommand(),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	configEndpoint  = core.APIPrefix + "/" + ModuleName + "/config"
	planEndpoint    = core.APIPrefix + "/" + ModuleName + "/plan"
	applyEndpoint   = core.APIPrefix + "/" + ModuleName + "/apply"
	statusEndpoint  = core.APIPrefix + "/" + ModuleName + "/status"
	trafficEndpoint = core.APIPrefix + "/" + ModuleName + "/traffic"
)

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
	c := showConfigCommand()
	c.AddCommand(showTrafficCommand())
	return c
}

func showTrafficCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "traffic",
		Short: "Show how much each device has used, and through which way out",
		Long: "Show how much each device has sent and received.\n\n" +
			"Counted in the kernel and read on every request — never stored, so the\n" +
			"numbers start from zero when the router reboots. What they cannot see is\n" +
			"printed underneath rather than left to be discovered, because every one of\n" +
			"those limits makes a number smaller than you would expect.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var traffic trafficView
			if err := cli.ClientFor(c).Get(ctxOf(c), trafficEndpoint, &traffic); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), traffic)
			}
			return writeTrafficText(c.OutOrStdout(), traffic)
		},
	}
}

func showConfigCommand() *cobra.Command {
	return verb("show", "Show exits and which networks use them", func(c *cobra.Command) {
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
}

// ---------------------------------------------------------------- set

func setCommand() *cobra.Command {
	c := verb("set", "Choose which exit a network uses", func(*cobra.Command) {})
	c.AddCommand(setDefaultCommand(), setViaCommand(), setStatsCommand())
	return c
}

// setStatsCommand is §7.5's visible off switch.
//
// A subcommand of `set` rather than a flag on `enable`/`disable`, because
// accounting has its own lifetime: turning routing off leaves it counting, and
// turning it off leaves routing alone.
func setStatsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "traffic-counting on|off",
		Short: "Count how much each device uses",
		Long: "Turn per-device traffic counting on or off.\n\n" +
			"Independent of everything else here: with no way out configured it still\n" +
			"counts, it just has nothing to attribute the traffic to. Turning it off\n" +
			"discards the running totals, which are only ever held in memory anyway.",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"on", "off"},
		RunE: func(c *cobra.Command, args []string) error {
			var on bool
			switch args[0] {
			case "on":
				on = true
			case "off":
				on = false
			default:
				return fmt.Errorf("want on or off, got %q", args[0])
			}
			return mutate(c, func(cfg *Config) error { cfg.Stats = &on; return nil })
		},
	}
}

func setDefaultCommand() *cobra.Command {
	var none bool

	c := &cobra.Command{
		Use:   "default [EXIT]",
		Short: "Set the exit everything uses unless something more specific says otherwise",
		Long: "Set the box-wide exit — the top rung of the ladder.\n\n" +
			"Every network follows this unless it has been given an exit of its own,\n" +
			"so `set default Clash` plus one `rm via` for the NAS is how you say\n" +
			"\"everything through Clash except that\". Use --none to go back to the\n" +
			"box's own internet connection.",
		Args: cobra.MaximumNArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if none == (len(args) == 1) {
				return fmt.Errorf("give an exit name, or --none for the box's own connection")
			}
			return mutate(c, func(cfg *Config) error {
				if none {
					cfg.Default = ""
					return nil
				}
				if _, ok := cfg.Find(args[0]); !ok {
					return unknownExit(cfg, args[0])
				}
				cfg.Default = args[0]
				return nil
			})
		},
	}
	c.Flags().BoolVar(&none, "none", false, "use the box's own internet connection")
	return c
}

func setViaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "via NETWORK EXIT",
		Short: "Send one network's traffic through an exit",
		Long: "Send one network's traffic through an exit.\n\n" +
			"NETWORK is an interface name for now; it becomes a network name when the\n" +
			"link module lands, and the stored configuration will not need changing.\n" +
			"Use `olr routing rm via NETWORK` to go back to following the box-wide\n" +
			"setting.",
		Args: cobra.ExactArgs(2),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				if _, ok := cfg.Find(args[1]); !ok {
					return unknownExit(cfg, args[1])
				}
				cfg.SetAssignment(args[0], args[1])
				return nil
			})
		},
	}
}

// ---------------------------------------------------------------- add / rm

func addCommand() *cobra.Command {
	c := verb("add", "Add an exit", func(*cobra.Command) {})
	c.AddCommand(addExitCommand())
	return c
}

func rmCommand() *cobra.Command {
	c := verb("rm", "Remove an exit, or a network's assignment", func(*cobra.Command) {})
	c.AddCommand(rmExitCommand(), rmViaCommand())
	return c
}

func addExitCommand() *cobra.Command {
	var f exitFlags

	c := &cobra.Command{
		Use:   "exit NAME",
		Short: "Add or replace an exit",
		Long: "Add or replace an exit: a way out of this box.\n\n" +
			"An exit is anything that accepts traffic addressed somewhere else and\n" +
			"takes responsibility for delivering it — a WireGuard or proxy interface,\n" +
			"another box on the network, or nothing at all.\n\n" +
			"  olr routing add exit Clash  --next-hop 192.168.1.50 --probe 1.1.1.1:443\n" +
			"  olr routing add exit Office --interface wg0\n" +
			"  olr routing add exit Blocked --blocked\n\n" +
			"A SOCKS5 or HTTP proxy port cannot be an exit: the client has to ask it,\n" +
			"in its own protocol, so it carries no UDP, no ICMP and nothing that does\n" +
			"not know about it. Point tun2socks at it and use the interface that makes.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				e, existing := cfg.Find(args[0])
				e.Name = args[0]
				if err := f.apply(c, &e, existing); err != nil {
					return err
				}
				cfg.Upsert(e)
				return nil
			})
		},
	}
	f.register(c)
	return c
}

type exitFlags struct {
	iface     string
	nextHop   string
	dev       string
	blocked   bool
	ipv6      string
	onFailure string
	snat      bool
	probe     string
	interval  string
	timeout   string
}

func (f *exitFlags) register(c *cobra.Command) {
	c.Flags().StringVar(&f.iface, "interface", "",
		"send traffic out this device, e.g. wg0 or a proxy's TUN")
	c.Flags().StringVar(&f.nextHop, "next-hop", "",
		"hand traffic to this box, e.g. 192.168.1.50")
	c.Flags().StringVar(&f.dev, "dev", "",
		"the interface the next hop is reached through (usually derived)")
	c.Flags().BoolVar(&f.blocked, "blocked", false,
		"refuse traffic, so applications fail immediately instead of hanging")
	c.Flags().StringVar(&f.ipv6, "ipv6", "",
		"what happens to IPv6: "+join(IPv6Modes())+" (default block)")
	c.Flags().StringVar(&f.onFailure, "on-failure", "",
		"what happens when the health check fails: "+join(FailureModes())+" (default block)")
	c.Flags().BoolVar(&f.snat, "snat", true,
		"rewrite the source address of traffic sent to a next hop, so replies come back through this router")
	c.Flags().StringVar(&f.probe, "probe", "",
		"health-check target on the far side of the exit, as address:port, e.g. 1.1.1.1:443")
	// Registered on `set` rather than here; see setStatsCommand.
	c.Flags().StringVar(&f.interval, "probe-interval", "", "how often to health-check, e.g. 30s")
	c.Flags().StringVar(&f.timeout, "probe-timeout", "", "how long one health check may take, e.g. 5s")
}

func (f *exitFlags) apply(c *cobra.Command, e *Exit, existing bool) error {
	changed := c.Flags().Changed

	forms := 0
	for _, set := range []bool{changed("interface"), changed("next-hop"), f.blocked} {
		if set {
			forms++
		}
	}
	switch {
	case forms > 1:
		return fmt.Errorf("an exit has one form: give --interface, --next-hop or --blocked, not more than one")
	case forms == 0 && !existing:
		return fmt.Errorf("a new exit needs a form: --interface, --next-hop or --blocked")
	}

	if forms == 1 {
		// Switching form clears the other form's fields rather than leaving
		// them behind, where they would read as still being in force.
		e.Via = Via{}
		switch {
		case changed("interface"):
			e.Via.Kind, e.Via.Interface = ViaInterface, f.iface
		case changed("next-hop"):
			hop, err := netip.ParseAddr(strings.TrimSpace(f.nextHop))
			if err != nil {
				return fmt.Errorf("--next-hop: %q is not an address", f.nextHop)
			}
			e.Via.Kind, e.Via.NextHop = ViaNextHop, &hop
		case f.blocked:
			e.Via.Kind = ViaBlocked
		}
	}
	if changed("dev") {
		e.Via.Dev = f.dev
	}

	if changed("ipv6") {
		mode := IPv6Mode(f.ipv6)
		if !mode.Valid() {
			return fmt.Errorf("--ipv6: unknown value %q (want %s)", f.ipv6, join(IPv6Modes()))
		}
		e.IPv6 = mode
	}
	if changed("on-failure") {
		mode := FailureMode(f.onFailure)
		if !mode.Valid() {
			return fmt.Errorf("--on-failure: unknown value %q (want %s)", f.onFailure, join(FailureModes()))
		}
		e.OnFailure = mode
	}
	if changed("snat") {
		snat := f.snat
		e.SNAT = &snat
	}

	if changed("probe") {
		if strings.TrimSpace(f.probe) == "" {
			e.Probe = nil
		} else {
			target, err := netip.ParseAddrPort(strings.TrimSpace(f.probe))
			if err != nil {
				return fmt.Errorf("--probe: %q is not an address:port, e.g. 1.1.1.1:443", f.probe)
			}
			if e.Probe == nil {
				e.Probe = &Probe{}
			}
			e.Probe.Target = target
		}
	}
	for _, d := range []struct {
		flag  string
		value string
		into  *Duration
	}{
		{"probe-interval", f.interval, nil},
		{"probe-timeout", f.timeout, nil},
	} {
		if !changed(d.flag) {
			continue
		}
		if e.Probe == nil {
			return fmt.Errorf("--%s needs --probe as well; there is nothing to time otherwise", d.flag)
		}
		if d.flag == "probe-interval" {
			d.into = &e.Probe.Interval
		} else {
			d.into = &e.Probe.Timeout
		}
		if err := d.into.UnmarshalText([]byte(d.value)); err != nil {
			return fmt.Errorf("--%s: %v", d.flag, err)
		}
	}
	return nil
}

func rmExitCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "exit NAME",
		Short: "Remove an exit",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				if !cfg.Remove(args[0]) {
					return unknownExit(cfg, args[0])
				}
				// Deliberately not cleaning up references. A network still
				// pointing at the deleted exit is a validation error naming
				// both ends, which beats silently re-pointing somebody's phones
				// at the modem because an exit was removed in another window.
				return nil
			})
		},
	}
}

func rmViaCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "via NETWORK",
		Short: "Stop overriding a network's exit, so it follows the box-wide setting",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			return mutate(c, func(cfg *Config) error {
				if !cfg.RemoveAssignment(args[0]) {
					return fmt.Errorf("%q has no exit of its own", args[0])
				}
				return nil
			})
		},
	}
}

// ---------------------------------------------------------------- lifecycle

func enableCommand() *cobra.Command {
	return verb("enable", "Apply routing policy", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { cfg.Enabled = true; return nil })
		}
	})
}

func disableCommand() *cobra.Command {
	return verb("disable", "Remove routing policy, leaving the box routing normally", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, func(cfg *Config) error { cfg.Enabled = false; return nil })
		}
	})
}

func statusCommand() *cobra.Command {
	return verb("status", "Show each exit's health, what uses it, and drift", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			var status statusView
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

	// A dry run asks what would happen and stops. On this module that question
	// is worth more than on most: the difference between an edit nobody
	// notices and one that disconnects the machine you are typing on is a
	// single line of the plan (§5.1's lockout row).
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

// unknownExit names the exits that do exist, because "no exit called X" is only
// half an answer when the reason is usually a typo.
func unknownExit(cfg *Config, name string) error {
	if len(cfg.Exits) == 0 {
		return fmt.Errorf("there is no exit called %q, and none are configured yet; "+
			"add one with `olr routing add exit`", name)
	}
	names := make([]string, 0, len(cfg.Exits))
	for _, e := range cfg.Exits {
		names = append(names, e.Name)
	}
	return fmt.Errorf("there is no exit called %q (have %s)", name, strings.Join(names, ", "))
}
