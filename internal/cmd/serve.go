package cmd

import (
	"context"
	"crypto/tls"
	"errors"
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
		dataDir        string
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
				return fmt.Errorf("serve: no ward runtime found — run 'warded new --commit' first")
			}
			if runtime.JWTSigningSecret == "" {
				return fmt.Errorf("serve: JWT signing secret not found — run 'warded new --commit' first")
			}
			platformURL, err := resolvePlatformOrigin(runtime.Site, baseDomain, platformOrigin)
			if err != nil {
				return fmt.Errorf("serve: %w", err)
			}

			platformClient := platformapi.NewClient(platformURL, version)

			// Draft refresh/claim gate: check and finalize pending draft before starting
			if runtime.WardID == "" && runtime.WardDraftID != "" {
				draftService := application.DraftActivationService{
					ConfigStore: store,
					PlatformAPI: platformClient,
				}
				updatedRuntime, finalized, err := draftService.FinalizeIfConverted(cmd.Context())
				if err != nil {
					return fmt.Errorf("serve: failed to check draft status: %w", err)
				}
				if finalized && updatedRuntime != nil {
					runtime = updatedRuntime
				} else if runtime.WardID == "" {
					return fmt.Errorf("serve: ward is not activated yet (draft=%s). Run 'warded status' to check progress, or visit the activation URL to complete setup", runtime.WardDraftID)
				}
			}

			// Active ward status verification: must verify with platform before starting
			if runtime.WardID != "" {
				wardResp, err := platformClient.GetWard(cmd.Context(), string(runtime.Site), runtime.WardSecret, runtime.WardID)
				if err != nil {
					return fmt.Errorf("serve: cannot verify ward status with platform: %w", err)
				}

				switch wardResp.Status {
				case "active":
					// Update local state and continue starting
					runtime.WardStatus = domain.WardStatusActive
					if expiresAt, err := time.Parse(time.RFC3339, wardResp.ExpiresAt); err == nil {
						runtime.ExpiresAt = expiresAt
					}
					runtime.LastRefreshedAt = time.Now().UTC()
					runtime.UpdatedAt = time.Now().UTC()
					if saveErr := store.SaveWardRuntime(cmd.Context(), *runtime); saveErr != nil {
						return fmt.Errorf("serve: failed to save updated ward status: %w", saveErr)
					}
				case "expired":
					runtime.WardStatus = domain.WardStatusExpired
					runtime.LastRefreshedAt = time.Now().UTC()
					runtime.UpdatedAt = time.Now().UTC()
					if saveErr := store.SaveWardRuntime(cmd.Context(), *runtime); saveErr != nil {
						return fmt.Errorf("serve: failed to save expired ward status: %w", saveErr)
					}
					return fmt.Errorf("serve: ward has expired. Run 'warded new --commit' to create a new ward")
				case "suspended":
					runtime.WardStatus = domain.WardStatusSuspended
					runtime.LastRefreshedAt = time.Now().UTC()
					runtime.UpdatedAt = time.Now().UTC()
					if saveErr := store.SaveWardRuntime(cmd.Context(), *runtime); saveErr != nil {
						return fmt.Errorf("serve: failed to save suspended ward status: %w", saveErr)
					}
					return fmt.Errorf("serve: ward is suspended. Visit https://%s to resolve", runtime.Domain)
				case "deleted":
					runtime.WardStatus = domain.WardStatusDeleted
					runtime.LastRefreshedAt = time.Now().UTC()
					runtime.UpdatedAt = time.Now().UTC()
					if saveErr := store.SaveWardRuntime(cmd.Context(), *runtime); saveErr != nil {
						return fmt.Errorf("serve: failed to save deleted ward status: %w", saveErr)
					}
					return fmt.Errorf("serve: ward has been deleted. Run 'warded new --commit' to create a new ward")
				default:
					return fmt.Errorf("serve: ward status is %s, cannot start serve", wardResp.Status)
				}
			}

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
				UpstreamAddr:      runtime.UpstreamAddr,
				PlatformAPI:       platformClient,
				JWTSigner:         signer,
				JWTVerifier:       verifier,
				TLSConfig:         tlsProvider.TLSConfig(),
				WebhookAllowPaths: runtime.WebhookAllowPaths,
			}

			service := application.ServeService{
				ConfigStore: store,
				AuthProxy:   proxy.NewServer(proxyConfig),
			}
			serveCtx, cancelServe := context.WithCancel(cmd.Context())
			defer cancelServe()
			heartbeatErrs := startServeHeartbeat(serveCtx, cancelServe, store, platformClient, runtime, version)

			if err := service.Execute(serveCtx, application.ServeInput{}); err != nil {
				select {
				case heartbeatErr := <-heartbeatErrs:
					if heartbeatErr != nil {
						return heartbeatErr
					}
				default:
				}
				return err
			}
			select {
			case heartbeatErr := <-heartbeatErrs:
				if heartbeatErr != nil {
					return heartbeatErr
				}
			default:
			}
			fmt.Fprintln(cmd.OutOrStdout(), "warded serve: exited")
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example dev.warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")

	// Hide development/testing flags from help output
	_ = command.Flags().MarkHidden("platform-origin")

	return command
}

