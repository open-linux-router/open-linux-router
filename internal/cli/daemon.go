package cli

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/core"
)

// DaemonUnit is olrd's own systemd unit — not to be confused with a module's
// backend unit (design.md §4.2: "daemon" is olrd, "backend" is what it drives).
const DaemonUnit = "olrd.service"

// daemonTimeout bounds a job. Long enough for a slow box, short enough that a
// wedged systemd reports rather than hanging the terminal — which is the exact
// situation these commands exist for.
const daemonTimeout = 30 * time.Second

// serviceCommands are the commands that manage olrd itself.
//
// This is the one place the CLI is not an API client. `olr start` cannot be an
// HTTP call to the thing it is starting, and `olr status` has to answer
// truthfully when olrd is wedged. These talk to systemd directly, and
// design.md §6.1 calls that the lower of the CLI's two tiers.
//
// They sit at the top level rather than under an `olr daemon` group, and the
// tier survives the move: it is marked by the `Service:` heading in `olr
// --help` instead of by a word in the command path. What the group bought was
// never the boundary — it was the word "daemon" in front of an operator who
// has one program installed and reasonably thinks of it as one program.
func serviceCommands() []*cobra.Command {
	return []*cobra.Command{
		daemonJob("start", "Start olr", "started", "",
			func(ctx context.Context, u core.Unit) error { return u.Start(ctx) }),

		// The note is not decoration. design.md §3.5 guarantees that stopping
		// the control plane never touches the data plane, so this command does
		// markedly less than "stop" sounds like it does: leases keep being
		// handed out and names keep resolving. Under `olr daemon stop` the
		// scope was at least visible in the command. At the top level the only
		// place left to say it is here.
		daemonJob("stop", "Stop olr", "stopped",
			"The network keeps running — DHCP is still handing out leases and DNS is\n"+
				"still resolving. Stopping the control plane never interrupts them.\n\n"+
				"  olr dhcp disable    stop handing out leases\n"+
				"  olr dns disable     give port 53 back\n",
			func(ctx context.Context, u core.Unit) error { return u.Stop(ctx) }),

		daemonJob("restart", "Restart olr", "restarted", "",
			func(ctx context.Context, u core.Unit) error { return u.Restart(ctx) }),

		daemonListenCommand(),
	}
}

// EnvPath is the file olrd's unit reads its arguments from.
const EnvPath = "/etc/open-linux-router/olrd.env"

// envKey is the variable inside it that the unit expands.
const envKey = "OLRD_ARGS"

// daemonListenCommand opens or closes the web UI's listener.
//
// This is the one piece of configuration that cannot go through the API,
// because it is what makes the API reachable. An operator who has just
// installed the package has a working `olr` over the control socket and no way
// to open a browser at the box; the alternative to this command is telling them
// to edit a systemd drop-in, which is both more to get wrong and the kind of
// instruction that ends up pasted into a forum post with the wrong path in it.
//
// Off by default is deliberate and stays that way (design.md §7): installing a
// router's control plane must not put an admin port on the LAN uninvited. What
// this removes is the difficulty of saying yes, not the requirement to.
func daemonListenCommand() *cobra.Command {
	var off bool

	c := &cobra.Command{
		Use:     "listen <address>",
		Short:   "Serve the web UI on a network address",
		GroupID: GroupService,
		Long: "Open the web UI on an address, by writing " + EnvPath + " and\n" +
			"restarting the service.\n\n" +
			"olr always serves its control socket, which is what the command line\n" +
			"talks to. It listens on the network only when told to, so this is the\n" +
			"step that makes the web UI reachable from another machine.\n\n" +
			"Anything that is not a loopback address requires a token, generated on\n" +
			"first start into " + core.TokenPath + ". The UI asks for it on\n" +
			"first use.\n\n" +
			"Examples:\n" +
			"  olr listen 0.0.0.0:8080   reachable from your network\n" +
			"  olr listen 127.0.0.1:8080 this box only, for an ssh tunnel\n" +
			"  olr listen --off          stop listening on the network",
		Args: func(c *cobra.Command, args []string) error {
			// The address is required unless --off, which is a shape cobra has
			// no built-in validator for. Spelled out rather than declared as
			// MaximumNArgs(1), so that a bare `olr listen` says what is
			// missing instead of failing later with an empty address.
			if off {
				return cobra.NoArgs(c, args)
			}
			return cobra.ExactArgs(1)(c, args)
		},
		RunE: func(c *cobra.Command, args []string) error {
			if err := RejectDryRun(c); err != nil {
				return err
			}

			var address string
			if !off {
				address = args[0]
				if err := checkListenAddress(address); err != nil {
					return err
				}
			}

			if err := writeListenEnv(address); err != nil {
				return err
			}

			unit, ctx, cancel, err := daemonUnit(c)
			if err != nil {
				return err
			}
			defer cancel()
			if err := unit.Restart(ctx); err != nil {
				return fmt.Errorf("wrote %s but could not restart %s: %w", EnvPath, DaemonUnit, err)
			}

			return reportListening(c.OutOrStdout(), address)
		},
	}

	c.Flags().BoolVar(&off, "off", false, "stop listening on the network")
	return c
}

