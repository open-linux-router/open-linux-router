package dial

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"net/url"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the config document directly: a CLI that wrote it would be a second writer,
// holding a lock olrd cannot see, running its own copy of the validation rules,
// and — specifically here — leaving the publisher watching a config that no
// longer exists.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
//
// Two objects under one module, addressed the way docs/cli.md §1 lays the
// surface out: `<module> <verb> <object> [<name>]`. So the uplink is
// `olr dial show|set|rm uplink`, in the object position, exactly as
// `olr remote set wireguard` puts a singleton there. A record is the one with
// names, so it keeps the bare `olr dial add <name>` form.
func Command() *cobra.Command {
	return cli.NewModule("dial", "Internet uplink, and a public name that follows it",
		showCommand(),
		addCommand(),
		setCommand(),
		rmCommand(),
		statusCommand(),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	base            = core.APIPrefix + "/" + ModuleName
	configEndpoint  = base + "/config"
	planEndpoint    = base + "/plan"
	statusEndpoint  = base + "/status"
	recordsEndpoint = base + "/records"
	uplinkEndpoint  = base + "/uplink"
)

// recordEndpoint is the item route for one name.
//
// Escaped, because a name arrives from an operator's argument and reaches a URL
// path. A record called `../config` should fail as a validation error, not as a
// request to somewhere else.
func recordEndpoint(name string) string {
	return recordsEndpoint + "/" + url.PathEscape(normalizeName(name))
}

func ctxOf(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

// loadConfig reads stored intent.
//
// What comes back has both credential fields masked. That is not a problem for
// the read-modify-write below, because the mask means "unchanged" on the way
// back in (http.go's preserveSecrets) — so a command that edits one field sends
// the mask for the others and the daemon keeps what it had.
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
	c := verb("show", "Show the names olr keeps current", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			// --dry-run on `show` is the drift question (design.md §5.4).
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

	c.AddCommand(showUplinkCommand(), showRecordsCommand(), showRecordCommand(), showProvidersCommand())
	return c
}

func showUplinkCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uplink",
		Short: "Show how this router reaches the internet",
		Args:  cobra.NoArgs,
		Long: "Show how this router itself reaches the internet — and whether it is\n" +
			"actually doing it.\n\n" +
			"Two halves, printed separately on purpose. The top is what olr was told:\n" +
			"the interface, its address and the gateway. The bottom is what the kernel\n" +
			"has right now. The failure worth catching is the one where the top looks\n" +
			"perfect and the default route goes out of a different interface.",
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var resp uplinkResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), uplinkEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp.Uplink)
			}
			return writeUplinkText(c.OutOrStdout(), resp.Uplink)
		},
	}
}

func showRecordsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "records",
		Short: "List the names olr keeps current",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), cfg.Records)
			}
			return writeConfigText(c.OutOrStdout(), cfg)
		},
	}
}

// showRecordCommand is the detail half docs/cli.md R6 requires: a module that
// can list an object has to be able to show one.
func showRecordCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "record <name>",
		Short: "Show one name in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			rec, found := cfg.Find(args[0])
			if !found {
				return unknownRecord(cfg.Names(), args[0])
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), rec)
			}
			return writeRecordText(c.OutOrStdout(), rec)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(recordNames)
	return c
}

func showProvidersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "providers",
		Short: "List the DNS providers olr can publish through",
		Args:  cobra.NoArgs,
		Long: "List the DNS providers this build can publish a record through.\n\n" +
			"The list grows by request rather than by completeness: a provider is\n" +
			"added when somebody asks for it, because each one is a hand-written\n" +
			"client of somebody else's API and carrying thirty of them means\n" +
			"tracking thirty APIs (docs/ddns.md §4.4).\n\n" +
			"`callback` covers most of what is not listed. It requests a URL you\n" +
			"supply with the address substituted in, which is the whole of the\n" +
			"DynDNS protocol that No-IP, DuckDNS and Dynu speak.",
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			names := ProviderNames()
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), names)
			}
			for _, n := range names {
				if _, err := fmt.Fprintln(c.OutOrStdout(), n); err != nil {
					return err
				}
			}
			return nil
		},
	}
}

