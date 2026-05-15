package cmd

import (
	"fmt"
	"io"

	"github.com/herewei/warded/internal/adapters/servemon"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/spf13/cobra"
)

func newDoctorCommand() *cobra.Command {
	var dataDir string
	var agent string
	var baseline bool

	command := &cobra.Command{
		Use:   "doctor",
		Short: "Run interactive diagnostics for the current node",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)
			serveMon := servemon.ServeMonitor{}
			if !baseline {
				runtime, err := store.LoadWardRuntime(cmd.Context())
				if err != nil {
					return err
				}
				if runtime != nil {
					serveMon.FallbackPort = runtime.ListenPort
					serveMon.FallbackFamily = runtime.IngressFamily
				}
			}
			service := application.DoctorService{
				ConfigStore:     store,
				ServeMonitor:    serveMon,
				ServeTLSMonitor: serveMon,
			}
			out, err := service.Execute(cmd.Context(), application.DoctorInput{
				Agent:    agent,
				Baseline: baseline,
			})
			if err != nil {
				return err
			}

			printWardHeader(cmd.OutOrStdout(), doctorWardLabel(out))

			if baseline {
				renderBaselineDoctor(cmd.OutOrStdout(), out)
			} else {
				renderLegacyDoctor(cmd.OutOrStdout(), out)
			}
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&agent, "agent", "", "target agent for diagnosis (e.g. openclaw)")
	command.Flags().BoolVar(&baseline, "baseline", false, "run OpenClaw security baseline diagnosis")

	return command
}

func renderLegacyDoctor(w io.Writer, out *application.DoctorOutput) {
	for _, result := range out.Results {
		status := "FAIL"
		if result.State == application.CheckOK {
			status = "OK"
		}
		fmt.Fprintf(w, "  [%-4s]  %-20s  %s\n", status, result.Name, result.Detail)
	}
	fmt.Fprintln(w)
}

func renderBaselineDoctor(w io.Writer, out *application.DoctorOutput) {
	passed := 0
	total := 0

	for _, r := range out.Results {
		state := string(r.State)
		fmt.Fprintf(w, "  %d. [%-4s] %s\n", r.Number, state, r.Name)
		fmt.Fprintf(w, "     %s\n", r.Detail)
		if r.State == application.CheckOK {
			passed++
			total++
		} else if r.State == application.CheckFAIL {
			total++
		}
	}

	if total == 0 {
		fmt.Fprintf(w, "\n  Summary: no actionable checks\n")
	} else {
		fmt.Fprintf(w, "\n  Summary: %d/%d checks passed\n", passed, total)
	}
	if out.BaselineOK {
		fmt.Fprintf(w, "  Baseline: acceptable\n")
	} else {
		fmt.Fprintf(w, "  Baseline: needs repair\n")
	}

	if len(out.NextSteps) > 0 {
		fmt.Fprintf(w, "\n  Next steps:\n")
		for _, step := range out.NextSteps {
			fmt.Fprintf(w, "    - %s\n", step)
		}
	}
	fmt.Fprintln(w)
}

func doctorWardLabel(out *application.DoctorOutput) string {
	if out == nil || out.Runtime == nil {
		return "(not configured)"
	}
	runtime := out.Runtime
	if runtime.Domain != "" {
		// For active wards, add status suffix for non-active states
		status := string(runtime.WardStatus)
		switch status {
		case "active":
			return runtime.Domain
		case "expired":
			return fmt.Sprintf("%s (expired)", runtime.Domain)
		case "suspended":
			return fmt.Sprintf("%s (suspended)", runtime.Domain)
		case "deleted":
			return fmt.Sprintf("%s (deleted)", runtime.Domain)
		default:
			return runtime.Domain
		}
	}
	if runtime.RequestedDomain != "" && runtime.WardID == "" {
		status := string(runtime.WardStatus)
		switch status {
		case "", "initializing", "pending_activation", "activating":
			return fmt.Sprintf("%s (pending)", runtime.RequestedDomain)
		case "expired":
			return fmt.Sprintf("%s (expired)", runtime.RequestedDomain)
		case "failed":
			return fmt.Sprintf("%s (failed)", runtime.RequestedDomain)
		default:
			return runtime.RequestedDomain
		}
	}
	if runtime.WardStatus == domain.WardStatusInitializing || runtime.WardDraftID != "" {
		return "(pending setup)"
	}
	return "(not configured)"
}
