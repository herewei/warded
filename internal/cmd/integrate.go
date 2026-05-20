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
		agent           string
		apply           bool
		baseline        bool
		repair          bool
		adoptPublicPort int
		dataDir         string
		openClawPath    string
		domain          string
	)

	command := &cobra.Command{
		Use:   "integrate",
		Short: "Inspect or repair local agent integration",
		Long: `Inspect or apply local agent integration patches.

For OpenClaw baseline repair, use:
  warded integrate --agent openclaw --baseline repair
  warded integrate --agent openclaw --baseline repair --adopt-public-port <port>

The --apply flag is deprecated for baseline mode; use --baseline repair instead.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)
			cli, err := application.NewOpenClawCLI(openClawPath)
			if err != nil {
				if wantsJSON(cmd) {
					writeJSONError(cmd, "integrate", "", err)
					return nil
				}
				return err
			}
			service := application.IntegrateService{
				ConfigStore: store,
				OpenClawCLI: cli,
			}

			out, err := service.Execute(cmd.Context(), application.IntegrateInput{
				Agent:           agent,
				OpenClawPath:    openClawPath,
				Domain:          domain,
				Baseline:        baseline,
				AdoptPublicPort: adoptPublicPort,
				Apply:           apply,
				Repair:          repair,
			})
			if err != nil {
				if wantsJSON(cmd) {
					if strings.Contains(err.Error(), "ward is not active") {
						writeJSON(cmd.OutOrStdout(), Envelope{
							OK:      true,
							Command: "integrate",
							Data: map[string]any{
								"agent":  agent,
								"status": "not_configured",
							},
						})
						return nil
					}
					writeJSONError(cmd, "integrate", "", err)
				}
				return err
			}
			if wantsJSON(cmd) {
				writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "integrate", Data: integrateOutputDTO(out)})
				return nil
			}
			renderIntegrateResult(cmd.OutOrStdout(), out)
			return nil
		},
	}

	command.Flags().StringVar(&agent, "agent", "", "target local agent integration, for example openclaw")
	command.Flags().BoolVar(&apply, "apply", false, "deprecated: use --baseline repair instead")
	command.Flags().BoolVar(&baseline, "baseline", false, "inspect or repair the OpenClaw security baseline instead of allowedOrigins")
	command.Flags().BoolVar(&repair, "repair", false, "apply the baseline repair via openclaw CLI")
	command.Flags().IntVar(&adoptPublicPort, "adopt-public-port", 0, "when used with --baseline repair, move OpenClaw off this currently public port and reserve it for Warded")
	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&openClawPath, "openclaw-path", "", "path to the openclaw binary (auto-detected from PATH if empty)")
	command.Flags().StringVar(&domain, "domain", "", "override the ward domain or origin used for integration")
	_ = command.MarkFlagRequired("agent")

	return command
}

func integrateOutputDTO(out *application.IntegrateOutput) map[string]any {
	if out == nil {
		return map[string]any{}
	}
	data := map[string]any{
		"agent":   out.Agent,
		"status":  out.Status,
		"updated": out.Updated,
	}
	if out.OpenClawPath != "" {
		data["openclaw_path"] = out.OpenClawPath
	}
	if out.Mode != "" {
		data["mode"] = out.Mode
	}
	if out.RequiredOrigin != "" {
		data["required_origin"] = out.RequiredOrigin
	}
	if out.Message != "" {
		data["message"] = out.Message
	}
	if out.RestartRequired {
		data["restart_required"] = true
	}
	return data
}

func renderIntegrateResult(w io.Writer, out *application.IntegrateOutput) {
	if out == nil {
		return
	}
	fmt.Fprintf(w, "Agent: %s\n", out.Agent)
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
	if out.Updated {
		fmt.Fprintf(w, "Updated: yes\n")
	}
	if out.RestartRequired {
		fmt.Fprintf(w, "Next: restart OpenClaw gateway, then rerun `warded doctor --agent openclaw --baseline` before continuing.\n")
	}
}

func safeCLIValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
