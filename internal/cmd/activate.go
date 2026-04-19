package cmd

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/adapters/ui"
	"github.com/herewei/warded/internal/adapters/upstream"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/spf13/cobra"
)

func newActivateCommand(version string) *cobra.Command {
	var (
		site            string
		spec            string
		billingMode     string
		domainType      string
		requestedDomain string
		upstreamPort    int
		listenPort      int
		dataDir       string
		baseDomain      string
		platformOrigin  string
		noWait          bool
		waitTimeout     time.Duration
		pollInterval    time.Duration
	)

	command := &cobra.Command{
		Use:   "activate",
		Short: "Activate protection for the current OpenClaw",
		RunE: func(cmd *cobra.Command, args []string) error {
			if site != string(domain.SiteCN) && site != string(domain.SiteGlobal) {
				return fmt.Errorf("unsupported site: %s (expected cn or global)", site)
			}

			platformURL, err := resolvePlatformOrigin(domain.Site(site), baseDomain, platformOrigin)
			if err != nil {
				return fmt.Errorf("activate: %w", err)
			}
			publicBaseURL, err := resolvePublicPlatformBaseURL(domain.Site(site), baseDomain)
			if err != nil {
				return fmt.Errorf("activate: %w", err)
			}
			store := storage.NewJSONStore(dataDir)
			platformClient := platformapi.NewClient(platformURL, version)
			activationService := application.DraftActivationService{
				ConfigStore: store,
				PlatformAPI: platformClient,
			}

			if runtime, err := store.LoadWardRuntime(cmd.Context()); err != nil {
				return fmt.Errorf("activate: load ward runtime: %w", err)
			} else if runtime != nil && runtime.WardID != "" && runtime.WardSecret != "" {
				renderActivateSuccess(cmd.OutOrStdout(), runtime)
				return nil
			}

			if runtime, finalized, err := activationService.FinalizeIfConverted(cmd.Context()); err != nil {
				return fmt.Errorf("activate: finalize pending activation: %w", err)
			} else if finalized {
				renderActivateSuccess(cmd.OutOrStdout(), runtime)
				return nil
			}
			if err := ensureDataDirWritable(dataDir); err != nil {
				return fmt.Errorf("activate: %w", err)
			}
			if err := ensureListenPortAvailable(listenPort); err != nil {
				return fmt.Errorf("activate: %w", err)
			}
			probeChallenge, err := randomProbeChallenge()
			if err != nil {
				return fmt.Errorf("activate: %w", err)
			}
			stopProbe, err := startTemporaryProbeServer(cmd.Context(), listenPort)
			if err != nil {
				return fmt.Errorf("activate: %w", err)
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
				Site:            domain.Site(site),
				Mode:            "new",
				Spec:            domain.Spec(spec),
				BillingMode:     domain.BillingMode(billingMode),
				DomainType:      domain.DomainType(domainType),
				RequestedDomain: requestedDomain,
				UpstreamPort:    upstreamPort,
				ListenPort:      listenPort,
				ProbeChallenge:  probeChallenge,
				PublicBaseURL:   publicBaseURL,
			})
			if err != nil {
				return explainActivateError(err, domain.DomainType(domainType), requestedDomain, listenPort)
			}

			renderActivateSetup(cmd.OutOrStdout(), out, domain.DomainType(domainType), requestedDomain)

			if noWait {
				return nil
			}

			fmt.Fprintln(cmd.OutOrStdout())
			spinner := ui.New(cmd.OutOrStdout(),
				ui.WithMessage("Waiting for activation to complete..."),
				ui.WithInterval(200*time.Millisecond),
			)
			spinner.Start()

			ctx := cmd.Context()
			if waitTimeout > 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, waitTimeout)
				defer cancel()
			}

			runtime, err := activationService.WaitUntilActivated(ctx, pollInterval)
			if err != nil {
				spinner.Fail()
				return err
			}

			spinner.Succeed()
			renderActivateSuccess(cmd.OutOrStdout(), runtime)
			return nil
		},
	}

	command.Flags().StringVar(&site, "site", string(domain.SiteGlobal), "target site: cn or global")
	command.Flags().StringVar(&spec, "spec", string(domain.SpecStarter), "ward spec")
	command.Flags().StringVar(&billingMode, "billing-mode", string(domain.BillingModeMonthly), "billing mode")
	command.Flags().StringVar(&domainType, "domain-type", string(domain.DomainTypePlatformSubdomain), "domain type")
	command.Flags().StringVar(&requestedDomain, "domain", "", "requested custom domain or preferred subdomain")
	command.Flags().IntVar(&upstreamPort, "upstream-port", 0, "local upstream port to protect; 0 means auto-detect/default 18789")
	command.Flags().IntVar(&listenPort, "port", 443, "local listen port for warded")
	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")
	command.Flags().BoolVar(&noWait, "no-wait", false, "print the activation link and exit without waiting")
	command.Flags().DurationVar(&waitTimeout, "wait-timeout", 3*time.Minute, "maximum time to wait for activation")
	command.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "poll interval while waiting for activation")

	return command
}

