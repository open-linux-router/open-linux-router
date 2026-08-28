package dhcp

import (
	"fmt"
	"io"
	"net/netip"
	"strings"
	"text/tabwriter"

	"github.com/open-linux-router/open-linux-router/internal/core"
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

func writeLeasesText(w io.Writer, resp leasesResponse) error {
	if len(resp.Leases) == 0 {
		fmt.Fprintln(w, "No leases held.")
	} else {
		t := table(w)
		fmt.Fprintln(t, "ADDRESS\tCLIENT\tHOSTNAME\tEXPIRES")
		for _, l := range resp.Leases {
			client := l.MAC
			if client == "" {
				client = "iaid " + l.IAID
			}
			// Active is computed by the server against the same instant it
			// stamped the response with, so it is not recomputed here against a
			// different clock.
			expires := "never"
			if l.Expires != nil {
				expires = humanTime(*l.Expires)
			}
			if !l.Active {
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
	if len(resp.Problems) > 0 {
		fmt.Fprintf(w, "\n%s in the lease database could not be read:\n", plural(len(resp.Problems), "line"))
		for _, p := range resp.Problems {
			fmt.Fprintf(w, "  %s\n", problemText(p))
		}
	}
	return nil
}

// problemText renders a core.Problem the way dhcp.Problem renders itself, so
// the CLI's wording does not depend on which side of the API it came from.
func problemText(p core.Problem) string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}

func writeStatusText(w io.Writer, status statusResponse, leases leasesResponse) error {
	state := "disabled"
	if status.Enabled {
		state = "enabled"
	}
	fmt.Fprintf(w, "configuration: %s\n", state)

	switch {
	case status.Service == nil:
		fmt.Fprintf(w, "service:       unknown (%s)\n", orDash(status.ServiceError))
	case status.Service.Active:
		fmt.Fprintf(w, "service:       %s, running since %s",
			status.Service.Unit, humanTime(status.Service.Since))
		if status.Service.MainPID > 0 {
			fmt.Fprintf(w, " (pid %d)", status.Service.MainPID)
		}
		fmt.Fprintln(w)
	default:
		fmt.Fprintf(w, "service:       %s, %s\n", status.Service.Unit, orDash(status.Service.State))
	}

	// Reported on its own line, because a unit that is running now and not
	// enabled is the failure that costs nothing until the next reboot and then
	// costs every address on the network at once.
	if status.Service != nil {
		boot := "yes"
		if !status.Service.Enabled {
			boot = "NO — DHCP will not come back after a reboot"
			if !status.Service.Installed {
				boot = "no — the unit is not installed; reinstall the olr package"
			}
		}
		fmt.Fprintf(w, "starts at boot: %s\n", boot)
	}

	switch {
	case status.DriftError != "":
		// Honest about not knowing, rather than reporting "no drift" because we
		// could not look.
		fmt.Fprintf(w, "drift:         unknown (%s)\n", status.DriftError)
	case status.Drifted:
		fmt.Fprintln(w, "drift:         yes — run `olr dhcp show --dry-run` to see it")
	default:
		fmt.Fprintln(w, "drift:         none")
	}

	fmt.Fprintf(w, "leases:        %d\n", len(leases.Leases))

	if len(leases.Usage) > 0 {
		fmt.Fprintln(w)
		t := table(w)
		fmt.Fprintln(t, "INTERFACE\tSIZE\tACTIVE\tEXPIRED\tFREE\tUSED")
		for _, u := range leases.Usage {
			fmt.Fprintf(t, "%s\t%d\t%d\t%d\t%d\t%d%%\n",
				u.Interface, u.Size, u.Active, u.Expired, u.Free, u.Percent)
		}
		if err := t.Flush(); err != nil {
			return err
		}
	}

	if len(leases.Problems) > 0 {
		fmt.Fprintf(w, "\n%s in the lease database could not be read\n",
			plural(len(leases.Problems), "line"))
	}
	return nil
}

// writePlanText reports a change. dryRun switches between the future and the
// past tense, because "would restart" and "restarted" are answers to different
// questions and conflating them is how an operator ends up unsure whether
// anything happened.
func writePlanText(w io.Writer, plan planView, dryRun bool) error {
	if plan.Empty {
		fmt.Fprintln(w, "Nothing to do; the configuration is already applied.")
		return writeWarnings(w, plan.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", plural(len(plan.Changes), "file"), verb)
	for _, c := range plan.Changes {
		fmt.Fprintf(w, "  %-6s %s\n", c.Kind, c.Path)
	}

	if plan.Action != ActionNone {
		fmt.Fprintf(w, "\nservice: %s\n", describeAction(plan.Action, dryRun))
	}
	if plan.Enable != nil {
		fmt.Fprintf(w, "boot:    %s\n",
			describeEnable(*plan.Enable, dryRun))
	}
	fmt.Fprintf(w, "impact:  %s\n", plan.Impact)
	for _, reason := range plan.Reasons {
		fmt.Fprintf(w, "         %s\n", reason)
	}

	if dryRun {
		fmt.Fprintln(w)
		for _, c := range plan.Changes {
			fmt.Fprintln(w, c.Diff)
		}
	}

	return writeWarnings(w, plan.Warnings)
}

func writeWarnings(w io.Writer, warnings []core.Problem) error {
	if len(warnings) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\n%s:\n", plural(len(warnings), "warning"))
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

// describeEnable words the boot-time change, in the same two tenses.
func describeEnable(enable, dryRun bool) string {
	switch {
	case enable && dryRun:
		return "would be set to start at boot"
	case enable:
		return "set to start at boot"
	case dryRun:
		return "would no longer start at boot"
	default:
		return "no longer starts at boot"
	}
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
