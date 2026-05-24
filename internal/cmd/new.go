package cmd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/adapters/upstream"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/herewei/warded/internal/sitepolicy"
	"github.com/spf13/cobra"
)

func effectiveNewSpec(existing *domain.LocalWardRuntime, cmd *cobra.Command, spec string) domain.Spec {
	if cmd.Flags().Changed("spec") {
		return domain.Spec(spec)
	}
	if existing != nil && existing.Spec != "" {
		return existing.Spec
	}
	return domain.Spec(spec)
}

func effectiveNewDomainType(existing *domain.LocalWardRuntime, cmd *cobra.Command, domainType string) domain.DomainType {
	if cmd.Flags().Changed("domain-type") {
		return domain.DomainType(domainType)
	}
	if existing != nil && existing.DomainType != "" {
		return existing.DomainType
	}
	return domain.DomainType(domainType)
}

func effectiveRequestedDomain(existing *domain.LocalWardRuntime, cmd *cobra.Command, requestedDomain string) string {
	if cmd.Flags().Changed("domain") {
		return requestedDomain
	}
	if existing != nil {
		return existing.RequestedDomain
	}
	return requestedDomain
}

func effectiveUpstreamMode(existing *domain.LocalWardRuntime, cmd *cobra.Command, upstreamMode string) domain.UpstreamMode {
	if cmd.Flags().Changed("upstream-mode") {
		return domain.UpstreamMode(upstreamMode)
	}
	if existing != nil && existing.UpstreamMode != "" {
		return existing.UpstreamMode
	}
	return domain.UpstreamMode(upstreamMode)
}

func effectiveUpstreamCommand(existing *domain.LocalWardRuntime, cmd *cobra.Command, upstreamCommand string, mode domain.UpstreamMode) string {
	if cmd.Flags().Changed("upstream-command") {
		return upstreamCommand
	}
	if cmd.Flags().Changed("upstream-mode") && mode == domain.UpstreamModeDaemon {
		return ""
	}
	if existing != nil {
		return existing.UpstreamCommand
	}
	return upstreamCommand
}

func validateFullDomainForCLI(site domain.Site, domainType domain.DomainType, requestedDomain string) error {
	value := strings.TrimSpace(strings.ToLower(requestedDomain))
	if value == "" {
		return nil
	}
	if strings.Contains(value, "://") || strings.Contains(value, "/") {
		return fmt.Errorf("--domain must be a full domain without scheme or path")
	}
	parts := strings.Split(value, ".")
	if len(parts) < 2 {
		return fmt.Errorf("--domain must be a full domain (e.g., myrobot.warded.me or robot.example.com)")
	}
	for _, part := range parts {
		if part == "" {
			return fmt.Errorf("--domain must be a valid full domain")
		}
	}
	policy := sitepolicy.ForSite(site)
	for _, suffix := range policy.AllowedBaseDomains() {
		if strings.HasSuffix(value, "."+suffix) || value == suffix {
			if domainType == domain.DomainTypeCustomDomain {
				return fmt.Errorf("--domain %s is a platform-managed domain; use --domain-type=platform_subdomain instead, or provide your own domain for custom_domain", requestedDomain)
			}
			return nil
		}
	}
	if domainType == domain.DomainTypePlatformSubdomain {
		return fmt.Errorf("--domain %s is not an allowed platform domain for site %s", requestedDomain, site)
	}
	return nil
}

