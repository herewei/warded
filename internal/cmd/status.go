package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/spf13/cobra"
)

func newStatusCommand(version string) *cobra.Command {
	var dataDir string
	var baseDomain string
	var platformOrigin string
	var local bool
	var wardID string
	var draftID string
	var domainFlag string

	command := &cobra.Command{
		Use:   "status [index]",
		Short: "Show current ward and runtime status",
		Args:  jsonArgs(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)
			service := application.StatusService{ConfigStore: store}

			listOut, err := service.ListRuntimes(cmd.Context())
			if err != nil {
				return fmt.Errorf("status: %w", err)
			}
			runtimes := listOut.Runtimes

			hasSelector := len(args) > 0 || wardID != "" || draftID != "" || domainFlag != ""

			if !hasSelector {
				switch len(runtimes) {
				case 0:
					if wantsJSON(cmd) {
						writeJSON(cmd.OutOrStdout(), Envelope{
							OK:      true,
							Command: "status",
							Data:    map[string]any{"configured": false},
							NextSteps: []NextStep{{
								Kind:    "command",
								Command: "warded",
								Args:    []string{"new", "--site", "global"},
							}},
						})
						return nil
					}
					renderStatusOutput(cmd.OutOrStdout(), nil)
					return nil
				case 1:
					return runStatusForTarget(cmd, store, &runtimes[0], local, baseDomain, platformOrigin, version)
				default:
					if wantsJSON(cmd) {
						writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "status", Data: map[string]any{"runtimes": runtimeListDTO(runtimes)}})
						return nil
					}
					renderRuntimeList(cmd.OutOrStdout(), runtimes, dataDir)
					return nil
				}
			}

			matched, resolveErr := resolveStatusTarget(runtimes, args, wardID, draftID, domainFlag)
			if resolveErr != nil {
				if wantsJSON(cmd) {
					writeJSONError(cmd, "status", "", resolveErr)
					return resolveErr
				}
				fmt.Fprintln(cmd.OutOrStdout())
				renderRuntimeList(cmd.OutOrStdout(), runtimes, dataDir)
				cmd.SilenceUsage = true
				return resolveErr
			}
			return runStatusForTarget(cmd, store, matched, local, baseDomain, platformOrigin, version)
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().BoolVar(&local, "local", false, "show local config only without calling the platform API")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example dev.warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")
	command.Flags().StringVar(&wardID, "ward-id", "", "select a specific ward by its ID")
	command.Flags().StringVar(&draftID, "draft-id", "", "select a specific ward draft by its ID")
	command.Flags().StringVar(&domainFlag, "domain", "", "select a ward by domain or requested domain")

	_ = command.Flags().MarkHidden("platform-origin")

	return command
}

// resolveStatusTarget finds the single matching runtime from the list.
// Returns an error if 0 or more than 1 runtimes match.
func resolveStatusTarget(runtimes []application.RuntimeSummary, args []string, wardID, draftID, domain string) (*application.RuntimeSummary, error) {
	if len(args) > 0 {
		idx, err := strconv.Atoi(args[0])
		if err != nil || idx < 1 || idx > len(runtimes) {
			return nil, fmt.Errorf("invalid index %q: must be between 1 and %d", args[0], len(runtimes))
		}
		rt := runtimes[idx-1]
		return &rt, nil
	}
	if wardID != "" {
		for _, rt := range runtimes {
			if rt.Runtime.WardID == wardID {
				return &rt, nil
			}
		}
		return nil, fmt.Errorf("no ward found with --ward-id %q", wardID)
	}
	if draftID != "" {
		for _, rt := range runtimes {
			if rt.Runtime.WardDraftID == draftID {
				return &rt, nil
			}
		}
		return nil, fmt.Errorf("no draft found with --draft-id %q", draftID)
	}
	if domain != "" {
		var matches []application.RuntimeSummary
		for _, rt := range runtimes {
			if rt.Runtime.Domain == domain || rt.Runtime.RequestedDomain == domain {
				matches = append(matches, rt)
			}
		}
		switch len(matches) {
		case 0:
			return nil, fmt.Errorf("no ward found with --domain %q", domain)
		case 1:
			return &matches[0], nil
		default:
			return nil, fmt.Errorf("--domain %q matches multiple runtimes, use --ward-id or --draft-id", domain)
		}
	}
	return nil, fmt.Errorf("no selector specified")
}

