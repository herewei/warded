package e2e_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if !strings.Contains(out, "Not attached") {
		t.Errorf("expected 'Not attached' in output, got: %s", out)
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
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
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
}
