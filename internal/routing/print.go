package routing

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Text output for humans; -o json is the machine-readable surface. Both come
// from the same structs, so the two never disagree about what exists.
//
// The vocabulary here is the operator's, not the schema's (§1.3). "Exit" is the
// word in the config and the API; on screen the sentence is *Internet via …*,
// with the preposition doing the work.

func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func writeConfigText(w io.Writer, c Config) error {
	state := "on"
	if !c.Enabled {
		state = "off — every network is using this box's own connection"
	}
	fmt.Fprintf(w, "Routing policy is %s\n", state)
	fmt.Fprintf(w, "\nInternet via:  %s\n", describeDefault(c.Default))

	fmt.Fprintln(w)
	if err := writeExitsText(w, c); err != nil {
		return err
	}
	fmt.Fprintln(w)
	return writeAssignmentsText(w, c)
}

func writeExitsText(w io.Writer, c Config) error {
	if len(c.Exits) == 0 {
		fmt.Fprintln(w, "No exits configured. Add one with `olr routing add exit`.")
		return nil
	}
	t := table(w)
	fmt.Fprintln(t, "EXIT\tGOES\tIPV6\tIF DOWN\tHEALTH CHECK\tUSED BY\tMARK")
	for _, e := range c.Exits {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			e.Name,
			describeVia(e.Via),
			e.IPv6OrDefault(),
			e.OnFailure.OrDefault(),
			describeProbe(e.Probe),
			orDash(strings.Join(c.UsedBy(e.Name), ", ")),
			markString(e.Mark()),
		)
	}
	return t.Flush()
}

func writeAssignmentsText(w io.Writer, c Config) error {
	if len(c.Interfaces) == 0 {
		fmt.Fprintf(w, "No network has an exit of its own; every one uses %s.\n",
			describeDefault(c.Default))
		return nil
	}
	t := table(w)
	fmt.Fprintln(t, "NETWORK\tINTERNET VIA\tFROM")
	for _, a := range c.Interfaces {
		name, source := c.Assigned(a.Interface)
		fmt.Fprintf(t, "%s\t%s\t%s\n", a.Interface, describeDefault(name), describeSource(source))
	}
	return t.Flush()
}

