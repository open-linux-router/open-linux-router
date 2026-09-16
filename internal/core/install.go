package core

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Running the distribution's package manager, which is the one fix olr performs
// that it cannot perform in-process.
//
// This is a subprocess started by olrd, and design.md §3.6 says olrd executes
// none — so the exception is taken deliberately, and handed to systemd as a
// transient unit rather than forked from here.
//
// That shape was originally forced: olrd ran under ProtectSystem=strict, and an
// apt-get inside that mount namespace cannot write /var/lib/dpkg and fails on a
// read-only filesystem for reasons that read like a bug in olr. The sandbox is
// gone (design.md §3.5, "Privileges") and the shape stays, because it was the
// better one anyway — a package manager that can run for minutes belongs in a
// unit of its own, where it is one line in the journal with its own name rather
// than output interleaved into olrd's.
//
// systemd-run --wait --pipe is how that is asked for. It ships with systemd, it
// propagates the service's exit status, and it hands the transient unit our own
// pipes so the package manager's output comes back here rather than only to the
// journal — which matters because every interesting failure (no network, dpkg
// lock held, unknown package) explains itself in that output and nothing olr
// could say would be as useful.

// installPackage installs one package and reports how it went.
func installPackage(ctx context.Context, argv []string) []Step {
	step := Step{Description: strings.Join(argv, " ")}

	// Refused rather than escalated. Shelling out to sudo has no tty here and
	// would hang or fail obscurely; olrd is root, and the CLI paths that reach
	// this check for root themselves before they get here.
	if geteuid() != 0 {
		step.Error = "installing a package needs root, and this olr is not running as root"
		return []Step{step}
	}

	out, err := runPrivileged(ctx, argv)
	if err != nil {
		step.Error = strings.TrimSpace(err.Error())
		if tail := outputTail(out); tail != "" {
			step.Error += "\n" + tail
		}
		return []Step{step}
	}
	step.Done = true
	return []Step{step}
}

// geteuid is a variable so a test can be a box it is not running as.
var geteuid = os.Geteuid

// runPrivileged is a variable for the same reason newUnit is: a test must be
// able to answer for a package manager this box does not have, and must never
// actually run one.
var runPrivileged = runOutsideTheSandbox

// runOutsideTheSandbox runs argv as a transient systemd unit, or directly when
// there is no systemd to ask.
//
// The direct fallback is for the box that is not running olrd under the unit
// file — a developer build, a container — where there is no sandbox to escape
// and systemd-run would fail to reach a bus that is not there. On the boxes
// this feature exists for, systemd-run is present and is the path taken.
func runOutsideTheSandbox(ctx context.Context, argv []string) ([]byte, error) {
	// DEBIAN_FRONTEND unconditionally: it is meaningless everywhere except
	// Debian, and on Debian it is the difference between a package that
	// installs and one that stops to draw a dialog nobody is watching.
	const noninteractive = "DEBIAN_FRONTEND=noninteractive"

	// Resolved here rather than left to systemd-run's own $PATH lookup, for the
	// error message: "apt-get is not on this box" is a sentence an operator can
	// act on, and it is the honest answer when a distribution we recognised from
	// /etc/os-release turns out not to have the package manager that family is
	// supposed to use. LookTool also searches the sbin directories $PATH omits.
	if path := LookTool(argv[0]); path != "" {
		argv = append([]string{path}, argv[1:]...)
	} else {
		return nil, fmt.Errorf("%s is not installed on this box, so olr cannot install anything with it", argv[0])
	}

	if run := LookTool("systemd-run"); run != "" {
		args := []string{
			"--quiet",   // no "Running as unit ..." line mixed into the output
			"--wait",    // block until it finishes, and exit with its status
			"--pipe",    // give the unit our pipes, so we get its output
			"--collect", // unload the unit afterwards, even if it failed
			"--setenv=" + noninteractive,
			"--",
		}
		cmd := exec.CommandContext(ctx, run, append(args, argv...)...)
		return cmd.CombinedOutput()
	}

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), noninteractive)
	return cmd.CombinedOutput()
}

// outputTailLines is how much of a failed install is quoted back.
//
// Enough for apt's actual complaint and the line before it, and not so much
// that a failed `apt update` fills a toast with mirror URLs.
const outputTailLines = 12

// outputTail is the last few lines of a command's output, trimmed.
func outputTail(out []byte) string {
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	// Blank lines carry nothing and apt emits plenty.
	kept := lines[:0]
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			kept = append(kept, l)
		}
	}
	if len(kept) > outputTailLines {
		kept = kept[len(kept)-outputTailLines:]
	}
	return strings.Join(kept, "\n")
}

// installArgvFor is the command that installs pkg on this distribution, ready
// to run rather than ready to paste.
func installArgvFor(distro Distro, pkg string) ([]string, bool) {
	for _, family := range installOrder {
		if !distro.Is(family) {
			continue
		}
		if argv, known := installArgv[family]; known {
			out := make([]string, 0, len(argv)+1)
			out = append(out, argv...)
			return append(out, pkg), true
		}
	}
	return nil, false
}
