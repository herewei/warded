package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/herewei/warded/internal/domain"
)

func TestJSONStoreRoundTrip(t *testing.T) {
	t.Parallel()

	store := NewJSONStore(filepath.Join(t.TempDir(), "warded"))
	now := time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC)

	runtime := domain.LocalWardRuntime{
		Site:                   domain.SiteGlobal,
		WardDraftID:            "draft_123",
		WardDraftSecret:        "wdd_secret",
		WardID:                 "ward_123",
		WardSecret:             "wrd_secret",
		JWTSigningSecret:       "jwt_secret",
		WardStatus:             domain.WardStatusActive,
		Spec:                   domain.SpecPro,
		BillingMode:            domain.BillingModeMonthly,
		ActivationMode:         domain.ActivationModeTrial,
		DomainType:             domain.DomainTypePlatformSubdomain,
		Domain:                 "a1b2c3d4.warded.me",
		UpstreamPort:           3000,
		ListenAddr:             ":443",
		TLSMode:                domain.TLSModePlatformWildcard,
		LastPublicIP:           "1.2.3.4",
		LastPublicIPReportedAt: now,
		ExpiresAt:              now.Add(24 * time.Hour),
		WebhookAllowPaths:      []string{"/webhook/wechat"},
		UpdatedAt:              now,
	}
	if err := store.SaveWardRuntime(context.Background(), runtime); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	gotRuntime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if gotRuntime == nil || gotRuntime.WardID != runtime.WardID || gotRuntime.Domain != runtime.Domain {
		t.Fatalf("unexpected runtime: %#v", gotRuntime)
	}
	if gotRuntime.JWTSigningSecret != runtime.JWTSigningSecret {
		t.Fatalf("unexpected jwt_signing_secret: %#v", gotRuntime)
	}
	if gotRuntime.WardDraftSecret != runtime.WardDraftSecret || gotRuntime.WardSecret != runtime.WardSecret {
		t.Fatalf("unexpected bearer secrets: %#v", gotRuntime)
	}
	if gotRuntime.ActivationMode != runtime.ActivationMode {
		t.Fatalf("unexpected activation_mode: %#v", gotRuntime)
	}
}
