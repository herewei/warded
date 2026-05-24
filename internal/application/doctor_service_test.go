package application

import (
	"context"
	"errors"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type doctorServeMonitorStub struct {
	running bool
	detail  string
}

func (s doctorServeMonitorStub) CheckServe(context.Context) (bool, string) {
	return s.running, s.detail
}

type doctorTLSMonitorStub struct {
	fallback bool
	detail   string
	addr     *string
}

type preflightDataDirCheckerStub struct {
	err error
}

func (s preflightDataDirCheckerStub) EnsureWritable(string) error {
	return s.err
}

type preflightListenResolverStub struct {
	host   string
	port   int
	family domain.IngressFamily
	err    error
}

func (s preflightListenResolverStub) ResolveListen(*domain.LocalWardRuntime, string, string, int, bool, bool, bool) (string, int, domain.IngressFamily, error) {
	return s.host, s.port, s.family, s.err
}

type preflightListenCheckerStub struct {
	err error
}

func (s preflightListenCheckerStub) EnsureAvailable(string) error {
	return s.err
}

type preflightUpstreamCheckerStub struct {
	err error
}

func (s preflightUpstreamCheckerStub) Check(context.Context, string) error {
	return s.err
}

type preflightDNSResolverStub struct {
	err error
}

func (s preflightDNSResolverStub) LookupHost(context.Context, string) ([]string, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []string{"203.0.113.10"}, nil
}

type preflightChallengeStub struct {
	value string
	err   error
}

func (s preflightChallengeStub) GenerateProbeChallenge() (string, error) {
	return s.value, s.err
}

type preflightProbeServerStub struct {
	err error
}

func (s preflightProbeServerStub) StartProbeServer(context.Context, string) (func(context.Context) error, error) {
	if s.err != nil {
		return nil, s.err
	}
	return func(context.Context) error { return nil }, nil
}

type preflightIngressProbeFactoryStub struct {
	api ports.IngressProbeAPI
	err error
}

func (s preflightIngressProbeFactoryStub) NewIngressProbeAPI(domain.Site, string, string, string) (ports.IngressProbeAPI, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.api, nil
}

type mockOpenClawCLI struct {
	values map[string]string
}

func newMockOpenClawCLI(values map[string]string) *mockOpenClawCLI {
	return &mockOpenClawCLI{values: values}
}

func (m *mockOpenClawCLI) Get(key string) (string, error) {
	if v, ok := m.values[key]; ok {
		return v, nil
	}
	return "", fmt.Errorf("key not found: %s", key)
}

func (m *mockOpenClawCLI) Set(key string, value string) error {
	if m.values == nil {
		m.values = make(map[string]string)
	}
	m.values[key] = value
	return nil
}

func (m *mockOpenClawCLI) Validate() error {
	return nil
}

type preflightIngressAPIStub struct {
	resp *ports.IngressProbeResponse
	err  error
	req  ports.IngressProbeRequest
}

func (s *preflightIngressAPIStub) CreateIngressProbe(_ context.Context, req ports.IngressProbeRequest) (*ports.IngressProbeResponse, error) {
	s.req = req
	if s.err != nil {
		return nil, s.err
	}
	return s.resp, nil
}

func newTestPreflightService(api ports.IngressProbeAPI) DoctorPreflightService {
	return DoctorPreflightService{
		DataDirCheck:   preflightDataDirCheckerStub{},
		ListenResolver: preflightListenResolverStub{host: "127.0.0.1", port: 8443, family: domain.IngressFamilyIPv4},
		ListenCheck:    preflightListenCheckerStub{},
		UpstreamCheck:  preflightUpstreamCheckerStub{},
		DNSResolver:    preflightDNSResolverStub{},
		ChallengeGen:   preflightChallengeStub{value: "challenge-123"},
		ProbeServer:    preflightProbeServerStub{},
		IngressProbe:   preflightIngressProbeFactoryStub{api: api},
	}
}

func TestDoctorPreflightService_Execute_Success(t *testing.T) {
	api := &preflightIngressAPIStub{resp: &ports.IngressProbeResponse{
		Result:           "reachable",
		ResolvedPublicIP: "203.0.113.10",
		RequestID:        "req_ok",
	}}
	service := newTestPreflightService(api)

	out, err := service.Execute(context.Background(), DoctorPreflightInput{
		DataDir:      t.TempDir(),
		Site:         "global",
		ListenHost:   "127.0.0.1",
		ListenPort:   8443,
		UpstreamAddr: "127.0.0.1:18789",
		DomainType:   string(domain.DomainTypePlatformSubdomain),
		Version:      "test",
	})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if len(out.Results) != 5 {
		t.Fatalf("expected 5 checks without custom DNS, got %d: %#v", len(out.Results), out.Results)
	}
	if out.ResolvedPublicIP != "203.0.113.10" {
		t.Fatalf("expected resolved public IP, got %q", out.ResolvedPublicIP)
	}
	if api.req.ProbeChallenge != "challenge-123" || api.req.ListenPort != 8443 {
		t.Fatalf("unexpected ingress probe request: %#v", api.req)
	}
	for i, result := range out.Results {
		if result.Number != i+1 {
			t.Fatalf("expected continuous check number %d, got %#v", i+1, result)
		}
	}
}

func TestDoctorPreflightService_Execute_IngressReasonPreserved(t *testing.T) {
	api := &preflightIngressAPIStub{resp: &ports.IngressProbeResponse{
		Result:    "unreachable",
		Reason:    "tcp_connect_failed",
		RequestID: "req_probe",
	}}
	service := newTestPreflightService(api)

	out, err := service.Execute(context.Background(), DoctorPreflightInput{
		DataDir:      t.TempDir(),
		Site:         "global",
		ListenHost:   "127.0.0.1",
		ListenPort:   8443,
		UpstreamAddr: "127.0.0.1:18789",
		DomainType:   string(domain.DomainTypePlatformSubdomain),
		Version:      "test",
	})
	if err == nil {
		t.Fatal("expected ingress probe error")
	}
	var platformErr *ports.PlatformError
	if !errors.As(err, &platformErr) {
		t.Fatalf("expected PlatformError, got %T: %v", err, err)
	}
	if platformErr.Code != "ingress_unreachable" || platformErr.Reason != "tcp_connect_failed" {
		t.Fatalf("unexpected platform error: %#v", platformErr)
	}
	if out.ProbeReason != "tcp_connect_failed" {
		t.Fatalf("expected output probe reason, got %q", out.ProbeReason)
	}
}

func (s doctorTLSMonitorStub) CheckServeTLS(_ context.Context, addr string, _ string) (bool, string) {
	if s.addr != nil {
		*s.addr = addr
	}
	return s.fallback, s.detail
}

func TestDoctorService_Execute_TLSFallbackActive(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:           "ward_123",
		WardStatus:       domain.WardStatusActive,
		Domain:           "demo.warded.me",
		ListenPort:       8443,
		ListenHost:       "0.0.0.0",
		IngressFamily:    domain.IngressFamilyIPv4,
		JWTSigningSecret: "jwt_secret",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	var tlsAddr string
	service := DoctorService{
		ConfigStore:     store,
		ServeMonitor:    doctorServeMonitorStub{running: true, detail: "warded.service is active"},
		ServeTLSMonitor: doctorTLSMonitorStub{fallback: true, detail: "serving fallback self-signed certificate for demo.warded.me", addr: &tlsAddr},
	}
	out, err := service.Execute(context.Background(), DoctorInput{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if tlsAddr != "0.0.0.0:8443" {
		t.Fatalf("expected TLS probe addr from listen_host/listen_port, got %q", tlsAddr)
	}

	for _, result := range out.Results {
		if result.Name != "tls_platform_cert" {
			continue
		}
		if result.State == CheckOK {
			t.Fatalf("expected tls_platform_cert to be false, got %#v", result)
		}
		if result.Detail == "" {
			t.Fatalf("expected tls_platform_cert detail, got %#v", result)
		}
		return
	}
	t.Fatal("expected tls_platform_cert result")
}

func TestDoctorService_Execute_OpenClawBaselineUnsafe(t *testing.T) {
	dir := t.TempDir()
	service := DoctorService{
		ConfigStore: storage.NewJSONStore(dir),
		OpenClawCLI: newMockOpenClawCLI(map[string]string{
			"gateway.bind": "lan",
			"gateway.port": "18789",
		}),
	}

	out, err := service.Execute(context.Background(), DoctorInput{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, result := range out.Results {
		if result.Name != "openclaw_baseline" {
			continue
		}
		if result.State == CheckOK {
			t.Fatalf("expected openclaw_baseline to fail, got %#v", result)
		}
		if result.Detail == "" {
			t.Fatalf("expected openclaw_baseline detail, got %#v", result)
		}
		return
	}
	t.Fatal("expected openclaw_baseline result")
}

func TestDoctorService_Execute_OpenClawBaselineLoopbackReachableOnly(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	origLocalIPv4Addrs := localIPv4AddrsFunc
	localIPv4AddrsFunc = func() []string { return nil }
	defer func() { localIPv4AddrsFunc = origLocalIPv4Addrs }()

	dir := t.TempDir()
	service := DoctorService{
		ConfigStore: storage.NewJSONStore(dir),
		OpenClawCLI: newMockOpenClawCLI(map[string]string{
			"gateway.bind": "loopback",
			"gateway.port": fmt.Sprintf("%d", port),
		}),
	}

	out, err := service.Execute(context.Background(), DoctorInput{})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, result := range out.Results {
		if result.Name != "openclaw_baseline" {
			continue
		}
		if result.State != CheckOK {
			t.Fatalf("expected openclaw_baseline to pass, got %#v", result)
		}
		if result.Detail != fmt.Sprintf("gateway.bind=loopback port=%d loopback=true", port) {
			t.Fatalf("expected compact loopback detail, got %#v", result)
		}
		return
	}
	t.Fatal("expected openclaw_baseline result")
}
