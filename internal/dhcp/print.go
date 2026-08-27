package dhcp

import (
	"fmt"
	"io"
	"net/netip"
	"strings"
	"text/tabwriter"
	"time"
)

// Text output for humans; -o json is the machine-readable surface. Both come
// from the same structs, so the two never disagree about what exists.

func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func writeConfigText(w io.Writer, c Config) error {
	state := "disabled"
	if c.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(w, "DHCP is %s (dnsmasq)\n", state)

	fmt.Fprintln(w)
	if err := writePoolsText(w, c.Pools); err != nil {
		return err
	}
	fmt.Fprintln(w)
	if err := writeReservationsText(w, c.Reservations); err != nil {
		return err
	}
	if c.ExtraConf != "" {
		fmt.Fprintf(w, "\nextra dnsmasq configuration:\n")
		for _, line := range strings.Split(c.ExtraConf, "\n") {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	return nil
}

func writePoolsText(w io.Writer, pools []Pool) error {
	if len(pools) == 0 {
		fmt.Fprintln(w, "No pools configured.")
		return nil
	}
	t := table(w)
	fmt.Fprintln(t, "INTERFACE\tRANGE\tLEASE\tGATEWAY\tDNS\tDOMAIN\tIPv6")
	for _, p := range pools {
		fmt.Fprintf(t, "%s\t%s-%s\t%s\t%s\t%s\t%s\t%s\n",
			p.Interface, p.Start, p.End,
			p.LeaseTimeOrDefault(),
			orRouter(p.Gateway == nil, addrOrEmpty(p.Gateway)),
			orRouter(len(p.DNS) == 0, joinAddrs(p.DNS)),
			orDash(p.Domain),
			p.RA.OrDefault(),
		)
	}
	return t.Flush()
}

func writeReservationsText(w io.Writer, reservations []Reservation) error {
	if len(reservations) == 0 {
		fmt.Fprintln(w, "No reservations configured.")
		return nil
	}
	t := table(w)
	fmt.Fprintln(t, "MAC\tADDRESS\tHOSTNAME\tLEASE")
	for _, r := range reservations {
		lease := "pool default"
		if r.LeaseTime > 0 {
			lease = r.LeaseTime.String()
		}
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", r.MAC, r.IP, orDash(r.Hostname), lease)
	}
	return t.Flush()
}

func writeLeasesText(w io.Writer, leases []Lease, problems []Problem) error {
	if len(leases) == 0 {
		fmt.Fprintln(w, "No leases held.")
	} else {
		now := time.Now()
		t := table(w)
		fmt.Fprintln(t, "ADDRESS\tCLIENT\tHOSTNAME\tEXPIRES")
		for _, l := range leases {
			client := l.MAC
			if client == "" {
				client = "iaid " + l.IAID
			}
			expires := humanTime(l.Expires)
			if !l.Active(now) {
				expires += " (expired)"
			}
			fmt.Fprintf(t, "%s\t%s\t%s\t%s\n", l.IP, client, orDash(l.Hostname), expires)
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}

	// Unparsable lines are reported rather than dropped: a silently short count
	// looks exactly like a quiet network (design.md §3.4, report honestly).
	if len(problems) > 0 {
		fmt.Fprintf(w, "\n%s in the lease database could not be read:\n", plural(len(problems), "line"))
		for _, p := range problems {
			fmt.Fprintf(w, "  %s\n", p)
		}
	}
	return nil
}

func writeStatusText(
	w io.Writer, cfg Config, a Applier,
	service ServiceStatus, serviceErr error,
	leases []Lease, problems []Problem, extra map[string]any,
) error {
	state := "disabled"
	if cfg.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(w, "configuration: %s\n", state)

	switch {
	case serviceErr != nil:
		fmt.Fprintf(w, "service:       unknown (%v)\n", serviceErr)
	case service.Active:
		fmt.Fprintf(w, "service:       %s, running since %s", service.Unit, humanTime(service.Since))
		if service.MainPID > 0 {
			fmt.Fprintf(w, " (pid %d)", service.MainPID)
		}
		fmt.Fprintln(w)
	default:
		fmt.Fprintf(w, "service:       %s, %s\n", service.Unit, orDash(service.State))
	}

	if drifted, ok := extra["drifted"].(bool); ok {
		if drifted {
			fmt.Fprintf(w, "drift:         yes — run `olr dhcp show --dry-run` to see it\n")
		} else {
			fmt.Fprintln(w, "drift:         none")
		}
	} else {
		// Honest about not knowing, rather than reporting "no drift" because
		// we could not look.
		fmt.Fprintln(w, "drift:         unknown (pass --links to check)")
	}

	fmt.Fprintf(w, "leases:        %d\n", len(leases))

	if usage := a.Usage(cfg, leases); len(usage) > 0 {
		fmt.Fprintln(w)
		t := table(w)
		fmt.Fprintln(t, "INTERFACE\tSIZE\tACTIVE\tEXPIRED\tFREE\tUSED")
		for _, u := range usage {
			fmt.Fprintf(t, "%s\t%d\t%d\t%d\t%d\t%d%%\n",
				u.Interface, u.Size, u.Active, u.Expired, u.Free(), u.Percent())
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}

	if len(problems) > 0 {
		fmt.Fprintf(w, "\n%s in the lease database could not be read\n", plural(len(problems), "line"))
	}
	return nil
}

// writePlanText reports a change. dryRun switches between the future and the
// past tense, because "would restart" and "restarted" are answers to different
// questions and conflating them is how an operator ends up unsure whether
// anything happened.
func writePlanText(w io.Writer, plan Plan, dryRun bool) error {
	if plan.Empty() {
		fmt.Fprintln(w, "Nothing to do; the configuration is already applied.")
		return writeWarnings(w, plan.Validation.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", plural(len(plan.Changes), "file"), verb)
	for _, c := range plan.Changes {
		fmt.Fprintf(w, "  %-6s %s\n", c.Kind, c.Path)
	}

	if plan.Action != ActionNone {
		fmt.Fprintf(w, "\nservice: %s\n", describeAction(plan.Action, dryRun))
	}
	fmt.Fprintf(w, "impact:  %s\n", plan.Impact)
	for _, reason := range plan.Reasons {
		fmt.Fprintf(w, "         %s\n", reason)
	}

	if dryRun {
		fmt.Fprintln(w)
		for _, c := range plan.Changes {
			fmt.Fprintln(w, c.Diff())
		}
	}

	return writeWarnings(w, plan.Validation.Warnings)
}

func writeWarnings(w io.Writer, warnings []Problem) error {
	if len(warnings) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\n%s:\n", plural(len(warnings), "warning"))
	for _, p := range warnings {
		fmt.Fprintf(w, "  %s\n", p)
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
	fmt.Fprintln(w, "steps attempted:")
	for _, s := range steps {
		mark := "ok  "
		if !s.Done {
			mark = "FAIL"
		}
		fmt.Fprintf(w, "  [%s] %s\n", mark, s.Description)
		if s.Error != "" {
			fmt.Fprintf(w, "         %s\n", s.Error)
		}
	}
	fmt.Fprintln(w, "nothing was rolled back; fix the cause and re-run to finish.")
}

// describeAction words a service action for the tense the caller is in.
// "would restart" and "restarted" answer different questions, and blurring them
// is how an operator ends up unsure whether anything actually happened.
var actionPastTense = map[ServiceAction]string{
	ActionStart:   "started",
	ActionStop:    "stopped",
	ActionReload:  "reloaded",
	ActionRestart: "restarted",
}

func describeAction(action ServiceAction, dryRun bool) string {
	if dryRun {
		return "would " + string(action)
	}
	if past, ok := actionPastTense[action]; ok {
		return past
	}
	return string(action)
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func orRouter(isDefault bool, s string) string {
	if isDefault {
		return "this router"
	}
	return s
}

func addrOrEmpty(a *netip.Addr) string {
	if a == nil {
		return ""
	}
	return a.String()
}
