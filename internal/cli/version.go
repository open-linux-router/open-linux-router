package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/open-linux-router/open-linux-router/internal/buildinfo"
)

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "version",
		Short:   "Print version information",
		GroupID: GroupLocal,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "olr %s (commit %s, built %s)\n",
				buildinfo.Version, buildinfo.Commit, buildinfo.Date)
			return err
		},
	}
}
