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

func newNewCommand(version string) *cobra.Command {
	var (
		site            string
		spec            string
		billingMode     string
		domainType      string
		requestedDomain string
		upstreamPort    int
		listenPort      int
		dataDir         string
		baseDomain      string
		platformOrigin  string
		commit          bool
	)

	command := &cobra.Command{
		Use:   "new",
		Short: "Prepare or submit a new ward setup",
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// Validate site - can be from flag or from existing pending config
			if site == "" {
				// Try to load site from existing pending config
				store := storage.NewJSONStore(dataDir)
				if runtime, err := store.LoadWardRuntime(cmd.Context()); err == nil && runtime != nil && runtime.Site != "" {
					site = string(runtime.Site)
				} else {
					return fmt.Errorf("--site is required: must be cn (warded.cn) or global (warded.me)")
				}
			}
			if site != string(domain.SiteCN) && site != string(domain.SiteGlobal) {
				return fmt.Errorf("invalid --site: %s (must be cn or global)", site)
			}
			if spec != string(domain.SpecStarter) && spec != string(domain.SpecPro) {
				return fmt.Errorf("invalid --spec: %s (must be starter or pro)", spec)
			}
			if billingMode != string(domain.BillingModeMonthly) && billingMode != string(domain.BillingModeYearly) {
				return fmt.Errorf("invalid --billing-mode: %s (must be monthly or yearly)", billingMode)
			}
			if domainType != string(domain.DomainTypePlatformSubdomain) && domainType != string(domain.DomainTypeCustomDomain) {
				return fmt.Errorf("invalid --domain-type: %s (must be platform_subdomain or custom_domain)", domainType)
			}

			// Validate spec/domain_type combination (basic validation without --commit)
			if spec == string(domain.SpecStarter) && domainType == string(domain.DomainTypeCustomDomain) {
				return fmt.Errorf("starter spec only supports platform_subdomain")
			}
			// Per contract: starter spec must not have user-provided requested_domain
			// (platform will assign a random subdomain)
			// Note: we need to check against the effective spec (flag or pending config)
			if cmd.Flags().Changed("domain") && requestedDomain != "" {
				// Load existing pending config to determine effective spec
				store := storage.NewJSONStore(dataDir)
				existingRuntime, _ := store.LoadWardRuntime(cmd.Context())
				// Determine effective spec: flag takes precedence, then pending config
				effectiveSpec := spec
				if !cmd.Flags().Changed("spec") && existingRuntime != nil && existingRuntime.Spec != "" {
					effectiveSpec = string(existingRuntime.Spec)
				}
				if effectiveSpec == string(domain.SpecStarter) {
					return fmt.Errorf("starter spec does not support --domain (platform assigns subdomain automatically)")
				}
			}

			// Validate port ranges
			if upstreamPort < 0 || upstreamPort > 65535 {
				return fmt.Errorf("invalid --upstream-port: %d (must be between 0 and 65535)", upstreamPort)
			}
			if listenPort < 1 || listenPort > 65535 {
				return fmt.Errorf("invalid --port: %d (must be between 1 and 65535)", listenPort)
			}

			// Preflight checks for --commit
			if commit {
				// Load existing pending config to merge with flags
				store := storage.NewJSONStore(dataDir)
				existingRuntime, _ := store.LoadWardRuntime(cmd.Context())

				// Determine effective requested_domain (flag takes precedence, then existing pending config)
				effectiveDomain := requestedDomain
				if effectiveDomain == "" && existingRuntime != nil && existingRuntime.RequestedDomain != "" {
					effectiveDomain = existingRuntime.RequestedDomain
				}

				// Validate pro spec requires requested_domain
				if spec == string(domain.SpecPro) && effectiveDomain == "" {
					return fmt.Errorf("pro spec requires --domain (full domain, e.g., myrobot.warded.me or robot.example.com)")
				}

				// Validate platform_subdomain uses a full domain with allowed suffix
				if domainType == string(domain.DomainTypePlatformSubdomain) && effectiveDomain != "" {
					if !strings.Contains(effectiveDomain, ".") {
						return fmt.Errorf("--domain must be a full platform domain (e.g., myrobot.warded.me)")
					}
					policy := sitepolicy.ForSite(domain.Site(site))
					allowed := false
					for _, suffix := range policy.AllowedBaseDomains() {
						if strings.HasSuffix(strings.ToLower(effectiveDomain), "."+suffix) {
							allowed = true
							break
						}
					}
					if !allowed {
						return fmt.Errorf("--domain %s is not an allowed platform domain for site %s", effectiveDomain, site)
					}
				}

				// Check data directory writability
				if err := ensureDataDirWritable(dataDir); err != nil {
					return err
				}
				// Check listen port availability
				if err := ensureListenPortAvailable(listenPort); err != nil {
					return err
				}
				// Check upstream port reachability (only if explicitly specified or from pending)
				checkUpstreamPort := upstreamPort
				if checkUpstreamPort == 0 && existingRuntime != nil && existingRuntime.UpstreamPort > 0 {
					checkUpstreamPort = existingRuntime.UpstreamPort
				}
				if checkUpstreamPort > 0 {
					checker := upstream.NewChecker()
					if err := checker.Check(cmd.Context(), checkUpstreamPort); err != nil {
						return fmt.Errorf("upstream port %d is not reachable: %w", checkUpstreamPort, err)
					}
				}
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {

			platformURL, err := resolvePlatformOrigin(domain.Site(site), baseDomain, platformOrigin)
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			publicBaseURL, err := resolvePublicPlatformBaseURL(domain.Site(site), baseDomain)
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			store := storage.NewJSONStore(dataDir)
			platformClient := platformapi.NewClient(platformURL, version)
			activationService := application.DraftActivationService{
				ConfigStore: store,
				PlatformAPI: platformClient,
			}

			wardRuntime, err := store.LoadWardRuntime(cmd.Context())
			if err != nil {
				return fmt.Errorf("new: load pending runtime: %w", err)
			}
			pendingRuntime, err := mergePendingRuntime(wardRuntime, pendingMergeInput{
				Site:            domain.Site(site),
				Spec:            domain.Spec(spec),
				BillingMode:     domain.BillingMode(billingMode),
				DomainType:      domain.DomainType(domainType),
				RequestedDomain: requestedDomain,
				UpstreamPort:    upstreamPort,
				ListenPort:      listenPort,
				SiteChanged:     cmd.Flags().Changed("site"),
				SpecChanged:     cmd.Flags().Changed("spec"),
				BillingChanged:  cmd.Flags().Changed("billing-mode"),
				DomainChanged:   cmd.Flags().Changed("domain-type"),
				RequestChanged:  cmd.Flags().Changed("domain"),
				UpstreamChanged: cmd.Flags().Changed("upstream-port"),
				PortChanged:     cmd.Flags().Changed("port"),
			})
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}

			if !commit {
				if pendingRuntime.WardDraftID != "" && pendingRuntime.WardDraftSecret != "" {
					renderPendingExists(cmd.OutOrStdout())
					return nil
				}
				upstreamOk, err := runPendingFlagPrechecks(cmd, pendingRuntime, dataDir)
				if err != nil {
					return fmt.Errorf("new: %w", err)
				}
				if err := store.SaveWardRuntime(cmd.Context(), *pendingRuntime); err != nil {
					return fmt.Errorf("new: save pending runtime: %w", err)
				}
				renderPendingSaved(cmd.OutOrStdout(), pendingRuntime, upstreamOk)
				return nil
			}

			// --commit: Check for existing active ward
			if pendingRuntime.WardID != "" && pendingRuntime.WardSecret != "" {
				renderNewSuccess(cmd.OutOrStdout(), pendingRuntime)
				return nil
			}

			// --commit: Try to finalize if draft was converted
			if runtime, finalized, err := activationService.FinalizeIfConverted(cmd.Context()); err != nil {
				return fmt.Errorf("new: finalize pending activation: %w", err)
			} else if finalized {
				renderNewSuccess(cmd.OutOrStdout(), runtime)
				return nil
			} else if runtime != nil && runtime.WardDraftID != "" && runtime.WardDraftSecret != "" {
				renderNewSetup(cmd.OutOrStdout(), &application.InitOutput{
					WardDraftID:      runtime.WardDraftID,
					ActivationURL:    runtime.ActivationURL,
					ResolvedPublicIP: runtime.LastPublicIP,
					RequestedDomain:  runtime.RequestedDomain,
				}, runtime.DomainType, runtime.RequestedDomain)
				return nil
			}

			// Reload runtime after FinalizeIfConverted - it may have cleared expired draft state
			if reloadedRuntime, err := store.LoadWardRuntime(cmd.Context()); err == nil && reloadedRuntime != nil {
				pendingRuntime = reloadedRuntime
			}

			clearPlatformDraftState(pendingRuntime)

			if err := ensureDataDirWritable(dataDir); err != nil {
				return fmt.Errorf("new: %w", err)
			}
			if err := store.SaveWardRuntime(cmd.Context(), *pendingRuntime); err != nil {
				return fmt.Errorf("new: save pending runtime: %w", err)
			}
			if err := ensureListenPortAvailable(listenPortFromRuntime(pendingRuntime)); err != nil {
				return fmt.Errorf("new: %w", err)
			}
			probeChallenge, err := randomProbeChallenge()
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			stopProbe, err := startTemporaryProbeServer(cmd.Context(), listenPortFromRuntime(pendingRuntime))
			if err != nil {
				return fmt.Errorf("new: %w", err)
			}
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = stopProbe(shutdownCtx)
			}()

			initService := application.InitService{
				ConfigStore:   store,
				PlatformAPI:   platformClient,
				UpstreamCheck: upstream.NewChecker(),
			}

			out, err := initService.Execute(cmd.Context(), application.InitInput{
				Site:            pendingRuntime.Site,
				Mode:            "new",
				Spec:            pendingRuntime.Spec,
				BillingMode:     pendingRuntime.BillingMode,
				DomainType:      pendingRuntime.DomainType,
				RequestedDomain: pendingRuntime.RequestedDomain,
				UpstreamPort:    pendingRuntime.UpstreamPort,
				ListenPort:      listenPortFromRuntime(pendingRuntime),
				ProbeChallenge:  probeChallenge,
				PublicBaseURL:   publicBaseURL,
			})
			if err != nil {
				return explainNewError(err, pendingRuntime.DomainType, pendingRuntime.RequestedDomain, listenPortFromRuntime(pendingRuntime))
			}

			renderNewSetup(cmd.OutOrStdout(), out, pendingRuntime.DomainType, pendingRuntime.RequestedDomain)

			// After creating draft, exit immediately without waiting for activation
			// Status updates will be handled by warded status and warded serve
			return nil
		},
	}

	command.Flags().StringVar(&site, "site", "", "target site: cn (warded.cn) or global (warded.me)")
	command.Flags().StringVar(&spec, "spec", string(domain.SpecStarter), "ward spec: starter or pro")
	command.Flags().StringVar(&billingMode, "billing-mode", string(domain.BillingModeMonthly), "billing mode: monthly or yearly")
	command.Flags().StringVar(&domainType, "domain-type", string(domain.DomainTypePlatformSubdomain), "domain type: platform_subdomain (auto-assigned) or custom_domain (bring your own)")
	command.Flags().StringVar(&requestedDomain, "domain", "", "requested full domain (e.g., myrobot.warded.me or robot.example.com)")
	command.Flags().IntVar(&upstreamPort, "upstream-port", 0, "local upstream port to protect; 0 means auto-detect/default 18789")
	command.Flags().IntVar(&listenPort, "port", 443, "local listen port for warded")
	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")
	command.Flags().BoolVar(&commit, "commit", false, "submit the pending configuration to the platform and create a draft")

	// Hide development/testing flags from help output
	_ = command.Flags().MarkHidden("platform-origin")

	return command
}

