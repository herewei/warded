package application

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

type doctorServeCheckerStub struct {
	running bool
	detail  string
}

func (s doctorServeCheckerStub) CheckServe(context.Context) (bool, string) {
	return s.running, s.detail
}

type doctorTLSCheckerStub struct {
	fallback bool
	detail   string
}

func (s doctorTLSCheckerStub) CheckServeTLS(context.Context, string, string) (bool, string) {
	return s.fallback, s.detail
}

func TestDoctorService_Execute_TLSFallbackActive(t *testing.T) {
	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:           "ward_123",
		WardStatus:       domain.WardStatusActive,
		Domain:           "demo.warded.me",
		ListenAddr:       ":443",
		JWTSigningSecret: "jwt_secret",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	service := DoctorService{
		ConfigStore:     store,
		ServeChecker:    doctorServeCheckerStub{running: true, detail: "warded.service is active"},
		ServeTLSChecker: doctorTLSCheckerStub{fallback: true, detail: "serving fallback self-signed certificate for demo.warded.me"},
	}
	out, err := service.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, result := range out.Results {
		if result.Name != "tls_platform_cert" {
			continue
		}
		if result.OK {
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
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	if err := os.MkdirAll(filepath.Join(tempHome, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempHome, ".openclaw", "openclaw.json"), []byte(`{"port":18789,"gateway":{"bind":"lan"}}`), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}

	dir := t.TempDir()
	service := DoctorService{
		ConfigStore: storage.NewJSONStore(dir),
	}

	out, err := service.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, result := range out.Results {
		if result.Name != "openclaw_baseline" {
			continue
		}
		if result.OK {
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
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port

	if err := os.MkdirAll(filepath.Join(tempHome, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempHome, ".openclaw", "openclaw.json"), []byte(fmt.Sprintf(`{"port":%d,"gateway":{"bind":"loopback"}}`, port)), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}

	origLocalIPv4Addrs := localIPv4AddrsFunc
	localIPv4AddrsFunc = func() []string { return nil }
	defer func() { localIPv4AddrsFunc = origLocalIPv4Addrs }()

	dir := t.TempDir()
	service := DoctorService{
		ConfigStore: storage.NewJSONStore(dir),
	}

	out, err := service.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	for _, result := range out.Results {
		if result.Name != "openclaw_baseline" {
			continue
		}
		if !result.OK {
			t.Fatalf("expected openclaw_baseline to pass, got %#v", result)
		}
		if result.Detail == "" || result.Detail == "gateway.bind=loopback" {
			t.Fatalf("expected detailed baseline probe output, got %#v", result)
		}
		return
	}
	t.Fatal("expected openclaw_baseline result")
}
