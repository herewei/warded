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
		wardIDs        []string
		serveAll       bool
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

			if serveAll && len(wardIDs) > 0 {
				return returnErr(fmt.Errorf("serve: --all cannot be combined with --ward-id"))
			}

			var runtimes []*domain.LocalWardRuntime
			if serveAll {
				allRuntimes, err := store.ListWardRuntimes(cmd.Context())
				if err != nil {
					return returnErr(fmt.Errorf("serve: list ward runtimes: %w", err))
				}
				for i := range allRuntimes {
					if allRuntimes[i].WardID == "" {
						continue
					}
					rt := allRuntimes[i]
					runtimes = append(runtimes, &rt)
				}
				if len(runtimes) == 0 {
					return returnErr(fmt.Errorf("serve: no committed ward runtime found"))
				}
			} else if len(wardIDs) > 0 {
				seen := map[string]bool{}
				for _, id := range wardIDs {
					id = strings.TrimSpace(id)
					if id == "" || seen[id] {
						continue
					}
					seen[id] = true
					rt, err := store.LoadRuntimeByID(cmd.Context(), id)
					if errors.Is(err, storage.ErrNotFound) {
						return returnErr(fmt.Errorf("serve: no ward runtime found for --ward-id %q", id))
					}
					if err != nil {
						return returnErr(fmt.Errorf("serve: load ward runtime: %w", err))
					}
					if rt == nil || rt.WardID == "" {
						return returnErr(fmt.Errorf("serve: --ward-id %q does not select a committed ward runtime", id))
					}
					runtimes = append(runtimes, rt)
				}
				if len(runtimes) == 0 {
					return returnErr(fmt.Errorf("serve: no ward runtime found"))
				}
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
				runtimes = append(runtimes, rt)
			}

			if len(runtimes) == 1 && runtimes[0].WardID == "" && runtimes[0].WardDraftID != "" {
				runtime := runtimes[0]
				platformURL, err := resolvePlatformOrigin(runtime.Site, baseDomain, platformOrigin)
				if err != nil {
					return returnErr(fmt.Errorf("serve: %w", err))
				}
				platformClient := platformapi.NewClient(platformURL, version)
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
					runtimes[0] = updatedRuntime
				} else if runtime.WardID == "" {
					return returnErr(fmt.Errorf("serve: ward is not activated yet (draft=%s). Run 'warded status' to check progress, or visit the activation URL to complete setup", runtime.WardDraftID))
				}
			}

			if err := validateSharedServeRuntimes(runtimes); err != nil {
				return returnErr(invalidArgumentError(err))
			}

			prepared, err := prepareServeRuntimes(cmd.Context(), store, runtimes, version, baseDomain, platformOrigin, dataDir, setHost)
			if err != nil {
				return returnErr(err)
			}
			defer func() {
				for _, preparedRuntime := range prepared {
					if preparedRuntime.upstreamManager != nil {
						_ = preparedRuntime.upstreamManager.Shutdown(context.Background())
					}
				}
			}()

			var authProxy ports.AuthProxy
			if len(prepared) == 1 {
				authProxy = prepared[0].server
			} else {
				servers := make([]*proxy.Server, 0, len(prepared))
				for _, preparedRuntime := range prepared {
					servers = append(servers, preparedRuntime.server)
				}
				multiServer, err := proxy.NewMultiServer(servers)
				if err != nil {
					return returnErr(err)
				}
				authProxy = multiServer
			}

			serveCtx, stopServe := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stopServe()
			heartbeatErrs := make([]<-chan error, 0, len(prepared))
			for _, preparedRuntime := range prepared {
				heartbeatErrs = append(heartbeatErrs, startServeHeartbeat(serveCtx, stopServe, store, preparedRuntime.platformClient, preparedRuntime.runtime, version, preparedRuntime.agentVerifier))
			}

			if wantsJSON(cmd) {
				writeJSON(cmd.OutOrStdout(), serveStartedEnvelope(prepared))
			} else {
				fmt.Fprintf(cmd.OutOrStdout(), "warded serve: started at %s for %d ward(s)\n", formatListenForDisplay(prepared[0].runtime), len(prepared))
			}

			if err := authProxy.Serve(serveCtx, listenAddrFromRuntime(prepared[0].runtime)); err != nil {
				if heartbeatErr := firstHeartbeatErr(heartbeatErrs); heartbeatErr != nil {
					return heartbeatErr
				}
				return err
			}
			if heartbeatErr := firstHeartbeatErr(heartbeatErrs); heartbeatErr != nil {
				return heartbeatErr
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
	command.Flags().StringArrayVar(&wardIDs, "ward-id", nil, "select one or more wards by ID when multiple local wards exist")
	command.Flags().BoolVar(&serveAll, "all", false, "serve all committed local wards in one listener group")
	command.Flags().StringVar(&setHost, "set-host", "", "override the Host header sent to the upstream server")

	// Hide development/testing flags from help output
	_ = command.Flags().MarkHidden("platform-origin")

	return command
}