func explainNewError(err error, domainType domain.DomainType, requestedDomain string, listenPort int) error {
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
		return fmt.Errorf("port %d is in use\n  Stop the conflicting process or use --port to choose a different port", listenPort)
	}
	if errors.Is(err, application.ErrUpstreamUnreachable) {
		return fmt.Errorf("OpenClaw not running on the selected upstream port\n  Start OpenClaw before running `warded new --commit`")
	}
	if domainType == domain.DomainTypeCustomDomain && strings.Contains(err.Error(), "no such host") {
		return fmt.Errorf("DNS lookup failed for %s\n  No usable A record found. Add an A record pointing to your public IP, then re-run `warded new --commit`", requestedDomain)
	}
	return err
}

func renderNewSetup(w io.Writer, out *application.InitOutput, domainType domain.DomainType, requestedDomain string) {
	if out == nil {
		return
	}

	if out.RequestedDomain != "" {
		fmt.Fprintf(w, "✓ Your Domain: https://%s\n", out.RequestedDomain)
	}

	if out.ResolvedPublicIP != "" {
		fmt.Fprintf(w, "\n✓ Public IP: %s\n", out.ResolvedPublicIP)
	}

	fmt.Fprintf(w, "✓ Setup link ready\n")

	fmt.Fprintf(w, "\nOpen this link in a browser to claim this ward and continue setup:\n")
	fmt.Fprintf(w, "\n  %s\n", out.ActivationURL)
	fmt.Fprintf(w, "\nAfter opening it you can:\n")
	fmt.Fprintf(w, "  • Claim your one-time free trial\n")
	fmt.Fprintf(w, "  • Or pay to activate\n")

	customDomain := out.RequestedDomain
	if customDomain == "" {
		customDomain = requestedDomain
	}
	if domainType == domain.DomainTypeCustomDomain && customDomain != "" && out.ResolvedPublicIP != "" {
		fmt.Fprintf(w, "\nCustom domain: %s\n", customDomain)
		fmt.Fprintf(w, "  Point its DNS A record to %s\n", out.ResolvedPublicIP)
		fmt.Fprintf(w, "  Otherwise the domain will not work after activation\n")
	}
}