// ---------------------------------------------------------------- add / set

// recordFlags are the fields of a record, as flags.
//
// One struct shared by `add` and `set` so the two halves of an object's
// lifecycle cannot disagree about what a record is — which is the defect
// docs/cli.md R2 was written about, in the one place it is easiest to
// reintroduce.
type recordFlags struct {
	provider    string
	zone        string
	keyID       string
	token       string
	tokenFile   string
	callbackURL string
	iface       string
	reflector   string
	noReflector bool
	interval    string
	ttl         int
}

func (f *recordFlags) register(c *cobra.Command) {
	// No backticks around the command name: cobra reads the first backticked
	// word in a usage string as the flag's value placeholder, so `olr dial show
	// providers` there renders as `--provider olr dial show providers` instead
	// of `--provider string`.
	c.Flags().StringVar(&f.provider, "provider", "",
		"DNS provider hosting the name (see olr dial show providers)")
	c.Flags().StringVar(&f.zone, "zone", "",
		"registered domain the name sits in, when olr cannot work it out from the name itself")
	c.Flags().StringVar(&f.keyID, "key-id", "",
		"the non-secret half of the credential: an AccessKey ID at Alibaba Cloud, a SecretId at Tencent Cloud")
	c.Flags().StringVar(&f.token, "token", "",
		"the provider credential. Prefer --token-file: a value here is recorded in your shell history")
	c.Flags().StringVar(&f.tokenFile, "token-file", "",
		"read the provider credential from a file, or from standard input when given -")
	c.Flags().StringVar(&f.callbackURL, "url", "",
		"for the callback provider: the URL to request, with #{ip} where the address goes")
	c.Flags().StringVar(&f.iface, "interface", "",
		"the uplink: the interface whose address is published, or the one a reflector is asked through")
	c.Flags().StringVar(&f.reflector, "reflector", "",
		"ask this endpoint what address the internet sees, instead of reading the interface")
	c.Flags().BoolVar(&f.noReflector, "no-reflector", false,
		"read the address off the interface instead of asking an endpoint")
	c.Flags().StringVar(&f.interval, "interval", "",
		"how often to check the address, with a unit (default "+Duration(DefaultInterval).String()+")")
	c.Flags().IntVar(&f.ttl, "ttl", 0,
		"the published record's time to live in seconds (default the provider's own)")

	c.MarkFlagsMutuallyExclusive("token", "token-file")
	c.MarkFlagsMutuallyExclusive("reflector", "no-reflector")
	cli.EnumFlag(c, "provider", providerFlagValues()...)
	_ = c.RegisterFlagCompletionFunc("interface", cli.CompleteFlag(interfaceNames))
}

