package dial

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

// addHint is the one command that fills an empty list, spelled once.
const addHint = "Add one with `olr dial add <name> --provider <provider> --token <token> " +
	"--reflector <url>`."

func writeConfigText(w io.Writer, c Config) error {
	if c.Empty() {
		return cli.NoObjects(w, "names", addHint)
	}

	t := table(w)
	fmt.Fprintln(t, "NAME\tPROVIDER\tADDRESS FROM\tEVERY\tCREDENTIAL")
	for _, r := range c.Records {
		fmt.Fprintf(t, "%s\t%s\t%s\t%s\t%s\n",
			r.Name, r.Provider, orDash(recordFrom(r)),
			Duration(r.ResolvedInterval()), credentialState(r))
	}
	return t.Flush()
}

func writeRecordText(w io.Writer, r Record) error {
	t := table(w)
	fmt.Fprintf(t, "name\t%s\n", r.Name)
	fmt.Fprintf(t, "zone\t%s\n", orDash(r.ZoneOrDerived()))
	fmt.Fprintf(t, "provider\t%s\n", r.Provider)
	if r.KeyID != "" {
		fmt.Fprintf(t, "key id\t%s\n", r.KeyID)
	}
	fmt.Fprintf(t, "credential\t%s\n", credentialState(r))
	fmt.Fprintf(t, "source\t%s\n", r.Source)
	switch r.Source {
	case SourceInterface:
		fmt.Fprintf(t, "interface\t%s\n", orDash(r.Interface))
	case SourceReflector:
		// The URL is not printed for the callback provider's sibling field, but
		// a reflector URL is not a credential and is the first thing to check
		// when a record stops updating.
		fmt.Fprintf(t, "reflector\t%s\n", orDash(r.ReflectorURL))
		fmt.Fprintf(t, "asked through\t%s\n", orAny(r.Interface))
	}
	fmt.Fprintf(t, "record type\t%s\n", RecordType)
	fmt.Fprintf(t, "checked every\t%s\n", Duration(r.ResolvedInterval()))
	fmt.Fprintf(t, "ttl\t%s\n", ttlText(r.TTL))
	return t.Flush()
}

// credentialState says whether there is one without saying what it is. The
// stored value reaching here is already `********` — printing "set" says the
// same thing without inviting anyone to think eight asterisks is the value.
func credentialState(r Record) string {
	if r.Token == "" && r.CallbackURL == "" {
		return "not set"
	}
	return "set"
}

func ttlText(ttl int) string {
	if ttl <= 0 {
		return "the provider's default"
	}
	return Duration(time.Duration(ttl) * time.Second).String()
}

// writeStatusText is the three-questions surface (docs/ddns.md §6).
//
// One block per record rather than one row, because the answer has three parts
// and a table with three timestamp columns is a table nobody reads. The
// important property is that a record is never described with one word: if the
// address was read two minutes ago and the last publish failed four days ago,
// both lines appear and neither hides the other.
func writeStatusText(w io.Writer, resp statusResponse) error {
	if len(resp.Records) == 0 {
		return cli.NoObjects(w, "names", addHint)
	}

	for i, r := range resp.Records {
		if i > 0 {
			fmt.Fprintln(w)
		}
		fmt.Fprintf(w, "%s  →  %s\n", r.Name, r.Provider)

		t := table(w)
		fmt.Fprintf(t, "  address\t%s\n", addressLine(r))
		fmt.Fprintf(t, "  checked\t%s\n", checkedLine(r, resp.AsOf))
		fmt.Fprintf(t, "  published\t%s\n", publishedLine(r, resp.AsOf))
		if err := t.Flush(); err != nil {
			return err
		}

		for _, line := range alerts(r) {
			fmt.Fprintf(w, "\n  ! %s\n", line)
		}
		for _, p := range r.Problems {
			// The field, not the whole path. `records[0].reflector_url` under a
			// heading that already says which record it is spends its first ten
			// characters repeating the line above it.
			fmt.Fprintf(w, "  ! %s: %s\n", fieldOf(p.Path), p.Message)
		}
	}
	return nil
}

func addressLine(r recordView) string {
	if r.Address == "" {
		return "not read yet, from " + orDash(r.From)
	}
	return fmt.Sprintf("%s, from %s", r.Address, orDash(r.From))
}

func checkedLine(r recordView, now time.Time) string {
	switch {
	case r.Checked == nil && !r.Watched:
		return "never — olr is not publishing this record"
	case r.Checked == nil:
		return "not yet; the first check happens as soon as olrd picks this up"
	case r.CheckError != "":
		return fmt.Sprintf("%s ago, and it failed: %s", ago(r.Checked, now), r.CheckError)
	default:
		return ago(r.Checked, now) + " ago"
	}
}

func publishedLine(r recordView, now time.Time) string {
	switch {
	case r.PublishError != "" && r.Published == nil:
		// The worst state and the one that must not read as "pending": nothing
		// has ever been published and the provider is refusing.
		return "never — the provider refused: " + r.PublishError
	case r.PublishError != "":
		return fmt.Sprintf("%s ago at %s, and every attempt since has been refused: %s",
			ago(r.Published, now), r.PublishedAddress, r.PublishError)
	case r.Published == nil:
		return "not yet"
	case r.PublishedAddress != r.Address && r.Address != "":
		return fmt.Sprintf("%s ago at %s, which is no longer the address",
			ago(r.Published, now), r.PublishedAddress)
	default:
		return fmt.Sprintf("%s ago at %s", ago(r.Published, now), r.PublishedAddress)
	}
}

// alerts are the conditions worth a line of their own.
func alerts(r recordView) []string {
	var out []string
	if r.CGNAT {
		out = append(out, fmt.Sprintf(
			"%s is inside the carrier-grade NAT range, so %s resolves to an address "+
				"nobody outside your ISP can reach. No port forward will make it reachable; "+
				"ask your ISP for a public address.", r.Address, r.Name))
	}
	if r.Retry != nil {
		out = append(out, fmt.Sprintf(
			"backing off after %s; the next attempt is at %s",
			core.Plural(r.Failures, "refusal"), r.Retry.Format(time.Kitchen)))
	}
	return out
}

// fieldOf trims `records[3].interface` down to `interface`, for a finding shown
// under a heading that already names the record.
func fieldOf(path string) string {
	if i := strings.LastIndexByte(path, '.'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// ago renders a gap the way somebody reading a status line thinks about it.
func ago(then *time.Time, now time.Time) string {
	if then == nil {
		return "never"
	}
	d := now.Sub(*then)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return core.Plural(int(d.Hours()/24), "day")
	}
}

// --- plan -------------------------------------------------------------------

func writePlanText(w io.Writer, plan planView, dryRun bool) error {
	if plan.Empty {
		fmt.Fprintln(w, cli.NothingToDo)
		return writeWarnings(w, plan.Warnings)
	}

	verb := map[bool]string{true: "would change", false: "changed"}[dryRun]
	fmt.Fprintf(w, "%s %s:\n", core.Plural(len(plan.Changes), "record"), verb)
	for _, c := range plan.Changes {
		fmt.Fprintf(w, "  %-6s %s\n", c.Kind, c.Path)
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
	fmt.Fprintf(w, "\n%s:\n", core.Plural(len(warnings), "note"))
	for _, p := range warnings {
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

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func orAny(s string) string {
	if s == "" {
		return "whichever way out the routing table picks"
	}
	return s
}

func unknownRecord(have []string, name string) error {
	return cli.UnknownObject("record", name,
		"olr dial add <name> --provider <provider>", have)
}
