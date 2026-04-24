package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/spf13/cobra"
)

func newStatusCommand(version string) *cobra.Command {
	var dataDir string
	var baseDomain string
	var platformOrigin string
	var local bool

	command := &cobra.Command{
		Use:   "status",
		Short: "Show current ward and runtime status",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)
			service := application.StatusService{
				ConfigStore: store,
			}

			if !local {
				// Derive platform URL from the persisted site; skip if not yet initialised.
				runtime, err := store.LoadWardRuntime(cmd.Context())
				if err != nil {
					return fmt.Errorf("status: load runtime: %w", err)
				}
				if runtime != nil {
					url, err := resolvePlatformOrigin(runtime.Site, baseDomain, platformOrigin)
					if err != nil {
						return fmt.Errorf("status: %w", err)
					}
					service.PlatformAPI = platformapi.NewClient(url, version)
				}
			}

			out, err := service.Execute(cmd.Context())
			if err != nil {
				return err
			}
			renderStatusOutput(cmd.OutOrStdout(), out)
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().BoolVar(&local, "local", false, "show local config only without calling the platform API")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example dev.warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")

	// Hide development/testing flags from help output
	_ = command.Flags().MarkHidden("platform-origin")

	return command
}

func renderStatusOutput(w io.Writer, out *application.StatusOutput) {
	if out == nil || out.Runtime == nil {
		fmt.Fprintln(w, "Ward Status:")
		fmt.Fprintln(w, "  Not attached")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Run `warded new --commit` to create a new ward.")
		return
	}

	fmt.Fprintln(w, "Ward Status:")

	// Primary: user access entry point.
	if out.Runtime.Domain != "" {
		fmt.Fprintf(w, "  Entry point: https://%s\n", out.Runtime.Domain)
	} else if out.Runtime.RequestedDomain != "" {
		fmt.Fprintf(w, "  Entry point: https://%s (pending)\n", out.Runtime.RequestedDomain)
	} else {
		fmt.Fprintln(w, "  Entry point: (not yet assigned)")
	}

	if out.WardDraft != nil {
		fmt.Fprintf(w, "  Setup:       %s\n", humanStatus(out.WardDraft.Status))
		if out.WardDraft.ExpiresAt != "" {
			fmt.Fprintf(w, "  Expires at:  %s\n", out.WardDraft.ExpiresAt)
		}
	} else {
		status := out.Runtime.WardStatus
		if status == "" {
			status = "unknown"
		}
		fmt.Fprintf(w, "  Status:      %s\n", humanStatus(string(status)))
	}

	// Site
	fmt.Fprintf(w, "  Site:        %s\n", out.Runtime.Site)

	// Upstream Port
	if out.Runtime.UpstreamPort > 0 {
		fmt.Fprintf(w, "  Upstream:    localhost:%d\n", out.Runtime.UpstreamPort)
	}

	// Billing Mode
	if out.Runtime.BillingMode != "" {
		fmt.Fprintf(w, "  Billing:     %s\n", out.Runtime.BillingMode)
	}

	// Activation Mode
	if out.Runtime.ActivationMode != "" {
		fmt.Fprintf(w, "  Activation:  %s\n", out.Runtime.ActivationMode)
	}

	// Activation URL
	if out.Runtime.ActivationURL != "" {
		fmt.Fprintf(w, "\n  Setup Link: %s\n", out.Runtime.ActivationURL)
	}
}

func humanStatus(status string) string {
	status = strings.TrimSpace(status)
	if status == "" {
		return "unknown"
	}
	switch status {
	case "pending_activation":
		return "pending activation"
	case "converted_pending_claim":
		return "ready to finish"
	default:
		return strings.ReplaceAll(status, "_", " ")
	}
}
