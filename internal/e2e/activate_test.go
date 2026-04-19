package e2e_test

// init_test.go contains cobra-command-level tests for `warded activate`,
// organised in two tiers:
//
//   - Tier 2 (HTTP mock)  — always runs, local httptest.Server only.
//   - Tier 3 (live)       — requires -platform-url flag; skipped otherwise.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

// ── Tier 2: local HTTP mock (always runs) ─────────────────────────────────────

// TestE2E_ActivateCmd_HTTPMode_UserAgent verifies the CLI sends a versioned
// User-Agent header on every platform API request.
func TestE2E_ActivateCmd_HTTPMode_UserAgent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--no-wait",
	})
	if err != nil {
		t.Fatalf("activate: %v\noutput: %s", err, out)
	}

	mock.mu.Lock()
	ua := mock.LastUA
	mock.mu.Unlock()

	if !strings.HasPrefix(ua, "warded-cli/") {
		t.Errorf("expected User-Agent to start with warded-cli/, got %q", ua)
	}
}

// TestE2E_ActivateCmd_HTTPMode_SiteHeader verifies the CLI sends an X-Warded-Site
// header matching the --site flag value.
func TestE2E_ActivateCmd_HTTPMode_SiteHeader(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=cn",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--no-wait",
	})
	if err != nil {
		t.Fatalf("activate: %v\noutput: %s", err, out)
	}

	mock.mu.Lock()
	site := mock.LastSite
	mock.mu.Unlock()

	if site != "cn" {
		t.Errorf("expected X-Warded-Site=cn, got %q", site)
	}
}

// TestE2E_ActivateCmd_HTTPMode_CNSiteOutputURL verifies that --site=cn results in
// a warded.cn activation URL printed to the CLI output.
func TestE2E_ActivateCmd_HTTPMode_CNSiteOutputURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=cn",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--no-wait",
	})
	if err != nil {
		t.Fatalf("activate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "warded.cn") {
		t.Errorf("expected warded.cn in output, got:\n%s", out)
	}
}

// TestE2E_ActivateCmd_HTTPMode_CustomDomainDNSHint verifies that --domain-type=custom_domain
// with a domain name causes the CLI to append a DNS hint to its output.
func TestE2E_ActivateCmd_HTTPMode_CustomDomainDNSHint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		"--spec=pro",
		"--domain-type=custom_domain",
		"--domain=example.com",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--no-wait",
	})
	if err != nil {
		t.Fatalf("activate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "example.com") {
		t.Errorf("expected DNS hint mentioning example.com, got:\n%s", out)
	}
}

func TestE2E_ActivateCmd_HTTPMode_ActivationURLUsesBaseDomain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL, // mock returns activation_url using warded.me/warded.cn
		"--base-domain=preview.warded.me",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--no-wait",
	})
	if err != nil {
		t.Fatalf("activate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "https://preview.warded.me/activate/") {
		t.Fatalf("expected activate output URL to use base-domain, got:\n%s", out)
	}

	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected runtime to be persisted")
	}
	if !strings.Contains(runtime.ActivationURL, "https://preview.warded.me/activate/") {
		t.Fatalf("expected persisted activation_url to use base-domain, got %s", runtime.ActivationURL)
	}
}

// TestE2E_ActivateCmd_HTTPMode_DefaultOrigin verifies that the platform URL
// is derived from site policy when --platform-origin is not passed.
func TestE2E_ActivateCmd_HTTPMode_DefaultOrigin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)

	out, err := runActivate(t, []string{
		// --platform-origin intentionally omitted; should use site policy default
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--no-wait",
	})
	if err != nil {
		t.Fatalf("activate: %v\noutput: %s", err, out)
	}

	// Verify the command succeeded - it should use the default platform URL
	// based on site policy (global -> warded.me)
	if !strings.Contains(out, "warded.me") {
		t.Errorf("expected output to contain warded.me, got:\n%s", out)
	}
}