// runStatusForTarget runs the detail view for a single resolved runtime.
func runStatusForTarget(cmd *cobra.Command, store ports.LocalConfigStore, rt *application.RuntimeSummary, local bool, baseDomain, platformOrigin, version string) error {
	// pending-config has no IDs: render locally, no platform refresh.
	if rt.Kind == application.RuntimeKindPendingConfig {
		out := &application.StatusOutput{Runtime: &rt.Runtime}
		if wantsJSON(cmd) {
			writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "status", Data: statusOutputDTO(out)})
		} else {
			renderStatusOutput(cmd.OutOrStdout(), out)
		}
		return nil
	}

	// Prime wardDir so SaveWardRuntime renames the directory correctly if a
	// draft is claimed and promoted to a ward during Execute.
	id := rt.Runtime.WardID
	if id == "" {
		id = rt.Runtime.WardDraftID
	}
	if _, err := store.LoadRuntimeByID(cmd.Context(), id); err != nil {
		return fmt.Errorf("status: load runtime: %w", err)
	}

	service := application.StatusService{ConfigStore: store}
	if !local {
		url, err := resolvePlatformOrigin(rt.Runtime.Site, baseDomain, platformOrigin)
		if err != nil {
			return fmt.Errorf("status: %w", err)
		}
		client := platformapi.NewClient(url, version)
		service.DraftAPI = client
		service.RuntimeAPI = client
	}

	out, err := service.Execute(cmd.Context())
	if err != nil {
		if wantsJSON(cmd) {
			writeJSONError(cmd, "status", "", err)
		}
		return err
	}
	if wantsJSON(cmd) {
		writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "status", Data: statusOutputDTO(out)})
		return nil
	}
	renderStatusOutput(cmd.OutOrStdout(), out)
	return nil
}

func runtimeListDTO(runtimes []application.RuntimeSummary) []map[string]any {
	out := make([]map[string]any, 0, len(runtimes))
	for _, rt := range runtimes {
		out = append(out, map[string]any{
			"index":   rt.Index,
			"kind":    rt.Kind,
			"domain":  firstNonEmpty(rt.Runtime.Domain, rt.Runtime.RequestedDomain),
			"status":  runtimeListStatus(rt),
			"id":      runtimeListID(rt),
			"ward_id": runtimeListID(rt),
		})
	}
	return out
}

func statusOutputDTO(out *application.StatusOutput) map[string]any {
	if out == nil || out.Runtime == nil {
		return map[string]any{"configured": false}
	}
	rt := out.Runtime
	data := map[string]any{
		"configured":    true,
		"site":          rt.Site,
		"spec":          rt.Spec,
		"status":        rt.WardStatus,
		"listen":        formatListenForDisplay(rt),
		"upstream":      normalizeUpstreamAddrForDisplay(rt.UpstreamAddr),
		"upstream_mode": upstreamModeOrDefault(rt),
		"billing":       rt.BillingMode,
	}
	if rt.Domain != "" {
		data["domain"] = rt.Domain
	}
	if rt.RequestedDomain != "" {
		data["requested_domain"] = rt.RequestedDomain
	}
	if rt.ActivationURL != "" {
		data["setup_link"] = rt.ActivationURL
	}
	if !out.LastRefreshedAt.IsZero() {
		data["last_refreshed_at"] = out.LastRefreshedAt.Format(time.RFC3339)
	}
	return data
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func renderRuntimeList(w io.Writer, runtimes []application.RuntimeSummary, dataDir string) {
	fmt.Fprintf(w, "Multiple local wards found under %s\n\n", dataDir)
	fmt.Fprintf(w, "  %-4s  %-16s  %-26s  %-15s  %s\n", "#", "Kind", "Domain", "Status", "ID")
	for _, rt := range runtimes {
		dom := rt.Runtime.Domain
		if dom == "" {
			dom = rt.Runtime.RequestedDomain
		}
		if dom == "" {
			dom = "(no domain)"
		}
		status := runtimeListStatus(rt)
		id := runtimeListID(rt)
		fmt.Fprintf(w, "  %-4d  %-16s  %-26s  %-15s  %s\n", rt.Index, string(rt.Kind), dom, status, id)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next:")
	fmt.Fprintln(w, "  Run `warded status <index>` to inspect one ward.")
	fmt.Fprintln(w, "  Use `warded status --ward-id <id>`, `--draft-id <id>`, or `--domain <domain>` for stable selection.")
}

func runtimeListStatus(rt application.RuntimeSummary) string {
	if rt.Kind == application.RuntimeKindPendingConfig {
		return "not submitted"
	}
	s := string(rt.Runtime.WardStatus)
	if s == "" {
		return "unknown"
	}
	return strings.ReplaceAll(s, "_", " ")
}

func runtimeListID(rt application.RuntimeSummary) string {
	if rt.Runtime.WardID != "" {
		return rt.Runtime.WardID
	}
	if rt.Runtime.WardDraftID != "" {
		return rt.Runtime.WardDraftID
	}
	return "-"
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

	fmt.Fprintf(w, "  Listen:      %s\n", formatListenForDisplay(out.Runtime))
	fmt.Fprintf(w, "  Upstream:    %s\n", normalizeUpstreamAddrForDisplay(out.Runtime.UpstreamAddr))
	fmt.Fprintf(w, "  Mode:        %s\n", upstreamModeOrDefault(out.Runtime))
	if out.Runtime.UpstreamCommand != "" {
		fmt.Fprintf(w, "  Command:     %s\n", out.Runtime.UpstreamCommand)
	}
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

func upstreamModeOrDefault(rt *domain.LocalWardRuntime) string {
	if rt == nil || rt.UpstreamMode == "" {
		return string(domain.UpstreamModeDaemon)
	}
	return string(rt.UpstreamMode)
}
