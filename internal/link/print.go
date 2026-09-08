package link

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/core"
)

// Text output for humans; -o json is the machine-readable surface. Both come
// from the same structs, so the two never disagree about what exists.

func table(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
}

func writeConfigText(w io.Writer, c Config) error {
	if c.Empty() {
		return cli.NoObjects(w, "adopted interfaces",
			"Hand one to olr with `olr adopt <interface>`, and see what this machine has\n"+
				"with `olr link show interfaces`.")
	}
	fmt.Fprintf(w, "%s adopted:\n", core.Plural(len(c.Adopted), "interface"))
	for _, name := range c.Adopted {
		fmt.Fprintf(w, "  %s\n", name)
	}
	return nil
}

func writeInterfacesText(w io.Writer, resp listResponse) error {
	if len(resp.Interfaces) == 0 {
		return cli.NoneObserved(w, "interfaces", "")
	}

	t := table(w)
	fmt.Fprintln(t, "INTERFACE\tOLR\tSTATE\tADDRESSES")
	for _, iface := range resp.Interfaces {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\n",
			iface.Name, adoptedText(iface), stateText(iface), addressesText(iface))
	}
	if err := t.Flush(); err != nil {
		return err
	}

	return writeProblems(w, resp.Problems)
}

// adoptedText is the column an operator scans for. "yes"/"-" rather than a
// tick, because this is also what `grep` sees.
func adoptedText(iface interfaceView) string {
	if iface.Adopted {
		return "yes"
	}
	return "-"
}

// stateText collapses three booleans into the one sentence fragment that
// distinguishes them, because "up" alone cannot say "up with no cable in it" —
// which is the single most common reason a freshly configured DHCP server
// appears to do nothing.
func stateText(iface interfaceView) string {
	switch {
	case !iface.Present:
		return "absent"
	case iface.Loopback:
		return "loopback"
	case !iface.Up:
		return "down"
	case !iface.Running:
		return "up, no carrier"
	default:
		return "up"
	}
}

func addressesText(iface interfaceView) string {
	if len(iface.Prefixes) == 0 {
		return "-"
	}
	return strings.Join(iface.Prefixes, " ")
}

func writePlanText(w io.Writer, plan planView, dryRun bool) error {
	if plan.Empty {
		fmt.Fprintln(w, cli.NothingToDo)
		return writeWarnings(w, plan.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", core.Plural(len(plan.Changes), "interface"), verb)
	for _, c := range plan.Changes {
		// adopt/release rather than create/delete: the shared plan shape says
		// what happened to a document entry, and this module's entries are
		// permissions. "delete eth0" reads like the interface went away.
		fmt.Fprintf(w, "  %-8s %s\n", changeVerb(c.Kind), nameOf(c.Path))
	}

	// No service line and no impact line. Every other module prints them
	// because something on the box moves; here nothing does, and printing
	// "impact: none" under a change that genuinely has none would train an
	// operator to skip the line on the modules where it matters.
	return writeWarnings(w, plan.Warnings)
}

func changeVerb(kind string) string {
	if kind == kindDelete {
		return "release"
	}
	return "adopt"
}

// nameOf unwraps "adopted[eth0]" back to "eth0" for display. The path shape is
// the API's, and it is right there — a UI attaches a message to a field with
// it. It is only in a terminal that it is noise.
func nameOf(path string) string {
	name := strings.TrimPrefix(path, "adopted[")
	return strings.TrimSuffix(name, "]")
}

func writeWarnings(w io.Writer, warnings []core.Problem) error {
	return writeLabelled(w, "warning", warnings)
}

func writeProblems(w io.Writer, problems []core.Problem) error {
	return writeLabelled(w, "note", problems)
}

func writeLabelled(w io.Writer, noun string, in []core.Problem) error {
	if len(in) == 0 {
		return nil
	}
	fmt.Fprintf(w, "\n%s:\n", core.Plural(len(in), noun))
	for _, p := range in {
		fmt.Fprintf(w, "  %s\n", problemText(p))
	}
	return nil
}

func problemText(p core.Problem) string {
	if p.Path == "" {
		return p.Message
	}
	return p.Path + ": " + p.Message
}