func newNewCommand(version string) *cobra.Command {
	var (
		site            string
		spec            string
		billingMode     string
		domainType      string
		requestedDomain string
		upstreamAddr    string
		upstreamMode    string
		upstreamCommand string
		listenPort      int
		listenHost      string
		listenV6Host    string
		dataDir         string
		baseDomain      string
		platformOrigin  string
		commit          bool
		show            bool
		managedMgr      *upstream.ProcessManager
	)

	command := &cobra.Command{
		Use:   "new",
		Short: "Prepare or submit a new ward setup",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Helper to return validation errors with JSON envelope support
			validationErr := func(err error) error {
				err = invalidArgumentError(err)
				if wantsJSON(cmd) {
					writeJSONError(cmd, "new", "", err)
				}
				return err
			}

			if show && commit {
				return validationErr(fmt.Errorf("--show cannot be combined with --commit"))
			}
			// Only validate flags that are explicitly set
			// --site validation is deferred to RunE for show mode
			if site != "" && site != string(domain.SiteCN) && site != string(domain.SiteGlobal) {
				return validationErr(fmt.Errorf("invalid --site: %s (must be cn or global)", site))
			}
			if spec != string(domain.SpecStarter) && spec != string(domain.SpecPro) {
				return validationErr(fmt.Errorf("invalid --spec: %s (must be starter or pro)", spec))
			}
			if billingMode != string(domain.BillingModeMonthly) && billingMode != string(domain.BillingModeYearly) {
				return validationErr(fmt.Errorf("invalid --billing-mode: %s (must be monthly or yearly)", billingMode))
			}
			if domainType != string(domain.DomainTypePlatformSubdomain) && domainType != string(domain.DomainTypeCustomDomain) {
				return validationErr(fmt.Errorf("invalid --domain-type: %s (must be platform_subdomain or custom_domain)", domainType))
			}
			if upstreamMode != "" && upstreamMode != string(domain.UpstreamModeDaemon) && upstreamMode != string(domain.UpstreamModeManaged) {
				return validationErr(fmt.Errorf("invalid --upstream-mode: %s (must be daemon or managed)", upstreamMode))
			}

			// Load existing runtime for validation
			store := storage.NewJSONStore(dataDir)
			existingRuntime, err := store.LoadPendingRuntime(cmd.Context())
			if err != nil {
				return validationErr(fmt.Errorf("new: load pending runtime: %w", err))
			}

			// Resolve effective values for validation
			effectiveSite := site
			if effectiveSite == "" && existingRuntime != nil && existingRuntime.Site != "" {
				effectiveSite = string(existingRuntime.Site)
			}
			effectiveSpec := effectiveNewSpec(existingRuntime, cmd, spec)
			effectiveDomainType := effectiveNewDomainType(existingRuntime, cmd, domainType)
			effectiveDomain := effectiveRequestedDomain(existingRuntime, cmd, requestedDomain)
			effectiveUpstreamMode := effectiveUpstreamMode(existingRuntime, cmd, upstreamMode)
			effectiveUpstreamCommand := effectiveUpstreamCommand(existingRuntime, cmd, upstreamCommand, effectiveUpstreamMode)

			// Validate spec/domain_type combination
			if effectiveSpec == domain.SpecStarter && effectiveDomainType == domain.DomainTypeCustomDomain {
				return validationErr(fmt.Errorf("starter spec only supports platform_subdomain"))
			}
			if cmd.Flags().Changed("domain") && requestedDomain != "" && effectiveSpec == domain.SpecStarter {
				return validationErr(fmt.Errorf("starter spec does not support --domain (platform assigns subdomain automatically)"))
			}
			if effectiveSpec == domain.SpecPro && effectiveDomain != "" && effectiveSite != "" {
				if err := validateFullDomainForCLI(domain.Site(effectiveSite), effectiveDomainType, effectiveDomain); err != nil {
					return validationErr(err)
				}
			}

			// Validate upstream mode constraints
			if effectiveUpstreamMode == domain.UpstreamModeManaged {
				if strings.TrimSpace(effectiveUpstreamCommand) == "" {
					return validationErr(fmt.Errorf("--upstream-command is required when --upstream-mode is managed"))
				}
			}
			if effectiveUpstreamMode == domain.UpstreamModeDaemon && strings.TrimSpace(effectiveUpstreamCommand) != "" {
				return validationErr(fmt.Errorf("--upstream-command must not be set when --upstream-mode is daemon"))
			}

			effectiveUpstreamAddr := resolveUpstreamAddr(existingRuntime, upstreamAddr, cmd.Flags().Changed("upstream"))
			if effectiveUpstreamAddr != "" {
				if err := validateUpstreamAddrHost(effectiveUpstreamAddr); err != nil {
					return validationErr(err)
				}
			}

			// Preflight checks only for --commit mode
			if commit {
				if effectiveSite == "" {
					return validationErr(fmt.Errorf("--site is required: must be cn (warded.cn) or global (warded.me)"))
				}
				if effectiveSpec == domain.SpecPro && effectiveDomain == "" {
					return validationErr(fmt.Errorf("pro spec requires --domain (full domain, e.g., myrobot.warded.me or robot.example.com)"))
				}

				if err := ensureDataDirWritable(dataDir); err != nil {
					return validationErr(err)
				}
				effectiveListenHost, effectiveListenPort, effectiveIngressFamily, err := resolveListenParams(existingRuntime, listenHost, listenV6Host, listenPort, cmd.Flags().Changed("listen"), cmd.Flags().Changed("listen-v6"), cmd.Flags().Changed("port"))
				if err != nil {
					return validationErr(err)
				}
				listenAddr := fmt.Sprintf("%s:%d", effectiveListenHost, effectiveListenPort)
				if effectiveIngressFamily == domain.IngressFamilyIPv6 {
					listenAddr = fmt.Sprintf("[%s]:%d", effectiveListenHost, effectiveListenPort)
				}
				if err := ensureAddrAvailable(listenAddr); err != nil {
					return validationErr(err)
				}
				effectiveUpstreamPort := extractPortFromAddr(effectiveUpstreamAddr)
				if effectiveUpstreamPort == 0 {
					return validationErr(fmt.Errorf("upstream address is not configured\n  Run `warded new` first to save and confirm the upstream address"))
				}
				if effectiveUpstreamMode == domain.UpstreamModeManaged {
					mgr := upstream.NewProcessManager()
					cmdCtx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
					defer cancel()
					_, err := mgr.EnsureRunning(cmdCtx, effectiveUpstreamAddr, effectiveUpstreamCommand)
					if err != nil {
						return validationErr(fmt.Errorf("failed to start managed upstream: %w", err))
					}
					managedMgr = mgr
				} else {
					checker := upstream.NewChecker()
					if err := checker.Check(cmd.Context(), effectiveUpstreamAddr); err != nil {
						return validationErr(fmt.Errorf("upstream %s is not reachable: %w", effectiveUpstreamAddr, err))
					}
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)

			wardRuntime, err := store.LoadPendingRuntime(cmd.Context())
			if err != nil {
				return fmt.Errorf("new: load pending runtime: %w", err)
			}

			hasFlags := cmd.Flags().Changed("site") ||
				cmd.Flags().Changed("spec") ||
				cmd.Flags().Changed("billing-mode") ||
				cmd.Flags().Changed("domain-type") ||
				cmd.Flags().Changed("domain") ||
				cmd.Flags().Changed("upstream") ||
				cmd.Flags().Changed("upstream-mode") ||
				cmd.Flags().Changed("upstream-command") ||
				cmd.Flags().Changed("listen") ||
				cmd.Flags().Changed("listen-v6") ||
				cmd.Flags().Changed("port")

			if show && hasFlags {
				return fmt.Errorf("--show cannot be combined with configuration flags")
			}

			// Show mode: display pending config without requiring --site
			if show || (!commit && !hasFlags) {
				if wardRuntime == nil {
					if wantsJSON(cmd) {
						writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "new", Data: map[string]any{"pending": false}})
						return nil
					}
					renderNoPendingSetup(cmd.OutOrStdout())
					return nil
				}
				if wardRuntime.WardDraftID != "" && wardRuntime.WardDraftSecret != "" {
					if wantsJSON(cmd) {
						writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "new", Data: pendingRuntimeDTO(wardRuntime)})
						return nil
					}
					renderPendingDraftExists(cmd.OutOrStdout(), wardRuntime)
					return nil
				}
				if wantsJSON(cmd) {
					writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "new", Data: pendingRuntimeDTO(wardRuntime)})
					return nil
				}
				renderPendingShow(cmd.OutOrStdout(), wardRuntime)
				return nil
			}

			// Resolve site from existing runtime or require it for operations with flags
			effectiveSite := site
			if effectiveSite == "" {
				if wardRuntime != nil && wardRuntime.Site != "" {
					effectiveSite = string(wardRuntime.Site)
				} else {
					return fmt.Errorf("--site is required: must be cn (warded.cn) or global (warded.me)")
				}
			}

			existingDraftID := ""
			if wardRuntime != nil {
				existingDraftID = wardRuntime.WardDraftID
			}

			pendingRuntime, err := mergePendingRuntime(wardRuntime, pendingMergeInput{
				Site:                   domain.Site(effectiveSite),
				Spec:                   domain.Spec(spec),
				BillingMode:            domain.BillingMode(billingMode),
				DomainType:             domain.DomainType(domainType),
				RequestedDomain:        requestedDomain,
				UpstreamAddr:           upstreamAddr,
				UpstreamMode:           domain.UpstreamMode(upstreamMode),
				UpstreamCommand:        upstreamCommand,
				ListenPort:             listenPort,
				ListenHost:             listenHost,
				ListenV6Host:           listenV6Host,
				SiteChanged:            cmd.Flags().Changed("site"),
				SpecChanged:            cmd.Flags().Changed("spec"),
				BillingChanged:         cmd.Flags().Changed("billing-mode"),
				DomainChanged:          cmd.Flags().Changed("domain-type"),
				RequestChanged:         cmd.Flags().Changed("domain"),
				UpstreamChanged:        cmd.Flags().Changed("upstream"),
				UpstreamModeChanged:    cmd.Flags().Changed("upstream-mode"),
				UpstreamCommandChanged: cmd.Flags().Changed("upstream-command"),
				ListenChanged:          cmd.Flags().Changed("listen"),
				ListenV6Changed:        cmd.Flags().Changed("listen-v6"),
				PortChanged:            cmd.Flags().Changed("port"),
			})
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			if pendingRuntime.Spec == domain.SpecPro && pendingRuntime.RequestedDomain != "" {
				if err := validateFullDomainForCLI(pendingRuntime.Site, pendingRuntime.DomainType, pendingRuntime.RequestedDomain); err != nil {
					return fmt.Errorf("new: %w", err)
				}
			}

			if !commit {
				upstreamOk, err := runPendingFlagPrechecksAddr(cmd, pendingRuntime, dataDir, cmd.Flags().Changed("listen") || cmd.Flags().Changed("listen-v6") || cmd.Flags().Changed("port"))
				if err != nil {
					return fmt.Errorf("new: %w", err)
				}
				if err := store.SavePendingRuntime(cmd.Context(), *pendingRuntime); err != nil {
					return fmt.Errorf("new: save pending runtime: %w", err)
				}
				if wantsJSON(cmd) {
					data := pendingRuntimeDTO(pendingRuntime)
					data["upstream_reachable"] = upstreamOk
					writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "new", Data: data})
					return nil
				}
				renderPendingSaved(cmd.OutOrStdout(), pendingRuntime, upstreamOk)
				return nil
			}

			clearPlatformDraftState(pendingRuntime)

			if err := ensureDataDirWritable(dataDir); err != nil {
				return fmt.Errorf("new: %w", err)
			}
			if err := store.SavePendingRuntime(cmd.Context(), *pendingRuntime); err != nil {
				return fmt.Errorf("new: save pending runtime: %w", err)
			}
			listenAddr := listenAddrFromRuntime(pendingRuntime)
			if err := ensureAddrAvailable(listenAddr); err != nil {
				return fmt.Errorf("new: %w", err)
			}
			probeChallenge, err := randomProbeChallenge()
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			stopProbe, err := startTemporaryProbeServerAddr(cmd.Context(), listenAddr)
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = stopProbe(shutdownCtx)
			}()

			if pendingRuntime.UpstreamAddr == "" {
				return fmt.Errorf("upstream address is not configured\n  Run `warded new` first to save and confirm the upstream address")
			}

			// Cleanup any temporary managed upstream process started in PreRunE
			defer func() {
				if managedMgr != nil {
					_ = managedMgr.Shutdown(context.Background())
				}
			}()

			if !wantsJSON(cmd) {
				renderPendingCommitPreview(cmd.OutOrStdout(), pendingRuntime)
			}

			platformURL, err := resolvePlatformOrigin(pendingRuntime.Site, baseDomain, platformOrigin)
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			publicBaseURL, err := resolvePublicPlatformBaseURL(pendingRuntime.Site, baseDomain)
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			platformClient := platformapi.NewClient(platformURL, version)

			initService := application.NewService{
				ConfigStore: store,
				DraftAPI:    platformClient,
			}

			out, err := initService.Execute(cmd.Context(), application.NewInput{
				Site:            pendingRuntime.Site,
				Spec:            pendingRuntime.Spec,
				BillingMode:     pendingRuntime.BillingMode,
				DomainType:      pendingRuntime.DomainType,
				RequestedDomain: pendingRuntime.RequestedDomain,
				UpstreamAddr:    pendingRuntime.UpstreamAddr,
				UpstreamMode:    pendingRuntime.UpstreamMode,
				UpstreamCommand: pendingRuntime.UpstreamCommand,
				ListenPort:      pendingRuntime.ListenPort,
				ListenHost:      pendingRuntime.ListenHost,
				IngressFamily:   pendingRuntime.IngressFamily,
				ProbeChallenge:  probeChallenge,
				PublicBaseURL:   publicBaseURL,
			})
			if err != nil {
				if wantsJSON(cmd) {
					writeJSONError(cmd, "new", "commit", err)
					return err
				}
				return explainNewErrorAddr(err, pendingRuntime.DomainType, pendingRuntime.RequestedDomain, pendingRuntime.ListenPort)
			}
			if existingDraftID != "" && existingDraftID == out.WardDraftID {
				out.DraftAction = "updated"
			}

			if wantsJSON(cmd) {
				writeJSON(cmd.OutOrStdout(), Envelope{OK: true, Command: "new", Mode: "commit", Data: newOutputDTO(out, pendingRuntime)})
			} else {
				renderNewSetup(cmd.OutOrStdout(), out, pendingRuntime.DomainType, pendingRuntime.RequestedDomain)
			}

			return nil
		},
	}

	command.Flags().StringVar(&site, "site", "", "target site: cn (warded.cn) or global (warded.me)")
	command.Flags().StringVar(&spec, "spec", string(domain.SpecStarter), "ward spec: starter or pro")
	command.Flags().StringVar(&billingMode, "billing-mode", string(domain.BillingModeMonthly), "billing mode: monthly or yearly")
	command.Flags().StringVar(&domainType, "domain-type", string(domain.DomainTypePlatformSubdomain), "domain type: platform_subdomain (auto-assigned) or custom_domain (bring your own)")
	command.Flags().StringVar(&requestedDomain, "domain", "", "requested full domain (e.g., myrobot.warded.me or robot.example.com)")
	command.Flags().StringVar(&upstreamAddr, "upstream", "", "upstream address to protect (host:port); default 127.0.0.1:18789")
	command.Flags().StringVar(&upstreamMode, "upstream-mode", string(domain.UpstreamModeDaemon), "upstream mode: daemon (external process) or managed (warded starts it)")
	command.Flags().StringVar(&upstreamCommand, "upstream-command", "", "command to start the upstream server (required for managed mode)")
	command.Flags().IntVar(&listenPort, "port", 443, "listen port for warded serve")
	command.Flags().StringVar(&listenHost, "listen", "0.0.0.0", "IPv4 listen host for warded serve")
	command.Flags().StringVar(&listenV6Host, "listen-v6", "", "IPv6 listen host for warded serve (MVP single-stack: mutually exclusive with --listen)")
	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")
	command.Flags().BoolVar(&commit, "commit", false, "submit the pending configuration to the platform and create a draft")
	command.Flags().BoolVar(&show, "show", false, "show current pending setup without modifying")

	_ = command.Flags().MarkHidden("platform-origin")

	return command
}

