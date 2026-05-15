package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

// TestE2E_Status_LocalWithoutRuntime verifies that `warded status --local`
// shows "Not attached" when no runtime exists.
func TestE2E_Status_LocalWithoutRuntime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	out, err := runStatus(t, []string{
		"--local",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "No pending setup.") {
		t.Errorf("expected 'No pending setup.' in output, got: %s", out)
	}
}

// TestE2E_Status_LocalWithActiveWard verifies that `warded status --local`
// shows the active ward details.
func TestE2E_Status_LocalWithActiveWard(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:           domain.SiteGlobal,
		WardID:         "ward_123",
		WardSecret:     "wrd_secret",
		WardStatus:     domain.WardStatusActive,
		Spec:           domain.SpecStarter,
		BillingMode:    domain.BillingModeMonthly,
		ActivationMode: domain.ActivationModeTrial,
		DomainType:     domain.DomainTypePlatformSubdomain,
		Domain:         "demo.warded.me",
		UpstreamPort:   18789,
		ListenAddr:     ":443",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runStatus(t, []string{
		"--local",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "demo.warded.me") {
		t.Errorf("expected domain in output, got: %s", out)
	}
	if !strings.Contains(out, "Billing") || !strings.Contains(out, "monthly") {
		t.Errorf("expected billing in output, got: %s", out)
	}
	if !strings.Contains(out, "Activation") || !strings.Contains(out, "trial") {
		t.Errorf("expected activation mode in output, got: %s", out)
	}
}

// TestE2E_Status_LocalWithPendingDraft verifies that `warded status --local`
// shows pending setup status when a draft exists but is not yet activated.
func TestE2E_Status_LocalWithPendingDraft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:            domain.SiteCN,
		WardDraftID:     "d_123",
		WardDraftSecret: "wdd_secret",
		WardStatus:      domain.WardStatusInitializing,
		RequestedDomain: "abcd.warded.cn",
		ActivationURL:   "https://warded.cn/activate/d_123",
		UpstreamPort:    18789,
		BillingMode:     domain.BillingModeMonthly,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runStatus(t, []string{
		"--local",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "abcd.warded.cn") {
		t.Errorf("expected requested domain in output, got: %s", out)
	}
	if !strings.Contains(out, "pending") {
		t.Errorf("expected 'pending' in output, got: %s", out)
	}
	if !strings.Contains(out, "warded new --commit") {
		t.Errorf("expected recommit hint in output, got: %s", out)
	}
}

// TestE2E_Status_MultipleRuntimesShowsList verifies that `warded status` with
// multiple local runtimes renders an index list instead of reporting an error.
func TestE2E_Status_MultipleRuntimesShowsList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Now().UTC()

	// Save two runtimes using separate store instances to bypass the single-ward guard.
	s1 := storage.NewJSONStore(dir)
	if err := s1.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:        domain.SiteGlobal,
		WardID:      "ward_multi_a",
		WardSecret:  "wrs_a",
		Domain:      "alpha.warded.me",
		WardStatus:  domain.WardStatusActive,
		BillingMode: domain.BillingModeMonthly,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("save ward a: %v", err)
	}
	s2 := storage.NewJSONStore(dir)
	if err := s2.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:        domain.SiteGlobal,
		WardID:      "ward_multi_b",
		WardSecret:  "wrs_b",
		Domain:      "beta.warded.me",
		WardStatus:  domain.WardStatusActive,
		BillingMode: domain.BillingModeMonthly,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("save ward b: %v", err)
	}

	out, err := runStatus(t, []string{"--local", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Multiple local wards found") {
		t.Errorf("expected list header, got: %s", out)
	}
	if !strings.Contains(out, "alpha.warded.me") || !strings.Contains(out, "beta.warded.me") {
		t.Errorf("expected both domains in list, got: %s", out)
	}
	if !strings.Contains(out, "warded status <index>") {
		t.Errorf("expected Next hint in list, got: %s", out)
	}
}

