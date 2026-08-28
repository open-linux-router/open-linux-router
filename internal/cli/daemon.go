package cli

import (
	"context"
	"fmt"
	"io"
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

// daemonCommand groups the commands that manage olrd itself.
//
// This is the one place the CLI is not an API client. `olr daemon start` cannot
// be an HTTP call to the thing it is starting, and `olr daemon status` has to
// answer truthfully when olrd is wedged. These talk to systemd directly.
func daemonCommand() *cobra.Command {
	c := &cobra.Command{
		Use:     "daemon",
		Short:   "Manage the olrd service itself",
		Long:    "Commands that manage olrd. Unlike the rest of olr, these do not go\nthrough olrd's API — they work when it is stopped or unresponsive.",
		GroupID: GroupLocal,
	}

	c.AddCommand(
		daemonJob("start", "Start olrd", "started",
			func(ctx context.Context, u core.Unit) error { return u.Start(ctx) }),
		daemonJob("stop", "Stop olrd", "stopped",
			func(ctx context.Context, u core.Unit) error { return u.Stop(ctx) }),
		daemonJob("restart", "Restart olrd", "restarted",
			func(ctx context.Context, u core.Unit) error { return u.Restart(ctx) }),
		daemonStatusCommand(),
	)

	return c
}

func daemonJob(use, short, done string, run func(context.Context, core.Unit) error) *cobra.Command {
	return &cobra.Command{
		Use:   use,
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
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
			return nil
		},
	}
}

func daemonStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Report whether olrd is running",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := ValidateOutput(cmd); err != nil {
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
