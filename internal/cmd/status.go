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
		fmt.Fprintln(w, "  No pending setup.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next:")
		fmt.Fprintln(w, "  Run `warded new --site global` to start a setup.")
		return
	}

	label := statusWardLabel(out)
	printWardHeader(w, label)

	fmt.Fprintf(w, "  Site:        %s\n", out.Runtime.Site)
	fmt.Fprintf(w, "  Spec:        %s\n", out.Runtime.Spec)

	isDraft := out.Runtime.WardID == "" && out.Runtime.WardDraftID != ""
	if isDraft {
		if out.WardDraft != nil {
			fmt.Fprintf(w, "  Setup:       %s\n", humanStatus(out.WardDraft.Status))
		} else {
			fmt.Fprintf(w, "  Setup:       %s\n", humanStatus(string(out.Runtime.WardStatus)))
		}
	} else {
		status := string(out.Runtime.WardStatus)
		if status == "" {
			status = "unknown"
		}
		if out.RefreshError != nil && out.Runtime.LastRefreshedAt.IsZero() {
			status = status + " (local)"
		} else if out.RefreshError != nil {
			status = status + " (cached)"
		}
		fmt.Fprintf(w, "  Status:      %s\n", humanStatus(status))
	}

	fmt.Fprintf(w, "  Listen:      %s\n", normalizeListenAddrForDisplay(out.Runtime.ListenAddr))
	fmt.Fprintf(w, "  Upstream:    %s\n", normalizeUpstreamAddrForDisplay(out.Runtime.UpstreamAddr))
	fmt.Fprintf(w, "  Billing:     %s\n", out.Runtime.BillingMode)

	if !isDraft && out.Runtime.ActivationMode != "" {
		fmt.Fprintf(w, "  Activation:  %s\n", out.Runtime.ActivationMode)
	}

	if !out.Runtime.ExpiresAt.IsZero() {
		fmt.Fprintf(w, "  Expires at:  %s\n", out.Runtime.ExpiresAt.Format(time.RFC3339))
	} else if out.WardDraft != nil && out.WardDraft.ExpiresAt != "" {
		fmt.Fprintf(w, "  Expires at:  %s\n", out.WardDraft.ExpiresAt)
	}

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

renderStatusFooter(w, out, isDraft)
}

func renderStatusFooter(w io.Writer, out *application.StatusOutput, isDraft bool) {
	if out.RefreshError != nil {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Warning:")
		fmt.Fprintln(w, "  Could not refresh from platform. Showing local cached state.")
		return
	}

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
			fmt.Fprintln(w, "  Run `warded new` to review or update the pending setup.")
			fmt.Fprintln(w, "  Run `warded new --commit` when the setup looks correct.")
		case "failed":
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Error:")
			fmt.Fprintln(w, "  This setup failed. This may be due to a network issue or validation failure.")
			fmt.Fprintln(w)
			fmt.Fprintln(w, "Next:")
			fmt.Fprintln(w, "  Run `warded new` to review or update the pending setup.")
			fmt.Fprintln(w, "  Run `warded new --commit` when the setup looks correct.")
		}
		return
	}

	switch out.Runtime.WardStatus {
	case domain.WardStatusExpired:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Error:")
		fmt.Fprintln(w, "  This ward has expired.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next:")
		fmt.Fprintln(w, "  Run `warded new` to review or update the pending setup.")
		fmt.Fprintln(w, "  Run `warded new --commit` when the setup looks correct.")
	case domain.WardStatusSuspended:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Error:")
		fmt.Fprintln(w, "  This ward is suspended. This may be due to a billing issue or policy violation.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next:")
		fmt.Fprintf(w, "  Visit https://%s to resolve the suspension.\n", baseDomainForSite(out.Runtime.Site))
	case domain.WardStatusDeleted:
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Error:")
		fmt.Fprintln(w, "  This ward has been deleted.")
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Next:")
		fmt.Fprintln(w, "  Run `warded new` to review or update the pending setup.")
		fmt.Fprintln(w, "  Run `warded new --commit` when the setup looks correct.")
	}
}

func statusWardLabel(out *application.StatusOutput) string {
	if out == nil || out.Runtime == nil {
		return "(not configured)"
	}
	if out.Runtime.Domain != "" {
		// For active wards, add status suffix for non-active states
		status := string(out.Runtime.WardStatus)
		switch status {
		case "active":
			return out.Runtime.Domain
		case "expired":
			return fmt.Sprintf("%s (expired)", out.Runtime.Domain)
		case "suspended":
			return fmt.Sprintf("%s (suspended)", out.Runtime.Domain)
		case "deleted":
			return fmt.Sprintf("%s (deleted)", out.Runtime.Domain)
		default:
			return out.Runtime.Domain
		}
	}
	if out.Runtime.RequestedDomain != "" {
		status := string(out.Runtime.WardStatus)
		if out.WardDraft != nil && out.WardDraft.Status != "" {
			status = out.WardDraft.Status
		}
		switch status {
		case "", "initializing", "pending_activation", "activating":
			return fmt.Sprintf("%s (pending)", out.Runtime.RequestedDomain)
		case "expired":
			return fmt.Sprintf("%s (expired)", out.Runtime.RequestedDomain)
		case "failed":
			return fmt.Sprintf("%s (failed)", out.Runtime.RequestedDomain)
		default:
			return out.Runtime.RequestedDomain
		}
	}
	return "(not configured)"
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
