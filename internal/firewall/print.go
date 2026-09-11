package firewall

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Text output for humans; -o json is the machine-readable surface. Both come
// from the same structs, so the two never disagree about what exists.
//
// The vocabulary here is the operator's, not the schema's. "Forward" is the word
// in the config and the API; on screen the sentence is *wan0:8080 → 192.168.1.10:80*,
// with the arrow doing the work.

func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func writeConfigText(w io.Writer, c Config) error {
	state := "on"
	if !c.Enabled {
		state = "off — nothing from outside is being sent to a device here"
	}
	fmt.Fprintf(w, "Port forwarding is %s\n\n", state)
	return writeForwardsText(w, c)
}

func writeForwardsText(w io.Writer, c Config) error {
	if len(c.Forwards) == 0 {
		return cli.NoObjects(w, "port forwards", "Add one with `olr firewall add forward`.")
	}
	t := table(w)
	fmt.Fprintln(t, "FORWARD\tARRIVES\tGOES TO\tFROM INSIDE TOO")
	for _, f := range c.Forwards {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\n",
			f.Name, describeArrival(f), f.To, yesNo(f.HairpinOrDefault()))
	}
	return t.Flush()
}

// writeForwardText is the detail half of docs/cli.md R6's pair. The table above
// has to fit four columns on a terminal; this one can afford to answer "what did
// I actually configure here" without abbreviating.
func writeForwardText(w io.Writer, f Forward) error {
	t := table(w)
	for _, row := range [][2]string{
		{"name", f.Name},
		{"arrives on", f.In},
		{"protocol", string(f.ProtocolOrDefault())},
		{"port", f.Port.String()},
		{"goes to", f.To.String()},
		{"from inside too", yesNo(f.HairpinOrDefault())},
		{"counter", f.Counter()},
	} {
		fmt.Fprintf(t, "%s\t%s\n", row[0], row[1])
	}
	if err := t.Flush(); err != nil {
		return err
	}
	fmt.Fprintln(w, "\nRun `olr firewall status` to see whether anything has arrived through it.")
	return nil
}

func writeStatusText(w io.Writer, s statusView) error {
	state := "on"
	if !s.Enabled {
		state = "off"
	}
	fmt.Fprintf(w, "Port forwarding is %s\n", state)

	if !s.Known {
		// Said first, because everything below it is then configuration rather
		// than observation, and a reader who missed the distinction would take
		// the table as proof the box is doing this.
		fmt.Fprintln(w,
			"\nThe kernel could not be read, so nothing below reflects what is actually running.\n"+
				"On Linux this usually means olrd is missing CAP_NET_ADMIN.")
	} else if s.Drifted {
		fmt.Fprintln(w,
			"\nThe kernel does not match this configuration. Run `olr firewall show --dry-run`\n"+
				"to see the difference, or re-apply to correct it.")
	}

	if len(s.Forwards) > 0 {
		fmt.Fprintln(w)
		t := table(w)
		fmt.Fprintln(t, "FORWARD\tARRIVES\tGOES TO\tARRIVED\tTRAFFIC")
		for _, f := range s.Forwards {
			fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n",
				f.Name,
				fmt.Sprintf("%s %s/%s", f.In, f.Protocol, f.Port),
				f.To,
				describeCount(f),
				describeBytes(f),
			)
		}
		if err := t.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w, "\nCounts start again whenever a forward is added, changed or removed.")
	}

	// Somebody else's filtering, reported rather than hidden, which is what
	// makes a hand-rolled setup legible instead of mysterious.
	if len(s.Foreign) > 0 {
		fmt.Fprintf(w, "\n%s on this box filtering forwarded traffic:\n",
			core.Plural(len(s.Foreign), "nftables chain"))
		t := table(w)
		fmt.Fprintln(t, "  TABLE\tCHAIN\tPOLICY")
		for _, f := range s.Foreign {
			fmt.Fprintf(t, "  %s %s\t%s\t%s\n", f.Family, f.Table, f.Chain, f.Policy)
		}
		if err := t.Flush(); err != nil {
			return err
		}
		fmt.Fprintln(w,
			"\n  A drop there cannot be overridden from here, so a forward may be correct\n"+
				"  and still not reach. The ARRIVED column says whether packets get this far.")
	}

	if err := writeWarnings(w, s.Problems); err != nil {
		return err
	}

	fmt.Fprintf(w, "\nas of %s\n", s.AsOf.Format("2006-01-02 15:04:05"))
	return nil
}

// describeCount is the first thing anybody looks at when a port does not work,
// and it has three answers rather than two.
//
// "never" and "not counted" are different facts: the first says the rule is in
// the kernel and no packet has matched it, which points outward at the ISP, the
// modem or somebody else's filter. The second says we could not read the
// counter, which points at this box. Collapsing them into a zero would send the
// operator to debug the wrong half of the problem.
func describeCount(f forwardStatusView) string {
	switch {
	case !f.Counted:
		return "not counted"
	case f.Packets == 0:
		return "never"
	default:
		return core.Plural(int(f.Packets), "packet")
	}
}

func describeBytes(f forwardStatusView) string {
	if !f.Counted || f.Packets == 0 {
		return "-"
	}
	return humanBytes(f.Bytes)
}

// humanBytes renders a count the way an operator reads it.
//
// Powers of 1024 with the short suffixes, because that is what every other tool
// on the box prints and a second convention would make two numbers about the
// same traffic look different.
func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit && exp < 4; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTP"[exp])
}

func writePlanText(w io.Writer, plan planView, dryRun bool) error {
	if !plan.Known {
		fmt.Fprintln(w, "The kernel could not be read, so there is nothing to compare against.")
		return writeWarnings(w, plan.Warnings)
	}

	switch {
	case plan.Empty:
		// One phrasing for both dry run and apply (docs/cli.md R8).
		fmt.Fprintln(w, cli.NothingToDo)
	case dryRun:
		fmt.Fprintf(w, "%s would change (%s)\n",
			core.Plural(len(plan.Changes), "thing"), plan.Impact)
	default:
		fmt.Fprintf(w, "%s changed (%s)\n",
			core.Plural(len(plan.Changes), "thing"), plan.Impact)
	}

	for _, r := range plan.Reasons {
		fmt.Fprintf(w, "  %s\n", r)
	}

	if plan.Diff != "" {
		fmt.Fprintln(w)
		fmt.Fprint(w, plan.Diff)
	}

	return writeWarnings(w, plan.Warnings)
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

// writeStepsText reports what an apply managed to do before it failed.
//
// design.md §5.3.2 has no rollback, so this is the whole substitute for one.
func writeStepsText(w io.Writer, steps []Step) {
	if len(steps) == 0 {
		return
	}
	fmt.Fprintln(w, "\nWhat was done before the failure:")
	for _, s := range steps {
		switch {
		case s.Error != "":
			fmt.Fprintf(w, "  failed  %s: %s\n", s.Description, s.Error)
		case s.Done:
			fmt.Fprintf(w, "  done    %s\n", s.Description)
		default:
			fmt.Fprintf(w, "  skipped %s\n", s.Description)
		}
	}
}

func problemText(p core.Problem) string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

// describeArrival renders where a forward listens the way `nft` and `ss` would
// spell it, so the three are comparable without translation.
func describeArrival(f Forward) string {
	return fmt.Sprintf("%s %s/%s", f.In, f.ProtocolOrDefault(), f.Port)
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}
