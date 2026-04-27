package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/spf13/cobra"
)

func newIntegrateCommand() *cobra.Command {
	var (
		agent      string
		apply      bool
		baseline   bool
		adoptPublicPort int
		dataDir    string
		configFile string
		domain     string
	)

	command := &cobra.Command{
		Use:   "integrate",
		Short: "Inspect or apply local agent integration patches",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)
			service := application.IntegrateService{
				ConfigStore: store,
			}

			out, err := service.Execute(cmd.Context(), application.IntegrateInput{
				Agent:           agent,
				ConfigFile:      configFile,
				Domain:          domain,
				Baseline:        baseline,
				AdoptPublicPort: adoptPublicPort,
				Apply:           apply,
			})
			if err != nil {
				return err
			}
			renderIntegrateResult(cmd.OutOrStdout(), out)
			return nil
		},
	}

	command.Flags().StringVar(&agent, "agent", "", "target local agent integration, for example openclaw")
	command.Flags().BoolVar(&apply, "apply", false, "apply the integration patch to the target config file")
	command.Flags().BoolVar(&baseline, "baseline", false, "inspect or repair the OpenClaw security baseline instead of allowedOrigins")
	command.Flags().IntVar(&adoptPublicPort, "adopt-public-port", 0, "when used with --baseline, move OpenClaw off this currently public port and reserve it for Warded")
	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&configFile, "config-file", "", "override the target agent config file path")
	command.Flags().StringVar(&domain, "domain", "", "override the ward domain or origin used for integration")
	_ = command.MarkFlagRequired("agent")

	return command
}

func renderIntegrateResult(w io.Writer, out *application.IntegrateOutput) {
	if out == nil {
		return
	}
	fmt.Fprintf(w, "Agent: %s\n", out.Agent)
	fmt.Fprintf(w, "Config file: %s\n", out.ConfigFile)
	if out.Mode != "" {
		fmt.Fprintf(w, "Mode: %s\n", out.Mode)
	}
	if out.RequiredOrigin != "" {
		fmt.Fprintf(w, "Required origin: %s\n", out.RequiredOrigin)
	}
	fmt.Fprintf(w, "Status: %s\n", out.Status)
	if out.CurrentBind != "" || out.CurrentPort > 0 {
		fmt.Fprintf(w, "Current bind/port: %s / %d\n", safeCLIValue(out.CurrentBind, "(unset)"), out.CurrentPort)
	}
	if out.DesiredBind != "" || out.DesiredPort > 0 {
		fmt.Fprintf(w, "Desired bind/port: %s / %d\n", safeCLIValue(out.DesiredBind, "(unset)"), out.DesiredPort)
	}

	if len(out.CurrentAllowed) > 0 {
		fmt.Fprintf(w, "Current allowedOrigins: %s\n", strings.Join(out.CurrentAllowed, ", "))
	}
	if len(out.DesiredAllowed) > 0 {
		fmt.Fprintf(w, "Desired allowedOrigins: %s\n", strings.Join(out.DesiredAllowed, ", "))
	}
	if out.Message != "" {
		fmt.Fprintf(w, "Message: %s\n", out.Message)
	}
	if out.SuggestedPatch != "" {
		fmt.Fprintf(w, "\nSuggested patch:\n%s\n", out.SuggestedPatch)
	}
	if out.BackupFile != "" {
		fmt.Fprintf(w, "Backup file: %s\n", out.BackupFile)
	}
	if out.Updated {
		fmt.Fprintf(w, "Updated: yes\n")
	}
}

func safeCLIValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
