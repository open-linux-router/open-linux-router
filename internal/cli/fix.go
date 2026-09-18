package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// `olr <module> fix`, built once for every module that has blockers.
//
// Here rather than in each module for the reason core.WriteBlockersText is in
// core: one dnsmasq holds both UDP/67 and :53, so it appears under `olr dhcp`
// and under `olr dns`, and an operator who is told two different things has to
// work out whether they are looking at one problem or two. Nothing in this file
// knows anything about either module — it reads blockers, which are core's shape,
// off a path built from the module's name.
//
// It is a client of the API like everything else in this package (§6.1). olrd
// does the work; this asks, waits, and prints what happened.

// fixStatus is the sliver of a module's /status this needs.
//
// Deliberately a fragment rather than the module's own statusResponse, which
// internal/cli cannot see: the modules import this package, so it can never
// import them (internal/dhcp/cli.go and friends). Decoding a fragment works
// against every module's status and stays working when one of them grows a
// field of its own.
//
// What the fragment assumes is that a module which can be switched on publishes
// these three at the top level of its status, and the assumption is load-bearing
// in a way that hides its own failures: a module that spells them differently
// decodes as three zero values, which is indistinguishable from a module that is
// switched off, and gets skipped in silence. `remote` did exactly that — its two
// halves each carried `enabled` and `drifted` under `tunnel` and `proxy` and
// nothing carried them above — so neither the blocker clearing nor the drift
// warning below ever reached it. internal/remote's moduleStatus now answers at
// the top level too, and internal/cli/fix_test.go holds the shape.
//
// `link`, `dial` and `devices` publish none of the three and are meant to: none
// of them has a switch to be on, so "switched on and not what is on the box" is
// not a sentence about them. They decode as off and are skipped, which is the
// right answer arrived at honestly.
type fixStatus struct {
	Enabled bool `json:"enabled"`

	// Drifted is §5.4's answer to "is the box what the configuration says",
	// and it is the one field every switchable module publishes that answers
	// it. The backend's own state is the sharper signal and is spelled
	// differently in each — internal/dns publishes `services`, internal/dhcp
	// `service` — so reading that here would make this fragment depend on which
	// module it is looking at, which is the property these fields exist to keep.
	Drifted bool `json:"drifted"`

	Blockers []core.Blocker `json:"blockers,omitempty"`
}

// fixResult is what POST /blockers/fix answers with, successful or not.
type fixResult struct {
	Steps []core.Step     `json:"steps,omitempty"`
	Error *core.ErrorBody `json:"error,omitempty"`
}

// statusPath and fixPath are where a module serves those.
func statusPath(module string) string { return core.APIPrefix + "/" + module + "/status" }
func fixPath(module string) string    { return core.APIPrefix + "/" + module + "/blockers/fix" }

// FixCommand builds `olr <module> fix`.
//
// what is the module's half of the sentence — "DNS's way", "DHCP's way" — so
// the help reads as that module's rather than as a generic facility.
func FixCommand(module, what string) *cobra.Command {
	c := Verb("fix", "Clear what is standing in "+what+" way on this box")
	c.Args = cobra.NoArgs
	c.Long = "Clear what is standing in " + what + " way on this box.\n\n" +
		"These are problems with the machine rather than with olr's configuration:\n" +
		"a backend that is not installed, or one of the distribution's own daemons\n" +
		"holding a port olr needs. `olr " + module + " status` reports them; this does\n" +
		"something about them, using the same commands it would otherwise print.\n\n" +
		"olr installs packages with your distribution's package manager and stands\n" +
		"down a shadow copy of a daemon it runs itself. It never stops an OS\n" +
		"component: systemd-resolved is asked to give up the socket and keeps\n" +
		"running, because this box resolves names through it.\n\n" +
		"Use --dry-run to see the exact commands first. Nothing here is undone\n" +
		"automatically, so a step that fails leaves the ones before it in place."
	c.RunE = func(c *cobra.Command, _ []string) error {
		return runFix(c, module)
	}
	return c
}

func runFix(c *cobra.Command, module string) error {
	if err := ValidateOutput(c); err != nil {
		return err
	}
	ctx, client, out := ctxOfCommand(c), ClientFor(c), c.OutOrStdout()

	var status fixStatus
	if err := client.Get(ctx, statusPath(module), &status); err != nil {
		return err
	}

	// Asked before anything is sent, so the common case — a box with nothing
	// wrong — says so instead of reporting an empty list of steps that reads
	// like something was attempted and did nothing.
	actionable := core.Actionable(status.Blockers)
	if len(actionable) == 0 {
		if IsJSON(c) {
			return JSON(out, fixResult{})
		}
		return writeNothingToFix(out, status.Blockers)
	}

	if DryRun(c) {
		if IsJSON(c) {
			return JSON(out, actionable)
		}
		return writeWouldFix(out, actionable)
	}

	// The one call in this CLI that may take minutes. See FixTimeout.
	var result fixResult
	err := client.WithTimeout(FixTimeout).Post(ctx, fixPath(module), nil, &result)
	if IsJSON(c) {
		if jsonErr := JSON(out, result); jsonErr != nil {
			return jsonErr
		}
		return err
	}
	// Steps before the error, and printed even when there is one: §5.3.2 has no
	// rollback, so which halves landed is the operator's starting point.
	core.WriteStepsText(out, result.Steps)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "\nDone. `olr %s status` will say whether anything is still in the way.\n", module)
	return nil
}