func writeStatusText(w io.Writer, s statusView) error {
	state := "on"
	if !s.Enabled {
		state = "off"
	}
	fmt.Fprintf(w, "Routing policy is %s\n", state)

	if !s.Known {
		// Said first, because everything below it is then configuration rather
		// than observation, and a reader who missed the distinction would take
		// the table as proof the box is doing this.
		fmt.Fprintln(w,
			"\nThe kernel could not be read, so nothing below reflects what is actually running.\n"+
				"On Linux this usually means olrd is missing CAP_NET_ADMIN.")
	} else if s.Drifted {
		fmt.Fprintln(w,
			"\nThe kernel does not match this configuration. Run `olr routing show --dry-run`\n"+
				"to see the difference, or re-apply to correct it.")
	}

	if len(s.Exits) > 0 {
		fmt.Fprintln(w)
		t := table(w)
		fmt.Fprintln(t, "EXIT\tSTATE\tUSED BY\tMARK\tTABLE\tIP RULE")
		for _, e := range s.Exits {
			fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%d\t%d\n",
				e.Name, describeHealth(e), orDash(strings.Join(e.UsedBy, ", ")),
				e.Mark, e.Table, e.Priority)
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}

	if len(s.Assignments) > 0 {
		fmt.Fprintln(w)
		t := table(w)
		fmt.Fprintln(t, "NETWORK\tINTERNET VIA\tFROM\tNOTE")
		for _, a := range s.Assignments {
			fmt.Fprintf(t, "%s\t%s\t%s\t%s\n",
				a.Interface, describeDefault(a.Exit), describeSource(a.Source), orDash(a.Reason))
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}

	// §7.3's residual rule: show what you cannot account for. Somebody else's
	// rules are reported rather than hidden, which is what makes a hand-rolled
	// setup legible instead of mysterious.
	if len(s.Foreign) > 0 {
		fmt.Fprintf(w, "\n%s not managed by olr:\n", core.Plural(len(s.Foreign), "ip rule"))
		t := table(w)
		fmt.Fprintln(t, "  PRIORITY\tFAMILY\tTABLE\tRULE")
		for _, f := range s.Foreign {
			fmt.Fprintf(t, "  %d\t%s\t%d\t%s\n", f.Priority, f.Family, f.Table, f.Selector)
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}

	if err := writeWarnings(w, s.Problems); err != nil {
		return err
	}

	fmt.Fprintf(w, "\nas of %s\n", s.AsOf.Format("2006-01-02 15:04:05"))
	return nil
}

func writeTrafficText(w io.Writer, t trafficView) error {
	switch {
	case !t.Enabled:
		fmt.Fprintln(w, "Traffic counting is off. Turn it on with "+
			"`olr routing set traffic-counting on`.")
		return nil
	case !t.Counting:
		// Intent and reality disagree, which is a different sentence from "off".
		fmt.Fprintln(w, "Traffic counting is on, but this router could not read the counters.\n"+
			"On Linux this usually means olrd is missing CAP_NET_ADMIN.")
		return nil
	case len(t.Usage) == 0:
		fmt.Fprintln(w, "Nothing counted yet. Counting starts when the router forwards traffic,\n"+
			"and the totals reset when it restarts.")
	default:
		tw := table(w)
		fmt.Fprintln(tw, "DEVICE\tINTERNET VIA\tSENT\tRECEIVED\tTOTAL")
		for _, u := range t.Usage {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				u.Address, describeUsageExit(u),
				humanBytes(u.UpBytes), humanBytes(u.DownBytes),
				humanBytes(u.UpBytes+u.DownBytes))
		}
		if err := tw.Flush(); err != nil {
			return err
		}
	}

	// §7.4 asks for these to be printed rather than left to be discovered.
	// Every one explains a number being smaller than expected, and the first
	// question a surprising number produces is "is this broken?".
	if len(t.Limits) > 0 {
		fmt.Fprintln(w, "\nWhat these numbers do not include:")
		for _, l := range t.Limits {
			fmt.Fprintf(w, "  · %s\n", l)
		}
	}

	fmt.Fprintf(w, "\nas of %s\n", t.AsOf.Format("2006-01-02 15:04:05"))
	return nil
}

func describeUsageExit(u usageView) string {
	switch {
	case u.Unknown:
		// Distinguished from the residual on purpose: "went somewhere that no
		// longer exists" and "matched no rule" are different facts.
		return "a way out that was removed"
	case u.Exit == "":
		// §7.3's residual, named rather than left blank so the row reads as an
		// answer instead of a gap.
		return "not routed by olr"
	default:
		return u.Exit
	}
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
	if plan.Blocked != "" {
		// Printed before anything else and without the diff, because the diff
		// describes work that is not going to happen.
		fmt.Fprintf(w, "Refused: %s\n", plan.Blocked)
		if len(plan.Foreign) > 0 {
			t := table(w)
			fmt.Fprintln(t, "  PRIORITY\tFAMILY\tTABLE\tRULE")
			for _, f := range plan.Foreign {
				fmt.Fprintf(t, "  %d\t%s\t%d\t%s\n", f.Priority, f.Family, f.Table, f.Selector)
			}
			if err := t.Flush(); err != nil {
				return err
			}
		}
		return nil
	}

	if !plan.Known {
		fmt.Fprintln(w, "The kernel could not be read, so there is nothing to compare against.")
		return writeWarnings(w, plan.Warnings)
	}

	switch {
	case plan.Empty && dryRun:
		fmt.Fprintln(w, "Nothing to change.")
	case plan.Empty:
		fmt.Fprintln(w, "No change.")
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
// design.md §5.3.2 has no rollback, so this is the whole substitute for one:
// the operator needs to know which half of the change is live, and on this
// module that is the difference between "traffic is routed" and "traffic is
// marked for a table that does not exist".
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

// describeVia renders an exit's form the way an operator would say it.
func describeVia(v Via) string {
	switch v.Kind {
	case ViaInterface:
		return "out " + v.Interface
	case ViaNextHop:
		if v.NextHop == nil {
			return "next hop (unset)"
		}
		if v.Dev != "" {
			return "to " + v.NextHop.String() + " on " + v.Dev
		}
		return "to " + v.NextHop.String()
	case ViaBlocked:
		return "nowhere — refused"
	}
	return string(v.Kind)
}

// describeDefault renders an exit name, or the absence of one.
//
// "this box's own connection" rather than a dash, because the empty value is a
// real answer here and a dash reads as "nothing is set" — which would leave the
// operator looking for the setting they had already made.
func describeDefault(name string) string {
	if name == "" {
		return "this box's own connection"
	}
	return name
}

func describeSource(s Source) string {
	switch s {
	case SourceInterface:
		return "this network"
	default:
		return "the box-wide setting"
	}
}

func describeProbe(p *Probe) string {
	if p == nil {
		return "none"
	}
	r := p.Resolved()
	return fmt.Sprintf("%s every %s", r.Target, r.Interval)
}

func describeHealth(e exitStatusView) string {
	switch {
	case !e.Probed:
		// Not "up". Claiming health we never measured is exactly the place
		// §5.6 says faults go to hide.
		return "not checked"
	case e.Up:
		return "up"
	default:
		return "DOWN"
	}
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
