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
		GroupID: GroupOther,
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := RejectDryRun(cmd); err != nil {
				return err
			}
			_, err := fmt.Fprintf(cmd.OutOrStdout(), "olr %s\n", buildinfo.String())
			return err
		},
	}
}