// apply folds the flags that were given into a record.
//
// Only the ones the operator typed, so `olr dial set <name> --interval 10m`
// leaves the credential alone. The source is derived from *which* flag was
// given rather than from a `--source` flag of its own: `--reflector <url>` and
// `--no-reflector` each say one of the two things unambiguously, and a third
// spelling of the same choice is a second thing that can disagree with the
// first.
//
// That derivation is not the inference docs/ddns.md §3.1 forbids. What that
// refuses is olr choosing a source by looking at the box — "this address looks
// private, I will use a reflector". Choosing by which flag the operator typed is
// the operator choosing.
func (f *recordFlags) apply(c *cobra.Command, rec *Record) error {
	secret, err := readToken(c, f.token, f.tokenFile)
	if err != nil {
		return err
	}

	if c.Flags().Changed("provider") {
		rec.Provider = ProviderName(f.provider)
	}
	if c.Flags().Changed("zone") {
		rec.Zone = f.zone
	}
	if c.Flags().Changed("key-id") {
		rec.KeyID = f.keyID
	}
	if secret != "" {
		rec.Token = secret
	}
	if c.Flags().Changed("url") {
		rec.CallbackURL = f.callbackURL
	}
	if c.Flags().Changed("interface") {
		rec.Interface = f.iface
	}

	switch {
	case c.Flags().Changed("reflector"):
		rec.Source = SourceReflector
		rec.ReflectorURL = f.reflector
	case f.noReflector:
		rec.Source = SourceInterface
		rec.ReflectorURL = ""
	case rec.Source == "" && rec.Interface != "":
		// `add <name> --interface eth0` with no reflector. The operator named an
		// interface and nothing else, which is the interface source said in the
		// only way this command offers to say it.
		rec.Source = SourceInterface
	}

	if c.Flags().Changed("interval") {
		var d Duration
		if err := d.UnmarshalText([]byte(f.interval)); err != nil {
			return err
		}
		rec.Interval = d
	}
	if c.Flags().Changed("ttl") {
		rec.TTL = f.ttl
	}
	return nil
}

func addCommand() *cobra.Command {
	var flags recordFlags

	c := verb("add", "Start keeping a public name pointing at this router", func(c *cobra.Command) {
		c.Use = "add <name>"
		c.Args = cobra.ExactArgs(1)
		c.Long = "Keep a public name pointing at this router, updating it whenever the\n" +
			"address changes.\n\n" +
			"Two things have to be said: whose DNS the name lives in, and where the\n" +
			"address comes from. The second is the one to get right.\n\n" +
			"  --reflector <url>   ask an endpoint what address the internet sees.\n" +
			"                      Right when olr sits behind a modem, which is the\n" +
			"                      usual setup, and the only form that can tell you\n" +
			"                      your ISP has put you behind carrier-grade NAT.\n" +
			"  --interface <name>  read the address off an uplink this router owns.\n" +
			"                      Right when olr terminates the WAN itself — PPPoE,\n" +
			"                      or an address from the ISP by DHCP.\n\n" +
			"olr will not choose between them. One of them makes this box talk to a\n" +
			"third party every few minutes and the other does not, and that is not a\n" +
			"decision to make on your behalf.\n\n" +
			"Adding a name does not expose anything. A current DNS record and an\n" +
			"open port are two separate decisions; `olr firewall` is the other one.\n\n" +
			"Examples:\n" +
			"  olr dial add home.example.net --provider cloudflare \\\n" +
			"      --token-file - --reflector https://api.ipify.org\n" +
			"  olr dial add home.example.net --provider alidns \\\n" +
			"      --key-id LTAI... --token-file key.txt --interface ppp0"
		flags.register(c)
		c.RunE = func(c *cobra.Command, args []string) error {
			rec := Record{Name: args[0]}
			if err := flags.apply(c, &rec); err != nil {
				return err
			}
			if rec.Source == "" {
				// Caught here rather than at the daemon so the message can name
				// the flags rather than the field.
				return fmt.Errorf("say where the address comes from: " +
					"--reflector <url> to ask an endpoint what the internet sees, " +
					"or --interface <name> to read it off an uplink this router owns")
			}
			return sendRecord(c, rec)
		}
	})
	return c
}

