package link

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
)

// `olr net` — the operator-facing half of the group object.
//
// design.md §4.4 settles the vocabulary: `group` is what the schema and the API
// call it, **network** is what the operator sees. That is progressive disclosure
// (§1), not two models — this file is a spelling of the same object the REST
// surface publishes as `groups`.
//
// §11.2 sets the bar for `add` and states it as a test rather than an
// aspiration: creating a network must take only a name. If it needs four flags,
// "UX first" has not been met. Everything below is in service of that sentence.

// NetCommand returns the `olr net` tree. Mounted by cmd/olr alongside the
// module trees.
func NetCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "net",
		Short:   "Networks this router serves",
		GroupID: cli.GroupOperations,
		Long: "A network is a subnet, the interface it lives on, and this router's own\n" +
			"address on it.\n\n" +
			"It is the object everything else keys off: an address range is served on a\n" +
			"network, not on an interface, and the range is checked against the network's\n" +
			"subnet rather than against whatever address the interface happens to hold.\n" +
			"That is what makes `olr net add lan --subnet 172.16.1.0/24` the way to\n" +
			"renumber a LAN, instead of configuring the address somewhere olr cannot see.",
	}
	c.AddCommand(netShowCommand(), netAddCommand(), netSetCommand(), netRemoveCommand())
	return c
}

func netShowCommand() *cobra.Command {
	return verb("show", "Show the networks this router serves", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.RunE = func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			resp, err := loadInterfaces(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp.Groups)
			}
			return writeGroupsText(c.OutOrStdout(), resp)
		}
	})
}

// netFlags are the fields a network has, shared by add and set.
type netFlags struct {
	subnet   string
	router   string
	member   string
	noSubnet bool
}

func (f *netFlags) bind(c *cobra.Command) {
	c.Flags().StringVar(&f.subnet, "subnet", "",
		"the network in CIDR form, e.g. 172.16.1.0/24 (default: the next free private /24)")
	c.Flags().StringVar(&f.router, "router", "",
		"this router's address on the network (default: the first address in the subnet)")
	c.Flags().StringVar(&f.member, "member", "",
		"the interface the network lives on (default: the only adopted interface without one)")
	c.Flags().BoolVar(&f.noSubnet, "no-subnet", false,
		"serve no IPv4 here, leaving the network to router advertisement alone")
}

func netAddCommand() *cobra.Command {
	f := &netFlags{}
	c := verb("add", "Create a network", func(c *cobra.Command) {
		c.Use = "add <name>"
		c.Long = "Create a network.\n\n" +
			"Only the name is required. The subnet defaults to the next free private /24,\n" +
			"this router takes the first address in it, and the interface defaults to the\n" +
			"one adopted interface that does not already carry a network. Every one of\n" +
			"those is overridable and none of them has to be typed.\n\n" +
			"Applying this writes the router's address to the interface. If you are\n" +
			"connected to this box over that interface, your session will drop — run it\n" +
			"with --dry-run first, which prints exactly what would change."
		c.Args = cobra.ExactArgs(1)
		c.RunE = func(c *cobra.Command, args []string) error {
			return mutateNet(c, func(cfg *Config, resp listResponse) (string, error) {
				name := args[0]
				if _, exists := cfg.Group(name); exists {
					return "", fmt.Errorf("a network called %q already exists; use `olr net set %s` to change it", name, name)
				}
				g := Group{Name: name}
				if err := applyNetFlags(&g, f, cfg, resp, true); err != nil {
					return "", err
				}
				cfg.SetGroup(g)
				return fmt.Sprintf(
					"Add an address range with `olr dhcp add pool %s`.", name), nil
			})
		}
	})
	f.bind(c)
	return c
}

func netSetCommand() *cobra.Command {
	f := &netFlags{}
	c := verb("set", "Change a network", func(c *cobra.Command) {
		c.Use = "set <name>"
		c.Long = "Change a network's subnet, router address or interface.\n\n" +
			"Changing the subnet renumbers the network: every client on it loses the\n" +
			"address it holds, and anything configured with a fixed address in the old\n" +
			"subnet stops being reachable. Use --dry-run to see the consequences first."
		c.Args = cobra.ExactArgs(1)
		c.RunE = func(c *cobra.Command, args []string) error {
			return mutateNet(c, func(cfg *Config, resp listResponse) (string, error) {
				g, ok := cfg.Group(args[0])
				if !ok {
					return "", fmt.Errorf("no network called %q; `olr net show` lists the ones there are", args[0])
				}
				if err := applyNetFlags(&g, f, cfg, resp, false); err != nil {
					return "", err
				}
				cfg.SetGroup(g)
				return "Clients renew on their own schedule; a client that is already up keeps its old address until then.", nil
			})
		}
	})
	f.bind(c)
	c.ValidArgsFunction = cli.CompleteArgs(groupNames)
	return c
}

func netRemoveCommand() *cobra.Command {
	c := verb("rm", "Delete a network", func(c *cobra.Command) {
		c.Use = "rm <name>"
		c.Long = "Delete a network and take its address off the interface.\n\n" +
			"Any address range served on this network stops validating, and `olr dhcp`\n" +
			"will say so against its own field the next time it is applied. Remove the\n" +
			"range first if you mean to stop serving cleanly."
		c.Args = cobra.ExactArgs(1)
		c.RunE = func(c *cobra.Command, args []string) error {
			return mutateNet(c, func(cfg *Config, _ listResponse) (string, error) {
				if !cfg.RemoveGroup(args[0]) {
					return "", fmt.Errorf("no network called %q", args[0])
				}
				return "Any address range on this network will be refused until the network exists again.", nil
			})
		}
	})
	c.ValidArgsFunction = cli.CompleteArgs(groupNames)
	return c
}