type preparedServeRuntime struct {
	runtime         *domain.LocalWardRuntime
	platformClient  *platformapi.Client
	agentVerifier   *platformjwt.Verifier
	upstreamManager *upstream.ProcessManager
	server          *proxy.Server
}

func serveStartedEnvelope(prepared []preparedServeRuntime) Envelope {
	var runtime *domain.LocalWardRuntime
	if len(prepared) > 0 {
		runtime = prepared[0].runtime
	}
	wards := make([]map[string]any, 0, len(prepared))
	for _, item := range prepared {
		wards = append(wards, map[string]any{
			"ward_id":       item.runtime.WardID,
			"domain":        item.runtime.Domain,
			"upstream_addr": item.runtime.UpstreamAddr,
			"upstream_mode": string(item.runtime.UpstreamMode),
		})
	}
	return Envelope{
		OK:      true,
		Command: "serve",
		Code:    "serve_started",
		Event:   "started",
		Data: map[string]any{
			"listen": formatListenForDisplay(runtime),
			"domain": firstServeDomain(prepared),
			"listener": map[string]any{
				"listen_host":    runtime.ListenHost,
				"listen_port":    runtime.ListenPort,
				"ingress_family": string(runtime.IngressFamily),
				"ingress_mode":   string(effectiveIngressMode(runtime)),
				"serve_tls":      effectiveServeTLS(runtime),
				"public_port":    effectivePublicPort(runtime),
			},
			"wards": wards,
		},
	}
}

func firstServeDomain(prepared []preparedServeRuntime) string {
	if len(prepared) == 0 || prepared[0].runtime == nil {
		return ""
	}
	return prepared[0].runtime.Domain
}

func validateSharedServeRuntimes(runtimes []*domain.LocalWardRuntime) error {
	if len(runtimes) == 0 {
		return fmt.Errorf("serve: no ward runtime found")
	}
	first := runtimes[0]
	domains := map[string]string{}
	for _, runtime := range runtimes {
		if runtime == nil {
			return fmt.Errorf("serve: nil ward runtime")
		}
		if runtime.WardID == "" {
			return fmt.Errorf("serve: runtime for %s is not a committed ward", runtime.Domain)
		}
		if strings.TrimSpace(runtime.Domain) == "" {
			return fmt.Errorf("serve: ward %s has no domain", runtime.WardID)
		}
		if runtime.JWTSigningSecret == "" {
			return fmt.Errorf("serve: JWT signing secret not found for ward %s", runtime.WardID)
		}
		if !sameListenerGroup(first, runtime) {
			return fmt.Errorf("serve: selected wards span multiple listener groups: %s uses %s, %s uses %s", first.WardID, formatListenForDisplay(first), runtime.WardID, formatListenForDisplay(runtime))
		}
		if effectiveIngressMode(first) != effectiveIngressMode(runtime) || effectiveServeTLS(first) != effectiveServeTLS(runtime) {
			return fmt.Errorf("serve: selected wards mix ingress modes: %s uses %s, %s uses %s", first.WardID, effectiveIngressMode(first), runtime.WardID, effectiveIngressMode(runtime))
		}
		if err := validateRuntimeIngressForServe(runtime); err != nil {
			return err
		}
		domainKey := strings.ToLower(strings.TrimSpace(runtime.Domain))
		if existingWardID := domains[domainKey]; existingWardID != "" {
			return fmt.Errorf("serve: duplicate domain %s for wards %s and %s", runtime.Domain, existingWardID, runtime.WardID)
		}
		domains[domainKey] = runtime.WardID
	}
	return nil
}

func sameListenerGroup(a, b *domain.LocalWardRuntime) bool {
	return effectiveListenHost(a) == effectiveListenHost(b) &&
		effectiveListenPort(a) == effectiveListenPort(b) &&
		effectiveIngressFamily(a) == effectiveIngressFamily(b)
}

func normalizeServeRuntimeIngress(runtime *domain.LocalWardRuntime) {
	if runtime == nil {
		return
	}
	if runtime.IngressMode == "" {
		runtime.IngressMode = domain.IngressModeStandalone
	}
	if runtime.PublicPort == 0 {
		if runtime.IngressMode == domain.IngressModeStandalone {
			runtime.PublicPort = effectiveListenPort(runtime)
		} else {
			runtime.PublicPort = 443
		}
	}
	runtime.ServeTLS = runtime.IngressMode != domain.IngressModeBehindProxy
	if runtime.IngressMode == domain.IngressModeStandalone {
		runtime.PublicPort = effectiveListenPort(runtime)
	}
}