func pendingRuntimeDTO(runtime *domain.LocalWardRuntime) map[string]any {
	if runtime == nil {
		return map[string]any{"pending": false}
	}
	data := map[string]any{
		"pending":          true,
		"site":             runtime.Site,
		"spec":             runtime.Spec,
		"billing":          runtime.BillingMode,
		"domain_type":      runtime.DomainType,
		"listen":           formatListenForDisplay(runtime),
		"upstream":         normalizeUpstreamAddrForDisplay(runtime.UpstreamAddr),
		"upstream_mode":    runtime.UpstreamMode,
		"upstream_command": runtime.UpstreamCommand,
	}
	if runtime.RequestedDomain != "" {
		data["requested_domain"] = runtime.RequestedDomain
	}
	if runtime.ActivationURL != "" {
		data["setup_link"] = runtime.ActivationURL
	}
	return data
}

func newOutputDTO(out *application.NewOutput, runtime *domain.LocalWardRuntime) map[string]any {
	data := pendingRuntimeDTO(runtime)
	data["pending"] = false
	if out == nil {
		return data
	}
	if out.ActivationURL != "" {
		data["setup_link"] = out.ActivationURL
	}
	if out.RequestedDomain != "" {
		data["requested_domain"] = out.RequestedDomain
	}
	if out.ResolvedPublicIP != "" {
		data["resolved_public_ip"] = out.ResolvedPublicIP
	}
	if out.IngressProbeStatus != "" {
		data["ingress_probe_status"] = out.IngressProbeStatus
	}
	if out.DomainCheckStatus != "" {
		data["domain_check_status"] = out.DomainCheckStatus
	}
	if out.Status != "" {
		data["status"] = out.Status
	}
	return data
}

