package remote

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Text output for humans; -o json is the machine-readable surface. Both come
// from the same structs, so the two never disagree about what exists.

func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func writeConfigText(w io.Writer, c Config) error {
	fmt.Fprintf(w, "Devices dial %s\n\n", orDash(c.EndpointHost()))
	if err := writeWireGuardText(w, c); err != nil {
		return err
	}
	fmt.Fprintln(w)
	return writeShadowsocksText(w, c)
}

// writeWireGuardText is the tunnel: the one that puts a device inside the
// network.
func writeWireGuardText(w io.Writer, c Config) error {
	g := c.WireGuard
	fmt.Fprintf(w, "Tunnel (WireGuard) is %s — devices reach your whole network\n\n", onOff(g.Enabled))

	t := table(w)
	fmt.Fprintf(t, "  devices dial\t%s\n", orDash(c.DialAddress(g.DialPort())))
	fmt.Fprintf(t, "  network\t%s\n", g.SubnetOrDefault())
	fmt.Fprintf(t, "  this box\t%s\n", g.RouterAddr())
	fmt.Fprintf(t, "  interface\t%s\n", g.InterfaceOrDefault())
	// "set" rather than the mask the API returned. Printing `********` invites
	// somebody to think eight asterisks is the key.
	fmt.Fprintf(t, "  key\t%s\n", keyState(g.PrivateKey))
	if err := t.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w)
	if err := writePeersText(w, peersOf(c)); err != nil {
		return err
	}
	return writeExtra(w, "WireGuard", g.ExtraConf)
}

// writeShadowsocksText is the proxy: the one that lends this box's way out and
// shows the network to nobody.
//
// The second line is the whole reason the two are printed differently. An
// operator scanning this has to be able to tell, without reading a manual,
// which of the two does what — and "one link for every device" against "one
// configuration per device" is the difference they will actually run into.
func writeShadowsocksText(w io.Writer, c Config) error {
	s := c.Shadowsocks
	fmt.Fprintf(w, "Proxy (Shadowsocks) is %s — devices borrow this box's way out, and see nothing else\n\n",
		onOff(s.Enabled))

	t := table(w)
	fmt.Fprintf(t, "  devices dial\t%s\n", orDash(c.DialAddress(s.DialPort())))
	fmt.Fprintf(t, "  cipher\t%s\n", s.Cipher.OrDefault())
	fmt.Fprintf(t, "  carries UDP\t%s\n", yesNo(s.UDPEnabled()))
	fmt.Fprintf(t, "  password\t%s\n", keyState(s.Password))
	if err := t.Flush(); err != nil {
		return err
	}
	if s.Password != "" {
		fmt.Fprintf(w, "\n  One link serves every device: `olr remote show link`\n")
	}
	return writeExtra(w, "Shadowsocks", s.ExtraConf)
}

func writeExtra(w io.Writer, what, extra string) error {
	if extra == "" {
		return nil
	}
	fmt.Fprintf(w, "\n  extra %s configuration:\n", what)
	for _, line := range strings.Split(extra, "\n") {
		fmt.Fprintf(w, "    %s\n", line)
	}
	return nil
}

func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

func yesNo(yes bool) string {
	if yes {
		return "yes"
	}
	return "no"
}

func keyState(key string) string {
	if key == "" {
		return "not generated yet"
	}
	return "set"
}

// peersOf renders stored peers without the kernel's half, for the surfaces that
// have the config and not the daemon's reading of it.
func peersOf(c Config) []peerView {
	out := make([]peerView, 0, len(c.WireGuard.Peers))
	for _, p := range c.WireGuard.Peers {
		out = append(out, peerView{
			Name:      p.Name,
			Address:   p.Address,
			Routes:    p.Routes.OrDefault(),
			PublicKey: p.PublicKey,
			Unknown:   true,
		})
	}
	return out
}

func writePeersText(w io.Writer, peers []peerView) error {
	if len(peers) == 0 {
		return cli.NoObjects(w, "devices",
			"Let one in with `olr remote add peer <name>`.")
	}

	t := table(w)
	fmt.Fprintln(t, "NAME\tADDRESS\tSENDS HOME\tLAST SEEN")
	for _, p := range peers {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", p.Name, p.Address, p.Routes, lastSeen(p))
	}
	return t.Flush()
}

// lastSeen is the only honest liveness answer WireGuard offers, in three
// states rather than two.
//
// "never" is not "a long time ago": a peer that has never handshaked almost
// always means the configuration was not imported, or the port is not reachable
// from outside — a setup problem — while a peer last seen yesterday is working
// and asleep. Collapsing them would hide the failure this module is most likely
// to produce.
func lastSeen(p peerView) string {
	switch {
	case p.Unknown:
		return "—"
	case p.Online:
		return "now"
	case p.LastHandshake.IsZero():
		return "never"
	default:
		return core.Plural(int(time.Since(p.LastHandshake).Minutes()), "minute") + " ago"
	}
}

