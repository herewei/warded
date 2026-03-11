package application

import (
	"context"
	"testing"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type statusTestConfigStore struct {
	runtime *domain.LocalWardRuntime
}

func (s *statusTestConfigStore) LoadWardRuntime(context.Context) (*domain.LocalWardRuntime, error) {
	if s.runtime == nil {
		return nil, nil
	}
	copy := *s.runtime
	return &copy, nil
}

func (s *statusTestConfigStore) SaveWardRuntime(_ context.Context, runtime domain.LocalWardRuntime) error {
	copy := runtime
	s.runtime = &copy
	return nil
}

type statusTestPlatformAPI struct {
	draft *ports.GetWardDraftStatusResponse
}

func (statusTestPlatformAPI) CreateWardDraft(context.Context, ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
	panic("unexpected call")
}

func (statusTestPlatformAPI) ExchangeAuthCode(context.Context, ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	panic("unexpected call")
}

func (p statusTestPlatformAPI) GetWardDraftStatus(_ context.Context, _ string, _ string, _ string) (*ports.GetWardDraftStatusResponse, error) {
	return p.draft, nil
}

func (statusTestPlatformAPI) ClaimWardDraft(_ context.Context, _ ports.ClaimWardDraftRequest, _ string) (*ports.ClaimWardDraftResponse, error) {
	panic("unexpected call")
}

func (statusTestPlatformAPI) GetWard(_ context.Context, _ string, _ string, wardID string) (*ports.GetWardResponse, error) {
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

func (statusTestPlatformAPI) GetTLSMaterial(context.Context, string, string, string) (*ports.GetTLSMaterialResponse, error) {
	return nil, nil
}

func TestStatusServiceReturnsLocalRuntimeWithoutMutatingIt(t *testing.T) {
	t.Parallel()

	store := &statusTestConfigStore{
		runtime: &domain.LocalWardRuntime{
			Site:           domain.SiteGlobal,
			WardID:         "ward_123",
			WardSecret:     "wrd_secret",
			WardStatus:     domain.WardStatusActive,
			Domain:         "demo.warded.me",
			BillingMode:    domain.BillingModeMonthly,
			ActivationMode: domain.ActivationModeTrial,
			DomainType:     domain.DomainTypePlatformSubdomain,
			TLSMode:        domain.TLSModePlatformWildcard,
		},
	}

	svc := StatusService{
		ConfigStore: store,
	}

	out, err := svc.Execute(context.Background())
	if err != nil {
		t.Fatalf("status execute: %v", err)
	}
	if out.Runtime == nil || out.Runtime.WardID != "ward_123" {
		t.Fatalf("unexpected runtime: %#v", out)
	}
	if store.runtime == nil || store.runtime.WardID != "ward_123" {
		t.Fatalf("expected stored runtime to remain unchanged, got %#v", store.runtime)
	}
}

func TestStatusServiceFetchesPendingDraftForInspection(t *testing.T) {
	t.Parallel()

	store := &statusTestConfigStore{
		runtime: &domain.LocalWardRuntime{
			Site:            domain.SiteGlobal,
			WardDraftID:     "draft_123",
			WardDraftSecret: "wdd_secret",
			WardStatus:      domain.WardStatusInitializing,
		},
	}

	svc := StatusService{
		ConfigStore: store,
		PlatformAPI: statusTestPlatformAPI{
			draft: &ports.GetWardDraftStatusResponse{
				WardDraftID: "draft_123",
				Status:      "pending_activation",
			},
		},
	}

	out, err := svc.Execute(context.Background())
	if err != nil {
		t.Fatalf("status execute: %v", err)
	}
	if out.WardDraft == nil || out.WardDraft.Status != "pending_activation" {
		t.Fatalf("expected pending draft in output, got %#v", out)
	}
	if store.runtime == nil && out.Runtime == nil {
		t.Fatalf("expected runtime to remain locally available")
	}
}
