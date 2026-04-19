package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"path/filepath"
	"time"

	jwtadapter "github.com/herewei/warded/internal/adapters/jwt"
	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/proxy"
	"github.com/herewei/warded/internal/adapters/storage"
	tlsadapter "github.com/herewei/warded/internal/adapters/tls"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/spf13/cobra"
)

func newServeCommand(version string) *cobra.Command {
	var (
		port           int
		dataDir      string
		baseDomain     string
		platformOrigin string
	)

	command := &cobra.Command{
		Use:   "serve",
		Short: "Run the identity-aware reverse proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			store := storage.NewJSONStore(dataDir)

			runtime, err := store.LoadWardRuntime(cmd.Context())
			if err != nil {
				return fmt.Errorf("serve: load ward runtime: %w", err)
			}
			if runtime == nil {
				return fmt.Errorf("serve: no ward runtime found — run 'warded activate' first")
			}
			if runtime.JWTSigningSecret == "" {
				return fmt.Errorf("serve: JWT signing secret not found — run 'warded activate' first")
			}
			platformURL, err := resolvePlatformOrigin(runtime.Site, baseDomain, platformOrigin)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}

			platformClient := platformapi.NewClient(platformURL, version)
			signer := jwtadapter.NewSigner(runtime.JWTSigningSecret)
			verifier := jwtadapter.NewVerifier(runtime.JWTSigningSecret)

			tlsProvider, err := newServeTLSProvider(cmd.Context(), runtime, dataDir, platformClient)
			if err != nil {
				return fmt.Errorf("cannot start: %w", err)
			}

			proxyConfig := proxy.ServerConfig{
				WardID:            runtime.WardID,
				Site:              runtime.Site,
				WardStatus:        runtime.WardStatus,
				Domain:            runtime.Domain,
				UpstreamPort:      runtime.UpstreamPort,
				PlatformAPI:       platformClient,
				JWTSigner:         signer,
				JWTVerifier:       verifier,
				TLSConfig:         tlsProvider.TLSConfig(),
				WebhookAllowPaths: runtime.WebhookAllowPaths,
			}

			service := application.ServeService{
				ConfigStore: store,
				ProxyRunner: proxy.NewRunner(proxyConfig),
			}
			if err := service.Execute(cmd.Context(), application.ServeInput{Port: port}); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "warded serve: exited")
			return nil
		},
	}

	command.Flags().IntVar(&port, "port", 443, "listen port for the proxy")
	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example dev.warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")

	return command
}

func newServeTLSProvider(ctx context.Context, runtime *domain.LocalWardRuntime, dataDir string, platformClient ports.PlatformAPI) (tlsadapter.Provider, error) {
	switch runtime.TLSMode {
	case domain.TLSModePlatformWildcard:
		if runtime.WardSecret == "" {
			return nil, fmt.Errorf("serve: ward secret not found — run 'warded activate' first")
		}
		if runtime.Domain == "" {
			return nil, fmt.Errorf("serve: domain not found — run 'warded activate' first")
		}

		tlsMaterial, err := platformClient.GetTLSMaterial(ctx, string(runtime.Site), runtime.WardSecret, runtime.WardID)
		var (
			initialCert         *tls.Certificate
			initialNotAfter     = timeZeroUTC()
			initialVersion      string
			initialRefreshAfter = 0
		)
		if err != nil {
			slog.Warn("serve: failed to fetch TLS certificate from platform, falling back to self-signed certificate", "domain", runtime.Domain, "error", err)
		} else {
			cert, certErr := tls.X509KeyPair([]byte(tlsMaterial.TLSCert), []byte(tlsMaterial.TLSKey))
			if certErr != nil {
				slog.Warn("serve: failed to load TLS certificate from platform, falling back to self-signed certificate", "domain", runtime.Domain, "error", certErr)
			} else {
				initialCert = &cert
				initialVersion = tlsMaterial.Version
				initialRefreshAfter = tlsMaterial.RefreshAfterSeconds
				if tlsMaterial.NotAfter != "" {
					if parsed, parseErr := time.Parse(time.RFC3339, tlsMaterial.NotAfter); parseErr == nil {
						initialNotAfter = parsed.UTC()
					}
				}
			}
		}

		return tlsadapter.NewPlatformCertProvider(ctx, runtime.Domain, initialCert, initialNotAfter, initialVersion, secondsToDuration(initialRefreshAfter), func(refreshCtx context.Context) (*ports.GetTLSMaterialResponse, error) {
			return platformClient.GetTLSMaterial(refreshCtx, string(runtime.Site), runtime.WardSecret, runtime.WardID)
		})
	case domain.TLSModeLocalACME:
		if runtime.DomainType != domain.DomainTypeCustomDomain {
			return nil, fmt.Errorf("serve: tls_mode %q requires domain_type %q", runtime.TLSMode, domain.DomainTypeCustomDomain)
		}
		return tlsadapter.NewACMEProvider(ctx, runtime.Domain, filepath.Join(dataDir, "certmagic"), 2*time.Minute)
	default:
		return nil, fmt.Errorf("serve: unsupported tls_mode %q", runtime.TLSMode)
	}
}

func timeZeroUTC() time.Time {
	return time.Time{}
}

func secondsToDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
