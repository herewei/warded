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

func (s *testNewServiceStore) LoadPendingRuntime(context.Context) (*domain.LocalWardRuntime, error) {
	if s.runtime == nil {
		return nil, nil
	}
	copy := *s.runtime
	return &copy, nil
}

func (s *testNewServiceStore) SavePendingRuntime(_ context.Context, runtime domain.LocalWardRuntime) error {
	copy := runtime
	s.runtime = &copy
	return nil
}

func (s *testNewServiceStore) CommitPendingRuntime(_ context.Context, runtime domain.LocalWardRuntime) error {
	copy := runtime
	s.runtime = &copy
	return nil
}

func (s *testNewServiceStore) ListWardRuntimes(context.Context) ([]domain.LocalWardRuntime, error) {
	if s.runtime != nil {
		return []domain.LocalWardRuntime{*s.runtime}, nil
	}
	return nil, nil
}

func (s *testNewServiceStore) LoadRuntimeByID(_ context.Context, _ string) (*domain.LocalWardRuntime, error) {
	return s.runtime, nil
}

type testNewServiceDraftAPI struct {
	createCalls []ports.CreateWardDraftRequest
	staleSecret string
}

func (p *testNewServiceDraftAPI) CreateWardDraft(_ context.Context, req ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
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

func (*testNewServiceDraftAPI) GetWardDraftStatus(context.Context, string, string, string) (*ports.GetWardDraftStatusResponse, error) {
	return nil, nil
}

func (*testNewServiceDraftAPI) ClaimWardDraft(context.Context, ports.ClaimWardDraftRequest, string) (*ports.ClaimWardDraftResponse, error) {
	return nil, nil
}

type testUpstreamChecker struct{}

func (testUpstreamChecker) Check(context.Context, string) error {
	return nil
}

func TestNewServiceExecute_RetriesCreateWithFreshDraftSecretWhenChallengeExpired(t *testing.T) {
	t.Parallel()

	store := &testNewServiceStore{
		runtime: &domain.LocalWardRuntime{
			Site:            domain.SiteGlobal,
			WardStatus:      domain.WardStatusInitializing,
			WardDraftSecret: "wdd_stale",
			ListenAddr:      "0.0.0.0:443",
		},
	}
	staleChallenge := draftSecretChallenge(store.runtime.WardDraftSecret)
	draftAPI := &testNewServiceDraftAPI{staleSecret: staleChallenge}

	svc := NewService{
		ConfigStore:   store,
		DraftAPI:      draftAPI,
		UpstreamCheck: testUpstreamChecker{},
	}

	out, err := svc.Execute(context.Background(), NewInput{
		Site:          domain.SiteGlobal,
		Spec:          domain.SpecStarter,
		BillingMode:   domain.BillingModeMonthly,
		DomainType:    domain.DomainTypePlatformSubdomain,
		UpstreamAddr:  "127.0.0.1:18789",
		ListenPort:    443,
		ListenHost:    "0.0.0.0",
		IngressFamily: domain.IngressFamilyIPv4,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if out == nil {
		t.Fatal("expected output")
	}
	if got := len(draftAPI.createCalls); got != 2 {
		t.Fatalf("expected 2 create calls, got %d", got)
	}
	if draftAPI.createCalls[0].DraftSecretChallenge != staleChallenge {
		t.Fatalf("expected first call to use stale challenge")
	}
	if draftAPI.createCalls[1].DraftSecretChallenge == staleChallenge {
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
