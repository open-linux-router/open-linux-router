// Command olr is open-linux-router: the operator CLI, and — behind the hidden
// `internal` subcommands — the control plane and the DNS relay as well.
//
// One binary carrying three roles is a packaging decision, not an architectural
// one. design.md §3.5 asks that anything which must keep running while olrd is
// stopped get its own *process* and its own unit, and two units invoking one
// binary under two subcommands satisfy that exactly as two binaries did. What
// collapsed is the file count: `systemctl restart olrd` still cannot disturb
// the relay, because they are still two processes with two sandboxes.
package main

import (
	"fmt"
	"os"

	"github.com/open-linux-router/open-linux-router/internal/cli"
	"github.com/open-linux-router/open-linux-router/internal/daemon"
	"github.com/open-linux-router/open-linux-router/internal/dnsd"
)

func main() {
	if code, handled := dispatchInternal(os.Args[1:]); handled {
		os.Exit(code)
	}
	if err := newRoot().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "olr:", err)
		os.Exit(1)
	}
}

// dispatchInternal routes the process entry points systemd starts, before cobra
// is given the arguments at all.
//
// Deliberately not cobra commands. They are not an operator surface: they take
// the daemons' own flag sets, they are typed by a unit file rather than by a
// person, and mounting them on the tree would hold them to the conformance
// rules in conformance_test.go — which describe how *operator* commands read,
// and which every command on that tree is meant to satisfy. Keeping the
// dispatch here means an entry point cannot appear in `olr --help`, cannot be
// offered by completion, and cannot drift into looking like something somebody
// ought to run.
func dispatchInternal(args []string) (code int, handled bool) {
	if len(args) == 0 || args[0] != "internal" {
		return 0, false
	}
	switch rest := args[1:]; {
	case len(rest) == 0:
		fmt.Fprintln(os.Stderr, internalUsage)
		return 2, true
	case rest[0] == "daemon":
		return daemon.Main(rest[1:]), true
	case rest[0] == "dns-relay":
		return dnsd.Main(rest[1:]), true
	case rest[0] == "urls":
		// Not a process, unlike its two neighbours, and here anyway: the .deb's
		// postinstall has to print the URLs the web UI answers on, and the
		// alternative is a shell pipeline parsing ip(8) in a maintainer script.
		// `olr enable` already works this out for the tarball path, so putting
		// it here is what keeps one answer to "which URLs reach this box"
		// rather than two that drift. Hidden for the same reason as the others:
		// there is no reason for an operator to type it.
		return cli.PrintWebURLs(os.Stdout), true
	default:
		fmt.Fprintf(os.Stderr, "olr internal: no entry point named %q\n\n%s\n", rest[0], internalUsage)
		return 2, true
	}
}

const internalUsage = `olr internal runs one of the processes systemd supervises. A unit file
starts these; there is no reason to type one:

  olr internal daemon      the control plane          (olrd.service)
  olr internal dns-relay   the DNS relay on :53       (olr-dnsd.service)
  olr internal urls        the web UI's addresses     (the .deb's postinstall)

To manage the services themselves, use olr start, olr stop or olr status.`
