package application

import (
	"context"
	"testing"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type testNewServiceStore struct {
	runtime *domain.LocalWardRuntime
}

func (s *testNewServiceStore) LoadWardRuntime(context.Context) (*domain.LocalWardRuntime, error) {
	if s.runtime == nil {
		return nil, nil
	}
	copy := *s.runtime
	return &copy, nil
}

func (s *testNewServiceStore) SaveWardRuntime(_ context.Context, runtime domain.LocalWardRuntime) error {
	copy := runtime
	s.runtime = &copy
	return nil
}

type testNewServicePlatformAPI struct {
	createCalls []ports.CreateWardDraftRequest
	staleSecret string
}

func (p *testNewServicePlatformAPI) CreateWardDraft(_ context.Context, req ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
	p.createCalls = append(p.createCalls, req)
	if req.DraftSecretChallenge == p.staleSecret {
		return nil, &ports.PlatformError{Code: "activation_link_expired", HTTPStatus: 401}
	}
	return &ports.CreateWardDraftResponse{
		WardDraftID:        "draft_fresh",
		Site:               req.Site,
		Status:             "pending_activation",
		RequestedDomain:    "fresh.warded.me",
		ResolvedPublicIP:   "203.0.113.10",
		IngressProbeStatus: "reachable",
	}, nil
}

func (*testNewServicePlatformAPI) GetWardDraftStatus(context.Context, string, string, string) (*ports.GetWardDraftStatusResponse, error) {
	return nil, nil
}

func (*testNewServicePlatformAPI) ClaimWardDraft(context.Context, ports.ClaimWardDraftRequest, string) (*ports.ClaimWardDraftResponse, error) {
	return nil, nil
}

func (*testNewServicePlatformAPI) GetWard(context.Context, string, string, string) (*ports.GetWardResponse, error) {
	return nil, nil
}

func (*testNewServicePlatformAPI) GetTLSMaterial(context.Context, string, string, string) (*ports.GetTLSMaterialResponse, error) {
	return nil, nil
}

func (*testNewServicePlatformAPI) ExchangeAuthCode(context.Context, ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	return nil, nil
}

type testUpstreamChecker struct{}

func (testUpstreamChecker) Check(context.Context, int) error {
	return nil
}

func TestNewServiceExecute_RetriesCreateWithFreshDraftSecretWhenChallengeExpired(t *testing.T) {
	t.Parallel()

	store := &testNewServiceStore{
		runtime: &domain.LocalWardRuntime{
			Site:            domain.SiteGlobal,
			WardStatus:      domain.WardStatusInitializing,
			WardDraftSecret: "wdd_stale",
			ListenAddr:      ":443",
		},
	}
	staleChallenge := draftSecretChallenge(store.runtime.WardDraftSecret)
	platformAPI := &testNewServicePlatformAPI{staleSecret: staleChallenge}

	svc := NewService{
		ConfigStore:   store,
		PlatformAPI:   platformAPI,
		UpstreamCheck: testUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), NewInput{
		Site:         domain.SiteGlobal,
		Mode:         "new",
		Spec:         domain.SpecStarter,
		BillingMode:  domain.BillingModeMonthly,
		DomainType:   domain.DomainTypePlatformSubdomain,
		UpstreamPort: 18789,
		ListenPort:   443,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out == nil {
		t.Fatal("expected output")
	}
	if got := len(platformAPI.createCalls); got != 2 {
		t.Fatalf("expected 2 create calls, got %d", got)
	}
	if platformAPI.createCalls[0].DraftSecretChallenge != staleChallenge {
		t.Fatalf("expected first call to use stale challenge")
	}
	if platformAPI.createCalls[1].DraftSecretChallenge == staleChallenge {
		t.Fatal("expected retry to use a fresh challenge")
	}
	if store.runtime == nil {
		t.Fatal("expected runtime to be saved")
	}
	if store.runtime.WardDraftSecret == "wdd_stale" {
		t.Fatal("expected runtime draft secret to be rotated")
	}
	if store.runtime.WardDraftID != "draft_fresh" {
		t.Fatalf("expected saved draft id draft_fresh, got %q", store.runtime.WardDraftID)
	}
}

func TestDiscoverOpenClawPort_UsesGatewayPort(t *testing.T) {
	origHome := userHomeDirFunc
	origRead := readFileFunc
	t.Cleanup(func() {
		userHomeDirFunc = origHome
		readFileFunc = origRead
	})

	userHomeDirFunc = func() (string, error) { return "/tmp/test-home", nil }
	readFileFunc = func(string) ([]byte, error) {
		return []byte(`{"port":9999,"gateway":{"port":18789,"bind":"loopback"}}`), nil
	}

	if got := DiscoverOpenClawPort(); got != 18789 {
		t.Fatalf("expected gateway.port=18789, got %d", got)
	}
}

func TestDiscoverOpenClawPort_FallsBackWhenGatewayPortMissing(t *testing.T) {
	origHome := userHomeDirFunc
	origRead := readFileFunc
	t.Cleanup(func() {
		userHomeDirFunc = origHome
		readFileFunc = origRead
	})

	userHomeDirFunc = func() (string, error) { return "/tmp/test-home", nil }
	readFileFunc = func(string) ([]byte, error) {
		return []byte(`{"port":9999,"gateway":{"bind":"loopback"}}`), nil
	}

	if got := DiscoverOpenClawPort(); got != 18789 {
		t.Fatalf("expected fallback port 18789, got %d", got)
	}
}

func TestDiscoverOpenClawPort_FallsBackOnInvalidJSON(t *testing.T) {
	origHome := userHomeDirFunc
	origRead := readFileFunc
	t.Cleanup(func() {
		userHomeDirFunc = origHome
		readFileFunc = origRead
	})

	userHomeDirFunc = func() (string, error) { return "/tmp/test-home", nil }
	readFileFunc = func(string) ([]byte, error) {
		return []byte(`{"gateway":`), nil
	}

	if got := DiscoverOpenClawPort(); got != 18789 {
		t.Fatalf("expected fallback port 18789, got %d", got)
	}
}