func setCommand() *cobra.Command {
	var flags recordFlags

	c := verb("set", "Change a name olr keeps current", func(c *cobra.Command) {
		c.Use = "set <name>"
		c.Args = cobra.ExactArgs(1)
		c.Long = "Change one of the names olr keeps current.\n\n" +
			"Only the fields you name are changed. The credential is left alone\n" +
			"unless you pass --token or --token-file, so changing the interval does\n" +
			"not mean retyping it."
		flags.register(c)
		c.RunE = func(c *cobra.Command, args []string) error {
			if c.Flags().NFlag() == 0 {
				return fmt.Errorf("nothing to set; see `olr dial set --help`")
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			rec, found := cfg.Find(args[0])
			if !found {
				return unknownRecord(cfg.Names(), args[0])
			}
			if err := flags.apply(c, &rec); err != nil {
				return err
			}
			return sendRecord(c, rec)
		}
	})
	c.ValidArgsFunction = cli.CompleteArgs(recordNames)
	c.AddCommand(setUplinkCommand())
	return c
}

// setUplinkCommand is `olr dial set uplink`.
//
// Flags rather than positionals for all three values, which R2 permits and here
// requires: none of them is the uplink's *identity*. There is one uplink, so
// there is nothing to say which — and `--interface` in particular is a property
// that an operator changes on the object that already exists, which is exactly
// what R2 says a positional must not be.
func setUplinkCommand() *cobra.Command {
	var (
		iface     string
		address   string
		gateway   string
		dns       []string
		noDNS     bool
		noAddress bool
	)

	c := &cobra.Command{
		Use:   "uplink",
		Short: "Set how this router reaches the internet",
		Args:  cobra.NoArgs,
		Long: "Tell olr how this router itself reaches the internet: which interface\n" +
			"faces your modem, what address it has, and where to send everything else.\n\n" +
			"  olr dial set uplink --interface enp2s0 \\\n" +
			"      --address 192.168.2.9/24 --gateway 192.168.2.1\n\n" +
			"The address keeps its host bits — 192.168.2.9/24, not 192.168.2.0/24.\n" +
			"It is this box's address on the link, and the mask says how big the link\n" +
			"is.\n\n" +
			"From then on olr owns that interface's address and the default route, and\n" +
			"puts both back after a reboot. The interface must be adopted and must not\n" +
			"carry a network: a network is something this router *serves*, and the way\n" +
			"out is not one.\n\n" +
			"This connects the router. It does not connect the networks behind it —\n" +
			"for that, add a gateway exit pointing at the same next hop and send your\n" +
			"networks through it, or their traffic leaves untranslated and nothing\n" +
			"comes back.\n\n" +
			"Changing the address or the gateway can drop the session you are typing\n" +
			"into; --dry-run shows what would move before you commit to it.",
		RunE: func(c *cobra.Command, _ []string) error {
			if c.Flags().NFlag() == 0 {
				return fmt.Errorf("nothing to set; see `olr dial set uplink --help`")
			}
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}

			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			next := cfg.Clone()
			u := next.Uplink.Clone()
			if u == nil {
				u = &Uplink{}
			}

			if c.Flags().Changed("interface") {
				u.Interface = iface
			}
			if u.Interface == "" {
				// Caught here rather than at the daemon so the message can name
				// the flag rather than the field.
				return fmt.Errorf("say which interface faces your modem: --interface <name>")
			}

			switch {
			case noAddress:
				u.IPv4 = nil
			case c.Flags().Changed("address"), c.Flags().Changed("gateway"):
				if u.IPv4 == nil {
					u.IPv4 = &UplinkIPv4{}
				}
				if c.Flags().Changed("address") {
					prefix, err := netip.ParsePrefix(strings.TrimSpace(address))
					if err != nil {
						return fmt.Errorf("--address: %w; give it with a mask, "+
							"such as 192.168.2.9/24", err)
					}
					u.IPv4.Address = prefix
				}
				if c.Flags().Changed("gateway") {
					gw, err := netip.ParseAddr(strings.TrimSpace(gateway))
					if err != nil {
						return fmt.Errorf("--gateway: %w", err)
					}
					u.IPv4.Gateway = gw
				}
			}

			switch {
			case noDNS:
				u.DNS = nil
			case c.Flags().Changed("dns"):
				u.DNS = nil
				for _, s := range dns {
					addr, err := netip.ParseAddr(strings.TrimSpace(s))
					if err != nil {
						return fmt.Errorf("--dns: %w", err)
					}
					u.DNS = append(u.DNS, addr)
				}
			}

			next.SetUplink(*u)
			if cli.DryRun(c) {
				return planAndPrint(c, next)
			}
			return applyAndPrint(c, "PUT", uplinkEndpoint, u)
		},
	}

	c.Flags().StringVar(&iface, "interface", "", "the interface facing your modem or your ISP")
	c.Flags().StringVar(&address, "address", "",
		"this box's address on that link, with its mask, such as 192.168.2.9/24")
	c.Flags().BoolVar(&noAddress, "no-address", false,
		"olr owns the interface but writes no address and no route")
	c.Flags().StringVar(&gateway, "gateway", "",
		"where to send everything else — your modem's address on that link")
	c.Flags().StringArrayVar(&dns, "dns", nil,
		"resolver your ISP gave you (repeatable). Recorded only; nothing reads these yet")
	c.Flags().BoolVar(&noDNS, "no-dns", false, "forget the recorded ISP resolvers")

	c.MarkFlagsMutuallyExclusive("address", "no-address")
	c.MarkFlagsMutuallyExclusive("gateway", "no-address")
	c.MarkFlagsMutuallyExclusive("dns", "no-dns")
	_ = c.RegisterFlagCompletionFunc("interface", cli.CompleteFlag(interfaceNames))
	return c
}