func explainActivateError(err error, domainType domain.DomainType, requestedDomain string, listenPort int) error {
	if err == nil {
		return nil
	}
	var platformErr *ports.PlatformError
	if errors.As(err, &platformErr) {
		switch platformErr.Code {
		case "ingress_unreachable":
			return fmt.Errorf("inbound probe failed\n  Check port %d, firewall, and security group settings", listenPort)
		case "domain_dns_not_ready":
			return fmt.Errorf("DNS lookup failed for %s\n  No usable A record found. Add an A record pointing to your public IP, then re-run activate", requestedDomain)
		case "domain_public_ip_mismatch":
			return fmt.Errorf("DNS points to the wrong public IP for %s\n  Update the A record so it resolves to this machine's public IP, then re-run activate", requestedDomain)
		case "public_ip_unavailable":
			return fmt.Errorf("public IP is unavailable\n  Make sure this machine has a reachable public IPv4 address before running activate")
		case "rate_limited":
			if platformErr.RetryAfter > 0 {
				return fmt.Errorf("activate is rate limited\n  Wait %d seconds before retrying", platformErr.RetryAfter)
			}
			return fmt.Errorf("activate is rate limited\n  Try again later")
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
		return fmt.Errorf("OpenClaw not running on the selected upstream port\n  Start OpenClaw before running activate")
	}
	if domainType == domain.DomainTypeCustomDomain && strings.Contains(err.Error(), "no such host") {
		return fmt.Errorf("DNS lookup failed for %s\n  No usable A record found. Add an A record pointing to your public IP, then re-run activate", requestedDomain)
	}
	return err
}

func renderActivateSetup(w io.Writer, out *application.InitOutput, domainType domain.DomainType, requestedDomain string) {
	if out == nil {
		return
	}
	if out.ResolvedPublicIP != "" {
		fmt.Fprintf(w, "\n✓ Public IP: %s\n", out.ResolvedPublicIP)
	}
	fmt.Fprintf(w, "✓ Setup link ready\n")

	fmt.Fprintf(w, "\nOpen this link in a browser to claim this OpenClaw and continue setup:\n")
	fmt.Fprintf(w, "\n  %s\n", out.ActivationURL)
	fmt.Fprintf(w, "\nAfter opening it you can:\n")
	fmt.Fprintf(w, "  • Start a free 72-hour trial\n")
	fmt.Fprintf(w, "  • Or pay to activate\n")

	if domainType == domain.DomainTypeCustomDomain && requestedDomain != "" && out.ResolvedPublicIP != "" {
		fmt.Fprintf(w, "\nCustom domain: %s\n", requestedDomain)
		fmt.Fprintf(w, "  Point its DNS A record to %s\n", out.ResolvedPublicIP)
		fmt.Fprintf(w, "  Otherwise the domain will not work after activation\n")
	}
}

func renderActivateSuccess(w io.Writer, runtime *domain.LocalWardRuntime) {
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
