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
		configDir  string
		configFile string
		domain     string
	)

	command := &cobra.Command{
		Use:   "integrate",
		Short: "Inspect or apply local agent integration patches",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(configDir)
			service := application.IntegrateService{
				ConfigStore: store,
			}

			out, err := service.Execute(cmd.Context(), application.IntegrateInput{
				Agent:      agent,
				ConfigFile: configFile,
				Domain:     domain,
				Apply:      apply,
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
	command.Flags().StringVar(&configDir, "config-dir", defaultConfigDir(), "local config directory")
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
	fmt.Fprintf(w, "Required origin: %s\n", out.RequiredOrigin)
	fmt.Fprintf(w, "Status: %s\n", out.Status)

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