func explainNewErrorAddr(err error, domainType domain.DomainType, requestedDomain string, listenPort int) error {
	if err == nil {
		return nil
	}
	var platformErr *ports.PlatformError
	if errors.As(err, &platformErr) {
		switch platformErr.Code {
		case "ingress_unreachable":
			return fmt.Errorf("inbound probe failed\n  Check port %d, firewall, and security group settings", listenPort)
		case "domain_dns_not_ready":
			return fmt.Errorf("DNS lookup failed for %s\n  No usable A record found. Add an A record pointing to your public IP, then re-run `warded new --commit`", requestedDomain)
		case "domain_public_ip_mismatch":
			return fmt.Errorf("DNS points to the wrong public IP for %s\n  Update the A record so it resolves to this machine's public IP, then re-run `warded new --commit`", requestedDomain)
		case "public_ip_unavailable":
			return fmt.Errorf("public IP is unavailable\n  Make sure this machine has a reachable public IPv4 address before running `warded new --commit`")
		case "domain_policy_violation":
			return fmt.Errorf("domain format is invalid\n  Use 3-63 lowercase letters, digits, and hyphens for the subdomain part, not all digits")
		case "domain_reserved":
			return fmt.Errorf("domain %s is reserved\n  Choose a different domain", requestedDomain)
		case "domain_unavailable":
			return fmt.Errorf("domain %s is already taken\n  Choose a different domain", requestedDomain)
		case "rate_limited":
			if platformErr.RetryAfter > 0 {
				return fmt.Errorf("new --commit is rate limited\n  Wait %d seconds before retrying", platformErr.RetryAfter)
			}
			return fmt.Errorf("new --commit is rate limited\n  Try again later")
		}
	}
	if errors.Is(err, application.ErrDataDirNotWritable) {
		return fmt.Errorf("data directory not writable\n  Fix directory permissions or use --data-dir to specify a writable path")
	}
	if errors.Is(err, application.ErrListenPortPermission) {
		if runtime.GOOS == "linux" && listenPort < 1024 {
			return fmt.Errorf("port %d requires elevated privileges\n  Run warded with permission to bind low ports, choose a port above 1024, or grant CAP_NET_BIND_SERVICE, for example:\n\n    sudo setcap 'cap_net_bind_service=+ep' /usr/local/bin/warded", listenPort)
		}
		return fmt.Errorf("port %d requires elevated privileges\n  Run warded with permission to bind low ports or choose a port above 1024", listenPort)
	}
	if errors.Is(err, application.ErrListenPortOccupied) {
		return fmt.Errorf("port %d is in use\n  Stop the conflicting process, use --port to change the port, or use --listen / --listen-v6 to bind a different address", listenPort)
	}
	if errors.Is(err, application.ErrUpstreamUnreachable) {
		return fmt.Errorf("OpenClaw not running on the selected upstream address\n  Start OpenClaw before running `warded new --commit`")
	}
	if domainType == domain.DomainTypeCustomDomain && strings.Contains(err.Error(), "no such host") {
		return fmt.Errorf("DNS lookup failed for %s\n  No usable A record found. Add an A record pointing to your public IP, then re-run `warded new --commit`", requestedDomain)
	}
	return err
}

