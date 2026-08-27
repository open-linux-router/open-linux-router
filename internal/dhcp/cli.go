// Package dhcp is the DHCP module.
//
// It owns dnsmasq and nothing else: DNS stays with the dns module and unbound
// (design.md §4.2).
package dhcp

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
)

// env carries the flags every dhcp command shares.
type env struct {
	configPath string
	linksPath  string
}

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	e := &env{}

	c := cli.NewModule("dhcp", "DHCP server (dnsmasq)",
		showCommand(e),
		setCommand(e),
		addCommand(e),
		rmCommand(e),
		statusCommand(e),
		logsCommand(e),
		enableCommand(e),
		disableCommand(e),
	)

	c.PersistentFlags().StringVar(&e.configPath, "config", ConfigPath,
		"module configuration file")
	c.PersistentFlags().StringVar(&e.linksPath, "links", "",
		"JSON file of interface facts, until the link module lands")

	return c
}

// applier builds the module's working object from the flags.
func (e *env) applier() (Applier, error) {
	links, err := e.links()
	if err != nil {
		return Applier{}, err
	}
	a, err := NewApplier(links)
	if err != nil {
		return Applier{}, err
	}
	if e.configPath != "" {
		a.ConfigPath = e.configPath
		a.Backend = NewDnsmasq(a.Paths).WithSource(e.configPath)
	}
	return a, nil
}

// links resolves the interface facts dhcp validates against.
//
// design.md §4.1 is explicit that these come from the link module and are never
// copied here. That module does not exist yet, so rather than inventing a
// second source of truth the CLI requires the operator to supply them and says
// why. A wrong answer here would silently produce a pool outside its subnet,
// which is the exact failure cross-module validation exists to catch.
func (e *env) links() (LinkView, error) {
	if e.linksPath == "" {
		return nil, fmt.Errorf(
			"interface facts are unavailable: the link module is not implemented yet, " +
				"so pass --links <file> with the adopted interfaces and their addresses")
	}
	return LoadLinks(e.linksPath)
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

func showCommand(e *env) *cobra.Command {
	c := verb("show", "Show DHCP configuration", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			cfg, err := LoadConfig(e.configPath)
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
		&cobra.Command{
			Use: "pools", Short: "List address pools", Args: cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				cfg, err := LoadConfig(e.configPath)
				if err != nil {
					return err
				}
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), cfg.Pools)
				}
				return writePoolsText(c.OutOrStdout(), cfg.Pools)
			},
		},
		&cobra.Command{
			Use: "reservations", Short: "List address reservations", Args: cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				cfg, err := LoadConfig(e.configPath)
				if err != nil {
					return err
				}
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), cfg.Reservations)
				}
				return writeReservationsText(c.OutOrStdout(), cfg.Reservations)
			},
		},
		&cobra.Command{
			Use:   "leases",
			Short: "List current leases",
			Long: "List the leases dnsmasq is currently holding.\n\n" +
				"Leases are observed, not configured: they are read from the daemon's\n" +
				"database and are never stored or revisioned by olr (design.md §6.2).",
			Args: cobra.NoArgs,
			RunE: func(c *cobra.Command, _ []string) error {
				a, err := e.applierWithoutLinks()
				if err != nil {
					return err
				}
				leases, problems, err := a.Leases()
				if err != nil {
					return err
				}
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), map[string]any{
						"leases": leases, "problems": problems,
					})
				}
				return writeLeasesText(c.OutOrStdout(), leases, problems)
			},
		},
	)
	return c
}

// applierWithoutLinks is for read-only commands that never validate or render,
// so they work before the link module exists and without --links.
func (e *env) applierWithoutLinks() (Applier, error) {
	a, err := NewApplier(nil)
	if err != nil {
		return Applier{}, err
	}
	if e.configPath != "" {
		a.ConfigPath = e.configPath
	}
	return a, nil
}

// ---------------------------------------------------------------- set / add

func setCommand(e *env) *cobra.Command {
	c := verb("set", "Change DHCP configuration", func(c *cobra.Command) {})
	c.AddCommand(poolCommand(e, "set"), extraConfCommand(e))
	return c
}

func addCommand(e *env) *cobra.Command {
	c := verb("add", "Add a reservation or pool", func(c *cobra.Command) {})
	c.AddCommand(poolCommand(e, "add"), reservationCommand(e))
	return c
}

// poolFlags is the pool's field set, shared by add and set.
type poolFlags struct {
	rng      string
	lease    string
	gateway  string
	dns      []string
	ntp      []string
	domain   string
	ra       string
	options  []string
	noGate   bool
	clearDNS bool
}

