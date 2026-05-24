package cmd

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	jwtadapter "github.com/herewei/warded/internal/adapters/jwt"
	"github.com/herewei/warded/internal/adapters/platformapi"
	"github.com/herewei/warded/internal/adapters/platformjwt"
	"github.com/herewei/warded/internal/adapters/proxy"
	"github.com/herewei/warded/internal/adapters/storage"
	tlsadapter "github.com/herewei/warded/internal/adapters/tls"
	"github.com/herewei/warded/internal/adapters/upstream"
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
		wardID         string
		setHost        string
	)

	command := &cobra.Command{
		Use:   "serve",
		Short: "Run the identity-aware reverse proxy",
		RunE: func(cmd *cobra.Command, args []string) error {
			returnErr := func(err error) error {
				if wantsJSON(cmd) {
					writeJSONError(cmd, "serve", "", err)
				}
				return err
			}

			store := storage.NewJSONStore(dataDir)

			var runtime *domain.LocalWardRuntime
			if wardID != "" {
				rt, err := store.LoadRuntimeByID(cmd.Context(), wardID)
				if errors.Is(err, storage.ErrNotFound) {
					err := fmt.Errorf("serve: no ward runtime found")
					if wantsJSON(cmd) {
						writeJSONError(cmd, "serve", "", err)
					}
					return err
				}
				if err != nil {
					if wantsJSON(cmd) {
						writeJSONError(cmd, "serve", "", fmt.Errorf("serve: load ward runtime: %w", err))
					}
					return fmt.Errorf("serve: load ward runtime: %w", err)
				}
				runtime = rt
			} else {
				rt, err := store.LoadWardRuntime(cmd.Context())
				if errors.Is(err, storage.ErrMultipleRuntimes) {
					cmd.SilenceUsage = true
					if wantsJSON(cmd) {
						env := Envelope{
							OK:      false,
							Command: "serve",
							Error:   &ErrorDetail{Code: "invalid_argument", RetryAfterSeconds: nil},
						}
						statusSvc := application.StatusService{ConfigStore: store}
						if listOut, listErr := statusSvc.ListRuntimes(cmd.Context()); listErr == nil {
							env.Data = map[string]any{"runtimes": runtimeListDTO(listOut.Runtimes)}
						}
						writeJSON(cmd.OutOrStdout(), env)
						return fmt.Errorf("serve: multiple local wards found — use --ward-id <id> to select one")
					}
					statusSvc := application.StatusService{ConfigStore: store}
					if listOut, listErr := statusSvc.ListRuntimes(cmd.Context()); listErr == nil {
						renderServeMultiRuntimeList(cmd.OutOrStdout(), listOut.Runtimes, dataDir)
					}
					return fmt.Errorf("serve: multiple local wards found — use --ward-id <id> to select one")
				}
				if err != nil {
					if wantsJSON(cmd) {
						writeJSONError(cmd, "serve", "", fmt.Errorf("serve: load ward runtime: %w", err))
					}
					return fmt.Errorf("serve: load ward runtime: %w", err)
				}
				if rt == nil {
					err := fmt.Errorf("serve: no ward runtime found")
					if wantsJSON(cmd) {
						writeJSONError(cmd, "serve", "", err)
					}
					return err
				}
				runtime = rt
			}
			if runtime.JWTSigningSecret == "" {
				err := fmt.Errorf("serve: JWT signing secret not found")
				if wantsJSON(cmd) {
					writeJSON(cmd.OutOrStdout(), Envelope{
						OK:      false,
						Command: "serve",
						Error: &ErrorDetail{
							Code:              "config_corrupted",
							RetryAfterSeconds: nil,
						},
					})
				}
				return err
			}
			platformURL, err := resolvePlatformOrigin(runtime.Site, baseDomain, platformOrigin)
			if err != nil {
				return returnErr(fmt.Errorf("serve: %w", err))
			}

			platformClient := platformapi.NewClient(platformURL, version)

			// Draft refresh/claim gate: check and finalize pending draft before starting
			if runtime.WardID == "" && runtime.WardDraftID != "" {
				draftService := application.DraftActivationService{
					ConfigStore: store,
					DraftAPI:    platformClient,
					RuntimeAPI:  platformClient,
				}
				updatedRuntime, finalized, err := draftService.FinalizeIfConverted(cmd.Context())
				if err != nil {
					return returnErr(fmt.Errorf("serve: failed to check draft status: %w", err))
				}
				if finalized && updatedRuntime != nil {
					runtime = updatedRuntime
				} else if runtime.WardID == "" {
					return returnErr(fmt.Errorf("serve: ward is not activated yet (draft=%s). Run 'warded status' to check progress, or visit the activation URL to complete setup", runtime.WardDraftID))
				}
			}

			// Active ward status verification: must verify with platform before starting
			if runtime.WardID != "" {
				wardResp, err := platformClient.GetWard(cmd.Context(), string(runtime.Site), runtime.WardSecret, runtime.WardID)
				if err != nil {
					return returnErr(fmt.Errorf("serve: cannot verify ward status with platform: %w", err))
				}

				switch wardResp.Status {
				case "active":
					// Update local state and continue starting
					runtime.WardStatus = domain.WardStatusActive
					if expiresAt, err := time.Parse(time.RFC3339, wardResp.ExpiresAt); err == nil {
						runtime.ExpiresAt = expiresAt
					}
					if wardResp.PlatformJWTPublicKeys != nil {
						runtime.PlatformJWTPublicKeys = wardResp.PlatformJWTPublicKeys
					}
					runtime.LastRefreshedAt = time.Now().UTC()
					runtime.UpdatedAt = time.Now().UTC()
					if saveErr := store.SaveWardRuntime(cmd.Context(), *runtime); saveErr != nil {
						return returnErr(fmt.Errorf("serve: failed to save updated ward status: %w", saveErr))
					}
				case "expired":
					runtime.WardStatus = domain.WardStatusExpired
					runtime.LastRefreshedAt = time.Now().UTC()
					runtime.UpdatedAt = time.Now().UTC()
					if saveErr := store.SaveWardRuntime(cmd.Context(), *runtime); saveErr != nil {
						return returnErr(fmt.Errorf("serve: failed to save expired ward status: %w", saveErr))
					}
					return returnErr(fmt.Errorf("serve: ward has expired. Run 'warded new --commit' to create a new ward"))
				case "suspended":
					runtime.WardStatus = domain.WardStatusSuspended
					runtime.LastRefreshedAt = time.Now().UTC()
					runtime.UpdatedAt = time.Now().UTC()
					if saveErr := store.SaveWardRuntime(cmd.Context(), *runtime); saveErr != nil {
						return returnErr(fmt.Errorf("serve: failed to save suspended ward status: %w", saveErr))
					}
					return returnErr(fmt.Errorf("serve: ward is suspended. Visit https://%s to resolve", runtime.Domain))
				case "deleted":
					runtime.WardStatus = domain.WardStatusDeleted
					runtime.LastRefreshedAt = time.Now().UTC()
					runtime.UpdatedAt = time.Now().UTC()
					if saveErr := store.SaveWardRuntime(cmd.Context(), *runtime); saveErr != nil {
						return returnErr(fmt.Errorf("serve: failed to save deleted ward status: %w", saveErr))
					}
					return returnErr(fmt.Errorf("serve: ward has been deleted. Run 'warded new --commit' to create a new ward"))
				default:
					return returnErr(fmt.Errorf("serve: ward status is %s, cannot start serve", wardResp.Status))
				}
			}

			signer := jwtadapter.NewSigner(runtime.JWTSigningSecret)
			verifier := jwtadapter.NewVerifier(runtime.JWTSigningSecret)
			platformIssuer, err := resolvePublicPlatformBaseURL(runtime.Site, baseDomain)
			if err != nil {
				return returnErr(fmt.Errorf("serve: resolve platform issuer: %w", err))
			}
			agentVerifier := platformjwt.NewVerifierWithIssuer(runtime.Site, runtime.WardID, runtime.PlatformJWTPublicKeys, platformIssuer)

			upstreamAddr := effectiveUpstreamAddr(runtime)
			runtime.UpstreamAddr = upstreamAddr

			var upstreamMgr *upstream.ProcessManager
			if runtime.UpstreamMode == domain.UpstreamModeManaged {
				if strings.TrimSpace(runtime.UpstreamCommand) == "" {
					return returnErr(fmt.Errorf("upstream_command is required when upstream_mode is managed"))
				}
				upstreamMgr = upstream.NewProcessManager()
				ctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
				_, err := upstreamMgr.EnsureRunning(ctx, upstreamAddr, runtime.UpstreamCommand)
				cancel()
				if err != nil {
					slog.Warn("managed upstream not ready at startup, will retry on first request", "error", err)
				}
				defer func() {
					_ = upstreamMgr.Shutdown(context.Background())
				}()
			}

			tlsProvider, err := newServeTLSProvider(cmd.Context(), runtime, dataDir, platformClient)
			if err != nil {
				return returnErr(fmt.Errorf("cannot start: %w", err))
			}

			proxyConfig := proxy.ServerConfig{
				WardID:          runtime.WardID,
				Site:            runtime.Site,
				WardStatus:      runtime.WardStatus,
				Domain:          runtime.Domain,
				UpstreamAddr:    upstreamAddr,
				UpstreamMode:    runtime.UpstreamMode,
				UpstreamCommand: runtime.UpstreamCommand,
				UpstreamManager: upstreamMgr,
				SetHost:         setHost,
				PlatformOrigin:  platformOrigin,
				ExpectedIssuer:  platformIssuer,
				AuthExchange:    platformClient,
				JWTSigner:       signer,
				JWTVerifier:     verifier,
				AgentVerifier:   agentVerifier,
				TLSConfig:       tlsProvider.TLSConfig(),
				AuthWhitelist:   runtime.AuthWhitelist,
			}

			service := application.ServeService{
				ConfigStore: store,
				AuthProxy:   proxy.NewServer(proxyConfig),
			}
			serveCtx, stopServe := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stopServe()
			heartbeatErrs := startServeHeartbeat(serveCtx, stopServe, store, platformClient, runtime, version, agentVerifier)

			if wantsJSON(cmd) {
				writeJSON(cmd.OutOrStdout(), serveStartedEnvelope(runtime))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "warded serve: started at %s for %s\n", formatListenForDisplay(runtime), runtime.Domain)
			}

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
			if !wantsJSON(cmd) {
				fmt.Fprintln(cmd.OutOrStdout(), "warded serve: exited")
			}
			return nil
		},
	}

	command.Flags().StringVar(&dataDir, "data-dir", defaultDataDir(), "local data directory")
	command.Flags().StringVar(&baseDomain, "base-domain", "", "override the platform base domain, for example dev.warded.me")
	command.Flags().StringVar(&platformOrigin, "platform-origin", "", "development/testing override for platform API origin only, for example http://127.0.0.1:8080")
	command.Flags().StringVar(&wardID, "ward-id", "", "select a specific ward by its ID when multiple local wards exist")
	command.Flags().StringVar(&setHost, "set-host", "", "override the Host header sent to the upstream server")

	// Hide development/testing flags from help output
	_ = command.Flags().MarkHidden("platform-origin")

	return command
}

