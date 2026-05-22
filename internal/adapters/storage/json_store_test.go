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
		AuthWhitelist:          []domain.AuthWhitelistRule{{Type: "exact", Path: "/webhook/wechat"}},
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
	if err := store.SavePendingRuntime(context.Background(), runtime); err != nil {
		t.Fatalf("save pending runtime: %v", err)
	}
	if _, err := os.Stat(filepath.Join(baseDir, ".pending", "ward.json")); err != nil {
		t.Fatalf("expected .pending ward file: %v", err)
	}
	pending, err := store.LoadPendingRuntime(context.Background())
	if err != nil {
		t.Fatalf("load pending runtime: %v", err)
	}
	if pending == nil || pending.WardDraftSecret != "wdd_secret" {
		t.Fatalf("unexpected pending runtime: %#v", pending)
	}
	loaded, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load formal runtime: %v", err)
	}
	if loaded != nil {
		t.Fatalf("expected pending runtime to be hidden from formal load, got %#v", loaded)
	}

	runtime.WardDraftID = "draft_abc"
	if err := store.CommitPendingRuntime(context.Background(), runtime); err != nil {
		t.Fatalf("commit draft runtime: %v", err)
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

func TestJSONStoreListWardRuntimes(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(t.TempDir(), "warded")

	// pending-config via SavePendingRuntime
	pending := NewJSONStore(baseDir)
	if err := pending.SavePendingRuntime(context.Background(), domain.LocalWardRuntime{
		Site:            domain.SiteGlobal,
		RequestedDomain: "pending.warded.me",
		BillingMode:     domain.BillingModeMonthly,
		UpdatedAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save pending: %v", err)
	}

	// draft
	draft := NewJSONStore(baseDir)
	if err := draft.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:            domain.SiteGlobal,
		WardDraftID:     "d_list_test",
		WardDraftSecret: "wdd_secret",
		RequestedDomain: "draft.warded.me",
		WardStatus:      domain.WardStatusInitializing,
		BillingMode:     domain.BillingModeMonthly,
		UpdatedAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save draft: %v", err)
	}

	// ward
	ward := NewJSONStore(baseDir)
	if err := ward.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:        domain.SiteGlobal,
		WardID:      "ward_list_test",
		WardSecret:  "wrs_secret",
		Domain:      "live.warded.me",
		WardStatus:  domain.WardStatusActive,
		BillingMode: domain.BillingModeMonthly,
		UpdatedAt:   time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save ward: %v", err)
	}

	store := NewJSONStore(baseDir)
	runtimes, err := store.ListWardRuntimes(context.Background())
	if err != nil {
		t.Fatalf("ListWardRuntimes: %v", err)
	}
	if len(runtimes) != 3 {
		t.Fatalf("expected 3 runtimes, got %d: %+v", len(runtimes), runtimes)
	}
	// first must be pending-config (no IDs)
	if runtimes[0].WardID != "" || runtimes[0].WardDraftID != "" {
		t.Errorf("expected pending-config first, got %+v", runtimes[0])
	}
}

func TestJSONStoreLoadRuntimeByID(t *testing.T) {
	t.Parallel()

	baseDir := filepath.Join(t.TempDir(), "warded")
	draft := NewJSONStore(baseDir)
	if err := draft.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:            domain.SiteGlobal,
		WardDraftID:     "d_byid",
		WardDraftSecret: "wdd_secret",
		RequestedDomain: "byid.warded.me",
		WardStatus:      domain.WardStatusInitializing,
		BillingMode:     domain.BillingModeMonthly,
		UpdatedAt:       time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save draft: %v", err)
	}

	store := NewJSONStore(baseDir)
	rt, err := store.LoadRuntimeByID(context.Background(), "d_byid")
	if err != nil {
		t.Fatalf("LoadRuntimeByID: %v", err)
	}
	if rt.WardDraftID != "d_byid" {
		t.Fatalf("expected d_byid, got %q", rt.WardDraftID)
	}

	// After LoadRuntimeByID, SaveWardRuntime should rename directory correctly.
	rt.WardID = "ward_promoted"
	rt.WardSecret = "wrs_promoted"
	rt.WardDraftSecret = ""
	if err := store.SaveWardRuntime(context.Background(), *rt); err != nil {
		t.Fatalf("SaveWardRuntime after LoadRuntimeByID: %v", err)
	}

	// Old draft dir should be gone; ward dir should exist.
	loaded, err := store.LoadRuntimeByID(context.Background(), "ward_promoted")
	if err != nil {
		t.Fatalf("LoadRuntimeByID after promotion: %v", err)
	}
	if loaded.WardID != "ward_promoted" {
		t.Fatalf("expected ward_promoted, got %q", loaded.WardID)
	}
	// Verify old draft dir no longer exists as a separate entry.
	runtimes, err := store.ListWardRuntimes(context.Background())
	if err != nil {
		t.Fatalf("ListWardRuntimes: %v", err)
	}
	if len(runtimes) != 1 {
		t.Fatalf("expected 1 runtime after promotion, got %d: %+v", len(runtimes), runtimes)
	}
}