func writePeerText(w io.Writer, p peerView) error {
	t := table(w)
	fmt.Fprintf(t, "name\t%s\n", p.Name)
	fmt.Fprintf(t, "address\t%s\n", p.Address)
	fmt.Fprintf(t, "sends home\t%s\n", p.Routes)
	fmt.Fprintf(t, "public key\t%s\n", orDash(p.PublicKey))
	fmt.Fprintf(t, "last seen\t%s\n", lastSeen(p))
	if p.Endpoint != "" {
		fmt.Fprintf(t, "last seen from\t%s\n", p.Endpoint)
	}
	if p.RxBytes > 0 || p.TxBytes > 0 {
		fmt.Fprintf(t, "transferred\t%s received, %s sent\n", bytesText(p.RxBytes), bytesText(p.TxBytes))
	}
	return t.Flush()
}

// bytesText is deliberately coarse. The exact byte count of a tunnel is never
// the question being asked; "has anything gone through this" is.
func bytesText(n uint64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}

func writeStatusText(w io.Writer, s moduleStatus) error {
	fmt.Fprintf(w, "Devices dial %s\n\n", orDash(s.Endpoint))

	if err := writeTunnelStatus(w, s.Tunnel); err != nil {
		return err
	}
	fmt.Fprintln(w)
	return writeProxyStatus(w, s.Proxy)
}

func writeTunnelStatus(w io.Writer, s tunnelStatus) error {
	fmt.Fprintf(w, "Tunnel (WireGuard) is %s\n", onOff(s.Enabled))

	t := table(w)
	fmt.Fprintf(t, "  network\t%s\n", s.Subnet)
	fmt.Fprintf(t, "  interface\t%s\n", tunnelLine(s))
	fmt.Fprintf(t, "  public key\t%s\n", orDash(s.PublicKey))
	if err := t.Flush(); err != nil {
		return err
	}

	core.WriteBlockersText(w, s.Blockers)
	core.WriteFixHint(w, ModuleName, s.Blockers)

	fmt.Fprintln(w)
	if err := writePeersText(w, s.Peers); err != nil {
		return err
	}
	fmt.Fprintf(w, "  config: %s\n", driftLine(s.Drifted, s.DriftError))
	if s.Drifted && s.Drift != nil {
		fmt.Fprintln(w)
		return writePlanText(w, *s.Drift, true)
	}
	return nil
}

func writeProxyStatus(w io.Writer, s proxyStatus) error {
	fmt.Fprintf(w, "Proxy (Shadowsocks) is %s\n", onOff(s.Enabled))

	t := table(w)
	fmt.Fprintf(t, "  port\tTCP%s/%d\n", map[bool]string{true: " and UDP", false: ""}[s.UDP], s.Port)
	fmt.Fprintf(t, "  cipher\t%s\n", s.Cipher)
	switch {
	case s.BinaryError != "":
		fmt.Fprintf(t, "  server\tnot installed\n")
	case s.Binary != "":
		fmt.Fprintf(t, "  server\t%s\n", s.Binary)
	}
	switch {
	case s.ServiceError != "":
		fmt.Fprintf(t, "  service\tunknown (%s)\n", s.ServiceError)
	case s.Service == nil:
		fmt.Fprintf(t, "  service\tunknown\n")
	default:
		fmt.Fprintf(t, "  service\t%s\n", unitLine(*s.Service))
	}
	if err := t.Flush(); err != nil {
		return err
	}

	if s.BinaryError != "" {
		fmt.Fprintf(w, "\n%s\n\n", s.BinaryError)
	}
	fmt.Fprintf(w, "  config: %s\n", driftLine(s.Drifted, s.DriftError))
	if s.Drifted && s.Drift != nil {
		fmt.Fprintln(w)
		return writeProxyPlanText(w, *s.Drift, true)
	}
	return nil
}

// tunnelLine folds the kernel's three booleans into one sentence, keeping the
// "we could not tell" case distinct from "it is not there" (design.md §3.4).
func tunnelLine(s tunnelStatus) string {
	switch {
	case !s.Known:
		return fmt.Sprintf("%s — unknown (this box cannot be read)", s.Interface)
	case s.Foreign:
		return fmt.Sprintf("%s exists and is not WireGuard; olr will not touch it", s.Interface)
	case !s.Present:
		return fmt.Sprintf("%s does not exist", s.Interface)
	case !s.Up:
		return fmt.Sprintf("%s exists but is down", s.Interface)
	default:
		return fmt.Sprintf("%s is up, listening on UDP/%d", s.Interface, s.Port)
	}
}

