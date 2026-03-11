package application_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/herewei/warded/internal/sitepolicy"
)

// mockPlatformAPI is a test-local implementation of ports.PlatformAPI.
// CreateWardDraft returns realistic responses derived from the request.
// All other methods return zero values (not used by InitService).
type mockPlatformAPI struct {
	createErr     error
	createCalls   int
	lastCreateReq ports.CreateWardDraftRequest
	getDraftResp  *ports.GetWardDraftStatusResponse
	getDraftErr   error
	getDraftCalls int
}

func (m *mockPlatformAPI) CreateWardDraft(_ context.Context, req ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
	m.createCalls++
	m.lastCreateReq = req
	if m.createErr != nil {
		return nil, m.createErr
	}
	policy := sitepolicy.ForSite(domain.Site(req.Site))
	draftID := fmt.Sprintf("draft_test_%d", time.Now().UnixNano())
	if req.DraftSecretChallenge == "" {
		return nil, fmt.Errorf("missing draft_secret_challenge")
	}
	domainCheckStatus := "not_required"
	if req.DomainType == "custom_domain" || req.RequestedDomain != "" {
		domainCheckStatus = "available"
	}
	return &ports.CreateWardDraftResponse{
		WardDraftID:        draftID,
		Site:               req.Site,
		Status:             "pending_activation",
		ExpiresAt:          time.Now().UTC().Add(15 * time.Minute).Format(time.RFC3339),
		ActivationURL:      policy.PlatformBaseURL() + "/activate/" + draftID,
		DomainCheckStatus:  domainCheckStatus,
		ResolvedPublicIP:   "1.2.3.4",
		IngressProbeStatus: "reachable",
	}, nil
}

func (m *mockPlatformAPI) GetWardDraftStatus(_ context.Context, _ string, _ string, _ string) (*ports.GetWardDraftStatusResponse, error) {
	m.getDraftCalls++
	if m.getDraftErr != nil {
		return nil, m.getDraftErr
	}
	return m.getDraftResp, nil
}

func (m *mockPlatformAPI) ClaimWardDraft(_ context.Context, _ ports.ClaimWardDraftRequest, _ string) (*ports.ClaimWardDraftResponse, error) {
	return nil, nil
}

func (m *mockPlatformAPI) GetWard(_ context.Context, _ string, _ string, _ string) (*ports.GetWardResponse, error) {
	return nil, nil
}

func (m *mockPlatformAPI) GetTLSMaterial(_ context.Context, _ string, _ string, _ string) (*ports.GetTLSMaterialResponse, error) {
	return nil, nil
}

func (m *mockPlatformAPI) ExchangeAuthCode(_ context.Context, _ ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	return nil, nil
}

type stubUpstreamChecker struct {
	err error
}

func (c stubUpstreamChecker) Check(_ context.Context, _ int) error {
	return c.err
}

func TestInitService_Execute_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	svc := application.InitService{
		ConfigStore:   storage.NewJSONStore(dir),
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), application.InitInput{
		Site:         domain.SiteGlobal,
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if out.WardDraftID == "" {
		t.Error("expected ward draft ID")
	}
	if out.Status != "pending_activation" {
		t.Errorf("expected pending_activation, got %s", out.Status)
	}
	if out.ActivationURL == "" {
		t.Error("expected activation URL")
	}
	if !strings.Contains(out.ActivationURL, "warded.me/activate/") {
		t.Errorf("expected activation URL to contain warded.me/activate/, got %s", out.ActivationURL)
	}
	if out.ResolvedPublicIP == "" {
		t.Error("expected resolved public IP")
	}
	if out.IngressProbeStatus == "" {
		t.Error("expected ingress probe status")
	}
}

func TestInitService_Execute_CNSite(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	svc := application.InitService{
		ConfigStore:   storage.NewJSONStore(dir),
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), application.InitInput{
		Site:         domain.SiteCN,
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.ActivationURL, "warded.cn/activate/") {
		t.Errorf("expected activation URL to contain warded.cn/activate/, got %s", out.ActivationURL)
	}
}