func renderNewSuccess(w io.Writer, runtime *domain.LocalWardRuntime) {
	if runtime == nil {
		return
	}
	fmt.Fprintf(w, "\nProtection is active.\n")
	fmt.Fprintf(w, "Domain: %s\n", runtime.Domain)
	fmt.Fprintf(w, "Ward ID: %s\n", runtime.WardID)
	fmt.Fprintf(w, "Billing: %s\n", runtime.BillingMode)
	fmt.Fprintf(w, "Activation: %s\n", runtime.ActivationMode)
	fmt.Fprintf(w, "\nNext: run `warded serve`\n")
}

func renderPendingSaved(w io.Writer, runtime *domain.LocalWardRuntime, upstreamOk bool) {
	if runtime == nil {
		return
	}
	fmt.Fprintf(w, "\nPending ward setup saved.\n")
	fmt.Fprintf(w, "Site: %s\n", runtime.Site)
	fmt.Fprintf(w, "Spec: %s\n", runtime.Spec)
	fmt.Fprintf(w, "Billing: %s\n", runtime.BillingMode)
	fmt.Fprintf(w, "Domain type: %s\n", runtime.DomainType)
	if runtime.RequestedDomain != "" {
		fmt.Fprintf(w, "Requested domain: %s\n", runtime.RequestedDomain)
	}
	if runtime.UpstreamPort > 0 {
		fmt.Fprintf(w, "Upstream port: %d\n", runtime.UpstreamPort)
		if !upstreamOk {
			fmt.Fprintf(w, "  ⚠ Warning: upstream port %d is not reachable\n", runtime.UpstreamPort)
			fmt.Fprintf(w, "    Start OpenClaw before running `warded new --commit`\n")
		}
	}
	fmt.Fprintf(w, "Listen addr: %s\n", runtime.ListenAddr)
	fmt.Fprintf(w, "\nNext: run `warded new --commit`\n")
}

