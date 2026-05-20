package application

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

// Typed errors for preflight checks
var (
	ErrDataDirNotWritable   = errors.New("data directory is not writable")
	ErrUpstreamUnreachable  = errors.New("upstream port is unreachable")
	ErrListenPortOccupied   = errors.New("listen port is occupied")
	ErrListenPortPermission = errors.New("listen port requires additional privileges")
)

type NewService struct {
	ConfigStore   ports.LocalConfigStore
	DraftAPI      ports.WardDraftAPI
	UpstreamCheck ports.UpstreamChecker
}

type NewInput struct {
	Site            domain.Site
	Spec            domain.Spec
	BillingMode     domain.BillingMode
	DomainType      domain.DomainType
	RequestedDomain string
	UpstreamAddr    string
	ListenPort      int
	ListenHost      string
	IngressFamily   domain.IngressFamily
	ProbeChallenge  string
	PublicBaseURL   string
}

type NewOutput struct {
	WardDraftID        string
	DraftAction        string
	Status             string
	ActivationURL      string
	DomainCheckStatus  string
	ResolvedPublicIP   string
	IngressProbeStatus string
	RequestedDomain    string
}

func (s NewService) Execute(ctx context.Context, input NewInput) (*NewOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("new service: config store is required")
	}
	if s.DraftAPI == nil {
		return nil, fmt.Errorf("new service: draft API is required")
	}
	if s.UpstreamCheck == nil {
		return nil, fmt.Errorf("new service: upstream checker is required")
	}

	upstreamAddr := input.UpstreamAddr
	if upstreamAddr == "" {
		upstreamAddr = defaultUpstreamAddr()
	}
	listenPort := input.ListenPort
	if listenPort <= 0 {
		listenPort = 443
	}
	listenHost := input.ListenHost
	if listenHost == "" {
		listenHost = "0.0.0.0"
	}
	ingressFamily := input.IngressFamily
	if ingressFamily == "" {
		ingressFamily = domain.IngressFamilyIPv4
	}
	upstreamPort := extractPortFromAddr(upstreamAddr)
	if err := validateSpecDomainCombination(input.Spec, input.DomainType, input.RequestedDomain); err != nil {
		return nil, err
	}
	if err := validateBillingMode(input.BillingMode); err != nil {
		return nil, err
	}
	tlsMode, err := domain.TLSModeForDomainType(input.DomainType)
	if err != nil {
		return nil, fmt.Errorf("init service: %w", err)
	}

	runtime, err := s.ConfigStore.LoadPendingRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		runtime = &domain.LocalWardRuntime{
			Site:          input.Site,
			WardStatus:    domain.WardStatusInitializing,
			ListenPort:    listenPort,
			ListenHost:    listenHost,
			IngressFamily: ingressFamily,
		}
	}
	if runtime.WardDraftID != "" && runtime.WardDraftSecret != "" {
		draftSite := runtime.Site
		if draftSite == "" {
			draftSite = input.Site
		}
		draft, err := s.DraftAPI.GetWardDraftStatus(ctx, string(draftSite), draftSecretChallenge(runtime.WardDraftSecret), runtime.WardDraftID)
		if err != nil {
			if shouldCreateFreshDraft(err) {
				clearDraftState(runtime)
				runtime.UpdatedAt = time.Now().UTC()
				if saveErr := s.ConfigStore.SavePendingRuntime(ctx, *runtime); saveErr != nil {
					return nil, saveErr
				}
			} else {
				return nil, err
			}
		} else if draft != nil {
			switch draft.Status {
			case "expired", "failed":
				clearDraftState(runtime)
				runtime.UpdatedAt = time.Now().UTC()
				if saveErr := s.ConfigStore.SavePendingRuntime(ctx, *runtime); saveErr != nil {
					return nil, saveErr
				}
			}
		}
	}
	if runtime.JWTSigningSecret == "" {
		jwtSecret, err := randomJWTSecret()
		if err != nil {
			return nil, fmt.Errorf("init service: generate JWT signing secret: %w", err)
		}
		runtime.JWTSigningSecret = jwtSecret
	}
	if runtime.WardDraftSecret == "" {
		draftSecret, err := randomDraftSecret()
		if err != nil {
			return nil, fmt.Errorf("init service: generate draft secret: %w", err)
		}
		runtime.WardDraftSecret = draftSecret
	}

	slog.Info("init: checking upstream reachability", "addr", upstreamAddr)
	if err := s.UpstreamCheck.Check(ctx, upstreamAddr); err != nil {
		return nil, err
	}
	slog.Info("init: upstream reachable", "addr", upstreamAddr)

	slog.Info("init: creating ward draft", "site", input.Site, "spec", input.Spec, "billing_mode", input.BillingMode)

	requestedDomainForRequest := input.RequestedDomain
	if input.Spec == domain.SpecStarter {
		requestedDomainForRequest = ""
	}

	req := ports.CreateWardDraftRequest{
		Site:                 string(input.Site),
		Mode:                 "new",
		Spec:                 string(input.Spec),
		BillingMode:          string(input.BillingMode),
		DomainType:           string(input.DomainType),
		RequestedDomain:      requestedDomainForRequest,
		UpstreamAddr:         upstreamAddr,
		UpstreamPort:         upstreamPort,
		ListenPort:           listenPort,
		ListenHost:           listenHost,
		IngressFamily:        string(ingressFamily),
		ProbeChallenge:       input.ProbeChallenge,
		DraftSecretChallenge: draftSecretChallenge(runtime.WardDraftSecret),
	}
	existingDraftID := runtime.WardDraftID
	resp, err := s.DraftAPI.CreateWardDraft(ctx, req)
	if err != nil {
		if shouldCreateFreshDraft(err) {
			slog.Info("init: draft challenge expired during create; retrying with fresh draft secret",
				"site", input.Site,
				"existing_draft_id", runtime.WardDraftID != "",
			)
			clearDraftState(runtime)
			draftSecret, secretErr := randomDraftSecret()
			if secretErr != nil {
				return nil, fmt.Errorf("init service: generate fresh draft secret: %w", secretErr)
			}
			runtime.WardDraftSecret = draftSecret
			runtime.UpdatedAt = time.Now().UTC()
			if saveErr := s.ConfigStore.SavePendingRuntime(ctx, *runtime); saveErr != nil {
				return nil, saveErr
			}
			req.DraftSecretChallenge = draftSecretChallenge(runtime.WardDraftSecret)
			resp, err = s.DraftAPI.CreateWardDraft(ctx, req)
		}
		if err != nil {
			return nil, err
		}
	}
	activationURL := buildActivationURL(input.PublicBaseURL, input.Site, resp.WardDraftID)
	slog.Info("init: ward draft created", "ward_draft_id", resp.WardDraftID, "status", resp.Status, "activation_url", activationURL)

	draftAction := "created"
	if existingDraftID != "" && existingDraftID == resp.WardDraftID {
		draftAction = "updated"
	}

	if runtime.WardDraftID == "" {
		runtime.WardDraftID = resp.WardDraftID
		runtime.WardStatus = domain.WardStatusInitializing
	}
	runtime.Spec = input.Spec
	runtime.BillingMode = input.BillingMode
	runtime.DomainType = input.DomainType
	runtime.RequestedDomain = resp.RequestedDomain
	runtime.TLSMode = tlsMode
	runtime.UpstreamAddr = upstreamAddr
	runtime.UpstreamPort = upstreamPort
	runtime.ListenPort = listenPort
	runtime.ListenHost = listenHost
	runtime.IngressFamily = ingressFamily
	runtime.ActivationURL = activationURL
	runtime.LastPublicIP = resp.ResolvedPublicIP
	runtime.Site = input.Site
	runtime.UpdatedAt = time.Now().UTC()

	if err := s.ConfigStore.CommitPendingRuntime(ctx, *runtime); err != nil {
		return nil, err
	}

	return &NewOutput{
		WardDraftID:        resp.WardDraftID,
		DraftAction:        draftAction,
		Status:             resp.Status,
		ActivationURL:      activationURL,
		DomainCheckStatus:  resp.DomainCheckStatus,
		ResolvedPublicIP:   resp.ResolvedPublicIP,
		IngressProbeStatus: resp.IngressProbeStatus,
		RequestedDomain:    resp.RequestedDomain,
	}, nil
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