func rmCommand() *cobra.Command {
	c := verb("rm", "Stop keeping a name current", func(c *cobra.Command) {
		c.Use = "rm <name>"
		c.Args = cobra.ExactArgs(1)
		c.Long = "Stop updating a name.\n\n" +
			"The name is not removed from your DNS. It keeps resolving to whatever\n" +
			"was last published and simply stops being refreshed, so it goes stale\n" +
			"the next time your address changes. Delete it at your provider if you\n" +
			"want it gone."
		c.RunE = func(c *cobra.Command, args []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			if _, found := cfg.Find(args[0]); !found {
				return unknownRecord(cfg.Names(), args[0])
			}
			if cli.DryRun(c) {
				desired := cfg.Clone()
				desired.RemoveRecord(args[0])
				return planAndPrint(c, desired)
			}
			return applyAndPrint(c, "DELETE", recordEndpoint(args[0]), nil)
		}
	})
	c.ValidArgsFunction = cli.CompleteArgs(recordNames)
	c.AddCommand(rmUplinkCommand())
	return c
}

func rmUplinkCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "uplink",
		Short: "Stop owning how this router reaches the internet",
		Args:  cobra.NoArgs,
		Long: "Hand the way out back.\n\n" +
			"Nothing is torn down. The interface keeps the address it has and the\n" +
			"default route stays exactly where it is, so this box does not go offline.\n" +
			"What stops is olr owning them: it will not put them back after a reboot,\n" +
			"and it will not replace the route when something else changes it.\n\n" +
			"Do this when you are handing the interface to your distribution's network\n" +
			"configuration. Give it the same address there before you reboot, or the\n" +
			"box comes back with no way out and nothing that knows what it had.",
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ValidateOutput(c); err != nil {
				return err
			}
			cfg, err := loadConfig(c)
			if err != nil {
				return err
			}
			if cfg.Uplink == nil {
				return fmt.Errorf("olr does not own this box's uplink; " +
					"there is nothing to hand back")
			}
			if cli.DryRun(c) {
				desired := cfg.Clone()
				desired.RemoveUplink()
				return planAndPrint(c, desired)
			}
			return applyAndPrint(c, "DELETE", uplinkEndpoint, nil)
		},
	}
}

// ---------------------------------------------------------------- status