func TestInitService_Execute_UsesPublicBaseURLForActivationLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	svc := application.InitService{
		ConfigStore:   storage.NewJSONStore(dir),
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), application.InitInput{
		Site:          domain.SiteGlobal,
		Spec:          domain.SpecStarter,
		BillingMode:   domain.BillingModeMonthly,
		DomainType:    domain.DomainTypePlatformSubdomain,
		UpstreamPort:  3000,
		PublicBaseURL: "https://preview.warded.me",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(out.ActivationURL, "https://preview.warded.me/activate/") {
		t.Fatalf("expected activation URL to use public base URL, got %s", out.ActivationURL)
	}
}

func TestInitService_Execute_RejectsInvalidSpecDomainCombinationsBeforePlatformCall(t *testing.T) {
	t.Parallel()

	type validationCase struct {
		name            string
		spec            domain.Spec
		domainType      domain.DomainType
		requestedDomain string
		billingMode     domain.BillingMode
		wantErr         string
	}

	tests := []validationCase{
		{
			name:            "starter_custom_domain_with_requested_domain",
			spec:            domain.SpecStarter,
			domainType:      domain.DomainTypeCustomDomain,
			requestedDomain: "example.com",
			wantErr:         "starter spec only supports platform_subdomain",
		},
		{
			name:            "starter_custom_domain_without_requested_domain",
			spec:            domain.SpecStarter,
			domainType:      domain.DomainTypeCustomDomain,
			requestedDomain: "",
			wantErr:         "starter spec only supports platform_subdomain",
		},
		{
			name:            "starter_platform_subdomain_with_requested_domain",
			spec:            domain.SpecStarter,
			domainType:      domain.DomainTypePlatformSubdomain,
			requestedDomain: "myrobot",
			wantErr:         "requested_domain is not allowed for starter spec",
		},
		{
			name:            "pro_platform_subdomain_missing_requested_domain",
			spec:            domain.SpecPro,
			domainType:      domain.DomainTypePlatformSubdomain,
			requestedDomain: "",
			wantErr:         "requested_domain is required for pro spec",
		},
		{
			name:            "pro_custom_domain_missing_requested_domain",
			spec:            domain.SpecPro,
			domainType:      domain.DomainTypeCustomDomain,
			requestedDomain: "",
			wantErr:         "requested_domain is required for pro spec",
		},
		{
			name:       "unknown_spec",
			spec:       domain.Spec("enterprise"),
			domainType: domain.DomainTypePlatformSubdomain,
			wantErr:    "spec is invalid",
		},
		{
			name:        "invalid_billing_mode",
			spec:        domain.SpecStarter,
			domainType:  domain.DomainTypePlatformSubdomain,
			billingMode: domain.BillingMode("trial"),
			wantErr:     "billing_mode is invalid",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			api := &mockPlatformAPI{}
			svc := application.InitService{
				ConfigStore:   storage.NewJSONStore(dir),
				PlatformAPI:   api,
				UpstreamCheck: stubUpstreamChecker{},
			}
			billingMode := tc.billingMode
			if billingMode == "" {
				billingMode = domain.BillingModeMonthly
			}

			_, err := svc.Execute(context.Background(), application.InitInput{
				Site:            domain.SiteGlobal,
				Spec:            tc.spec,
				BillingMode:     billingMode,
				DomainType:      tc.domainType,
				RequestedDomain: tc.requestedDomain,
				UpstreamPort:    3000,
			})
			if err == nil {
				t.Fatal("expected validation error")
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("expected error %q, got %q", tc.wantErr, err.Error())
			}
			if api.createCalls != 0 {
				t.Fatalf("expected no platform calls, got %d", api.createCalls)
			}
		})
	}
}