func renderPendingExists(w io.Writer) {
	fmt.Fprintf(w, "\nA pending draft already exists.\n")
	fmt.Fprintf(w, "To update configuration: run `warded new --commit`\n")
	fmt.Fprintf(w, "To check activation status: run `warded status`\n")
}

type pendingMergeInput struct {
	Site            domain.Site
	Spec            domain.Spec
	BillingMode     domain.BillingMode
	DomainType      domain.DomainType
	RequestedDomain string
	UpstreamPort    int
	ListenPort      int
	SiteChanged     bool
	SpecChanged     bool
	BillingChanged  bool
	DomainChanged   bool
	RequestChanged  bool
	UpstreamChanged bool
	PortChanged     bool
}

func mergePendingRuntime(existing *domain.LocalWardRuntime, input pendingMergeInput) (*domain.LocalWardRuntime, error) {
	runtime := &domain.LocalWardRuntime{}
	if existing != nil {
		*runtime = *existing
		runtime.WebhookAllowPaths = append([]string(nil), existing.WebhookAllowPaths...)
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
	if runtime.ListenAddr == "" {
		runtime.ListenAddr = pendingListenAddrForPort(input.ListenPort)
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
		runtime.UpstreamPort = input.UpstreamPort
	}
	if input.PortChanged {
		runtime.ListenAddr = pendingListenAddrForPort(input.ListenPort)
	}

	if runtime.WardStatus == "" {
		runtime.WardStatus = domain.WardStatusInitializing
	}
	if runtime.JWTSigningSecret == "" {
		jwtSecret, err := pendingRandomJWTSecret()
		if err != nil {
			return nil, fmt.Errorf("generate JWT signing secret: %w", err)
		}
		runtime.JWTSigningSecret = jwtSecret
	}
	if runtime.WardDraftSecret == "" {
		draftSecret, err := pendingRandomDraftSecret()
		if err != nil {
			return nil, fmt.Errorf("generate draft secret: %w", err)
		}
		runtime.WardDraftSecret = draftSecret
	}
	if tlsMode, err := pendingTLSModeForDomainType(runtime.DomainType); err == nil {
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

func runPendingFlagPrechecks(cmd *cobra.Command, runtime *domain.LocalWardRuntime, dataDir string) (bool, error) {
	if err := ensureDataDirWritable(dataDir); err != nil {
		return false, err
	}
	if cmd.Flags().Changed("port") {
		if err := ensureListenPortAvailable(listenPortFromRuntime(runtime)); err != nil {
			return false, err
		}
	}
	// Check upstream port reachability
	upstreamOk := true
	if runtime.UpstreamPort > 0 {
		checker := upstream.NewChecker()
		if err := checker.Check(cmd.Context(), runtime.UpstreamPort); err != nil {
			upstreamOk = false
		}
	}
	return upstreamOk, nil
}

func listenPortFromRuntime(runtime *domain.LocalWardRuntime) int {
	if runtime == nil || runtime.ListenAddr == "" {
		return 443
	}
	addr := strings.TrimPrefix(runtime.ListenAddr, ":")
	port, err := strconv.Atoi(addr)
	if err != nil || port <= 0 {
		return 443
	}
	return port
}

func pendingListenAddrForPort(port int) string {
	if port <= 0 {
		port = 443
	}
	return fmt.Sprintf(":%d", port)
}

func pendingRandomJWTSecret() (string, error) {
	return randomJWTSecret()
}

func randomJWTSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func pendingRandomDraftSecret() (string, error) {
	return randomDraftSecret()
}

func randomDraftSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate draft secret random bytes: %w", err)
	}
	return "wdd_" + hex.EncodeToString(buf), nil
}

func randomHexString(bytes int) (string, error) {
	buf := make([]byte, bytes)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func pendingTLSModeForDomainType(domainType domain.DomainType) (domain.TLSMode, error) {
	switch domainType {
	case domain.DomainTypePlatformSubdomain:
		return domain.TLSModePlatformWildcard, nil
	case domain.DomainTypeCustomDomain:
		return domain.TLSModeLocalACME, nil
	default:
		return "", fmt.Errorf("unsupported domain type: %s", domainType)
	}
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

func ensureListenPortAvailable(port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("%w: invalid port %d", application.ErrListenPortOccupied, port)
	}
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return classifyListenPortError(err)
	}
	return ln.Close()
}

func randomProbeChallenge() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate probe challenge: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

func startTemporaryProbeServer(ctx context.Context, port int) (func(context.Context) error, error) {
	addr := fmt.Sprintf(":%d", port)
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
		return nil, classifyListenPortError(err)
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

func classifyListenPortError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, syscall.EACCES) || errors.Is(err, syscall.EPERM) {
		return fmt.Errorf("%w: %v", application.ErrListenPortPermission, err)
	}
	return fmt.Errorf("%w: %v", application.ErrListenPortOccupied, err)
}
