package remote

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Every command here is a client of olrd (design.md §6.1). None of them touches
// the config document or the kernel directly: a CLI that programmed the tunnel
// on its own would be a second writer, holding a lock olrd cannot see, running
// its own copy of the validation rules, and publishing none of the change
// events the UI listens for.
//
// The surface spells no protocol — `olr remote set`, not `olr remote set
// wireguard`. There is one object today, and naming it in the command before
// there are two would be inventing a surface for something unbuilt, which is
// the mistake `dial` and `link` each refused in their own package comments. The
// consequence is recorded in docs/remote-access.md §11 #4: the day Shadowsocks
// lands, `set` splits and `enable` grows an object. docs/cli.md §12 is the
// licence for that — a pre-1.0 CLI change is cheap and a stored-config change
// is not, which is why the *document* nests and this does not.

// Command returns the module's command tree. Mounted explicitly by cmd/olr.
func Command() *cobra.Command {
	return cli.NewModule("remote", "Get back into your network from outside it",
		showCommand(),
		setCommand(),
		addCommand(),
		rmCommand(),
		statusCommand(),
		cli.FixCommand(ModuleName, "remote access's"),
		enableCommand(),
		disableCommand(),
	)
}

// Endpoints this module's commands call. Spelled once so a rename cannot leave
// half the commands pointing at the old path.
const (
	base           = core.APIPrefix + "/" + ModuleName
	configEndpoint = base + "/config"
	planEndpoint   = base + "/plan"
	statusEndpoint = base + "/status"
	peersEndpoint  = base + "/peers"
)

// peerEndpoint is the item route for one device.
//
// Escaped, because a name arrives from an operator's argument and reaches a URL
// path. A peer called `../config` should fail as a validation error, not as a
// request to somewhere else.
func peerEndpoint(name string) string {
	return peersEndpoint + "/" + url.PathEscape(name)
}

func ctxOf(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
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
	c := verb("show", "Show remote-access configuration", func(c *cobra.Command) {
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

			var cfg Config
			if err := cli.ClientFor(c).Get(ctxOf(c), configEndpoint, &cfg); err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), cfg)
			}
			return writeConfigText(c.OutOrStdout(), cfg)
		}
	})

	c.AddCommand(showPeersCommand(), showPeerCommand())
	return c
}

func showPeersCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "peers",
		Short: "List the devices that may dial in",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			resp, err := loadPeers(c)
			if err != nil {
				return err
			}
			if cli.IsJSON(c) {
				return cli.JSON(c.OutOrStdout(), resp.Peers)
			}
			return writePeersText(c.OutOrStdout(), resp.Peers)
		},
	}
}

// showPeerCommand is the detail half docs/cli.md R6 requires: a module that can
// list an object has to be able to show one.
func showPeerCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "peer <name>",
		Short: "Show one device in full",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			if err := cli.ReadOnly(c); err != nil {
				return err
			}
			resp, err := loadPeers(c)
			if err != nil {
				return err
			}
			want := normalizeName(args[0])
			for _, p := range resp.Peers {
				if p.Name != want {
					continue
				}
				if cli.IsJSON(c) {
					return cli.JSON(c.OutOrStdout(), p)
				}
				return writePeerText(c.OutOrStdout(), p)
			}
			return unknownPeer(resp.Peers, args[0])
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(peerNamesFor)
	return c
}

func loadPeers(c *cobra.Command) (peersResponse, error) {
	var resp peersResponse
	if err := cli.ClientFor(c).Get(ctxOf(c), peersEndpoint, &resp); err != nil {
		return peersResponse{}, err
	}
	return resp, nil
}

// ---------------------------------------------------------------- set