func renderNewSetup(w io.Writer, out *application.NewOutput, domainType domain.DomainType, requestedDomain string) {
	if out == nil {
		return
	}

	label := newWardLabel(out, domainType, requestedDomain)
	printWardHeader(w, label)

	if out.RequestedDomain != "" {
		fmt.Fprintf(w, "  ✓ Your Domain: https://%s\n", out.RequestedDomain)
	}

	if out.ResolvedPublicIP != "" {
		fmt.Fprintf(w, "  ✓ Public IP: %s\n", out.ResolvedPublicIP)
	}

	switch out.DraftAction {
	case "updated":
		fmt.Fprintf(w, "  ✓ Setup updated\n")
	default:
		fmt.Fprintf(w, "  ✓ Setup created\n")
	}

	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "  Open this link in a browser to claim this ward and continue setup:\n")
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "    %s\n", out.ActivationURL)
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "  After opening it you can:\n")
	fmt.Fprintf(w, "    • Claim your one-time free trial\n")
	fmt.Fprintf(w, "    • Or pay to activate\n")

	customDomain := out.RequestedDomain
	if customDomain == "" {
		customDomain = requestedDomain
	}
	if domainType == domain.DomainTypeCustomDomain && customDomain != "" && out.ResolvedPublicIP != "" {
		fmt.Fprintf(w, "\n")
		fmt.Fprintf(w, "  Custom domain: %s\n", customDomain)
		fmt.Fprintf(w, "    Point its DNS A record to %s\n", out.ResolvedPublicIP)
		fmt.Fprintf(w, "    Otherwise the domain will not work after activation\n")
	}
}

func renderNewSuccess(w io.Writer, runtime *domain.LocalWardRuntime) {
	if runtime == nil {
		return
	}
	label := activeWardLabel(runtime)
	printWardHeader(w, label)
	fmt.Fprintf(w, "  Site:        %s\n", runtime.Site)
	fmt.Fprintf(w, "  Spec:        %s\n", runtime.Spec)
	fmt.Fprintf(w, "  Status:      active\n")
	fmt.Fprintf(w, "  Entry point: https://%s\n", runtime.Domain)
	fmt.Fprintf(w, "  Listen:      %s\n", formatListenForDisplay(runtime))
	fmt.Fprintf(w, "  Upstream:    %s\n", normalizeUpstreamAddrForDisplay(runtime.UpstreamAddr))
	fmt.Fprintf(w, "  Billing:     %s\n", runtime.BillingMode)
	fmt.Fprintf(w, "  Activation:  %s\n", runtime.ActivationMode)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next:")
	fmt.Fprintln(w, "  Run `warded serve`.")
}

