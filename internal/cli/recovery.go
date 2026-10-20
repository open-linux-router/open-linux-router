package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/spf13/cobra"
)

func reportPendingRecovery(cmd *cobra.Command, _ []string) {
	if strings.HasPrefix(cmd.CommandPath(), "olr recovery") || DryRun(cmd) {
		return
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 2*time.Second)
	defer cancel()
	var v core.RecoveryView
	if err := NewClient(Socket(cmd)).Get(ctx, "/api/recovery/status", &v); err != nil || v.State == "idle" {
		return
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Connectivity change %s: verify network access, then run olr recovery confirm %s before %s; otherwise previous settings return automatically.\n", v.State, v.ID, v.Deadline.Format("15:04:05 MST"))
}

func recoveryCommand() *cobra.Command {
	group := &cobra.Command{Use: "recovery", Short: "Confirm or revert a pending connectivity change", GroupID: GroupOperations}
	status := &cobra.Command{Use: "status", Short: "Show the connectivity confirmation deadline", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		var v core.RecoveryView
		if err := NewClient(Socket(cmd)).Get(cmd.Context(), "/api/recovery/status", &v); err != nil {
			return err
		}
		if IsJSON(cmd) {
			return JSON(cmd.OutOrStdout(), v)
		}
		if v.State == "idle" {
			fmt.Fprintln(cmd.OutOrStdout(), "No connectivity change is pending.")
			return nil
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\nConfirm before %s with: olr recovery confirm %s\n", v.State, v.ID, v.Deadline.Format("15:04:05 MST"), v.ID)
		if v.Error != "" {
			fmt.Fprintln(cmd.OutOrStdout(), v.Error)
		}
		return nil
	}}
	group.AddCommand(status)
	for _, name := range []string{"confirm", "revert"} {
		group.AddCommand(&cobra.Command{Use: name + " <id>", Short: map[string]string{"confirm": "Keep a connectivity change after checking access", "revert": "Restore the configuration before a connectivity change"}[name], Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
			if DryRun(cmd) {
				fmt.Fprintln(cmd.OutOrStdout(), "Would "+cmd.Name()+" connectivity change "+args[0])
				return nil
			}
			var v core.RecoveryView
			if err := NewClient(Socket(cmd)).Post(cmd.Context(), "/api/recovery/"+cmd.Name(), map[string]string{"id": args[0]}, &v); err != nil {
				return err
			}
			return JSON(cmd.OutOrStdout(), v)
		}})
	}
	return group
}
