package cmd

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
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
		printWardHeader(w, "(not configured)")
		fmt.Fprintln(w, "  Not attached")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Run `warded new --commit` to create a new ward.")
		return
	}

	printWardHeader(w, statusWardLabel(out))

	// Primary: user access entry point.
	if out.Runtime.Domain != "" {
		fmt.Fprintf(w, "  Entry point: https://%s\n", out.Runtime.Domain)
	} else if out.Runtime.RequestedDomain != "" {
		fmt.Fprintf(w, "  Entry point: https://%s\n", out.Runtime.RequestedDomain)
	} else {
		fmt.Fprintln(w, "  Entry point: (not yet assigned)")
	}

	// Status or Setup
	isDraft := out.Runtime.WardID == "" && out.Runtime.WardDraftID != ""
	if isDraft {
		// Draft phase: show Setup status
		if out.WardDraft != nil {
			fmt.Fprintf(w, "  Setup:       %s\n", humanStatus(out.WardDraft.Status))
		} else {
			fmt.Fprintf(w, "  Setup:       %s\n", humanStatus(string(out.Runtime.WardStatus)))
		}
	} else {
		// Active ward: show Status with cached marker if needed
		status := string(out.Runtime.WardStatus)
		if status == "" {
			status = "unknown"
		}
		if out.RefreshError != nil && out.Runtime.LastRefreshedAt.IsZero() {
			// Never refreshed, using local state
			status = status + " (local)"
		} else if out.RefreshError != nil {
			// Had previous refresh but current failed
			status = status + " (cached)"
		}
		fmt.Fprintf(w, "  Status:      %s\n", humanStatus(status))
	}

	// Spec
	if out.Runtime.Spec != "" {
		fmt.Fprintf(w, "  Spec:        %s\n", out.Runtime.Spec)
	}

	// Activation Mode (only for active wards)
	if !isDraft && out.Runtime.ActivationMode != "" {
		fmt.Fprintf(w, "  Activation:  %s\n", out.Runtime.ActivationMode)
	}

	// Expires at
	if !out.Runtime.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "  Expires at:  %s\n", out.Runtime.ExpiresAt.Format(time.RFC3339))
	} else if out.WardDraft != nil && out.WardDraft.ExpiresAt != "" {
		fmt.Fprintf(w, "  Expires at:  %s\n", out.WardDraft.ExpiresAt)
	}

	// Site
	fmt.Fprintf(w, "  Site:        %s\n", out.Runtime.Site)

	// Listen Port
	listenPort := "443"
	if out.Runtime.ListenAddr != "" {
		listenPort = strings.TrimPrefix(out.Runtime.ListenAddr, ":")
	}
	fmt.Fprintf(w, "  Listen:      :%s\n", listenPort)

	// Upstream Port
	if out.Runtime.UpstreamPort > 0 {
		fmt.Fprintf(w, "  Upstream:    localhost:%d\n", out.Runtime.UpstreamPort)
	}

	// Billing Mode
	if out.Runtime.BillingMode != "" {
		fmt.Fprintf(w, "  Billing:     %s\n", out.Runtime.BillingMode)
	}

	// Refreshed time
	refreshedAt := out.LastRefreshedAt
	if refreshedAt.IsZero() && !out.Runtime.LastRefreshedAt.IsZero() {
		refreshedAt = out.Runtime.LastRefreshedAt
	}
	if !refreshedAt.IsZero() {
		if time.Since(refreshedAt) < time.Minute {
			fmt.Fprintln(w, "  Refreshed:   just now")
		} else {
			fmt.Fprintf(w, "  Refreshed:   %s\n", refreshedAt.Format(time.RFC3339))
		}
	}

	// Error/Warning/Next sections
	renderStatusFooter(w, out, isDraft)
}

func renderStatusFooter(w io.Writer, out *application.StatusOutput, isDraft bool) {
	// Handle refresh error (platform unreachable)
	if out.RefreshError != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Warning:")
		fmt.Fprintln(w, "  Could not refresh from platform. Showing local cached state.")
		return
	}

	// Draft phase next steps
	if isDraft {
		status := string(out.Runtime.WardStatus)
		if out.WardDraft != nil {
			status = out.WardDraft.Status
		}

		switch status {
		case "initializing", "pending_activation":
			fmt.Fprintln(w)
			if out.Runtime.ActivationURL != "" {
				fmt.Fprintf(w, "  Setup Link: %s\n", out.Runtime.ActivationURL)
			}
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Next:")
			fmt.Fprintln(w, "  Open the setup link to continue in the browser.")
			fmt.Fprintln(w, "  Before activation, you can still change settings with `warded new ...` and sync them with `warded new --commit`.")
		case "expired":
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Error:")
			fmt.Fprintln(w, "  This setup has expired. The setup link is no longer valid.")
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Next:")
			fmt.Fprintln(w, "  Run `warded new --commit` to create a new ward.")
		case "failed":
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Error:")
			fmt.Fprintln(w, "  This setup failed. This may be due to a network issue or validation failure.")
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Next:")
			fmt.Fprintln(w, "  Run `warded new --commit` to create a new ward.")
		}
		return
	}

	// Active ward next steps
	switch out.Runtime.WardStatus {
	case domain.WardStatusExpired:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Error:")
		fmt.Fprintln(w, "  This ward has expired.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next:")
		fmt.Fprintln(w, "  Run `warded new --commit` to create a new ward.")
	case domain.WardStatusSuspended:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Error:")
		fmt.Fprintln(w, "  This ward is suspended. This may be due to a billing issue or policy violation.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next:")
		if out.Runtime.Domain != "" {
			fmt.Fprintf(w, "  Visit https://%s/wards/%s to resolve the issue.\n", baseDomainForSite(out.Runtime.Site), out.Runtime.Domain)
		}
	case domain.WardStatusDeleted:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Error:")
		fmt.Fprintln(w, "  This ward has been deleted.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next:")
		fmt.Fprintln(w, "  Run `warded new --commit` to create a new ward.")
	}
}

func baseDomainForSite(site domain.Site) string {
	switch site {
	case domain.SiteCN:
		return "warded.cn"
	case domain.SiteGlobal:
		return "warded.me"
	default:
		return "warded.me"
	}
}

func statusWardLabel(out *application.StatusOutput) string {
	if out == nil || out.Runtime == nil {
		return "(not configured)"
	}
	if out.Runtime.Domain != "" {
		return out.Runtime.Domain
	}
	if out.Runtime.RequestedDomain != "" {
		return fmt.Sprintf("%s (pending)", out.Runtime.RequestedDomain)
	}
	return "(not configured)"
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
