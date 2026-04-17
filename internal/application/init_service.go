package application

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

// Typed errors for preflight checks
var (
	ErrConfigDirNotWritable = errors.New("config directory is not writable")
	ErrUpstreamUnreachable  = errors.New("upstream port is unreachable")
	ErrListenPortOccupied   = errors.New("listen port is occupied")
)

type InitService struct {
	ConfigStore   ports.LocalConfigStore
	PlatformAPI   ports.PlatformAPI
	UpstreamCheck ports.UpstreamChecker
}

type InitInput struct {
	Site            domain.Site
	Mode            string
	Spec            domain.Spec
	BillingMode     domain.BillingMode
	DomainType      domain.DomainType
	RequestedDomain string
	UpstreamPort    int
	PublicBaseURL   string
}

type InitOutput struct {
	WardDraftID        string
	Status             string
	ActivationURL      string
	DomainCheckStatus  string
	ResolvedPublicIP   string
	IngressProbeStatus string
}

func (s InitService) Execute(ctx context.Context, input InitInput) (*InitOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("init service: config store is required")
	}
	if s.PlatformAPI == nil {
		return nil, fmt.Errorf("init service: platform API is required")
	}
	if s.UpstreamCheck == nil {
		return nil, fmt.Errorf("init service: upstream checker is required")
	}

	upstreamPort := input.UpstreamPort
	if upstreamPort == 0 {
		upstreamPort = discoverOpenClawPort()
	}
	mode := input.Mode
	if mode == "" {
		mode = "new"
	}
	if err := validateSpecDomainCombination(input.Spec, input.DomainType, input.RequestedDomain); err != nil {
		return nil, err
	}
	if err := validateBillingMode(input.BillingMode); err != nil {
		return nil, err
	}
	tlsMode, err := tlsModeForDomainType(input.DomainType)
	if err != nil {
		return nil, fmt.Errorf("init service: %w", err)
	}

	runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, err
	}
	if runtime == nil {
		runtime = &domain.LocalWardRuntime{
			Site:       input.Site,
			WardStatus: domain.WardStatusInitializing,
			ListenAddr: ":443",
		}
	}
	if mode == "new" && runtime.WardID != "" && runtime.WardSecret != "" {
		return nil, fmt.Errorf("init service: ward already activated")
	}
	if runtime.WardID == "" && runtime.WardSecret == "" && runtime.WardDraftID != "" && runtime.WardDraftSecret != "" {
		draftSite := runtime.Site
		if draftSite == "" {
			draftSite = input.Site
		}
		draft, err := s.PlatformAPI.GetWardDraftStatus(ctx, string(draftSite), draftSecretChallenge(runtime.WardDraftSecret), runtime.WardDraftID)
		if err != nil {
			if shouldCreateFreshDraft(err) {
				clearDraftState(runtime)
				runtime.UpdatedAt = time.Now().UTC()
				if saveErr := s.ConfigStore.SaveWardRuntime(ctx, *runtime); saveErr != nil {
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
				if saveErr := s.ConfigStore.SaveWardRuntime(ctx, *runtime); saveErr != nil {
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

	slog.Info("init: checking upstream reachability", "port", upstreamPort)
	if err := s.UpstreamCheck.Check(ctx, upstreamPort); err != nil {
		return nil, err
	}
	slog.Info("init: upstream reachable", "port", upstreamPort)

	slog.Info("init: creating ward draft", "site", input.Site, "spec", input.Spec, "billing_mode", input.BillingMode)
	req := ports.CreateWardDraftRequest{
		Site:                 string(input.Site),
		Mode:                 mode,
		Spec:                 string(input.Spec),
		BillingMode:          string(input.BillingMode),
		DomainType:           string(input.DomainType),
		RequestedDomain:      input.RequestedDomain,
		UpstreamPort:         upstreamPort,
		DraftSecretChallenge: draftSecretChallenge(runtime.WardDraftSecret),
	}
	resp, err := s.PlatformAPI.CreateWardDraft(ctx, req)
	if err != nil {
		return nil, err
	}
	activationURL := buildActivationURL(input.PublicBaseURL, input.Site, resp.WardDraftID)
	slog.Info("init: ward draft created", "ward_draft_id", resp.WardDraftID, "status", resp.Status, "activation_url", activationURL)

	if runtime.WardDraftID == "" {
		runtime.WardDraftID = resp.WardDraftID
		runtime.WardStatus = domain.WardStatusInitializing
	}
	runtime.Spec = input.Spec
	runtime.BillingMode = input.BillingMode
	runtime.DomainType = input.DomainType
	runtime.TLSMode = tlsMode
	runtime.UpstreamPort = upstreamPort
	runtime.ActivationURL = activationURL
	runtime.LastPublicIP = resp.ResolvedPublicIP
	runtime.Site = input.Site // Ensure Site is saved
	runtime.UpdatedAt = time.Now().UTC()

	if err := s.ConfigStore.SaveWardRuntime(ctx, *runtime); err != nil {
		return nil, err
	}

	return &InitOutput{
		WardDraftID:        resp.WardDraftID,
		Status:             resp.Status,
		ActivationURL:      activationURL,
		DomainCheckStatus:  resp.DomainCheckStatus,
		ResolvedPublicIP:   resp.ResolvedPublicIP,
		IngressProbeStatus: resp.IngressProbeStatus,
	}, nil
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
		if requestedDomain != "" {
			return fmt.Errorf("requested_domain is not allowed for starter spec")
		}
	case domain.SpecPro:
		if domainType != domain.DomainTypePlatformSubdomain && domainType != domain.DomainTypeCustomDomain {
			return fmt.Errorf("domain_type is invalid")
		}
		if requestedDomain == "" {
			return fmt.Errorf("requested_domain is required for pro spec")
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

var portPattern = regexp.MustCompile(`"port"\s*:\s*([0-9]+)`)

func discoverOpenClawPort() int {
	home, err := os.UserHomeDir()
	if err != nil {
		return 18789
	}

	file, err := os.Open(filepath.Join(home, ".openclaw", "openclaw.json"))
	if err != nil {
		return 18789
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		matches := portPattern.FindStringSubmatch(scanner.Text())
		if len(matches) != 2 {
			continue
		}
		var port int
		if _, err := fmt.Sscanf(matches[1], "%d", &port); err == nil && port > 0 {
			return port
		}
	}

	return 18789
}