func effectiveIngressMode(runtime *domain.LocalWardRuntime) domain.IngressMode {
	if runtime == nil || runtime.IngressMode == "" {
		return domain.IngressModeStandalone
	}
	return runtime.IngressMode
}

func effectiveServeTLS(runtime *domain.LocalWardRuntime) bool {
	return effectiveIngressMode(runtime) != domain.IngressModeBehindProxy
}

func effectivePublicPort(runtime *domain.LocalWardRuntime) int {
	if runtime == nil {
		return 443
	}
	if runtime.PublicPort > 0 {
		return runtime.PublicPort
	}
	if effectiveIngressMode(runtime) == domain.IngressModeStandalone {
		return effectiveListenPort(runtime)
	}
	return 443
}

func validateRuntimeIngressForServe(runtime *domain.LocalWardRuntime) error {
	normalizeServeRuntimeIngress(runtime)
	if err := validateIngressModeConfig(effectiveIngressMode(runtime), false, effectivePublicPort(runtime), effectiveListenPort(runtime), effectiveListenHost(runtime), runtime.TrustedProxyCIDRs); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}

func prepareServeRuntimes(ctx context.Context, store ports.LocalConfigStore, runtimes []*domain.LocalWardRuntime, version, baseDomain, platformOrigin, dataDir, setHost string) ([]preparedServeRuntime, error) {
	prepared := make([]preparedServeRuntime, 0, len(runtimes))
	for _, runtime := range runtimes {
		item, err := prepareServeRuntime(ctx, store, runtime, version, baseDomain, platformOrigin, dataDir, setHost)
		if err != nil {
			for _, preparedRuntime := range prepared {
				if preparedRuntime.upstreamManager != nil {
					_ = preparedRuntime.upstreamManager.Shutdown(context.Background())
				}
			}
			return nil, err
		}
		prepared = append(prepared, item)
	}
	return prepared, nil
}

func prepareServeRuntime(ctx context.Context, store ports.LocalConfigStore, runtime *domain.LocalWardRuntime, version, baseDomain, platformOrigin, dataDir, setHost string) (preparedServeRuntime, error) {
	normalizeServeRuntimeIngress(runtime)
	if runtime.JWTSigningSecret == "" {
		return preparedServeRuntime{}, fmt.Errorf("serve: JWT signing secret not found for ward %s", runtime.WardID)
	}
	platformURL, err := resolvePlatformOrigin(runtime.Site, baseDomain, platformOrigin)
	if err != nil {
		return preparedServeRuntime{}, fmt.Errorf("serve: %w", err)
	}
	platformClient := platformapi.NewClient(platformURL, version)
	if err := verifyActiveServeRuntime(ctx, store, platformClient, runtime); err != nil {
		return preparedServeRuntime{}, err
	}

	platformIssuer, err := resolvePublicPlatformBaseURL(runtime.Site, baseDomain)
	if err != nil {
		return preparedServeRuntime{}, fmt.Errorf("serve: resolve platform issuer: %w", err)
	}
	agentVerifier := platformjwt.NewVerifierWithIssuer(runtime.Site, runtime.WardID, runtime.PlatformJWTPublicKeys, platformIssuer)

	upstreamAddr := effectiveUpstreamAddr(runtime)
	runtime.UpstreamAddr = upstreamAddr

	var upstreamMgr *upstream.ProcessManager
	if runtime.UpstreamMode == domain.UpstreamModeManaged {
		if strings.TrimSpace(runtime.UpstreamCommand) == "" {
			return preparedServeRuntime{}, fmt.Errorf("upstream_command is required when upstream_mode is managed")
		}
		upstreamMgr = upstream.NewProcessManager()
		runCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		_, err := upstreamMgr.EnsureRunning(runCtx, upstreamAddr, runtime.UpstreamCommand)
		cancel()
		if err != nil {
			slog.Warn("managed upstream not ready at startup, will retry on first request", "ward_id", runtime.WardID, "error", err)
		}
	}

	var tlsConfig *tls.Config
	if effectiveServeTLS(runtime) {
		tlsProvider, err := newServeTLSProvider(ctx, runtime, dataDir, platformClient)
		if err != nil {
			if upstreamMgr != nil {
				_ = upstreamMgr.Shutdown(context.Background())
			}
			return preparedServeRuntime{}, fmt.Errorf("cannot start: %w", err)
		}
		tlsConfig = tlsProvider.TLSConfig()
	}

	server := proxy.NewServer(proxy.ServerConfig{
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
		IngressMode:     effectiveIngressMode(runtime),
		ServeTLS:        effectiveServeTLS(runtime),
		PublicPort:      effectivePublicPort(runtime),
		PreserveHost:    effectiveIngressMode(runtime) == domain.IngressModeBehindProxy,
		AuthExchange:    platformClient,
		JWTSigner:       jwtadapter.NewSigner(runtime.JWTSigningSecret),
		JWTVerifier:     jwtadapter.NewVerifier(runtime.JWTSigningSecret),
		AgentVerifier:   agentVerifier,
		TLSConfig:       tlsConfig,
		AuthWhitelist:   runtime.AuthWhitelist,
	})

	return preparedServeRuntime{
		runtime:         runtime,
		platformClient:  platformClient,
		agentVerifier:   agentVerifier,
		upstreamManager: upstreamMgr,
		server:          server,
	}, nil
}

