package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/open-linux-router/open-linux-router/internal/core"
	"github.com/spf13/cobra"
)

func backupCommand() *cobra.Command {
	return &cobra.Command{Use: "backup <file>", Short: "Back up complete olr.json, including stored credentials", GroupID: GroupOperations, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ValidateOutput(cmd); err != nil {
			return err
		}
		if DryRun(cmd) {
			fmt.Fprintln(cmd.OutOrStdout(), "Would save configuration backup to "+args[0])
			return nil
		}
		var raw json.RawMessage
		if err := NewClient(Socket(cmd)).Get(cmd.Context(), "/api/configuration/config", &raw); err != nil {
			return err
		}
		file, err := os.OpenFile(args[0], os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err != nil {
			return fmt.Errorf("creating backup (existing files are never overwritten): %w", err)
		}
		saved := false
		defer func() {
			if !saved {
				_ = file.Close()
				_ = os.Remove(args[0])
			}
		}()
		if _, err := file.Write(append(raw, '\n')); err != nil {
			return err
		}
		if err := file.Sync(); err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
		dir, err := os.Open(filepath.Dir(args[0]))
		if err != nil {
			return err
		}
		defer dir.Close()
		if err := dir.Sync(); err != nil {
			return err
		}
		saved = true
		if IsJSON(cmd) {
			return JSON(cmd.OutOrStdout(), map[string]string{"file": args[0]})
		}
		fmt.Fprintf(cmd.OutOrStdout(), "Saved %s (0600). Contains credentials; keep it private.\n", args[0])
		return nil
	}}
}

func restoreCommand() *cobra.Command {
	var confirm bool
	cmd := &cobra.Command{Use: "restore <file>", Short: "Validate and restore a complete olr.json backup", GroupID: GroupOperations, Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		if err := ValidateOutput(cmd); err != nil {
			return err
		}
		if !DryRun(cmd) && !confirm {
			return errors.New("restore replaces all settings and credentials; review with --dry-run, then use --confirm to apply")
		}
		file, err := os.Open(args[0])
		if err != nil {
			return err
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, core.MaxBodyBytes+1))
		if err != nil {
			return err
		}
		if len(data) > core.MaxBodyBytes {
			return errors.New("configuration backup exceeds 4 MiB")
		}
		if !json.Valid(data) {
			return errors.New("backup is not valid JSON")
		}
		query := "?confirm=true"
		if DryRun(cmd) {
			query = "?dry_run=true"
		}
		var response json.RawMessage
		if err := NewClient(Socket(cmd)).Post(cmd.Context(), "/api/configuration/restore"+query, json.RawMessage(data), &response); err != nil {
			return err
		}
		return JSON(cmd.OutOrStdout(), response)
	}}
	cmd.Flags().BoolVar(&confirm, "confirm", false, "replace all settings after reviewing the backup")
	return cmd
}
