package cmd

import (
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
	var verbose bool

	root := &cobra.Command{
		Use:          "warded",
		Short:        "Warded CLI",
		Version:      info.Version,
		SilenceUsage: true,
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			if verbose {
				logLevel.Set(slog.LevelDebug)
			}
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable detailed diagnostic logging to stderr (redacted)")

	root.AddCommand(
		newNewCommand(info.Version),
		newIntegrateCommand(),
		newServeCommand(info.Version),
		newStatusCommand(info.Version),
		newDoctorCommand(),
		newRenewCertCommand(info.Version),
		newVersionCommand(info),
	)

	return root
}
