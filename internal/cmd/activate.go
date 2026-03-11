package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/adapters/ui"
	"github.com/herewei/warded/internal/adapters/upstream"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
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
		configDir       string
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

			store := storage.NewJSONStore(configDir)
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
				PublicBaseURL:   publicBaseURL,
			})
			if err != nil {
				return err
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
	command.Flags().StringVar(&configDir, "config-dir", defaultConfigDir(), "local config directory")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")
	command.Flags().BoolVar(&noWait, "no-wait", false, "print the activation link and exit without waiting")
	command.Flags().DurationVar(&waitTimeout, "wait-timeout", 3*time.Minute, "maximum time to wait for activation")
	command.Flags().DurationVar(&pollInterval, "poll-interval", 5*time.Second, "poll interval while waiting for activation")

	return command
}

func renderActivateSetup(w io.Writer, out *application.InitOutput, domainType domain.DomainType, requestedDomain string) {
	if out == nil {
		return
	}
	if out.ResolvedPublicIP != "" {
		fmt.Fprintf(w, "\n✓ Public IP: %s\n", out.ResolvedPublicIP)
	}
	fmt.Fprintf(w, "✓ Setup link ready\n")
	if out.IngressProbeStatus == "unreachable" {
		fmt.Fprintf(w, "⚠ Inbound probe failed. Check port 443, firewall, or security group.\n")
	}
	if out.IngressProbeStatus == "unknown" {
		fmt.Fprintf(w, "⚠ Inbound probe unavailable. Verify port 443 manually.\n")
	}

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