// TestE2E_Status_IndexSelectionShowsDetail verifies that `warded status 1`
// with multiple runtimes shows the detail for the selected runtime.
func TestE2E_Status_IndexSelectionShowsDetail(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Now().UTC()

	s1 := storage.NewJSONStore(dir)
	if err := s1.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:        domain.SiteGlobal,
		WardID:      "ward_idx_a",
		WardSecret:  "wrs_a",
		Domain:      "idx-alpha.warded.me",
		WardStatus:  domain.WardStatusActive,
		BillingMode: domain.BillingModeMonthly,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("save ward a: %v", err)
	}
	s2 := storage.NewJSONStore(dir)
	if err := s2.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:        domain.SiteGlobal,
		WardID:      "ward_idx_b",
		WardSecret:  "wrs_b",
		Domain:      "idx-beta.warded.me",
		WardStatus:  domain.WardStatusExpired,
		BillingMode: domain.BillingModeMonthly,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("save ward b: %v", err)
	}

	out, err := runStatus(t, []string{"--local", "--data-dir=" + dir, "1"})
	if err != nil {
		t.Fatalf("status 1: %v\noutput: %s", err, out)
	}
	// Index 1 is the first sorted ward dir (ward_idx_a < ward_idx_b alphabetically)
	if !strings.Contains(out, "idx-alpha.warded.me") {
		t.Errorf("expected idx-alpha.warded.me in detail, got: %s", out)
	}
	if strings.Contains(out, "Multiple local wards found") {
		t.Errorf("expected detail view, not list, got: %s", out)
	}
}

// TestE2E_Status_AmbiguousDomainShowsErrorAndList verifies that --domain
// matching two runtimes produces an error and shows the index list.
func TestE2E_Status_AmbiguousDomainShowsErrorAndList(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	now := time.Now().UTC()

	s1 := storage.NewJSONStore(dir)
	if err := s1.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:        domain.SiteGlobal,
		WardID:      "ward_ambig_draft",
		WardSecret:  "wrs_a",
		Domain:      "shared.warded.me",
		WardStatus:  domain.WardStatusExpired,
		BillingMode: domain.BillingModeMonthly,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("save ward a: %v", err)
	}
	s2 := storage.NewJSONStore(dir)
	if err := s2.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:        domain.SiteGlobal,
		WardID:      "ward_ambig_live",
		WardSecret:  "wrs_b",
		Domain:      "shared.warded.me",
		WardStatus:  domain.WardStatusActive,
		BillingMode: domain.BillingModeMonthly,
		UpdatedAt:   now,
	}); err != nil {
		t.Fatalf("save ward b: %v", err)
	}

	_, err := runStatus(t, []string{"--local", "--data-dir=" + dir, "--domain=shared.warded.me"})
	if err == nil {
		t.Fatal("expected error for ambiguous domain")
	}
	if !strings.Contains(err.Error(), "matches multiple") {
		t.Fatalf("expected 'matches multiple' error, got: %v", err)
	}
}

// TestE2E_Status_AutoClaimsConvertedDraft verifies that `warded status`
// (non-local) auto-claims a converted draft from the platform.
func TestE2E_Status_AutoClaimsConvertedDraft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{
		AutoConvertAfterPolls: 1,
	})

	// First, create a draft via `warded new --commit`
	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v", err)
	}

	// Now run `warded status` — the mock auto-converts on first GET,
	// and StatusService should auto-claim it.
	out, err := runStatus(t, []string{
		"--platform-origin=" + mock.URL,
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}

	// After auto-claim, the ward should be active
	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after status: %v", err)
	}
	if runtime.WardID == "" {
		t.Error("expected ward_id to be set after auto-claim")
	}
	if runtime.WardDraftSecret != "" {
		t.Errorf("expected ward_draft_secret to be cleared after claim, got %q", runtime.WardDraftSecret)
	}
	if runtime.WardStatus != "active" {
		t.Errorf("expected ward_status=active, got %s", runtime.WardStatus)
	}
}