// applyNetFlags folds the flags into a network, deriving what was not given.
//
// `creating` decides whether an absent flag means "derive it" or "leave it
// alone". That distinction is the whole difference between add and set: `olr
// net set lan --router 172.16.1.254` must not silently re-derive the subnet.
func applyNetFlags(g *Group, f *netFlags, cfg *Config, resp listResponse, creating bool) error {
	if f.member != "" {
		g.Members = []string{f.member}
	} else if creating {
		member, err := deriveMember(cfg, resp)
		if err != nil {
			return err
		}
		g.Members = []string{member}
	}

	if f.noSubnet {
		if f.subnet != "" || f.router != "" {
			return fmt.Errorf("--no-subnet cannot be combined with --subnet or --router")
		}
		g.IPv4 = nil
		return nil
	}

	v4 := GroupIPv4{}
	if g.IPv4 != nil {
		v4 = *g.IPv4
	}

	switch {
	case f.subnet != "":
		prefix, err := netip.ParsePrefix(f.subnet)
		if err != nil {
			return fmt.Errorf("--subnet %q is not a subnet in CIDR form like 172.16.1.0/24: %w", f.subnet, err)
		}
		if !prefix.Addr().Is4() {
			return fmt.Errorf("--subnet %s is IPv6; olr does not manage IPv6 prefixes yet, "+
				"they are derived from the uplink's delegation", prefix)
		}
		v4.Subnet = prefix.Masked()
	case creating && !v4.Subnet.IsValid():
		next, err := nextFreeSubnet(cfg)
		if err != nil {
			return err
		}
		v4.Subnet = next
	}

	if f.router != "" {
		addr, err := netip.ParseAddr(f.router)
		if err != nil {
			return fmt.Errorf("--router %q is not an address: %w", f.router, err)
		}
		if !addr.Is4() {
			return fmt.Errorf("--router %s is IPv6; the router address of an IPv4 network is IPv4", addr)
		}
		v4.Router = &addr
	}

	g.IPv4 = &v4
	return nil
}

// deriveMember picks the interface a new network lives on.
//
// The rule is "the only adopted interface not already carrying a network", and
// it refuses rather than guesses when there is more than one. Guessing wrong
// here writes an address onto the wrong NIC — quite possibly the uplink — which
// is a much worse outcome than one extra flag.
func deriveMember(cfg *Config, resp listResponse) (string, error) {
	var free []string
	for _, name := range cfg.Adopted {
		if _, taken := cfg.GroupFor(name); !taken {
			free = append(free, name)
		}
	}
	switch len(free) {
	case 1:
		return free[0], nil
	case 0:
		if len(cfg.Adopted) == 0 {
			return "", fmt.Errorf("no interface has been adopted yet; run `olr adopt <interface>` first " +
				"(`olr interfaces` lists what this machine has)")
		}
		return "", fmt.Errorf("every adopted interface already carries a network; " +
			"adopt another one, or pass --member to put two networks on one interface")
	default:
		return "", fmt.Errorf("several interfaces are free (%s); pass --member to say which one this "+
			"network lives on", strings.Join(free, ", "))
	}
}

// candidateSubnets are the private /24s a derived subnet is drawn from, in
// order.
//
// 192.168.1.0/24 is deliberately *not* first. It is the single most common home
// router default, so a box that picked it would collide with whatever network
// this one is being plugged into — which is exactly the situation somebody
// setting up a router behind another router is in.
var candidateSubnets = []string{
	"192.168.10.0/24", "192.168.20.0/24", "192.168.30.0/24",
	"10.10.10.0/24", "10.10.20.0/24", "10.20.0.0/24",
	"172.16.1.0/24", "172.16.2.0/24", "172.16.3.0/24",
}

// nextFreeSubnet picks a /24 that no existing network claims.
func nextFreeSubnet(cfg *Config) (netip.Prefix, error) {
	for _, candidate := range candidateSubnets {
		prefix := netip.MustParsePrefix(candidate)
		taken := false
		for _, g := range cfg.Groups {
			if g.IPv4 != nil && g.IPv4.Subnet.IsValid() && g.IPv4.Subnet.Overlaps(prefix) {
				taken = true
				break
			}
		}
		if !taken {
			return prefix, nil
		}
	}
	return netip.Prefix{}, fmt.Errorf("every subnet olr would pick from is already in use; pass --subnet")
}

// mutateNet is the read-modify-write path shared by add, set and remove.
//
// Identical in shape to mutate() above, and separate from it because the hint
// is computed from the edit rather than passed in: what to tell the operator
// next depends on whether they just created a network or deleted one.
func mutateNet(c *cobra.Command, edit func(*Config, listResponse) (string, error)) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	ctx, client := ctxOf(c), cli.ClientFor(c)

	cfg, err := loadConfig(c)
	if err != nil {
		return err
	}
	resp, err := loadInterfaces(c)
	if err != nil {
		return err
	}

	hint, err := edit(&cfg, resp)
	if err != nil {
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
	if err := writeStepsText(c.OutOrStdout(), result.Steps); err != nil {
		return err
	}
	_, err = fmt.Fprintf(c.OutOrStdout(), "\n%s\n", hint)
	return err
}

// groupNames completes a network argument from what exists.
func groupNames(c *cobra.Command) ([]string, error) {
	cfg, err := loadConfig(c)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(cfg.Groups))
	for _, g := range cfg.Groups {
		out = append(out, g.Name)
	}
	slices.Sort(out)
	return out, nil
}