func (f *poolFlags) register(c *cobra.Command) {
	c.Flags().StringVar(&f.rng, "range", "", "address range, e.g. 192.168.1.100-192.168.1.200")
	c.Flags().StringVar(&f.lease, "lease", "", "lease time, e.g. 12h (default 12h)")
	c.Flags().StringVar(&f.gateway, "gateway", "", "gateway to advertise (default: the router itself)")
	c.Flags().BoolVar(&f.noGate, "no-gateway", false, "advertise the router itself as the gateway")
	c.Flags().StringSliceVar(&f.dns, "dns", nil, "DNS servers to advertise (default: the router itself)")
	c.Flags().BoolVar(&f.clearDNS, "no-dns", false, "advertise the router itself as the DNS server")
	c.Flags().StringSliceVar(&f.ntp, "ntp", nil, "NTP servers to advertise")
	c.Flags().StringVar(&f.domain, "domain", "", "search domain to advertise")
	c.Flags().StringVar(&f.ra, "ra", "", fmt.Sprintf("IPv6 mode: %s", joinRAModes()))
	c.Flags().StringArrayVar(&f.options, "option", nil, "extra DHCP option as CODE=VALUE, repeatable")
}

// apply mutates a pool with only the flags the operator actually gave.
//
// Unset flags are left alone rather than overwritten with zero values, so
// `olr dhcp set pool br-lan --domain lan` is a single-field update and does not
// silently erase the lease time (design.md §10: the relaxed schema projection
// exists for exactly this).
func (f *poolFlags) apply(c *cobra.Command, p *Pool) error {
	if c.Flags().Changed("range") {
		start, end, err := parseRange(f.rng)
		if err != nil {
			return err
		}
		p.Start, p.End = start, end
	}
	if c.Flags().Changed("lease") {
		d, err := ParseDuration(f.lease)
		if err != nil {
			return fmt.Errorf("--lease: %w", err)
		}
		p.LeaseTime = d
	}
	if c.Flags().Changed("no-gateway") && f.noGate {
		p.Gateway = nil
	}
	if c.Flags().Changed("gateway") {
		gw, err := netip.ParseAddr(f.gateway)
		if err != nil {
			return fmt.Errorf("--gateway: %q is not an IP address", f.gateway)
		}
		p.Gateway = &gw
	}
	if c.Flags().Changed("no-dns") && f.clearDNS {
		p.DNS = nil
	}
	if c.Flags().Changed("dns") {
		addrs, err := parseAddrs("--dns", f.dns)
		if err != nil {
			return err
		}
		p.DNS = addrs
	}
	if c.Flags().Changed("ntp") {
		addrs, err := parseAddrs("--ntp", f.ntp)
		if err != nil {
			return err
		}
		p.NTP = addrs
	}
	if c.Flags().Changed("domain") {
		p.Domain = f.domain
	}
	if c.Flags().Changed("ra") {
		mode := RAMode(f.ra)
		if !mode.Valid() {
			return fmt.Errorf("--ra: unknown mode %q (want %s)", f.ra, joinRAModes())
		}
		p.RA = mode
	}
	if c.Flags().Changed("option") {
		opts, err := parseOptions(f.options)
		if err != nil {
			return err
		}
		p.Options = opts
	}
	return nil
}

func poolCommand(e *env, mode string) *cobra.Command {
	flags := &poolFlags{}

	short := "Add an address pool on an interface"
	if mode == "set" {
		short = "Change an existing pool"
	}

	c := &cobra.Command{
		Use:   "pool <interface>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			iface := args[0]
			return mutate(c, e, func(cfg *Config) error {
				pool, exists := cfg.Pool(iface)
				switch {
				case mode == "add" && exists:
					return fmt.Errorf("%s already has a pool; use `olr dhcp set pool %s` to change it", iface, iface)
				case mode == "set" && !exists:
					return fmt.Errorf("%s has no pool; use `olr dhcp add pool %s` to create one", iface, iface)
				}
				pool.Interface = iface
				if err := flags.apply(c, &pool); err != nil {
					return err
				}
				if !pool.Start.IsValid() || !pool.End.IsValid() {
					return fmt.Errorf("--range is required when adding a pool")
				}
				cfg.SetPool(pool)
				return nil
			})
		},
	}
	flags.register(c)
	return c
}