func setCommand() *cobra.Command {
	var (
		endpoint  string
		subnet    string
		address   string
		noAddress bool
		iface     string
		port      uint16
		raw       string
		clearRaw  bool
	)

	return verb("set", "Set how devices reach this box", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Set the tunnel's settings: the public address your devices dial,\n" +
			"the network they get addresses on, and which port to listen on.\n\n" +
			"The endpoint is the one thing olr cannot work out for itself. If a\n" +
			"name already tracks this box's address, use it — `olr dial show`\n" +
			"lists the ones olr keeps current.\n\n" +
			"Changing the network renumbers every device, so every configuration\n" +
			"already on a phone stops working. olr reports that as a disruptive\n" +
			"change; `--dry-run` shows it before you commit to it."

		c.Flags().StringVar(&endpoint, "endpoint", "",
			"public name or address your devices dial, optionally with a port")
		c.Flags().StringVar(&subnet, "subnet", "",
			"network dial-in devices get addresses on, e.g. "+DefaultSubnet.String())
		c.Flags().StringVar(&address, "address", "", "this box's own address on that network")
		c.Flags().BoolVar(&noAddress, "no-address", false,
			"use the first address in the network, which is the default")
		c.Flags().StringVar(&iface, "interface", "",
			"kernel interface name for the tunnel (default "+DefaultInterface+")")
		c.Flags().Uint16Var(&port, "port", 0,
			fmt.Sprintf("UDP port to listen on (default %d)", DefaultListenPort))
		c.Flags().StringVar(&raw, "raw-wireguard-conf", "",
			"extra WireGuard configuration, appended verbatim")
		c.Flags().BoolVar(&clearRaw, "no-raw-wireguard-conf", false,
			"remove the extra WireGuard configuration")

		c.MarkFlagsMutuallyExclusive("address", "no-address")
		c.MarkFlagsMutuallyExclusive("raw-wireguard-conf", "no-raw-wireguard-conf")

		c.RunE = func(c *cobra.Command, _ []string) error {
			if c.Flags().NFlag() == 0 {
				return fmt.Errorf("nothing to set; see `olr remote set --help`")
			}

			// A merge patch of only what was asked for. `wireguard` is an object
			// so RFC 7386 merges into it key by key, which is exactly what
			// "change the port and leave the devices alone" needs — and why the
			// peers get item routes instead (http.go).
			wg := map[string]any{}
			if c.Flags().Changed("endpoint") {
				wg["endpoint"] = endpoint
			}
			if c.Flags().Changed("subnet") {
				wg["subnet"] = subnet
			}
			if c.Flags().Changed("address") {
				wg["address"] = address
			}
			if noAddress {
				wg["address"] = nil
			}
			if c.Flags().Changed("interface") {
				wg["interface"] = iface
			}
			if c.Flags().Changed("port") {
				wg["listen_port"] = port
			}
			if c.Flags().Changed("raw-wireguard-conf") {
				wg["raw_wireguard_conf"] = raw
			}
			if clearRaw {
				wg["raw_wireguard_conf"] = ""
			}
			return send(c, "PATCH", configEndpoint, map[string]any{"wireguard": wg})
		}
	})
}

// ---------------------------------------------------------------- add / rm

func addCommand() *cobra.Command {
	c := verb("add", "Let a device dial in", func(*cobra.Command) {})
	c.AddCommand(addPeerCommand())
	return c
}

func addPeerCommand() *cobra.Command {
	var (
		routes    string
		publicKey string
	)

	c := &cobra.Command{
		Use:   "peer <name>",
		Short: "Let a device dial in",
		Args:  cobra.ExactArgs(1),
		Long: "Let a device dial in, and print the configuration to import on it.\n\n" +
			"olr generates the device's key pair, keeps the public half and gives\n" +
			"you the rest. It is printed once and cannot be shown again: the\n" +
			"private key is stored nowhere, so losing the file means removing the\n" +
			"device and adding it back.\n\n" +
			"Examples:\n" +
			"  olr remote add peer phone > phone.conf\n" +
			"  olr remote add peer laptop --routes everything\n" +
			"  olr remote add peer work --public-key <key generated on the device>",
		RunE: func(c *cobra.Command, args []string) error {
			// One request to the item route, not load-splice-save. The daemon
			// holds the lock across the whole edit that way — which here also
			// means two clients adding a device at once cannot allocate the
			// same address (http.go).
			body := peerBody{Routes: RouteScope(routes), PublicKey: publicKey}
			return send(c, "PUT", peerEndpoint(args[0]), body)
		},
	}

	c.Flags().StringVar(&routes, "routes", "",
		fmt.Sprintf("what the device sends through the tunnel: %s (default %s)",
			joinScopes(), RouteHome))
	c.Flags().StringVar(&publicKey, "public-key", "",
		"public key generated on the device itself, if you would rather olr never held the private one")
	cli.EnumFlag(c, "routes", scopeNames()...)
	return c
}

func rmCommand() *cobra.Command {
	c := verb("rm", "Stop a device dialling in", func(*cobra.Command) {})
	c.AddCommand(rmPeerCommand())
	return c
}

func rmPeerCommand() *cobra.Command {
	c := &cobra.Command{
		Use:   "peer <name>",
		Short: "Stop a device dialling in",
		Args:  cobra.ExactArgs(1),
		Long: "Stop a device dialling in.\n\n" +
			"Its access ends immediately and its configuration stops working for\n" +
			"good — there is no disabling a peer, because a WireGuard peer is a\n" +
			"key and removing the key is the whole of revoking it.",
		RunE: func(c *cobra.Command, args []string) error {
			return send(c, "DELETE", peerEndpoint(args[0]), nil)
		},
	}
	c.ValidArgsFunction = cli.CompleteArgs(peerNamesFor)
	return c
}

// ---------------------------------------------------------------- status