// TestE2E_ActivateCmd_HTTPMode_NoIngressWarning verifies that activate no longer
// renders a post-link ingress warning. Blocking ingress failures are expected to
// be rejected by the platform before an activation link is issued.
func TestE2E_ActivateCmd_HTTPMode_NoIngressWarning(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{IngressProbeStatus: "unreachable"})

	out, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--no-wait",
	})
	if err != nil {
		t.Fatalf("activate should succeed when the mock platform returns a draft: %v\noutput: %s", err, out)
	}
	if strings.Contains(out, "probe failed") || strings.Contains(out, "443") {
		t.Errorf("expected no ingress warning in output, got:\n%s", out)
	}
}

// TestE2E_ActivateCmd_HTTPMode_IdempotentReactivate verifies that running activate twice
// with the same data dir reuses the same draft ID (idempotent).
func TestE2E_ActivateCmd_HTTPMode_IdempotentReactivate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	args := []string{
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	}

	args = append(args, "--no-wait")

	_, err := runActivate(t, args)
	if err != nil {
		t.Fatalf("first activate: %v", err)
	}
	rt1, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after first init: %v", err)
	}
	draftID1 := rt1.WardDraftID

	_, err = runActivate(t, args)
	if err != nil {
		t.Fatalf("second activate: %v", err)
	}
	rt2, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after second init: %v", err)
	}

	if rt2.WardDraftID != draftID1 {
		t.Errorf("expected same draft ID on re-activate (idempotent), got %s vs %s",
			draftID1, rt2.WardDraftID)
	}
}

func TestE2E_ActivateCmd_HTTPMode_RecreatesExpiredDraft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	args := []string{
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	}

	args = append(args, "--no-wait")

	_, err := runActivate(t, args)
	if err != nil {
		t.Fatalf("first activate: %v", err)
	}
	rt1, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after first init: %v", err)
	}
	draftID1 := rt1.WardDraftID
	mock.setDraftStatus(draftID1, "expired")

	_, err = runActivate(t, args)
	if err != nil {
		t.Fatalf("second activate: %v", err)
	}
	rt2, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after second init: %v", err)
	}

	if rt2.WardDraftID == draftID1 {
		t.Fatalf("expected a fresh draft ID after expired draft, got %s", rt2.WardDraftID)
	}
}

// ── Tier 3: live platform (gated on -platform-url flag) ────────────────

// TestLive_ActivateCmd_HappyPath verifies the full CLI → Platform activate flow
// against a real platform deployment.
//
// Run with:
//
//	go test ./internal/e2e/ -v -count=1 -platform-url=https://dev.warded.me
func TestLive_ActivateCmd_HappyPath(t *testing.T) {
	t.Parallel()

	platformURL := livePlatformURL(t) // skips if env var absent
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)

	out, err := runActivate(t, []string{
		"--platform-origin=" + platformURL,
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--no-wait",
	})
	if err != nil {
		t.Fatalf("activate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "/activate/") {
		t.Errorf("expected activation URL in output, got:\n%s", out)
	}

	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected runtime to be persisted")
	}
	if runtime.WardDraftID == "" {
		t.Error("expected ward_draft_id to be persisted")
	}
	if runtime.WardDraftSecret == "" {
		t.Error("expected ward_draft_secret to be persisted")
	}
	if runtime.WardStatus != domain.WardStatusInitializing {
		t.Errorf("expected ward_status=initializing, got %s", runtime.WardStatus)
	}
}

func TestE2E_ActivateCmd_HTTPMode_WaitsUntilActive(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{AutoConvertAfterPolls: 2})

	out, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
		"--poll-interval=1ms",
		"--wait-timeout=1s",
	})
	if err != nil {
		t.Fatalf("activate: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Waiting for activation to complete") {
		t.Fatalf("expected waiting message, got:\n%s", out)
	}
	if !strings.Contains(out, "Protection is active.") {
		t.Fatalf("expected success message, got:\n%s", out)
	}

	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil || runtime.WardID == "" || runtime.WardSecret == "" {
		t.Fatalf("expected activated runtime to be persisted, got %#v", runtime)
	}
}