func reservationCommand(e *env) *cobra.Command {
	var mac, ip, hostname, lease string

	c := &cobra.Command{
		Use:   "reservation",
		Short: "Reserve an address for a client",
		Long: "Reserve an address for a client.\n\n" +
			"Reserving an address outside the pool's dynamic range is the safer habit:\n" +
			"it cannot then collide with an address the pool hands out.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			return mutate(c, e, func(cfg *Config) error {
				normalized, err := NormalizeMAC(mac)
				if err != nil {
					return fmt.Errorf("--mac: %w", err)
				}
				addr, err := netip.ParseAddr(ip)
				if err != nil {
					return fmt.Errorf("--ip: %q is not an IP address", ip)
				}
				res := Reservation{MAC: normalized, IP: addr, Hostname: hostname}
				if lease != "" {
					d, err := ParseDuration(lease)
					if err != nil {
						return fmt.Errorf("--lease: %w", err)
					}
					res.LeaseTime = d
				}
				cfg.SetReservation(res)
				return nil
			})
		},
	}

	c.Flags().StringVar(&mac, "mac", "", "client hardware address (required)")
	c.Flags().StringVar(&ip, "ip", "", "address to reserve (required)")
	c.Flags().StringVar(&hostname, "hostname", "", "hostname to assign the client")
	c.Flags().StringVar(&lease, "lease", "", "lease time for this client")
	_ = c.MarkFlagRequired("mac")
	_ = c.MarkFlagRequired("ip")
	return c
}

func extraConfCommand(e *env) *cobra.Command {
	return &cobra.Command{
		Use:   "extra-conf <file>",
		Short: "Replace the dnsmasq passthrough configuration",
		Long: "Replace the dnsmasq passthrough configuration from a file, or from\n" +
			"standard input when the file is `-`. Pass an empty file to clear it.\n\n" +
			"This is the module's declared escape hatch (design.md §3.2 rule 5). It is\n" +
			"revisioned along with everything else, which is the point: settings olr\n" +
			"does not model stay visible to olr rather than being hand-edited into a\n" +
			"file olr never reads. It may not set directives the module renders itself.",
		Args: cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			var (
				data []byte
				err  error
			)
			if args[0] == "-" {
				data, err = io.ReadAll(c.InOrStdin())
			} else {
				data, err = os.ReadFile(args[0])
			}
			if err != nil {
				return err
			}
			return mutate(c, e, func(cfg *Config) error {
				cfg.ExtraConf = strings.TrimRight(string(data), "\n")
				return nil
			})
		},
	}
}

// ---------------------------------------------------------------- rm

func rmCommand(e *env) *cobra.Command {
	c := verb("rm", "Remove a reservation or pool", func(c *cobra.Command) {})

	c.AddCommand(
		&cobra.Command{
			Use: "pool <interface>", Short: "Remove an interface's pool", Args: cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				return mutate(c, e, func(cfg *Config) error {
					if !cfg.RemovePool(args[0]) {
						return fmt.Errorf("%s has no pool", args[0])
					}
					return nil
				})
			},
		},
		&cobra.Command{
			Use: "reservation <mac>", Short: "Remove a reservation", Args: cobra.ExactArgs(1),
			RunE: func(c *cobra.Command, args []string) error {
				return mutate(c, e, func(cfg *Config) error {
					if !cfg.RemoveReservation(args[0]) {
						return fmt.Errorf("no reservation for %s", args[0])
					}
					return nil
				})
			},
		},
	)
	return c
}

// ---------------------------------------------------------------- lifecycle

func enableCommand(e *env) *cobra.Command {
	return verb("enable", "Enable the DHCP service", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, e, func(cfg *Config) error { cfg.Enabled = true; return nil })
		}
	})
}

func disableCommand(e *env) *cobra.Command {
	return verb("disable", "Disable the DHCP service", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			return mutate(c, e, func(cfg *Config) error { cfg.Enabled = false; return nil })
		}
	})
}

func statusCommand(e *env) *cobra.Command {
	return verb("status", "Show service state, leases and drift", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			a, err := e.applierWithoutLinks()
			if err != nil {
				return err
			}
			cfg, err := a.Load()
			if err != nil {
				return err
			}

			ctx := c.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			service, serviceErr := a.Service.Status(ctx)
			leases, problems, err := a.Leases()
			if err != nil {
				return err
			}

			status := map[string]any{
				"enabled": cfg.Enabled,
				"backend": a.Backend.Name(),
				"service": service,
				"leases":  len(leases),
				"pools":   a.Usage(cfg, leases),
			}
			if serviceErr != nil {
				status["service_error"] = serviceErr.Error()
			}

			// Drift needs interface facts, so it is reported only when they are
			// available. Saying "drift unknown" is honest; guessing is not.
			if links, linkErr := e.links(); linkErr == nil {
				a.Links = links
				if plan, err := a.Plan(ctx, cfg); err == nil {
					status["drifted"] = !plan.Empty()
					status["impact"] = plan.Impact
				}
			}

			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), status)
			}
			return writeStatusText(c.OutOrStdout(), cfg, a, service, serviceErr, leases, problems, status)
		}
	})
}