// TestE2E_Integrate_Preview verifies that `warded integrate` in preview mode
// shows patch_required without modifying the config file.
func TestE2E_Integrate_Preview(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := storage.NewJSONStore(dataDir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	original := `{"gateway":{"controlUi":{"allowedOrigins":["http://127.0.0.1:18789"]}}}`
	if err := os.WriteFile(openClawConfig, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--data-dir=" + dataDir,
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "patch_required") {
		t.Errorf("expected patch_required, got: %s", out)
	}

	data, err := os.ReadFile(openClawConfig)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != original {
		t.Fatalf("preview mode should not modify file, got %s", string(data))
	}
}

// TestE2E_Integrate_Apply verifies that `warded integrate --apply` updates
// the config file and creates a backup.
func TestE2E_Integrate_Apply(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := storage.NewJSONStore(dataDir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"controlUi":{"allowedOrigins":["http://127.0.0.1:18789"]}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--apply",
		"--data-dir=" + dataDir,
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "updated") {
		t.Errorf("expected updated status, got: %s", out)
	}

	data, err := os.ReadFile(openClawConfig)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "https://demo.warded.me") {
		t.Fatalf("expected config to contain required origin, got %s", string(data))
	}
}

// TestE2E_Integrate_AlreadyConfigured verifies that `warded integrate` shows
// already_configured when the origin is already present.
func TestE2E_Integrate_AlreadyConfigured(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := storage.NewJSONStore(dataDir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"controlUi":{"allowedOrigins":["https://demo.warded.me"]}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--data-dir=" + dataDir,
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "already_configured") {
		t.Errorf("expected already_configured, got: %s", out)
	}
}

func TestE2E_Integrate_BaselinePreview(t *testing.T) {
	t.Parallel()

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":18789,"bind":"lan"}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--baseline",
		"--adopt-public-port=18789",
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate baseline: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "baseline_repair_required") {
		t.Errorf("expected baseline_repair_required, got: %s", out)
	}
	if !strings.Contains(out, "Desired bind/port: loopback / 19789") {
		t.Errorf("expected desired bind/port in output, got: %s", out)
	}
}

func TestE2E_Integrate_BaselinePreviewBindOnly(t *testing.T) {
	t.Parallel()

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":18789,"bind":"lan"}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--baseline",
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate baseline bind-only: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "baseline_repair_required") {
		t.Errorf("expected baseline_repair_required, got: %s", out)
	}
	if !strings.Contains(out, "Desired bind/port: loopback / 18789") {
		t.Errorf("expected desired bind/port loopback / 18789, got: %s", out)
	}
}

func TestE2E_Integrate_AdoptPublicPortRequiresBaseline(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := storage.NewJSONStore(dataDir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":18789,"bind":"lan","controlUi":{"allowedOrigins":["https://demo.warded.me"]}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--adopt-public-port=18789",
		"--apply",
		"--data-dir=" + dataDir,
		"--config-file=" + openClawConfig,
	})
	if err == nil {
		t.Fatalf("expected integrate to fail without --baseline\noutput: %s", out)
	}
	if !strings.Contains(err.Error(), "--adopt-public-port requires --baseline") {
		t.Fatalf("expected adopt-public-port validation error, got: %v", err)
	}
}

func TestE2E_Integrate_RepairRequiresBaseline(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := storage.NewJSONStore(dataDir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":18789,"bind":"lan","controlUi":{"allowedOrigins":["https://demo.warded.me"]}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--repair",
		"--data-dir=" + dataDir,
		"--config-file=" + openClawConfig,
	})
	if err == nil {
		t.Fatalf("expected integrate to fail without --baseline\noutput: %s", out)
	}
	if !strings.Contains(err.Error(), "--repair requires --baseline") {
		t.Fatalf("expected repair validation error, got: %v", err)
	}
}

