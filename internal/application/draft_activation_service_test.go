package application

import (
	"context"
	"testing"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type activationTestPlatformAPI struct {
	polls int
}

func (activationTestPlatformAPI) CreateWardDraft(context.Context, ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
	panic("unexpected call")
}

func (activationTestPlatformAPI) ExchangeAuthCode(context.Context, ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	panic("unexpected call")
}

func (p *activationTestPlatformAPI) GetWardDraftStatus(_ context.Context, _ string, _ string, wardDraftID string) (*ports.GetWardDraftStatusResponse, error) {
	p.polls++
	if p.polls < 2 {
		return &ports.GetWardDraftStatusResponse{
			WardDraftID: wardDraftID,
			Status:      "pending_activation",
		}, nil
	}
	return &ports.GetWardDraftStatusResponse{
		WardDraftID: wardDraftID,
		Status:      "converted_pending_claim",
	}, nil
}

func (*activationTestPlatformAPI) ClaimWardDraft(_ context.Context, req ports.ClaimWardDraftRequest, wardDraftID string) (*ports.ClaimWardDraftResponse, error) {
	if req.DraftSecret != "wdd_secret" {
		return nil, context.DeadlineExceeded
	}
	return &ports.ClaimWardDraftResponse{
		WardID:         "ward_123",
		WardSecret:     "wrd_secret",
		Site:           "global",
		Status:         "active",
		Domain:         "demo.warded.me",
		BillingMode:    "monthly",
		ActivationMode: "trial",
		ActivatedAt:    time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:      time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339),
	}, nil
}

func (*activationTestPlatformAPI) GetWard(_ context.Context, _ string, bearerToken string, wardID string) (*ports.GetWardResponse, error) {
	if bearerToken != "wrd_secret" {
		return nil, context.DeadlineExceeded
	}
	return &ports.GetWardResponse{
		WardID:         wardID,
		Site:           "global",
		BillingMode:    "monthly",
		ActivationMode: "trial",
		DomainType:     "platform_subdomain",
		Domain:         "demo.warded.me",
		Status:         "active",
		ActivatedAt:    time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:      time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339),
		UpstreamPort:   18789,
	}, nil
}

func (*activationTestPlatformAPI) GetTLSMaterial(context.Context, string, string, string) (*ports.GetTLSMaterialResponse, error) {
	return nil, nil
}

func TestDraftActivationServiceWaitUntilActivated(t *testing.T) {
	t.Parallel()

	store := &statusTestConfigStore{
		runtime: &domain.LocalWardRuntime{
			Site:            domain.SiteGlobal,
			WardDraftID:     "draft_123",
			WardDraftSecret: "wdd_secret",
			WardStatus:      domain.WardStatusInitializing,
		},
	}
	api := &activationTestPlatformAPI{}

	svc := DraftActivationService{
		ConfigStore: store,
		PlatformAPI: api,
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	runtime, err := svc.WaitUntilActivated(ctx, time.Millisecond)
	if err != nil {
		t.Fatalf("wait until activated: %v", err)
	}
	if api.polls < 2 {
		t.Fatalf("expected multiple polls, got %d", api.polls)
	}
	if runtime == nil || runtime.WardID != "ward_123" {
		t.Fatalf("unexpected runtime: %#v", runtime)
	}
	if runtime.TLSMode != domain.TLSModePlatformWildcard {
		t.Fatalf("expected tls_mode to be persisted, got %#v", runtime)
	}
}

type convertedDraftPlatformAPI struct{}

func (convertedDraftPlatformAPI) CreateWardDraft(context.Context, ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
	panic("unexpected call")
}

func (convertedDraftPlatformAPI) ExchangeAuthCode(context.Context, ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	panic("unexpected call")
}

func (convertedDraftPlatformAPI) GetWardDraftStatus(_ context.Context, _ string, _ string, wardDraftID string) (*ports.GetWardDraftStatusResponse, error) {
	return &ports.GetWardDraftStatusResponse{
		WardDraftID: wardDraftID,
		Status:      "converted_pending_claim",
	}, nil
}

func (convertedDraftPlatformAPI) ClaimWardDraft(_ context.Context, req ports.ClaimWardDraftRequest, wardDraftID string) (*ports.ClaimWardDraftResponse, error) {
	if req.DraftSecret != "wdd_existing" || wardDraftID != "draft_456" {
		return nil, context.DeadlineExceeded
	}
	return &ports.ClaimWardDraftResponse{
		WardID:         "ward_456",
		WardSecret:     "wrd_converted",
		Site:           "global",
		Status:         "active",
		Domain:         "robot.example.com",
		BillingMode:    "yearly",
		ActivationMode: "paid",
		ActivatedAt:    time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:      time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339),
	}, nil
}

func (convertedDraftPlatformAPI) GetWard(_ context.Context, _ string, bearerToken string, wardID string) (*ports.GetWardResponse, error) {
	if bearerToken != "wrd_converted" {
		return nil, context.DeadlineExceeded
	}
	return &ports.GetWardResponse{
		WardID:         wardID,
		Site:           "global",
		BillingMode:    "yearly",
		ActivationMode: "paid",
		DomainType:     "custom_domain",
		Domain:         "robot.example.com",
		Status:         "active",
		ActivatedAt:    time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:      time.Now().UTC().Add(365 * 24 * time.Hour).Format(time.RFC3339),
		UpstreamPort:   3000,
	}, nil
}

func (convertedDraftPlatformAPI) GetTLSMaterial(context.Context, string, string, string) (*ports.GetTLSMaterialResponse, error) {
	return nil, nil
}

func TestDraftActivationServiceFinalizeIfConverted(t *testing.T) {
	t.Parallel()

	store := &statusTestConfigStore{
		runtime: &domain.LocalWardRuntime{
			Site:            domain.SiteGlobal,
			WardDraftID:     "draft_456",
			WardDraftSecret: "wdd_existing",
			WardStatus:      domain.WardStatusInitializing,
		},
	}

	svc := DraftActivationService{
		ConfigStore: store,
		PlatformAPI: convertedDraftPlatformAPI{},
	}

	runtime, finalized, err := svc.FinalizeIfConverted(context.Background())
	if err != nil {
		t.Fatalf("finalize converted draft: %v", err)
	}
	if !finalized {
		t.Fatal("expected converted draft to be finalized")
	}
	if runtime == nil || runtime.WardID != "ward_456" {
		t.Fatalf("unexpected runtime: %#v", runtime)
	}
	if runtime.TLSMode != domain.TLSModeLocalACME {
		t.Fatalf("expected tls_mode=%s, got %#v", domain.TLSModeLocalACME, runtime)
	}
}