func renderPendingSaved(w io.Writer, runtime *domain.LocalWardRuntime, upstreamOk bool) {
	if runtime == nil {
		return
	}
	label := pendingWardLabel(runtime)
	printWardHeader(w, label)
	renderWardBody(w, runtime)
	if !upstreamOk {
		upstreamAddr := normalizeUpstreamAddrForDisplay(runtime.UpstreamAddr)
		fmt.Fprintf(w, "    ⚠ Warning: upstream %s is not reachable\n", upstreamAddr)
		fmt.Fprintf(w, "      Start OpenClaw before running `warded new --commit`\n")
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next:")
	fmt.Fprintln(w, "  Run `warded new --commit` when the setup looks correct.")
}

func renderPendingCommitPreview(w io.Writer, runtime *domain.LocalWardRuntime) {
	if runtime == nil {
		return
	}
	label := pendingWardLabel(runtime)
	printWardHeader(w, label)
	fmt.Fprintf(w, "  Submitting pending configuration:\n")
	fmt.Fprintf(w, "  Site:        %s\n", runtime.Site)
	fmt.Fprintf(w, "  Spec:        %s\n", runtime.Spec)
	fmt.Fprintf(w, "  Billing:     %s\n", runtime.BillingMode)
	if runtime.RequestedDomain != "" {
		fmt.Fprintf(w, "  Domain:      %s\n", runtime.RequestedDomain)
	}
	fmt.Fprintf(w, "  Listen:      %s\n", formatListenForDisplay(runtime))
	fmt.Fprintf(w, "  Upstream:    %s\n", normalizeUpstreamAddrForDisplay(runtime.UpstreamAddr))
	fmt.Fprintln(w)
}

func renderNoPendingSetup(w io.Writer) {
	printWardHeader(w, "(not configured)")
	fmt.Fprintln(w, "  No pending setup.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next:")
	fmt.Fprintln(w, "  Run `warded new --site global` to start a setup.")
}

func renderPendingShow(w io.Writer, runtime *domain.LocalWardRuntime) {
	if runtime == nil {
		return
	}
	label := pendingWardLabel(runtime)
	printWardHeader(w, label)
	renderWardBody(w, runtime)
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next:")
	fmt.Fprintln(w, "  Run `warded new --commit` when the setup looks correct.")
}

func renderPendingDraftExists(w io.Writer, runtime *domain.LocalWardRuntime) {
	if runtime == nil {
		return
	}
	label := pendingWardLabelWithStatus(runtime, "pending")
	printWardHeader(w, label)
	fmt.Fprintf(w, "  A pending draft already exists.\n")
	if runtime.ActivationURL != "" {
		fmt.Fprintf(w, "  Setup Link: %s\n", runtime.ActivationURL)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next:")
	fmt.Fprintln(w, "  Open the setup link to continue in the browser.")
	fmt.Fprintln(w, "  Or run `warded new --commit` to update configuration.")
}

func renderWardBody(w io.Writer, runtime *domain.LocalWardRuntime) {
	if runtime == nil {
		return
	}
	fmt.Fprintf(w, "  Site:        %s\n", runtime.Site)
	fmt.Fprintf(w, "  Spec:        %s\n", runtime.Spec)
	if runtime.RequestedDomain != "" {
		fmt.Fprintf(w, "  Domain:      %s\n", runtime.RequestedDomain)
	}
	fmt.Fprintf(w, "  Setup:       pending\n")
	fmt.Fprintf(w, "  Listen:      %s\n", formatListenForDisplay(runtime))
	fmt.Fprintf(w, "  Upstream:    %s\n", normalizeUpstreamAddrForDisplay(runtime.UpstreamAddr))
	fmt.Fprintf(w, "  Billing:     %s\n", runtime.BillingMode)
}

func formatListenForDisplay(runtime *domain.LocalWardRuntime) string {
	if runtime.ListenHost != "" && runtime.ListenPort > 0 {
		if runtime.IngressFamily == domain.IngressFamilyIPv6 {
			return fmt.Sprintf("ipv6 [%s]:%d", runtime.ListenHost, runtime.ListenPort)
		}
		return fmt.Sprintf("ipv4 %s:%d", runtime.ListenHost, runtime.ListenPort)
	}
	return "ipv4 0.0.0.0:443"
}

func normalizeUpstreamAddrForDisplay(addr string) string {
	if addr == "" {
		return "127.0.0.1:18789"
	}
	return normalizeUpstreamAddr(addr)
}

type pendingMergeInput struct {
	Site                   domain.Site
	Spec                   domain.Spec
	BillingMode            domain.BillingMode
	DomainType             domain.DomainType
	RequestedDomain        string
	UpstreamAddr           string
	UpstreamMode           domain.UpstreamMode
	UpstreamCommand        string
	ListenPort             int
	ListenHost             string
	ListenV6Host           string
	SiteChanged            bool
	SpecChanged            bool
	BillingChanged         bool
	DomainChanged          bool
	RequestChanged         bool
	UpstreamChanged        bool
	UpstreamModeChanged    bool
	UpstreamCommandChanged bool
	ListenChanged          bool
	ListenV6Changed        bool
	PortChanged            bool
}

func mergePendingRuntime(existing *domain.LocalWardRuntime, input pendingMergeInput) (*domain.LocalWardRuntime, error) {
	if input.ListenChanged && input.ListenV6Changed {
		return nil, fmt.Errorf("--listen and --listen-v6 are mutually exclusive in MVP")
	}

	runtime := &domain.LocalWardRuntime{}
	if existing != nil {
		*runtime = *existing
		if len(existing.AuthWhitelist) > 0 {
			runtime.AuthWhitelist = make([]domain.AuthWhitelistRule, len(existing.AuthWhitelist))
			copy(runtime.AuthWhitelist, existing.AuthWhitelist)
		}
	}

	if runtime.Site == "" {
		runtime.Site = input.Site
	}
	if runtime.Spec == "" {
		runtime.Spec = input.Spec
	}
	if runtime.BillingMode == "" {
		runtime.BillingMode = input.BillingMode
	}
	if runtime.DomainType == "" {
		runtime.DomainType = input.DomainType
	}
	if runtime.ListenPort == 0 {
		runtime.ListenPort = 443
	}
	if runtime.ListenHost == "" {
		runtime.ListenHost = "0.0.0.0"
	}
	if runtime.IngressFamily == "" {
		runtime.IngressFamily = domain.IngressFamilyIPv4
	}
	if runtime.UpstreamAddr == "" {
		runtime.UpstreamAddr = defaultUpstreamAddr()
	}
	if runtime.UpstreamMode == "" {
		runtime.UpstreamMode = domain.UpstreamModeDaemon
	}
	if input.SiteChanged {
		runtime.Site = input.Site
	}
	if input.SpecChanged {
		runtime.Spec = input.Spec
	}
	if input.BillingChanged {
		runtime.BillingMode = input.BillingMode
	}
	if input.DomainChanged {
		runtime.DomainType = input.DomainType
	}
	if input.RequestChanged {
		runtime.RequestedDomain = input.RequestedDomain
	}
	if input.UpstreamChanged {
		if err := validateUpstreamAddr(input.UpstreamAddr); err != nil {
			return nil, err
		}
		runtime.UpstreamAddr = normalizeUpstreamAddr(input.UpstreamAddr)
	}
	if input.UpstreamModeChanged {
		if input.UpstreamMode != "" {
			runtime.UpstreamMode = input.UpstreamMode
		}
		if input.UpstreamMode == domain.UpstreamModeDaemon {
			runtime.UpstreamCommand = ""
		}
	}
	if input.UpstreamCommandChanged {
		runtime.UpstreamCommand = input.UpstreamCommand
	}
	if input.PortChanged {
		if input.ListenPort <= 0 || input.ListenPort > 65535 {
			return nil, fmt.Errorf("invalid port %d: must be between 1 and 65535", input.ListenPort)
		}
		runtime.ListenPort = input.ListenPort
	}
	if input.ListenChanged {
		if err := validateIPv4Host(input.ListenHost); err != nil {
			return nil, err
		}
		runtime.ListenHost = input.ListenHost
		runtime.IngressFamily = domain.IngressFamilyIPv4
	}
	if input.ListenV6Changed {
		if err := validateIPv6Host(input.ListenV6Host); err != nil {
			return nil, err
		}
		runtime.ListenHost = input.ListenV6Host
		runtime.IngressFamily = domain.IngressFamilyIPv6
	}

	runtime.UpstreamPort = extractPortFromAddr(runtime.UpstreamAddr)

	if runtime.WardStatus == "" {
		runtime.WardStatus = domain.WardStatusInitializing
	}
	if runtime.JWTSigningSecret == "" {
		jwtSecret, err := randomJWTSecret()
		if err != nil {
			return nil, fmt.Errorf("generate JWT signing secret: %w", err)
		}
		runtime.JWTSigningSecret = jwtSecret
	}
	if runtime.WardDraftSecret == "" {
		draftSecret, err := randomDraftSecret()
		if err != nil {
			return nil, fmt.Errorf("generate draft secret: %w", err)
		}
		runtime.WardDraftSecret = draftSecret
	}
	if tlsMode, err := domain.TLSModeForDomainType(runtime.DomainType); err == nil {
		runtime.TLSMode = tlsMode
	}
	runtime.UpdatedAt = time.Now().UTC()
	return runtime, nil
}

func clearPlatformDraftState(runtime *domain.LocalWardRuntime) {
	if runtime == nil {
		return
	}
	runtime.WardDraftID = ""
	runtime.ActivationURL = ""
	runtime.LastPublicIP = ""
	runtime.LastPublicIPReportedAt = time.Time{}
	runtime.ExpiresAt = time.Time{}
	if runtime.WardID == "" {
		runtime.Domain = ""
		runtime.ActivationMode = ""
		runtime.WardSecret = ""
		runtime.WardStatus = domain.WardStatusInitializing
	}
}

func runPendingFlagPrechecksAddr(cmd *cobra.Command, runtime *domain.LocalWardRuntime, dataDir string, listenChanged bool) (bool, error) {
	if err := ensureDataDirWritable(dataDir); err != nil {
		return false, err
	}
	if listenChanged {
		addr := listenAddrFromRuntime(runtime)
		if err := ensureAddrAvailable(addr); err != nil {
			return false, err
		}
	}
	// Validate upstream address format before attempting connection
	if err := validateUpstreamAddr(runtime.UpstreamAddr); err != nil {
		return false, err
	}
	upstreamOk := true
	if runtime.UpstreamAddr != "" && runtime.UpstreamMode != domain.UpstreamModeManaged {
		checker := upstream.NewChecker()
		if err := checker.Check(cmd.Context(), runtime.UpstreamAddr); err != nil {
			upstreamOk = false
		}
	}
	return upstreamOk, nil
}

func ensureAddrAvailable(addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return classifyAddrError(err)
	}
	return ln.Close()
}

func classifyAddrError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("%w: %v", application.ErrListenPortPermission, err)
	}
	return fmt.Errorf("%w: %v", application.ErrListenPortOccupied, err)
}