func statusCommand() *cobra.Command {
	return verb("status", "Show when each name was last checked and last published", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Show, for each name, three separate things: when its address was last\n" +
			"read, what that address was, and whether the last attempt to publish it\n" +
			"succeeded.\n\n" +
			"They are separate because the failure worth catching is the one where\n" +
			"the first two look fine and the third has been failing for a week — the\n" +
			"record goes stale, nothing looks wrong, and you find out at the moment\n" +
			"you are away from home and need it."
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			var resp statusResponse
			if err := cli.ClientFor(c).Get(ctxOf(c), statusEndpoint, &resp); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp)
			}
			return writeStatusText(c.OutOrStdout(), resp)
		}
	})
}

// ---------------------------------------------------------------- shared

// sendRecord writes one record, or plans the change it would make.
//
// The write goes to the item route so the daemon holds the lock across the whole
// edit; the plan goes to /plan with the whole document, because a plan is a diff
// and a diff needs both sides. That asymmetry is deliberate — the alternative is
// a client that splices the list itself and a lock that covers half of it.
func sendRecord(c *cobra.Command, rec Record) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	if cli.DryRun(c) {
		cfg, err := loadConfig(c)
		if err != nil {
			return err
		}
		desired := cfg.Clone()
		desired.SetRecord(rec)
		return planAndPrint(c, desired)
	}
	return applyAndPrint(c, "PUT", recordEndpoint(rec.Name), rec)
}

func planAndPrint(c *cobra.Command, desired Config) error {
	var plan planView
	if err := cli.ClientFor(c).Post(ctxOf(c), planEndpoint, desired, &plan); err != nil {
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), plan)
	}
	return writePlanText(c.OutOrStdout(), plan, true)
}

func applyAndPrint(c *cobra.Command, method, path string, body any) error {
	var result applyResponse
	if err := cli.ClientFor(c).Do(ctxOf(c), method, path, body, &result); err != nil {
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), result)
	}
	if err := writePlanText(c.OutOrStdout(), result.Plan, false); err != nil {
		return err
	}
	// An uplink change continues into the kernel and can land halfway. §5.2
	// gives it no rollback, so what did happen is printed rather than swallowed
	// — writeSteps stays quiet when every step succeeded.
	return writeSteps(c.OutOrStdout(), result.Steps)
}

// readToken gets the credential from wherever the operator put it.
//
// `--token-file -` reads standard input, which is the form that keeps a
// credential out of both the shell history and the process table. `--token`
// stays because refusing it outright would push people to paste the value into a
// config file instead, which is worse. Copied in shape from internal/ingress,
// which had this argument first.
func readToken(c *cobra.Command, token, tokenFile string) (string, error) {
	switch {
	case token != "":
		return token, nil
	case tokenFile == "":
		return "", nil
	case tokenFile == "-":
		data, err := io.ReadAll(c.InOrStdin())
		if err != nil {
			return "", fmt.Errorf("reading the credential from standard input: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	default:
		data, err := os.ReadFile(tokenFile)
		if err != nil {
			return "", fmt.Errorf("reading the credential: %w", err)
		}
		return strings.TrimSpace(string(data)), nil
	}
}

// ---------------------------------------------------------------- completion

func recordNames(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	return cfg.Names(), nil
}

// interfaceNames completes --interface from what this machine has and olr was
// given, which is the set an interface-sourced record may name.
func interfaceNames(c *cobra.Command) ([]string, error) {
	var resp struct {
		Interfaces []struct {
			Name    string `json:"name"`
			Adopted bool   `json:"adopted"`
		} `json:"interfaces"`
	}
	if err := cli.ClientFor(c).Get(ctxOf(c), core.APIPrefix+"/link/interfaces", &resp); err != nil {
		return nil, err
	}
	var out []string
	for _, iface := range resp.Interfaces {
		if iface.Adopted {
			out = append(out, iface.Name)
		}
	}
	return out, nil
}

func providerFlagValues() []string {
	out := make([]string, 0, len(ProviderNames()))
	for _, n := range ProviderNames() {
		out = append(out, string(n))
	}
	return out
}