// checkListenAddress rejects what olrd would reject, before restarting it.
//
// Restarting into a bad address would leave the daemon crash-looping, and the
// operator's next command would fail against a socket that is no longer there —
// which reads as "olr broke" rather than "that address was wrong".
func checkListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%q is not an address:port, e.g. 0.0.0.0:8080", address)
	}
	if port == "" {
		return fmt.Errorf("%q has no port; the web UI needs one, e.g. %s:8080", address, host)
	}
	if _, err := strconv.ParseUint(port, 10, 16); err != nil {
		return fmt.Errorf("%q is not a port number", port)
	}
	if host != "" && net.ParseIP(host) == nil {
		return fmt.Errorf("%q is not an IP address; use 0.0.0.0 for every interface", host)
	}
	return nil
}

// writeListenEnv rewrites the OLRD_ARGS line, leaving every other line alone.
//
// Rewriting the one line rather than the file keeps the comments — which are
// where the token and the --off spelling are documented — and keeps any other
// argument an operator added. A file that gets truncated by the tool meant to
// configure it is a bad trade for three lines of parsing.
func writeListenEnv(address string) error {
	value := ""
	if address != "" {
		value = "--listen " + address
	}
	line := envKey + "=" + value

	existing, err := os.ReadFile(EnvPath)
	switch {
	case os.IsNotExist(err):
		// No file: the tarball install, or a hand-built box. Write a minimal
		// one rather than refusing — the unit reads it with a leading `-`, so
		// creating it is enough.
		return writeEnvFile(line + "\n")
	case err != nil:
		return fmt.Errorf("reading %s: %w", EnvPath, err)
	}

	out := make([]string, 0, 8)
	replaced := false
	for _, existing := range strings.Split(strings.TrimRight(string(existing), "\n"), "\n") {
		trimmed := strings.TrimSpace(existing)
		// Commented-out examples are left as examples. Only a live assignment
		// is the setting.
		if !strings.HasPrefix(trimmed, envKey+"=") {
			out = append(out, existing)
			continue
		}
		if !replaced {
			out = append(out, line)
			replaced = true
		}
	}
	if !replaced {
		out = append(out, line)
	}
	return writeEnvFile(strings.Join(out, "\n") + "\n")
}

func writeEnvFile(body string) error {
	if err := os.MkdirAll(filepath.Dir(EnvPath), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(EnvPath), err)
	}
	if err := core.WriteFileAtomic(EnvPath, []byte(body), 0o644); err != nil {
		return fmt.Errorf("writing %s: %w", EnvPath, err)
	}
	return nil
}

// reportListening says what to do next, including where the token is.
//
// The token is the step people get stuck on: the UI loads, asks for something,
// and the answer is in a file nobody mentioned. Saying it here, at the moment
// the URL is printed, is the cheapest place to close that gap.
func reportListening(w io.Writer, address string) error {
	if address == "" {
		_, err := fmt.Fprintf(w,
			"%s is no longer listening on the network.\n"+
				"`olr` still works here over %s.\n",
			DaemonUnit, DefaultSocket)
		return err
	}

	host, port, _ := net.SplitHostPort(address)
	shown := host
	if host == "" || host == "0.0.0.0" || host == "::" {
		// The address it is bound to is not an address anybody can type.
		shown = "<this box>"
	}

	if _, err := fmt.Fprintf(w, "%s is listening on %s.\nOpen http://%s:%s\n",
		DaemonUnit, address, shown, port); err != nil {
		return err
	}
	if core.IsLoopback(address) {
		_, err := fmt.Fprintf(w,
			"\nThis is a loopback address, so no token is needed — but it is only\n"+
				"reachable from this box. From elsewhere:\n"+
				"  ssh -L %s:%s <this box>\n", port, address)
		return err
	}
	_, err := fmt.Fprintf(w,
		"\nIt will ask for an API token. Read it with:\n  sudo cat %s\n", core.TokenPath)
	return err
}