func TestE2E_Integrate_AdoptPublicPortMismatch(t *testing.T) {
	t.Parallel()

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":19789,"bind":"lan"}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--baseline",
		"--adopt-public-port=18789",
		"--repair",
		"--config-file=" + openClawConfig,
	})
	if err == nil {
		t.Fatalf("expected integrate baseline mismatch to fail\noutput: %s", out)
	}
	if !strings.Contains(err.Error(), "adopt-public-port 18789 does not match OpenClaw port 19789") {
		t.Fatalf("expected adopt-public-port mismatch error, got: %v", err)
	}
}

func TestE2E_Integrate_BaselineApply(t *testing.T) {
	t.Parallel()

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":18789,"bind":"lan"}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--baseline",
		"--adopt-public-port=18789",
		"--repair",
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate baseline repair: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "baseline_repaired_restart_required") {
		t.Errorf("expected baseline_repaired_restart_required, got: %s", out)
	}
	if !strings.Contains(out, "restart OpenClaw gateway") {
		t.Errorf("expected restart guidance, got: %s", out)
	}

	data, err := os.ReadFile(openClawConfig)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"bind": "loopback"`) {
		t.Fatalf("expected config to set loopback bind, got %s", string(data))
	}
	if !strings.Contains(string(data), `"port": 19789`) {
		t.Fatalf("expected config to move port to 19789, got %s", string(data))
	}
}

func TestE2E_Integrate_BaselineRepairBackwardCompat(t *testing.T) {
	t.Parallel()

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":18789,"bind":"lan"}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--baseline",
		"--adopt-public-port=18789",
		"--apply",
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate baseline apply backward compat: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "baseline_repaired_restart_required") {
		t.Errorf("expected baseline_repaired_restart_required via --apply backward compat, got: %s", out)
	}
}

func TestE2E_Integrate_BaselineRepairBindOnly(t *testing.T) {
	t.Parallel()

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":18789,"bind":"lan"}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--baseline",
		"--repair",
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate baseline repair bind-only: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "baseline_repaired") {
		t.Errorf("expected baseline_repaired, got: %s", out)
	}
	if strings.Contains(out, "restart OpenClaw") {
		t.Errorf("expected no restart guidance for bind-only repair, got: %s", out)
	}

	data, err := os.ReadFile(openClawConfig)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), `"bind": "loopback"`) {
		t.Fatalf("expected config to set loopback bind, got %s", string(data))
	}
}

func TestE2E_Integrate_BaselineAlreadyConfigured(t *testing.T) {
	t.Parallel()

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"port":18789,"bind":"loopback"}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	out, err := runIntegrate(t, []string{
		"--agent=openclaw",
		"--baseline",
		"--config-file=" + openClawConfig,
	})
	if err != nil {
		t.Fatalf("integrate baseline already-configured: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "already_configured") {
		t.Fatalf("expected already_configured, got: %s", out)
	}
	if strings.Contains(out, "Suggested patch:") {
		t.Fatalf("expected no suggested patch for already_configured baseline, got: %s", out)
	}
}

// TestE2E_Doctor_OpenClawIntegration verifies that `warded doctor` checks
// the openclaw integration status.
func TestE2E_Doctor_OpenClawIntegration(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)

	home := tempHome
	if err := os.MkdirAll(filepath.Join(home, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".openclaw", "openclaw.json"), []byte(`{"gateway":{"controlUi":{"allowedOrigins":["http://127.0.0.1:18789"]}}}`), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:           "ward_123",
		WardStatus:       domain.WardStatusActive,
		Domain:           "demo.warded.me",
		JWTSigningSecret: "jwt_secret",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runDoctor(t, []string{
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("doctor: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "openclaw_integration") {
		t.Errorf("expected openclaw_integration in output, got: %s", out)
	}
	if !strings.Contains(out, "openclaw_baseline") {
		t.Errorf("expected openclaw_baseline in output, got: %s", out)
	}
	if !strings.Contains(out, "gateway.bind=unset") {
		t.Errorf("expected baseline detail in output, got: %s", out)
	}
}

func TestE2E_Doctor_PendingSetupHeader(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	if err := os.MkdirAll(filepath.Join(tempHome, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempHome, ".openclaw", "openclaw.json"), []byte(`{"gateway":{"port":18789,"bind":"loopback"}}`), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:            domain.SiteCN,
		WardDraftID:     "d_123",
		WardDraftSecret: "wdd_secret",
		WardStatus:      domain.WardStatusInitializing,
		RequestedDomain: "abcd.warded.cn",
		ActivationURL:   "https://warded.cn/activate/d_123",
		UpstreamPort:    18789,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runDoctor(t, []string{
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("doctor: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Ward: abcd.warded.cn (pending)") {
		t.Fatalf("expected pending ward header, got: %s", out)
	}
	if strings.Contains(out, "(not configured)") {
		t.Fatalf("expected pending setup instead of not configured, got: %s", out)
	}
	if strings.Contains(out, "non_loopback=false") {
		t.Fatalf("expected compact baseline detail, got: %s", out)
	}
}

func TestE2E_Doctor_BaselineMode(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	if err := os.MkdirAll(filepath.Join(tempHome, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempHome, ".openclaw", "openclaw.json"), []byte(`{"gateway":{"port":18789,"bind":"lan"}}`), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}

	dir := t.TempDir()
	out, err := runDoctor(t, []string{
		"--data-dir=" + dir,
		"--agent=openclaw",
		"--baseline",
	})
	if err != nil {
		t.Fatalf("doctor baseline: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "1. [") {
		t.Errorf("expected numbered checklist, got: %s", out)
	}
	if !strings.Contains(out, "config file exists") {
		t.Errorf("expected config file check, got: %s", out)
	}
	if !strings.Contains(out, "gateway.bind is loopback") {
		t.Errorf("expected gateway.bind check, got: %s", out)
	}
	if !strings.Contains(out, "Summary:") {
		t.Errorf("expected summary, got: %s", out)
	}
	if !strings.Contains(out, "warded integrate --agent openclaw --baseline repair") {
		t.Errorf("expected repair hint, got: %s", out)
	}
}

func TestE2E_Doctor_BaselineModeUnsupportedAgent(t *testing.T) {
	dir := t.TempDir()
	_, err := runDoctor(t, []string{
		"--data-dir=" + dir,
		"--agent=somethingelse",
		"--baseline",
	})
	if err == nil {
		t.Fatal("expected error for unsupported agent")
	}
	if !strings.Contains(err.Error(), "baseline diagnosis currently only supports") {
		t.Fatalf("expected unsupported agent error, got: %v", err)
	}
}

func TestE2E_Doctor_BaselineModeHealthy(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	if err := os.MkdirAll(filepath.Join(tempHome, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tempHome, ".openclaw", "openclaw.json"), []byte(`{"gateway":{"port":18789,"bind":"loopback"}}`), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}

	dir := t.TempDir()
	out, err := runDoctor(t, []string{
		"--data-dir=" + dir,
		"--agent=openclaw",
		"--baseline",
	})
	if err != nil {
		t.Fatalf("doctor baseline: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Summary: 3/3 checks passed") {
		t.Errorf("expected healthy baseline to show 3/3 checks passed (INFO excluded), got: %s", out)
	}
	if !strings.Contains(out, "Baseline: acceptable") {
		t.Errorf("expected acceptable baseline, got: %s", out)
	}
	if !strings.Contains(out, "baseline acceptable, proceed to warded new") {
		t.Errorf("expected next step for healthy baseline, got: %s", out)
	}
}