func driftLine(drifted bool, err string) string {
	switch {
	case err != "":
		return "unknown (" + err + ")"
	case drifted:
		return "drifted — the box no longer matches what olr stored"
	default:
		return "matches"
	}
}

func unitLine(s core.UnitStatus) string {
	switch {
	case !s.Installed:
		return "not installed"
	case s.Active && s.Enabled:
		return "running, starts at boot"
	case s.Active:
		return "running, but will not start at boot"
	case s.Enabled:
		return "stopped, but set to start at boot"
	default:
		return "stopped"
	}
}

// writeProxyPlanText is the file-and-service plan, which reads nothing like the
// tunnel's line diff — one changes a daemon's configuration, the other changes
// the kernel.
func writeProxyPlanText(w io.Writer, plan proxyPlanView, dryRun bool) error {
	if plan.Empty {
		fmt.Fprintln(w, cli.NothingToDo)
		return writeWarnings(w, plan.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", core.Plural(len(plan.Changes), "file"), verb)
	for _, c := range plan.Changes {
		fmt.Fprintf(w, "  %-6s %s\n", c.Kind, c.Path)
	}
	if plan.Action != ActionNone {
		fmt.Fprintf(w, "\nservice: %s\n", plan.Action)
	}
	fmt.Fprintf(w, "impact:  %s\n", plan.Impact)
	for _, reason := range plan.Reasons {
		fmt.Fprintf(w, "         %s\n", reason)
	}
	return writeWarnings(w, plan.Warnings)
}

// writeEndpointText prints the answer for the one change that has no plan.
func writeEndpointText(w io.Writer, resp endpointResponse, dryRun bool) error {
	if resp.Impact == ImpactNone {
		fmt.Fprintln(w, map[bool]string{true: "Nothing would be invalidated.", false: "Saved."}[dryRun])
		return nil
	}
	fmt.Fprintf(w, "impact: %s\n", resp.Impact)
	for _, reason := range resp.Reasons {
		fmt.Fprintf(w, "        %s\n", reason)
	}
	return nil
}

func writePlanText(w io.Writer, plan planView, dryRun bool) error {
	if plan.Blocked != "" {
		fmt.Fprintf(w, "refused: %s\n", plan.Blocked)
		return nil
	}
	if plan.Empty {
		fmt.Fprintln(w, cli.NothingToDo)
		return writeWarnings(w, plan.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", core.Plural(len(plan.Changes), "line"), verb)
	for _, c := range plan.Changes {
		mark := "+"
		if c.Kind == ChangeRemove {
			mark = "-"
		}
		fmt.Fprintf(w, "  %s %s\n", mark, c.Line)
	}

	fmt.Fprintf(w, "\nimpact: %s\n", plan.Impact)
	for _, reason := range plan.Reasons {
		fmt.Fprintf(w, "        %s\n", reason)
	}

	return writeWarnings(w, plan.Warnings)
}

// writePeerResultText prints the one thing this module produces that an
// operator has to keep, and the one instruction only they can carry out.
//
// Printed to stdout rather than stderr so it can be redirected into a file:
// `olr remote add peer phone > phone.conf` is the whole workflow, and a note
// that scrolled past is a note nobody read.
func writePeerResultText(w io.Writer, result *peerResult) {
	if result == nil {
		return
	}
	fmt.Fprintf(w, "\n%s is at %s.\n", result.Name, result.Address)
	if result.Note != "" {
		fmt.Fprintf(w, "%s\n", result.Note)
	}
	if len(result.NextSteps) > 0 {
		fmt.Fprintln(w, "\nTo reach it by name from home:")
		for _, step := range result.NextSteps {
			fmt.Fprintf(w, "    %s\n", step)
		}
	}
	if result.ClientConfig == "" {
		return
	}
	fmt.Fprintf(w, "\n%s\n", strings.TrimRight(result.ClientConfig, "\n"))
}

func writeWarnings(w io.Writer, warnings []core.Problem) error {
	if len(warnings) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\n%s:\n", core.Plural(len(warnings), "warning"))
	for _, p := range warnings {
		fmt.Fprintf(w, "  %s\n", problemText(p))
	}
	return nil
}

// writeStepsText reports which steps landed before a failure. There is no
// rollback (design.md §5.3.2), so this is the operator's starting point for
// finishing the job.
func writeStepsText(w io.Writer, steps []Step) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintln(w, "What was done before the failure:")
	for _, s := range steps {
		mark := "ok  "
		if !s.Done {
			mark = "FAIL"
		}
		fmt.Fprintf(w, "  %s %s\n", mark, s.Description)
		if s.Error != "" {
			fmt.Fprintf(w, "       %s\n", s.Error)
		}
	}
}

func problemText(p core.Problem) string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