// writeNothingToFix distinguishes "nothing is wrong" from "something is wrong
// and olr cannot help", which are very different things to be told.
func writeNothingToFix(w io.Writer, blockers []core.Blocker) error {
	if len(blockers) == 0 {
		_, err := fmt.Fprintln(w, "Nothing is in the way.")
		return err
	}
	fmt.Fprintln(w, "Nothing here is something olr can clear for you:")
	core.WriteBlockersText(w, blockers)
	return nil
}

func writeWouldFix(w io.Writer, blockers []core.Blocker) error {
	fmt.Fprintln(w, "Would run, in this order:")
	for _, b := range blockers {
		fmt.Fprintf(w, "\n%s\n", b.Summary)
		for _, line := range b.Action.Runs {
			fmt.Fprintf(w, "  %s\n", line)
		}
	}
	_, err := fmt.Fprintln(w, "\nNothing was changed. Re-run without --dry-run to do it.")
	return err
}

// clearBlockers asks every switched-on module to clear what is in its way.
//
// What `olr enable` does once the daemon is up, and the reason the whole change
// exists: an operator who runs `sudo olr enable` on a tarball box should end up
// with a box that works, not with a web page telling them to go and type two
// more commands. The second of those two is *caused* by the first — Debian
// starts unbound.service when the unbound package lands — so stopping halfway
// is worse than not starting.
//
// Only modules that are switched on. That is the whole of df7d7bc's argument
// preserved: a box that serves DHCP and nothing else is still never made to
// fetch a resolver, because its dns module is off and is not asked. A module
// that is off keeps the warning warnDistroBackends already prints.
//
// Generic over the API — the module list comes from GET /api/modules and the
// blockers from each module's /status — so this needs no module imports and
// cannot create the cycle that keeps internal/cli out of them.
//
// Best-effort throughout. `olr enable` succeeded the moment olrd came up; a
// package mirror being down must not turn that into a failed install.
func clearBlockers(ctx context.Context, client *Client, out io.Writer) {
	var listing struct {
		Modules []string `json:"modules"`
	}
	if err := client.Get(ctx, core.APIPrefix+"/modules", &listing); err != nil {
		fmt.Fprintf(out, "\nwarning: could not ask olrd what is in the way: %v\n", err)
		return
	}

	for _, module := range listing.Modules {
		var status fixStatus
		if err := client.Get(ctx, statusPath(module), &status); err != nil {
			// A module with no /status, or one that cannot read the system
			// right now. Neither is a failure of `olr enable`.
			continue
		}
		if !status.Enabled {
			continue
		}
		actionable := core.Actionable(status.Blockers)
		if len(actionable) == 0 {
			// Nothing for olr to clear, which is not the same as nothing wrong.
			// A module can be switched on and still not be what is on the box:
			// its backend not running, a file it rendered not there yet, one it
			// no longer renders still there. `olr enable` printed its success
			// block over exactly that, because blockers were the only thing it
			// asked about — and a resolver that had been exiting every two
			// seconds for an hour was reported as "nothing else on this machine
			// has changed".
			if status.Drifted {
				fmt.Fprintf(out, "\nwarning: %s is switched on, and what it says should be on\n"+
					"this box is not — a backend that is not running is the usual reason.\n"+
					"`olr %s status` says what differs, and `sudo olr %s enable` re-applies\n"+
					"it.\n\n", module, module, module)
			}
			continue
		}

		// Said before it is done, not after. §7's promise is that installing
		// olr changes nothing until you say so — `olr enable` is saying so, and
		// it survives only if saying so tells you what changed.
		fmt.Fprintf(out, "\n%s has something in the way, and olr can clear it:\n\n", module)
		for _, b := range actionable {
			for _, line := range b.Action.Runs {
				fmt.Fprintf(out, "  %s\n", line)
			}
		}
		fmt.Fprintln(out)

		var result fixResult
		err := client.WithTimeout(FixTimeout).Post(ctx, fixPath(module), nil, &result)
		core.WriteStepsText(out, result.Steps)
		if err != nil {
			fmt.Fprintf(out, "warning: %v\nolr is running; `sudo olr %s fix` retries just this.\n",
				err, module)
		}
	}
}

// ctxOfCommand is the request context, falling back to Background for a command
// invoked outside cobra's execution (which the tests do). The same helper each
// module keeps as ctxOf; spelled out here because this package cannot import
// one of them to borrow it.
func ctxOfCommand(c *cobra.Command) context.Context {
	if ctx := c.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}