func startTemporaryProbeServerAddr(ctx context.Context, addr string) (func(context.Context) error, error) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_ward/probe", func(w http.ResponseWriter, r *http.Request) {
		challenge := strings.TrimSpace(r.URL.Query().Get("challenge"))
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if challenge == "" {
			http.Error(w, "missing challenge", http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte("warded-probe-ok:" + challenge))
	})
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		BaseContext: func(net.Listener) context.Context {
			return ctx
		},
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, classifyAddrError(err)
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	go func() {
		_ = server.Serve(ln)
	}()
	return server.Shutdown, nil
}

func resolveUpstreamAddr(existing *domain.LocalWardRuntime, input string, changed bool) string {
	if changed && input != "" {
		return normalizeUpstreamAddr(input)
	}
	if existing != nil && existing.UpstreamAddr != "" {
		return existing.UpstreamAddr
	}
	return defaultUpstreamAddr()
}

func defaultUpstreamAddr() string {
	return "127.0.0.1:18789"
}

func extractPortFromAddr(addr string) int {
	if addr == "" {
		return 0
	}
	lastColon := strings.LastIndex(addr, ":")
	if lastColon < 0 {
		return 0
	}
	portStr := addr[lastColon+1:]
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

func normalizeUpstreamAddr(input string) string {
	if input == "" {
		return defaultUpstreamAddr()
	}
	input = strings.TrimSpace(input)
	if !strings.Contains(input, ":") {
		port, err := strconv.Atoi(input)
		if err == nil && port > 0 && port <= 65535 {
			return fmt.Sprintf("127.0.0.1:%d", port)
		}
	}
	if strings.HasPrefix(input, ":") {
		portStr := strings.TrimPrefix(input, ":")
		port, err := strconv.Atoi(portStr)
		if err == nil && port > 0 && port <= 65535 {
			return fmt.Sprintf("127.0.0.1:%d", port)
		}
	}
	return input
}

func validateUpstreamAddr(input string) error {
	if input == "" {
		return nil
	}
	input = strings.TrimSpace(input)
	// Pure numeric input: validate port range
	if port, err := strconv.Atoi(input); err == nil {
		if port <= 0 || port > 65535 {
			return fmt.Errorf("invalid upstream address %q: port must be a number between 1 and 65535", input)
		}
		return nil
	}
	// :port format
	if strings.HasPrefix(input, ":") {
		portStr := strings.TrimPrefix(input, ":")
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("invalid upstream address %q: port must be a number between 1 and 65535", input)
		}
		return nil
	}
	// host:port format - must contain colon
	if !strings.Contains(input, ":") {
		return fmt.Errorf("invalid upstream address %q: must be in format host:port, :port, or just port number", input)
	}
	// Handle host:port format
	_, portStr, err := net.SplitHostPort(input)
	if err != nil {
		return fmt.Errorf("invalid upstream address %q: %w", input, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid upstream address %q: port must be a number between 1 and 65535", input)
	}
	// Note: we allow hostname for upstream (e.g., "openclaw.internal:18789")
	return nil
}

func pendingWardLabelWithStatus(runtime *domain.LocalWardRuntime, status string) string {
	if runtime == nil {
		return "(not configured)"
	}
	if runtime.RequestedDomain != "" {
		return fmt.Sprintf("%s (%s)", runtime.RequestedDomain, status)
	}
	return fmt.Sprintf("(%s)", status)
}

func newWardLabel(out *application.NewOutput, domainType domain.DomainType, requestedDomain string) string {
	if out == nil {
		return "(not configured)"
	}
	if out.RequestedDomain != "" {
		return out.RequestedDomain + " (pending)"
	}
	if requestedDomain != "" {
		return requestedDomain + " (pending)"
	}
	return "(pending)"
}

func pendingWardLabel(runtime *domain.LocalWardRuntime) string {
	if runtime == nil {
		return "(not configured)"
	}
	if runtime.RequestedDomain != "" {
		return runtime.RequestedDomain + " (pending)"
	}
	return "(pending)"
}

func activeWardLabel(runtime *domain.LocalWardRuntime) string {
	if runtime == nil {
		return "(not configured)"
	}
	if runtime.Domain != "" {
		return runtime.Domain
	}
	if runtime.RequestedDomain != "" {
		return runtime.RequestedDomain
	}
	return "(not configured)"
}

func randomJWTSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func randomDraftSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate draft secret random bytes: %w", err)
	}
	return "wdd_" + hex.EncodeToString(buf), nil
}