// daemonJob builds one lifecycle command. note, when non-empty, is printed
// after the result to say what the command did *not* do.
func daemonJob(use, short, done, note string, run func(context.Context, core.Unit) error) *cobra.Command {
	return &cobra.Command{
		Use:     use,
		Short:   short,
		GroupID: GroupService,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := RejectDryRun(cmd); err != nil {
				return err
			}
			unit, ctx, cancel, err := daemonUnit(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			if err := run(ctx, unit); err != nil {
				return err
			}
			// core.Unit waits for systemd's job result rather than returning
			// once the job is queued, so saying it is done is honest.
			fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", DaemonUnit, done)
			if note != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "\n%s", note)
			}
			return nil
		},
	}
}

// statusCommand answers "is olr running", and is the top-level `olr status`.
//
// It was two commands until the `daemon` group was flattened: a stub `olr
// status` in operations.go promising "aggregate drift and daemon liveness
// across modules", and a working `olr daemon status` that reported the
// liveness half. Merging them keeps the working half and drops the promise,
// rather than the other way round.
//
// The drift half is still per-module — `olr dhcp status` and its siblings —
// and aggregating it here is design.md §6.1's eventual shape. When that
// arrives it extends this command; it does not need a different one.
//
// GroupOperations, not GroupService: it fans out across modules, which is what
// that group means. It lives in this file because what it can answer today is
// a systemd query, and because it must keep answering when olrd is wedged —
// the same reason the lifecycle commands are not API clients.
func statusCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "status",
		Short:   "Report whether olr is running",
		Long: "Report whether the olr service is running.\n\n" +
			"Configuration drift is reported per module for now:\n" +
			"  olr dhcp status\n  olr dns status\n  olr gateway status",
		GroupID: GroupOperations,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ReadOnly(cmd); err != nil {
				return err
			}
			unit, ctx, cancel, err := daemonUnit(cmd)
			if err != nil {
				return err
			}
			defer cancel()

			status, err := unit.Status(ctx)
			if err != nil {
				// "We could not ask" and "it is not running" are different
				// answers, and this command exists for the moments when the
				// difference matters. Never flatten one into the other.
				return err
			}

			if IsJSON(cmd) {
				return JSON(cmd.OutOrStdout(), status)
			}
			return writeUnitStatus(cmd.OutOrStdout(), status)
		},
	}
}

func daemonUnit(cmd *cobra.Command) (core.Unit, context.Context, context.CancelFunc, error) {
	unit, err := core.NewUnit(DaemonUnit)
	if err != nil {
		return nil, nil, nil, err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), daemonTimeout)
	return unit, ctx, cancel, nil
}

func writeUnitStatus(w io.Writer, s core.UnitStatus) error {
	state := "stopped"
	if s.Active {
		state = "running"
	}

	line := fmt.Sprintf("%s is %s", s.Unit, state)
	if s.State != "" {
		line += " (" + s.State
		if s.SubState != "" {
			line += "/" + s.SubState
		}
		line += ")"
	}
	if _, err := fmt.Fprintln(w, line); err != nil {
		return err
	}

	if s.MainPID != 0 {
		if _, err := fmt.Fprintf(w, "  %-8s %d\n", "pid", s.MainPID); err != nil {
			return err
		}
	}
	if !s.Since.IsZero() {
		if _, err := fmt.Fprintf(w, "  %-8s %s\n", "since",
			s.Since.Local().Format(time.RFC1123)); err != nil {
			return err
		}
	}
	boot := "no"
	if s.Enabled {
		boot = "yes"
	}
	_, err := fmt.Fprintf(w, "  %-8s %s\n", "at boot", boot)
	return err
}
