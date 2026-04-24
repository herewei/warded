package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newVersionCommand(info BuildInfo) *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print CLI version and build info",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "warded %s\n", info.Version)
			fmt.Fprintf(cmd.OutOrStdout(), "Build date: %s\n", info.BuildDate)
			fmt.Fprintf(cmd.OutOrStdout(), "Git commit: %s\n", info.GitCommit)
			fmt.Fprintf(cmd.OutOrStdout(), "Go version: %s\n", info.GoVersion)
		},
	}
}