func ensureDataDirWritable(dir string) error {
	if dir == "" {
		return application.ErrDataDirNotWritable
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("%w: %v", application.ErrDataDirNotWritable, err)
	}
	f, err := os.CreateTemp(dir, ".warded-write-test-*")
	if err != nil {
		return fmt.Errorf("%w: %v", application.ErrDataDirNotWritable, err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return fmt.Errorf("%w: %v", application.ErrDataDirNotWritable, err)
	}
	_ = os.Remove(name)
	return nil
}

func listenAddrFromRuntime(runtime *domain.LocalWardRuntime) string {
	if runtime.ListenHost != "" && runtime.ListenPort > 0 {
		if runtime.IngressFamily == domain.IngressFamilyIPv6 {
			return fmt.Sprintf("[%s]:%d", runtime.ListenHost, runtime.ListenPort)
		}
		return fmt.Sprintf("%s:%d", runtime.ListenHost, runtime.ListenPort)
	}
	return "0.0.0.0:443"
}

func resolveListenParams(existing *domain.LocalWardRuntime, listenHost, listenV6Host string, listenPort int, listenChanged, listenV6Changed, portChanged bool) (string, int, domain.IngressFamily, error) {

	if listenChanged && listenV6Changed {
		return "", 0, "", fmt.Errorf("--listen and --listen-v6 are mutually exclusive in MVP")
	}

	host := "0.0.0.0"
	port := 443
	family := domain.IngressFamilyIPv4

	if existing != nil {
		host = existing.ListenHost
		port = existing.ListenPort
		family = existing.IngressFamily
		if host == "" {
			host = "0.0.0.0"
		}
		if port == 0 {
			port = 443
		}
		if family == "" {
			family = domain.IngressFamilyIPv4
		}
	}

	if portChanged {
		port = listenPort
	}

	if listenChanged {
		host = listenHost
		family = domain.IngressFamilyIPv4
	} else if listenV6Changed {
		host = listenV6Host
		family = domain.IngressFamilyIPv6
	}

	if port <= 0 || port > 65535 {
		return "", 0, "", fmt.Errorf("invalid port %d: must be between 1 and 65535", port)
	}

	return host, port, family, nil
}

func validateIPv4Host(host string) error {
	if host == "" {
		return nil
	}
	if strings.Contains(host, ":") {
		return fmt.Errorf("--listen only accepts IPv4 addresses; use --listen-v6 for IPv6")
	}
	if ip := net.ParseIP(host); ip == nil || ip.To4() == nil {
		return fmt.Errorf("--listen only accepts IPv4 addresses; use --listen-v6 for IPv6")
	}
	return nil
}

func validateIPv6Host(host string) error {
	if host == "" {
		return nil
	}
	if ip := net.ParseIP(host); ip == nil || ip.To4() != nil {
		return fmt.Errorf("--listen-v6 only accepts IPv6 addresses")
	}
	return nil
}

func validateUpstreamAddrHost(addr string) error {
	if addr == "" {
		return nil
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid upstream address %q", addr)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return fmt.Errorf("upstream host must not be 0.0.0.0, ::, or empty")
	}
	return nil
}

func randomProbeChallenge() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate probe challenge: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