func TestInitService_Execute_PersistsConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	store := storage.NewJSONStore(dir)
	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), application.InitInput{
		Site:         domain.SiteGlobal,
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 8080,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	runtime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load ward runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected ward runtime to be persisted")
	}
	if runtime.WardDraftID == "" {
		t.Error("expected persisted ward runtime to have ward draft ID")
	}
	if runtime.WardDraftSecret == "" {
		t.Error("expected persisted ward runtime to have ward_draft_secret")
	}
	if runtime.JWTSigningSecret == "" {
		t.Error("expected persisted ward runtime to have jwt_signing_secret")
	}
	if runtime.Site != domain.SiteGlobal {
		t.Errorf("expected persisted site=global, got %s", runtime.Site)
	}
	if runtime.LastPublicIP == "" {
		t.Error("expected persisted ward runtime to have public IP")
	}
	if runtime.UpstreamPort != 8080 {
		t.Errorf("expected persisted upstream_port=8080, got %d", runtime.UpstreamPort)
	}
	if runtime.ActivationURL != out.ActivationURL {
		t.Errorf("expected persisted activation_url=%s, got %s", out.ActivationURL, runtime.ActivationURL)
	}
	if runtime.ListenAddr != ":443" {
		t.Errorf("expected persisted listen_addr=:443, got %s", runtime.ListenAddr)
	}
	if runtime.TLSMode != domain.TLSModePlatformWildcard {
		t.Errorf("expected persisted tls_mode=%s, got %s", domain.TLSModePlatformWildcard, runtime.TLSMode)
	}
}

func TestInitService_Execute_PersistsLocalACMETLSModeForCustomDomain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	_, err := svc.Execute(context.Background(), application.InitInput{
		Site:            domain.SiteGlobal,
		Spec:            domain.SpecPro,
		BillingMode:     domain.BillingModeMonthly,
		DomainType:      domain.DomainTypeCustomDomain,
		RequestedDomain: "robot.example.com",
		UpstreamPort:    8080,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	runtime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected runtime to be persisted")
	}
	if runtime.TLSMode != domain.TLSModeLocalACME {
		t.Fatalf("expected tls_mode=%s, got %s", domain.TLSModeLocalACME, runtime.TLSMode)
	}
}

func TestInitService_Execute_ReusesExistingNode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	store := storage.NewJSONStore(dir)
	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	input := application.InitInput{
		Site:        domain.SiteGlobal,
		Spec:        domain.SpecStarter,
		BillingMode: domain.BillingModeMonthly,
		DomainType:  domain.DomainTypePlatformSubdomain,
	}

	// First init
	_, err := svc.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("first init: %v", err)
	}

	// Second init (simulating a new session)
	out2, err := svc.Execute(context.Background(), input)
	if err != nil {
		t.Fatalf("second init: %v", err)
	}

	if out2.WardDraftID == "" {
		t.Fatal("expected second init to return a ward_draft_id")
	}
	runtime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil || runtime.WardDraftSecret == "" {
		t.Fatal("expected persisted ward_draft_secret after repeated init")
	}
	if runtime.JWTSigningSecret == "" {
		t.Fatal("expected persisted jwt_signing_secret after repeated init")
	}
}

func TestInitService_Execute_CreatesFreshDraftWhenExistingDraftExpired(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		WardDraftID:      "draft_old",
		WardDraftSecret:  "lkd_old",
		JWTSigningSecret: "jwt_existing",
		WardStatus:       domain.WardStatusInitializing,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	api := &mockPlatformAPI{
		getDraftResp: &ports.GetWardDraftStatusResponse{
			WardDraftID: "draft_old",
			Status:      "expired",
		},
	}
	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   api,
		UpstreamCheck: stubUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), application.InitInput{
		Site:         domain.SiteGlobal,
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.getDraftCalls != 1 {
		t.Fatalf("expected one draft status lookup, got %d", api.getDraftCalls)
	}
	if api.lastCreateReq.DraftSecretChallenge == "" {
		t.Fatal("expected recreated draft to include challenge")
	}
	if out.WardDraftID == "draft_old" {
		t.Fatal("expected a fresh draft ID after expired draft")
	}

	runtime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected runtime to be persisted")
	}
	if runtime.WardDraftID == "draft_old" {
		t.Fatal("expected persisted runtime to contain a fresh draft ID")
	}
	if runtime.JWTSigningSecret != "jwt_existing" {
		t.Fatalf("expected jwt_signing_secret to be preserved, got %q", runtime.JWTSigningSecret)
	}
}