func serveStartedEnvelope(runtime *domain.LocalWardRuntime) Envelope {
	return Envelope{
		OK:      true,
		Command: "serve",
		Event:   "started",
		Data: map[string]any{
			"listen": formatListenForDisplay(runtime),
			"domain": runtime.Domain,
		},
	}
}

const (
	defaultHeartbeatInterval = 60 * time.Second
	minHeartbeatInterval     = 30 * time.Second
)

type agentTokenCache interface {
	UpdatePublicKeys([]domain.PlatformJWTPublicKey)
	UpdateValidTokens([]ports.ValidAgentToken)
}

func startServeHeartbeat(ctx context.Context, cancelServe context.CancelFunc, store ports.LocalConfigStore, platformClient ports.WardRuntimeAPI, runtime *domain.LocalWardRuntime, version string, agentTokens agentTokenCache) <-chan error {
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

			interval, terminalErr := runServeHeartbeat(ctx, store, platformClient, runtime, version, agentTokens)
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

func runServeHeartbeat(ctx context.Context, store ports.LocalConfigStore, platformClient ports.WardRuntimeAPI, runtime *domain.LocalWardRuntime, version string, agentTokens agentTokenCache) (time.Duration, error) {
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
	if resp.PlatformJWTPublicKeys != nil {
		runtime.PlatformJWTPublicKeys = resp.PlatformJWTPublicKeys
		if agentTokens != nil {
			agentTokens.UpdatePublicKeys(resp.PlatformJWTPublicKeys)
		}
	}
	if resp.ValidAgentTokens != nil && agentTokens != nil {
		agentTokens.UpdateValidTokens(resp.ValidAgentTokens)
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

func newServeTLSProvider(ctx context.Context, runtime *domain.LocalWardRuntime, dataDir string, platformClient ports.TLSMaterialAPI) (tlsadapter.Provider, error) {
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

func renderServeMultiRuntimeList(w io.Writer, runtimes []application.RuntimeSummary, dataDir string) {
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
		fmt.Fprintf(w, "  %-4d  %-16s  %-26s  %-15s  %s\n", rt.Index, string(rt.Kind), dom, runtimeListStatus(rt), runtimeListID(rt))
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Next:")
	fmt.Fprintln(w, "  Use `warded serve --ward-id <id>` to select which ward to serve.")
}

func effectiveUpstreamAddr(runtime *domain.LocalWardRuntime) string {
	if runtime == nil {
		return "127.0.0.1:18789"
	}
	if addr := strings.TrimSpace(runtime.UpstreamAddr); addr != "" {
		return addr
	}
	if runtime.UpstreamPort > 0 {
		return fmt.Sprintf("127.0.0.1:%d", runtime.UpstreamPort)
	}
	return "127.0.0.1:18789"
}