const (
	defaultHeartbeatInterval = 60 * time.Second
	minHeartbeatInterval     = 30 * time.Second
)

func startServeHeartbeat(ctx context.Context, cancelServe context.CancelFunc, store ports.LocalConfigStore, platformClient ports.PlatformAPI, runtime *domain.LocalWardRuntime, version string) <-chan error {
	errs := make(chan error, 1)
	if runtime == nil || runtime.WardID == "" || runtime.WardSecret == "" {
		close(errs)
		return errs
	}

	go func() {
		defer close(errs)
		next := time.Duration(0)
		for {
			if next > 0 {
				timer := time.NewTimer(next)
				select {
				case <-ctx.Done():
					timer.Stop()
					return
				case <-timer.C:
				}
			}

			interval, terminalErr := runServeHeartbeat(ctx, store, platformClient, runtime, version)
			if terminalErr != nil {
				select {
				case errs <- terminalErr:
				default:
				}
				cancelServe()
				return
			}
			if interval < minHeartbeatInterval {
				interval = minHeartbeatInterval
			}
			next = interval
		}
	}()
	return errs
}

func runServeHeartbeat(ctx context.Context, store ports.LocalConfigStore, platformClient ports.PlatformAPI, runtime *domain.LocalWardRuntime, version string) (time.Duration, error) {
	resp, err := platformClient.Heartbeat(ctx, string(runtime.Site), runtime.WardSecret, ports.HeartbeatRequest{
		WardID:       runtime.WardID,
		CLIVersion:   version,
		ProxyHealthy: true,
		ServeRunning: true,
		CheckResult:  "ok",
	})
	if err != nil {
		if ctx.Err() != nil {
			return defaultHeartbeatInterval, nil
		}
		var platformErr *ports.PlatformError
		if errors.As(err, &platformErr) && platformErr.Code == "credential_expired" {
			return defaultHeartbeatInterval, fmt.Errorf("serve: ward credential expired. Stop this node and run 'warded recover' or 'warded migrate' on the active node")
		}
		slog.Warn("serve: heartbeat failed", "ward_id", runtime.WardID, "error", err)
		return defaultHeartbeatInterval, nil
	}

	now := time.Now().UTC()
	if resp.WardStatus != "" {
		runtime.WardStatus = domain.WardStatus(resp.WardStatus)
	}
	if resp.ExpiresAt != "" {
		if expiresAt, parseErr := time.Parse(time.RFC3339, resp.ExpiresAt); parseErr == nil {
			runtime.ExpiresAt = expiresAt
		}
	}
	runtime.LastRefreshedAt = now
	runtime.UpdatedAt = now

	if saveErr := store.SaveWardRuntime(ctx, *runtime); saveErr != nil {
		return defaultHeartbeatInterval, fmt.Errorf("serve: failed to save heartbeat status: %w", saveErr)
	}

	if resp.RotationHint != "" {
		slog.Warn("serve: platform rotation hint", "hint", resp.RotationHint)
	}

	switch runtime.WardStatus {
	case "", domain.WardStatusActive:
		return heartbeatInterval(resp.NextHeartbeatAfter), nil
	case domain.WardStatusExpired:
		return defaultHeartbeatInterval, fmt.Errorf("serve: ward has expired. Run 'warded new --commit' to create a new ward")
	case domain.WardStatusSuspended:
		return defaultHeartbeatInterval, fmt.Errorf("serve: ward is suspended. Visit https://%s to resolve", runtime.Domain)
	case domain.WardStatusDeleted:
		return defaultHeartbeatInterval, fmt.Errorf("serve: ward has been deleted. Run 'warded new --commit' to create a new ward")
	default:
		return defaultHeartbeatInterval, fmt.Errorf("serve: ward status is %s, stopping serve", runtime.WardStatus)
	}
}

func heartbeatInterval(seconds int) time.Duration {
	if seconds <= 0 {
		return defaultHeartbeatInterval
	}
	return time.Duration(seconds) * time.Second
}

func newServeTLSProvider(ctx context.Context, runtime *domain.LocalWardRuntime, dataDir string, platformClient ports.PlatformAPI) (tlsadapter.Provider, error) {
	switch runtime.TLSMode {
	case domain.TLSModePlatformWildcard:
		if runtime.WardSecret == "" {
			return nil, fmt.Errorf("serve: ward secret not found — run 'warded new --commit' first")
		}
		if runtime.Domain == "" {
			return nil, fmt.Errorf("serve: domain not found — run 'warded new --commit' first")
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
