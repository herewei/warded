package cmd

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
)

type BuildInfo struct {
	Version   string
	BuildDate string
	GitCommit string
	GoVersion string
}

func NewRootCommand(logLevel *slog.LevelVar, info BuildInfo) *cobra.Command {
	var (
		verbose     bool
		showVersion bool
	)

	root := &cobra.Command{
		Use:          "warded",
		Short:        "Warded CLI",
		SilenceUsage: true,
		Run: func(cmd *cobra.Command, args []string) {
			if showVersion {
				if verbose {
					fmt.Fprintf(cmd.OutOrStdout(), "warded %s\n", info.Version)
					fmt.Fprintf(cmd.OutOrStdout(), "Build date: %s\n", info.BuildDate)
					fmt.Fprintf(cmd.OutOrStdout(), "Git commit: %s\n", info.GitCommit)
					fmt.Fprintf(cmd.OutOrStdout(), "Go version: %s\n", info.GoVersion)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "warded %s\n", info.Version)
				}
				return
			}
			_ = cmd.Help()
		},
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				logLevel.Set(slog.LevelDebug)
			}
		},
	}

	root.Flags().BoolVarP(&verbose, "verbose", "v", false, "enable detailed diagnostic logging to stderr (redacted)")
	root.Flags().BoolVarP(&showVersion, "version", "V", false, "print version information")

	root.AddCommand(
		newNewCommand(info.Version),
		newIntegrateCommand(),
		newServeCommand(info.Version),
		newStatusCommand(info.Version),
		newDoctorCommand(),
		newRenewCertCommand(info.Version),
	)

	return root
}