func buildActivationURL(publicBaseURL string, site domain.Site, wardDraftID string) string {
	if wardDraftID == "" {
		return ""
	}
	baseURL := strings.TrimSpace(strings.TrimSuffix(publicBaseURL, "/"))
	if baseURL == "" {
		baseURL = defaultPublicBaseURL(site)
	}
	return baseURL + "/activate/" + wardDraftID
}

func defaultPublicBaseURL(site domain.Site) string {
	switch site {
	case domain.SiteCN:
		return "https://warded.cn"
	default:
		return "https://warded.me"
	}
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

func draftSecretChallenge(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func validateSpecDomainCombination(spec domain.Spec, domainType domain.DomainType, requestedDomain string) error {
	switch spec {
	case domain.SpecStarter:
		if domainType != domain.DomainTypePlatformSubdomain {
			return fmt.Errorf("starter spec only supports platform_subdomain")
		}
		// Note: requested_domain validation for starter is handled in the request construction.
		// For starter spec, we don't send requested_domain in the request (platform assigns it).
		// The input.RequestedDomain here could be a platform-assigned value from a previous draft,
		// so we don't validate it here.
	case domain.SpecPro:
		if domainType != domain.DomainTypePlatformSubdomain && domainType != domain.DomainTypeCustomDomain {
			return fmt.Errorf("domain_type is invalid")
		}
		if requestedDomain == "" {
			return fmt.Errorf("requested_domain is required for pro spec")
		}
		if strings.Contains(requestedDomain, "://") || strings.Contains(requestedDomain, "/") {
			return fmt.Errorf("requested_domain must be a full domain without scheme or path")
		}
		if !strings.Contains(requestedDomain, ".") {
			return fmt.Errorf("requested_domain must be a full domain")
		}
	default:
		return fmt.Errorf("spec is invalid")
	}
	return nil
}

func validateBillingMode(billingMode domain.BillingMode) error {
	switch billingMode {
	case domain.BillingModeMonthly, domain.BillingModeYearly:
		return nil
	default:
		return fmt.Errorf("billing_mode is invalid")
	}
}

func clearDraftState(runtime *domain.LocalWardRuntime) {
	runtime.WardDraftID = ""
	runtime.WardDraftSecret = ""
	runtime.ActivationURL = ""
}

func shouldCreateFreshDraft(err error) bool {
	var platformErr *ports.PlatformError
	if !errors.As(err, &platformErr) {
		return false
	}
	switch platformErr.Code {
	case "access_denied", "activation_link_expired":
		return true
	default:
		return false
	}
}

var newOpenClawCLIFunc = NewOpenClawCLI

func DiscoverOpenClawPort() int {
	cli, err := newOpenClawCLIFunc("")
	if err != nil {
		return 18789
	}
	rawPort, err := cli.Get("gateway.port")
	if err != nil {
		return 18789
	}
	port := parseOpenClawPort(rawPort)
	if port <= 0 {
		return 18789
	}
	return port
}