func TestInitService_Execute_CreatesFreshDraftWhenTokenUnknownToServer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		WardDraftID:      "draft_old",
		WardDraftSecret:  "lkd_old",
		JWTSigningSecret: "jwt_existing",
		WardStatus:       domain.WardStatusInitializing,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	// Platform doesn't recognise the stored token at all (e.g. DB wiped).
	api := &mockPlatformAPI{
		getDraftErr: &ports.PlatformError{
			Code:       "access_denied",
			HTTPStatus: 403,
		},
	}
	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   api,
		UpstreamCheck: stubUpstreamChecker{},
	}

	_, err := svc.Execute(context.Background(), application.InitInput{
		Site:         domain.SiteGlobal,
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.lastCreateReq.DraftSecretChallenge == "" {
		t.Fatal("expected fresh draft to include challenge")
	}
}

func TestInitService_Execute_CreatesFreshDraftWhenExistingDraftCredentialExpired(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		WardDraftID:      "draft_old",
		WardDraftSecret:  "lkd_old",
		JWTSigningSecret: "jwt_existing",
		WardStatus:       domain.WardStatusInitializing,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	api := &mockPlatformAPI{
		getDraftErr: &ports.PlatformError{
			Code:       "activation_link_expired",
			HTTPStatus: 401,
		},
	}
	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   api,
		UpstreamCheck: stubUpstreamChecker{},
	}

	_, err := svc.Execute(context.Background(), application.InitInput{
		Site:         domain.SiteGlobal,
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if api.lastCreateReq.DraftSecretChallenge == "" {
		t.Fatal("expected expired credential fallback to include challenge")
	}
}

func TestInitService_Execute_DefaultUpstreamPort(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	store := storage.NewJSONStore(dir)
	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	_, err := svc.Execute(context.Background(), application.InitInput{
		Site:        domain.SiteGlobal,
		Spec:        domain.SpecStarter,
		BillingMode: domain.BillingModeMonthly,
		DomainType:  domain.DomainTypePlatformSubdomain,
		// UpstreamPort omitted — should default to 18789
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	runtime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load ward runtime: %v", err)
	}
	if runtime.UpstreamPort != 18789 {
		t.Errorf("expected default upstream_port=18789, got %d", runtime.UpstreamPort)
	}
}

func TestInitService_Execute_MissingDeps(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		svc  application.InitService
	}{
		{
			name: "nil config store",
			svc: application.InitService{
				PlatformAPI:   &mockPlatformAPI{},
				UpstreamCheck: stubUpstreamChecker{},
			},
		},
		{
			name: "nil platform API",
			svc: application.InitService{
				ConfigStore:   storage.NewJSONStore(t.TempDir()),
				UpstreamCheck: stubUpstreamChecker{},
			},
		},
		{
			name: "nil upstream checker",
			svc: application.InitService{
				ConfigStore: storage.NewJSONStore(t.TempDir()),
				PlatformAPI: &mockPlatformAPI{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tt.svc.Execute(context.Background(), application.InitInput{
				Site:        domain.SiteGlobal,
				Spec:        domain.SpecStarter,
				BillingMode: domain.BillingModeMonthly,
				DomainType:  domain.DomainTypePlatformSubdomain,
			})
			if err == nil {
				t.Error("expected error for missing dependency")
			}
		})
	}
}

func TestInitService_Execute_AllowsRecoverModeWithExistingActivatedWard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		WardID:           "ward_existing",
		WardSecret:       "lks_existing",
		JWTSigningSecret: "jwt_existing",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), application.InitInput{
		Site:         domain.SiteGlobal,
		Mode:         "recover",
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 3000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.WardDraftID == "" {
		t.Fatal("expected ward draft id")
	}
}

func TestInitService_Execute_RejectsWhenWardAlreadyActivated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		WardID:           "ward_existing",
		WardSecret:       "wrd_existing",
		WardStatus:       domain.WardStatusActive,
		JWTSigningSecret: "jwt_existing",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	svc := application.InitService{
		ConfigStore:   store,
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	_, err := svc.Execute(context.Background(), application.InitInput{
		Site:        domain.SiteGlobal,
		Mode:        "new",
		Spec:        domain.SpecStarter,
		BillingMode: domain.BillingModeMonthly,
		DomainType:  domain.DomainTypePlatformSubdomain,
	})
	if err == nil {
		t.Fatal("expected error when ward already activated")
	}
	if !strings.Contains(err.Error(), "already activated") {
		t.Fatalf("expected 'already activated' error, got: %v", err)
	}
}

func TestInitService_Execute_UpstreamUnreachable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	svc := application.InitService{
		ConfigStore:   storage.NewJSONStore(dir),
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{err: application.ErrUpstreamUnreachable},
	}

	_, err := svc.Execute(context.Background(), application.InitInput{
		Site:         domain.SiteGlobal,
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 3000,
	})
	if err == nil {
		t.Fatal("expected error when upstream unreachable")
	}
	if !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("expected upstream error, got: %v", err)
	}
}

