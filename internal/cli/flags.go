package cli

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Global flag names, so modules ask for them by constant rather than by a
// string literal that could drift from the definition in root.go.
const (
	FlagSocket = "socket"
	FlagOutput = "output"
	FlagDryRun = "dry-run"
)

// Output formats.
const (
	OutputText = "text"
	OutputJSON = "json"
)

// Modules read the global flags through these helpers rather than through a
// shared options struct. Core is a library that modules call, not a framework
// that hands them a context (design.md §3.1) — a lookup by name keeps that
// direction and means a module needs no reference to the root command.

// Output returns the resolved --output format.
func Output(cmd *cobra.Command) string {
	return lookupString(cmd, FlagOutput, OutputText)
}

// Socket returns the resolved --socket path.
func Socket(cmd *cobra.Command) string {
	return lookupString(cmd, FlagSocket, DefaultSocket)
}

// DryRun reports whether --dry-run was given.
//
// Every mutating command honours it. It is what lets an agent propose a change
// and a human review the diff before it applies (design.md §6.4), and it is how
// `olr diff` asks a module what it would do.
func DryRun(cmd *cobra.Command) bool {
	if f := cmd.Flags().Lookup(FlagDryRun); f != nil {
		v, err := cmd.Flags().GetBool(FlagDryRun)
		if err == nil {
			return v
		}
	}
	return false
}

func lookupString(cmd *cobra.Command, name, fallback string) string {
	if f := cmd.Flags().Lookup(name); f != nil {
		if v, err := cmd.Flags().GetString(name); err == nil && v != "" {
			return v
		}
	}
	return fallback
}

// JSON writes v as indented JSON. Used by every module's -o json path, so the
// shape of machine-readable output is decided in one place.
func JSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// IsJSON is shorthand for the common branch.
func IsJSON(cmd *cobra.Command) bool { return Output(cmd) == OutputJSON }

// ValidateOutput rejects an unknown --output early, with the vocabulary in the
// message, rather than silently falling back to text.
func ValidateOutput(cmd *cobra.Command) error {
	switch Output(cmd) {
	case OutputText, OutputJSON:
		return nil
	default:
		return fmt.Errorf("unknown output format %q (want %s or %s)",
			Output(cmd), OutputText, OutputJSON)
	}
}
