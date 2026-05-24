package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/ports"
	"github.com/spf13/cobra"
)

func newIntegrateCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "integrate",
		Short: "Inspect or repair local agent integration",
		Args:  jsonArgs(cobra.NoArgs),
	}
	command.AddCommand(newIntegrateAgentCommand())
	return command
}

func newIntegrateAgentCommand() *cobra.Command {
	command := &cobra.Command{
		Use:   "agent",
		Short: "Integrate supported local agent profiles",
		Args:  jsonArgs(cobra.NoArgs),
	}
	command.AddCommand(newIntegrateAgentOpenClawCommand())
	return command
}

func newIntegrateAgentOpenClawCommand() *cobra.Command {
	var (
		allowOrigins    bool
		baseline        bool
		repair          bool
		adoptPublicPort int
		dataDir         string
		openClawPath    string
		domain          string
		wardID          string
	)
	command := &cobra.Command{
		Use:   "openclaw",
		Short: "Inspect or repair OpenClaw integration",
		Args:  jsonArgs(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			if allowOrigins == baseline {
				err := fmt.Errorf("integrate agent openclaw: choose exactly one of --allow-origins or --baseline")
				if wantsJSON(cmd) {
					writeJSONError(cmd, "integrate", "", err)
				}
				return err
			}
			if baseline && !repair {
				err := fmt.Errorf("integrate agent openclaw: --repair is required with --baseline")
				if wantsJSON(cmd) {
					writeJSONError(cmd, "integrate", "agent_baseline_repair", err)
				}
				return err
			}

			store := storage.NewJSONStore(dataDir)
			if allowOrigins {
				if err := resolveIntegrateRuntimeTarget(cmd, store, wardID); err != nil {
					return err
				}
			}

			cli, err := application.NewOpenClawCLI(openClawPath)
			if err != nil {
				if wantsJSON(cmd) {
					writeJSONError(cmd, "integrate", integrateMode(allowOrigins, baseline), err)
				}
				return err
			}
			service := application.IntegrateService{
				ConfigStore: store,
				OpenClawCLI: cli,
			}
			out, err := service.Execute(cmd.Context(), application.IntegrateInput{
				Agent:           "openclaw",
				OpenClawPath:    openClawPath,
				Domain:          domain,
				Baseline:        baseline,
				AdoptPublicPort: adoptPublicPort,
				Apply:           allowOrigins,
				Repair:          repair,
			})
			mode := integrateMode(allowOrigins, baseline)
			if err != nil {
				if wantsJSON(cmd) {
					writeJSONError(cmd, "integrate", mode, err)
				}
				return err
			}
			if wantsJSON(cmd) {
				writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "integrate", Mode: mode, Data: integrateOutputDTO(out)})
				return nil
			}
			renderIntegrateResult(cmd.OutOrStdout(), out)
			return nil
		},
	}
	command.Flags().BoolVar(&allowOrigins, "allow-origins", false, "ensure OpenClaw allowedOrigins includes the selected ward origin")
	command.Flags().BoolVar(&baseline, "baseline", false, "repair the OpenClaw security baseline")
	command.Flags().BoolVar(&repair, "repair", false, "apply the baseline repair via openclaw CLI")
	command.Flags().IntVar(&adoptPublicPort, "adopt-public-port", 0, "when used with --baseline --repair, move OpenClaw off this currently public port and reserve it for Warded")
	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&openClawPath, "openclaw-path", "", "path to the openclaw binary (auto-detected from PATH if empty)")
	command.Flags().StringVar(&domain, "domain", "", "override the ward domain or origin used for integration")
	command.Flags().StringVar(&wardID, "ward-id", "", "select a specific ward by its ID when multiple local wards exist")
	return command
}

func integrateMode(allowOrigins, baseline bool) string {
	switch {
	case allowOrigins:
		return "agent_allow_origins"
	case baseline:
		return "agent_baseline_repair"
	default:
		return ""
	}
}

func resolveIntegrateRuntimeTarget(cmd *cobra.Command, store ports.LocalConfigStore, wardID string) error {
	listOut, err := application.StatusService{ConfigStore: store}.ListRuntimes(cmd.Context())
	if err != nil {
		return fmt.Errorf("integrate: list runtimes: %w", err)
	}
	var committed []application.RuntimeSummary
	for _, rt := range listOut.Runtimes {
		if rt.Kind != application.RuntimeKindPendingConfig {
			committed = append(committed, rt)
		}
	}
	if wardID == "" {
		switch len(committed) {
		case 0:
			return fmt.Errorf("integrate: no committed ward runtime found")
		case 1:
			_, err := store.LoadRuntimeByID(cmd.Context(), runtimeListID(committed[0]))
			if err != nil {
				return fmt.Errorf("integrate: load runtime: %w", err)
			}
			return nil
		default:
			err := fmt.Errorf("integrate: multiple local wards found, use --ward-id <id> to select one")
			if wantsJSON(cmd) {
				writeJSON(cmd.OutOrStdout(), Envelope{
					OK:      false,
					Command: "integrate",
					Mode:    "agent_allow_origins",
					Error:   classifyError(err),
					Data:    map[string]any{"runtimes": runtimeListDTO(committed)},
				})
				suppressCobraError(cmd)
			} else {
				renderRuntimeList(cmd.OutOrStdout(), committed, "")
			}
			return err
		}
	}
	matched, resolveErr := resolveStatusTarget(committed, nil, wardID, "", "")
	if resolveErr != nil {
		if wantsJSON(cmd) {
			writeJSONError(cmd, "integrate", "agent_allow_origins", resolveErr)
		}
		return resolveErr
	}
	if _, err := store.LoadRuntimeByID(cmd.Context(), runtimeListID(*matched)); err != nil {
		return fmt.Errorf("integrate: load runtime: %w", err)
	}
	return nil
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
		fmt.Fprintf(w, "Next: restart OpenClaw gateway, then rerun `warded doctor agent openclaw --baseline` before continuing.\n")
	}
}

func safeCLIValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
