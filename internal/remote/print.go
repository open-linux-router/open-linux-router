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
	g := c.WireGuard
	state := "off"
	if g.Enabled {
		state = "on"
	}
	fmt.Fprintf(w, "Remote access is %s\n\n", state)

	t := table(w)
	fmt.Fprintf(t, "endpoint\t%s\n", orDash(g.EndpointWithPort()))
	fmt.Fprintf(t, "network\t%s\n", g.SubnetOrDefault())
	fmt.Fprintf(t, "this box\t%s\n", g.RouterAddr())
	fmt.Fprintf(t, "interface\t%s\n", g.InterfaceOrDefault())
	fmt.Fprintf(t, "port\tUDP/%d\n", g.PortOrDefault())
	// "set" rather than the mask the API returned. Printing `********` invites
	// somebody to think eight asterisks is the key.
	fmt.Fprintf(t, "key\t%s\n", keyState(g.PrivateKey))
	if err := t.Flush(); err != nil {
		return err
	}

	fmt.Fprintln(w)
	if err := writePeersText(w, peersOf(c)); err != nil {
		return err
	}

	if g.ExtraConf != "" {
		fmt.Fprintf(w, "\nextra WireGuard configuration:\n")
		for _, line := range strings.Split(g.ExtraConf, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	return nil
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

func writeStatusText(w io.Writer, s statusResponse) error {
	state := "off"
	if s.Enabled {
		state = "on"
	}
	fmt.Fprintf(w, "Remote access is %s\n\n", state)

	t := table(w)
	fmt.Fprintf(t, "endpoint\t%s\n", orDash(s.Endpoint))
	fmt.Fprintf(t, "network\t%s\n", s.Subnet)
	fmt.Fprintf(t, "tunnel\t%s\n", tunnelLine(s))
	fmt.Fprintf(t, "public key\t%s\n", orDash(s.PublicKey))
	if err := t.Flush(); err != nil {
		return err
	}

	core.WriteBlockersText(w, s.Blockers)
	core.WriteFixHint(w, ModuleName, s.Blockers)

	fmt.Fprintln(w)
	if err := writePeersText(w, s.Peers); err != nil {
		return err
	}

	fmt.Fprintln(w)
	switch {
	case s.DriftError != "":
		fmt.Fprintf(w, "config: unknown (%s)\n", s.DriftError)
	case s.Drifted:
		fmt.Fprintf(w, "config: drifted — the box no longer matches what olr stored\n")
	default:
		fmt.Fprintf(w, "config: matches\n")
	}

	if s.Drifted && s.Drift != nil {
		fmt.Fprintln(w)
		return writePlanText(w, *s.Drift, true)
	}
	return nil
}

// tunnelLine folds the kernel's three booleans into one sentence, keeping the
// "we could not tell" case distinct from "it is not there" (design.md §3.4).
func tunnelLine(s statusResponse) string {
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