func TestInitService_Execute_PlatformAPICreateWardDraftFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	svc := application.InitService{
		ConfigStore: storage.NewJSONStore(dir),
		PlatformAPI: &mockPlatformAPI{
			createErr: &ports.PlatformError{Code: "internal_error", Message: "platform internal error"},
		},
		UpstreamCheck: stubUpstreamChecker{},
	}

	_, err := svc.Execute(context.Background(), application.InitInput{
		Site:        domain.SiteGlobal,
		Spec:        domain.SpecStarter,
		BillingMode: domain.BillingModeMonthly,
		DomainType:  domain.DomainTypePlatformSubdomain,
	})
	if err == nil {
		t.Fatal("expected error when platform API fails")
	}
}

func TestInitService_Execute_YearlyBillingMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mockAPI := &mockPlatformAPI{}
	svc := application.InitService{
		ConfigStore:   storage.NewJSONStore(dir),
		PlatformAPI:   mockAPI,
		UpstreamCheck: stubUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), application.InitInput{
		Site:        domain.SiteGlobal,
		Spec:        domain.SpecStarter,
		BillingMode: domain.BillingModeYearly,
		DomainType:  domain.DomainTypePlatformSubdomain,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.WardDraftID == "" {
		t.Fatal("expected ward draft id")
	}
	if mockAPI.lastCreateReq.BillingMode != string(domain.BillingModeYearly) {
		t.Fatalf("expected yearly billing mode, got: %s", mockAPI.lastCreateReq.BillingMode)
	}
}

func TestInitService_Execute_MigrateMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	svc := application.InitService{
		ConfigStore:   storage.NewJSONStore(dir),
		PlatformAPI:   &mockPlatformAPI{},
		UpstreamCheck: stubUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), application.InitInput{
		Site:            domain.SiteGlobal,
		Mode:            "migrate",
		Spec:            domain.SpecPro,
		BillingMode:     domain.BillingModeMonthly,
		DomainType:      domain.DomainTypePlatformSubdomain,
		RequestedDomain: "myrobot",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out.WardDraftID == "" {
		t.Fatal("expected ward draft id")
	}
}
