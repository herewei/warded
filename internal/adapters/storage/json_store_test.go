package storage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/herewei/warded/internal/domain"
)

func TestJSONStoreRoundTrip(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(t.TempDir(), "warded")
	store := NewJSONStore(baseDir)
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
		RequestedDomain:        "preferred-subdomain.warded.me",
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

	path := filepath.Join(baseDir, "ward_123", "ward.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected ward file at %s: %v", path, err)
	}

	gotRuntime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if gotRuntime == nil || gotRuntime.WardID != runtime.WardID || gotRuntime.Domain != runtime.Domain {
		t.Fatalf("unexpected runtime: %#v", gotRuntime)
	}
	if gotRuntime.RequestedDomain != runtime.RequestedDomain {
		t.Fatalf("unexpected requested_domain: %#v", gotRuntime)
	}
}

func TestJSONStoreBootstrapPendingThenRename(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(t.TempDir(), "warded")
	store := NewJSONStore(baseDir)
	runtime := domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		Spec:             domain.SpecStarter,
		BillingMode:      domain.BillingModeMonthly,
		DomainType:       domain.DomainTypePlatformSubdomain,
		WardDraftSecret:  "wdd_secret",
		JWTSigningSecret: "jwt_secret",
		ListenAddr:       ":443",
		WardStatus:       domain.WardStatusInitializing,
		UpdatedAt:        time.Now().UTC(),
	}
	if err := store.SaveWardRuntime(context.Background(), runtime); err != nil {
		t.Fatalf("save pending runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, ".pending", "ward.json")); err != nil {
		t.Fatalf("expected .pending ward file: %v", err)
	}

	runtime.WardDraftID = "draft_abc"
	if err := store.SaveWardRuntime(context.Background(), runtime); err != nil {
		t.Fatalf("save draft runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "draft_abc", "ward.json")); err != nil {
		t.Fatalf("expected draft ward file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, ".pending")); !os.IsNotExist(err) {
		t.Fatalf("expected .pending to be renamed away, stat err=%v", err)
	}

	runtime.WardID = "ward_final"
	if err := store.SaveWardRuntime(context.Background(), runtime); err != nil {
		t.Fatalf("save ward runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "ward_final", "ward.json")); err != nil {
		t.Fatalf("expected ward file after claim: %v", err)
	}
}

func TestJSONStoreScanExisting(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(t.TempDir(), "warded")
	store := NewJSONStore(baseDir)
	runtime := domain.LocalWardRuntime{
		Site:             domain.SiteCN,
		WardDraftID:      "draft_scan",
		WardDraftSecret:  "wdd_secret",
		JWTSigningSecret: "jwt_secret",
		Spec:             domain.SpecStarter,
		BillingMode:      domain.BillingModeMonthly,
		DomainType:       domain.DomainTypePlatformSubdomain,
		ListenAddr:       ":443",
		WardStatus:       domain.WardStatusInitializing,
		UpdatedAt:        time.Now().UTC(),
	}
	if err := store.SaveWardRuntime(context.Background(), runtime); err != nil {
		t.Fatalf("seed runtime: %v", err)
	}

	store2 := NewJSONStore(baseDir)
	got, err := store2.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load scanned runtime: %v", err)
	}
	if got == nil || got.WardDraftID != "draft_scan" {
		t.Fatalf("unexpected scanned runtime: %#v", got)
	}
}

func TestJSONStoreMultipleWardDirsFail(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(t.TempDir(), "warded")
	store := NewJSONStore(baseDir)
	for _, id := range []string{"draft_one", "draft_two"} {
		runtime := domain.LocalWardRuntime{
			Site:             domain.SiteGlobal,
			WardDraftID:      id,
			WardDraftSecret:  "wdd_" + id,
			JWTSigningSecret: "jwt_" + id,
			Spec:             domain.SpecStarter,
			BillingMode:      domain.BillingModeMonthly,
			DomainType:       domain.DomainTypePlatformSubdomain,
			ListenAddr:       ":443",
			WardStatus:       domain.WardStatusInitializing,
			UpdatedAt:        time.Now().UTC(),
		}
		other := NewJSONStore(baseDir)
		if err := other.SaveWardRuntime(context.Background(), runtime); err != nil {
			t.Fatalf("seed runtime %s: %v", id, err)
		}
	}

	_, err := store.LoadWardRuntime(context.Background())
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
}