func verifyActiveServeRuntime(ctx context.Context, store ports.LocalConfigStore, platformClient ports.WardRuntimeAPI, runtime *domain.LocalWardRuntime) error {
	if runtime.WardID == "" {
		return fmt.Errorf("serve: ward is not activated yet")
	}
	wardResp, err := platformClient.GetWard(ctx, string(runtime.Site), runtime.WardSecret, runtime.WardID)
	if err != nil {
		return fmt.Errorf("serve: cannot verify ward status with platform: %w", err)
	}

	now := time.Now().UTC()
	switch wardResp.Status {
	case "active":
		runtime.WardStatus = domain.WardStatusActive
		if expiresAt, err := time.Parse(time.RFC3339, wardResp.ExpiresAt); err == nil {
			runtime.ExpiresAt = expiresAt
		}
		if wardResp.PlatformJWTPublicKeys != nil {
			runtime.PlatformJWTPublicKeys = wardResp.PlatformJWTPublicKeys
		}
		if wardResp.IngressMode != "" {
			runtime.IngressMode = domain.IngressMode(wardResp.IngressMode)
		}
		if wardResp.PublicPort > 0 {
			runtime.PublicPort = wardResp.PublicPort
		}
		runtime.ServeTLS = runtime.IngressMode != domain.IngressModeBehindProxy
		if wardResp.TrustedProxyCIDRs != nil {
			runtime.TrustedProxyCIDRs = append([]string(nil), wardResp.TrustedProxyCIDRs...)
		}
		runtime.LastRefreshedAt = now
		runtime.UpdatedAt = now
		if saveErr := store.SaveWardRuntime(ctx, *runtime); saveErr != nil {
			return fmt.Errorf("serve: failed to save updated ward status: %w", saveErr)
		}
		return nil
	case "expired":
		runtime.WardStatus = domain.WardStatusExpired
		runtime.LastRefreshedAt = now
		runtime.UpdatedAt = now
		if saveErr := store.SaveWardRuntime(ctx, *runtime); saveErr != nil {
			return fmt.Errorf("serve: failed to save expired ward status: %w", saveErr)
		}
		return fmt.Errorf("serve: ward %s has expired. Run 'warded new --commit' to create a new ward", runtime.WardID)
	case "suspended":
		runtime.WardStatus = domain.WardStatusSuspended
		runtime.LastRefreshedAt = now
		runtime.UpdatedAt = now
		if saveErr := store.SaveWardRuntime(ctx, *runtime); saveErr != nil {
			return fmt.Errorf("serve: failed to save suspended ward status: %w", saveErr)
		}
		return fmt.Errorf("serve: ward %s is suspended. Visit https://%s to resolve", runtime.WardID, runtime.Domain)
	case "deleted":
		runtime.WardStatus = domain.WardStatusDeleted
		runtime.LastRefreshedAt = now
		runtime.UpdatedAt = now
		if saveErr := store.SaveWardRuntime(ctx, *runtime); saveErr != nil {
			return fmt.Errorf("serve: failed to save deleted ward status: %w", saveErr)
		}
		return fmt.Errorf("serve: ward %s has been deleted. Run 'warded new --commit' to create a new ward", runtime.WardID)
	default:
		return fmt.Errorf("serve: ward %s status is %s, cannot start serve", runtime.WardID, wardResp.Status)
	}
}

func firstHeartbeatErr(errs []<-chan error) error {
	for _, errCh := range errs {
		select {
		case heartbeatErr := <-errCh:
			if heartbeatErr != nil {
				return heartbeatErr
			}
		default:
		}
	}
	return nil
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

func effectiveListenHost(runtime *domain.LocalWardRuntime) string {
	if runtime != nil && strings.TrimSpace(runtime.ListenHost) != "" {
		return strings.TrimSpace(runtime.ListenHost)
	}
	return "0.0.0.0"
}

func effectiveListenPort(runtime *domain.LocalWardRuntime) int {
	if runtime != nil && runtime.ListenPort > 0 {
		return runtime.ListenPort
	}
	return 443
}

func effectiveIngressFamily(runtime *domain.LocalWardRuntime) domain.IngressFamily {
	if runtime != nil && runtime.IngressFamily != "" {
		return runtime.IngressFamily
	}
	return domain.IngressFamilyIPv4
}
