package cmd

import (
	"log/slog"

	"github.com/spf13/cobra"
)

func NewRootCommand(logLevel *slog.LevelVar, version string) *cobra.Command {
	var verbose bool

	root := &cobra.Command{
		Use:          "warded",
		Short:        "Warded CLI",
		Version:      version,
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				logLevel.Set(slog.LevelDebug)
			}
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable detailed diagnostic logging to stderr (redacted)")

	root.AddCommand(
		newActivateCommand(version),
		newIntegrateCommand(),
		newServeCommand(version),
		newStatusCommand(version),
		newDoctorCommand(),
		newRenewCertCommand(version),
		newVersionCommand(version),
	)

	return root
}
