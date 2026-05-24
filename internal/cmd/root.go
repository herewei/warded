package cmd

import (
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/spf13/cobra"
)

var cobraTemplateFuncsOnce sync.Once

var cmdDisplayOrder = map[string]int{
	"doctor":     0,
	"new":        1,
	"integrate":  2,
	"serve":      3,
	"status":     4,
	"whitelist":  5,
	"renew-cert": 6,
}

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
		format      string
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
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if format != "text" && format != "json" {
				return formatError(format)
			}
			if verbose {
				logLevel.Set(slog.LevelDebug)
			}
			return nil
		},
	}

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable detailed diagnostic logging to stderr (redacted)")
	root.PersistentFlags().StringVar(&format, "format", "text", "output format: text or json")
	root.Flags().BoolVarP(&showVersion, "version", "V", false, "print version information")
	root.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if format == "json" {
			writeJSONCommandError(cmd, err)
		}
		return err
	})

	root.AddGroup(
		&cobra.Group{ID: "1-diagnose", Title: "1. Diagnose:"},
		&cobra.Group{ID: "2-configure", Title: "2. Configure:"},
		&cobra.Group{ID: "3-run", Title: "3. Run:"},
		&cobra.Group{ID: "4-inspect", Title: "4. Inspect:"},
		&cobra.Group{ID: "5-maintain", Title: "5. Maintain:"},
	)

	cobraTemplateFuncsOnce.Do(func() {
		cobra.AddTemplateFunc("sortByOrder", func(cmds []*cobra.Command) []*cobra.Command {
			sorted := make([]*cobra.Command, len(cmds))
			copy(sorted, cmds)
			sort.Slice(sorted, func(i, j int) bool {
				oi, oki := cmdDisplayOrder[sorted[i].Name()]
				oj, okj := cmdDisplayOrder[sorted[j].Name()]
				if !oki {
					oi = 999
				}
				if !okj {
					oj = 999
				}
				return oi < oj
			})
			return sorted
		})
	})

	root.SetUsageTemplate(`Usage:{{if .Runnable}}
  {{.UseLine}}{{end}}{{if .HasAvailableSubCommands}}
  {{.CommandPath}} [command]{{end}}{{if gt (len .Aliases) 0}}

Aliases:
  {{.NameAndAliases}}{{end}}{{if .HasExample}}

Examples:
{{.Example}}{{end}}{{if .HasAvailableSubCommands}}{{$cmds := .Commands}}{{if eq (len .Groups) 0}}

Available Commands:{{range (sortByOrder $cmds)}}{{if (or .IsAvailableCommand (eq .Name "help"))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{else}}{{range $group := .Groups}}

{{.Title}}{{range (sortByOrder $cmds)}}{{if (and (eq .GroupID $group.ID) (or .IsAvailableCommand (eq .Name "help")))}}
  {{rpad .Name .NamePadding }} {{.Short}}{{end}}{{end}}{{end}}{{end}}{{end}}{{if .HasAvailableLocalFlags}}

Flags:
{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasAvailableInheritedFlags}}

Global Flags:
{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{if .HasHelpSubCommands}}

Additional help topics:{{range .Commands}}{{if .IsAdditionalHelpTopicCommand}}
  {{rpad .CommandPath .CommandPathPadding}} {{.Short}}{{end}}{{end}}{{end}}{{if .HasAvailableSubCommands}}

Use "{{.CommandPath}} [command] --help" for more information about a command.{{end}}
`)

	newCmd := newNewCommand(info.Version)
	newCmd.GroupID = "2-configure"

	integrateCmd := newIntegrateCommand()
	integrateCmd.GroupID = "2-configure"

	serveCmd := newServeCommand(info.Version)
	serveCmd.GroupID = "3-run"

	statusCmd := newStatusCommand(info.Version)
	statusCmd.GroupID = "4-inspect"

	doctorCmd := newDoctorCommand(info.Version)
	doctorCmd.GroupID = "1-diagnose"

	whitelistCmd := newWhitelistCommand()
	whitelistCmd.GroupID = "5-maintain"

	renewCmd := newRenewCertCommand(info.Version)
	renewCmd.GroupID = "5-maintain"

	root.AddCommand(doctorCmd, newCmd, integrateCmd, serveCmd, statusCmd, whitelistCmd, renewCmd)

	root.CompletionOptions.HiddenDefaultCmd = true

	root.SetHelpTemplate(helpTemplate())

	return root
}

func helpTemplate() string {
	return `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}

{{end}}{{if or .Runnable .HasSubCommands}}{{.UsageString}}{{end}}{{if eq .Name "warded"}}

Tip: First time? Run ` + "`warded doctor preflight`" + ` to verify this host is ready.{{end}}
`
}