func logsCommand(e *env) *cobra.Command {
	var (
		lines  int
		follow bool
	)

	c := verb("logs", "Show dnsmasq log output", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Show the DHCP daemon's log output.\n\n" +
			"olr stores no logs of its own — journald already does that\n" +
			"(design.md §3.4), so this streams from the journal rather than\n" +
			"reimplementing it. Anything journalctl can do to these logs, it can\n" +
			"still do directly."
		c.RunE = func(c *cobra.Command, _ []string) error {
			a, err := e.applierWithoutLinks()
			if err != nil {
				return err
			}

			journalctl, err := exec.LookPath("journalctl")
			if err != nil {
				return fmt.Errorf("journalctl not found; this module keeps its logs in the journal, under the unit %s", a.Backend.Unit())
			}

			args := []string{"-u", a.Backend.Unit(), "-n", strconv.Itoa(lines)}
			if follow {
				args = append(args, "-f")
			}

			ctx := c.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			cmd := exec.CommandContext(ctx, journalctl, args...)
			cmd.Stdout = c.OutOrStdout()
			cmd.Stderr = c.ErrOrStderr()
			return cmd.Run()
		}
	})

	c.Flags().IntVarP(&lines, "lines", "n", 50, "number of lines to show")
	c.Flags().BoolVarP(&follow, "follow", "f", false, "keep streaming new entries")
	return c
}

// ---------------------------------------------------------------- plumbing

// mutate is the shape every change shares: load, edit, plan, then apply unless
// asked not to.
//
// Applying happens on return, with no staged commit (design.md §5.1), so the
// diff and the impact are printed either way — the operator sees what happened
// rather than only that something did.
func mutate(c *cobra.Command, e *env, edit func(*Config) error) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}

	a, err := e.applier()
	if err != nil {
		return err
	}

	cfg, err := a.Load()
	if err != nil {
		return err
	}
	if err := edit(&cfg); err != nil {
		return err
	}

	ctx := c.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if cli.DryRun(c) {
		plan, err := a.Plan(ctx, cfg)
		if err != nil {
			return err
		}
		if cli.IsJSON(c) {
			return cli.JSON(c.OutOrStdout(), plan)
		}
		return writePlanText(c.OutOrStdout(), plan, true)
	}

	result, err := a.Apply(ctx, cfg)
	if err != nil {
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

func parseRange(s string) (start, end netip.Addr, err error) {
	first, last, ok := strings.Cut(s, "-")
	if !ok {
		return start, end, fmt.Errorf("--range: %q is not START-END, e.g. 192.168.1.100-192.168.1.200", s)
	}
	start, err = netip.ParseAddr(strings.TrimSpace(first))
	if err != nil {
		return start, end, fmt.Errorf("--range: %q is not an IP address", first)
	}
	end, err = netip.ParseAddr(strings.TrimSpace(last))
	if err != nil {
		return start, end, fmt.Errorf("--range: %q is not an IP address", last)
	}
	return start, end, nil
}

func parseAddrs(flag string, values []string) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		a, err := netip.ParseAddr(v)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not an IP address", flag, v)
		}
		out = append(out, a)
	}
	return out, nil
}

func parseOptions(values []string) ([]Option, error) {
	out := make([]Option, 0, len(values))
	for _, v := range values {
		name, value, ok := strings.Cut(v, "=")
		if !ok {
			return nil, fmt.Errorf("--option: %q is not CODE=VALUE", v)
		}
		name, value = strings.TrimSpace(name), strings.TrimSpace(value)
		if name == "" || value == "" {
			return nil, fmt.Errorf("--option: %q is not CODE=VALUE", v)
		}
		out = append(out, Option{Option: name, Value: value})
	}
	return out, nil
}

func joinRAModes() string {
	modes := RAModes()
	parts := make([]string, len(modes))
	for i, m := range modes {
		parts[i] = string(m)
	}
	return strings.Join(parts, "|")
}

func humanTime(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	return t.Local().Format("2006-01-02 15:04:05")
}