func statusCommand() *cobra.Command {
	return verb("status", "Show whether the tunnel is up and who has connected", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
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

func enableCommand() *cobra.Command {
	return verb("enable", "Start accepting connections from outside", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Create the tunnel and start accepting connections.\n\n" +
			"olr generates this box's key the first time, so this is also what\n" +
			"makes `olr remote add peer` able to write a configuration."
		c.RunE = func(c *cobra.Command, _ []string) error {
			return send(c, "PATCH", configEndpoint,
				map[string]any{"wireguard": map[string]any{"enabled": true}})
		}
	})
}

func disableCommand() *cobra.Command {
	return verb("disable", "Stop accepting connections from outside", func(c *cobra.Command) {
		c.Args = cobra.NoArgs
		c.Long = "Remove the tunnel. Every device loses its way in, and the\n" +
			"configuration is kept — including this box's key — so enabling again\n" +
			"needs no device to be re-issued."
		c.RunE = func(c *cobra.Command, _ []string) error {
			return send(c, "PATCH", configEndpoint,
				map[string]any{"wireguard": map[string]any{"enabled": false}})
		}
	})
}

// ---------------------------------------------------------------- shared

// send is the shape every change shares: one request naming the thing to
// change, then print what it did.
//
// Applying happens on return, with no staged commit (design.md §5.1), so the
// diff and the impact are printed either way. That is also why every request
// here carries confirm=true: the disruptive gate exists for the WebUI, which can
// put the question to somebody and wait. A command that returned "this would be
// disruptive, run it again" would be a staged commit by another name, and §5.1
// says this surface does not have one. `--dry-run` is how you look first.
//
// It reads more dangerous on this module than elsewhere, because what a
// disruptive change takes away is somebody's way in. That is recorded in
// docs/remote-access.md §11 #5 rather than special-cased here: one contract
// across every module beats a surprise in one of them.
func send(c *cobra.Command, method, path string, body any) error {
	if err := cli.ValidateOutput(c); err != nil {
		return err
	}
	ctx, client := ctxOf(c), cli.ClientFor(c)

	if cli.DryRun(c) {
		var plan planView
		if err := client.Do(ctx, method, path+"?dry_run=true", body, &plan); err != nil {
			return err
		}
		if cli.IsJSON(c) {
			return cli.JSON(c.OutOrStdout(), plan)
		}
		return writePlanText(c.OutOrStdout(), plan, true)
	}

	var result applyResponse
	if err := client.Do(ctx, method, path+"?confirm=true", body, &result); err != nil {
		// Report what landed before returning the failure: there is no
		// rollback, so which steps completed is the operator's starting point
		// (design.md §5.3.2).
		writeStepsText(c.ErrOrStderr(), result.Steps)
		return err
	}
	if cli.IsJSON(c) {
		return cli.JSON(c.OutOrStdout(), result)
	}
	// "Nothing to do" is suppressed when there is something for the operator to
	// do: narrowing a device's routes changes no kernel state at all, and a
	// line saying the configuration is already applied would be the opposite of
	// what the note under it is about to say.
	if result.Peer == nil || !result.Plan.Empty {
		if err := writePlanText(c.OutOrStdout(), result.Plan, false); err != nil {
			return err
		}
	} else if err := writeWarnings(c.OutOrStdout(), result.Plan.Warnings); err != nil {
		// Suppressing the plan must not suppress the findings under it. The
		// warning that a device will reach this box and nothing behind it
		// arrives on exactly this path — a peer added to a box with no networks
		// yet — and it is the one an operator most needs.
		return err
	}
	// The other half of the same honesty problem. `olr remote set --endpoint …`
	// on a box where remote access is off stores the endpoint and programs
	// nothing, so the shared "nothing to do" line — which is about the *kernel*
	// — reads as "your command did nothing". It is true and it is not the
	// sentence the operator needs, so the missing half is added rather than the
	// shared phrasing replaced (docs/cli.md R8).
	if result.Peer == nil && result.Plan.Empty && result.Config != nil && !result.Config.WireGuard.Enabled {
		fmt.Fprintln(c.OutOrStdout(),
			"Remote access is off, so nothing on the box changed. `olr remote enable` turns it on.")
	}

	// Last, and after the plan, so that the file is the final thing on screen
	// and a redirect of stdout captures it whole.
	writePeerResultText(c.OutOrStdout(), result.Peer)
	return nil
}

func unknownPeer(peers []peerView, name string) error {
	have := make([]string, 0, len(peers))
	for _, p := range peers {
		have = append(have, p.Name)
	}
	return cli.UnknownObject("device", name, "olr remote add peer <name>", have)
}

func peerNamesFor(c *cobra.Command) ([]string, error) {
	resp, err := loadPeers(c)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.Peers))
	for _, p := range resp.Peers {
		out = append(out, p.Name)
	}
	return out, nil
}

func scopeNames() []string {
	out := make([]string, 0, len(RouteScopes()))
	for _, s := range RouteScopes() {
		out = append(out, string(s))
	}
	return out
}

func joinScopes() string { return strings.Join(scopeNames(), ", ") }
