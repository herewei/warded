package application

import (
	"context"
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
