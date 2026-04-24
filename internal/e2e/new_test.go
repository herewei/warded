package e2e_test

// init_test.go contains cobra-command-level tests for `warded new --commit`,
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

// TestE2E_NewCmd_HTTPMode_UserAgent verifies the CLI sends a versioned
// User-Agent header on every platform API request.
func TestE2E_NewCmd_HTTPMode_UserAgent(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}

	mock.mu.Lock()
	ua := mock.LastUA
	mock.mu.Unlock()

	if !strings.HasPrefix(ua, "warded-cli/") {
		t.Errorf("expected User-Agent to start with warded-cli/, got %q", ua)
	}
}

// TestE2E_NewCmd_HTTPMode_SiteHeader verifies the CLI sends an X-Warded-Site
// header matching the --site flag value.
func TestE2E_NewCmd_HTTPMode_SiteHeader(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=cn",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}

	mock.mu.Lock()
	site := mock.LastSite
	mock.mu.Unlock()

	if site != "cn" {
		t.Errorf("expected X-Warded-Site=cn, got %q", site)
	}
}

// TestE2E_NewCmd_HTTPMode_CNSiteOutputURL verifies that --site=cn results in
// a warded.cn activation URL printed to the CLI output.
func TestE2E_NewCmd_HTTPMode_CNSiteOutputURL(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=cn",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "warded.cn") {
		t.Errorf("expected warded.cn in output, got:\n%s", out)
	}
}

// TestE2E_NewCmd_HTTPMode_CustomDomainDNSHint verifies that --domain-type=custom_domain
// with a domain name causes the CLI to append a DNS hint to its output.
func TestE2E_NewCmd_HTTPMode_CustomDomainDNSHint(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--spec=pro",
		"--domain-type=custom_domain",
		"--domain=example.com",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "example.com") {
		t.Errorf("expected DNS hint mentioning example.com, got:\n%s", out)
	}
}

func TestE2E_NewCmd_HTTPMode_ActivationURLUsesBaseDomain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL, // mock returns activation_url using warded.me/warded.cn
		"--site=global",
		"--base-domain=preview.warded.me",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "https://preview.warded.me/activate/") {
		t.Fatalf("expected new output URL to use base-domain, got:\n%s", out)
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

// TestE2E_NewCmd_HTTPMode_DefaultOrigin verifies that the platform URL
// is derived from site policy when --platform-origin is not passed.
func TestE2E_NewCmd_HTTPMode_DefaultOrigin(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)

	out, err := runNewCommit(t, []string{
		// --platform-origin intentionally omitted; should use site policy default
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}

	// Verify the command succeeded - it should use the default platform URL
	// based on site policy (global -> warded.me)
	if !strings.Contains(out, "warded.me") {
		t.Errorf("expected output to contain warded.me, got:\n%s", out)
	}
}

// TestE2E_NewCmd_HTTPMode_IngressUnreachable verifies that new --commit fails
// when the platform returns ingress_unreachable error. Per contract, the platform
// must reject draft creation when ingress probe fails, and CLI must not output
// a setup link.
func TestE2E_NewCmd_HTTPMode_IngressUnreachable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{IngressProbeStatus: "unreachable"})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatalf("new --commit should fail when platform returns ingress_unreachable, got success\noutput: %s", out)
	}
	// CLI translates ingress_unreachable to user-friendly message "inbound probe failed"
	// The error message is returned via err (SilenceErrors=true suppresses stdout/stderr output)
	if !strings.Contains(err.Error(), "inbound probe failed") {
		t.Errorf("expected error to contain 'inbound probe failed', got: %v", err)
	}
}

// TestE2E_NewCmd_HTTPMode_IdempotentReactivate verifies that running activate twice
// with the same data dir reuses the same draft ID (idempotent).
func TestE2E_NewCmd_HTTPMode_IdempotentReactivate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	args := []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	}

	_, err := runNewCommit(t, args)
	if err != nil {
		t.Fatalf("first new --commit: %v", err)
	}
	rt1, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after first init: %v", err)
	}
	draftID1 := rt1.WardDraftID

	_, err = runNewCommit(t, args)
	if err != nil {
		t.Fatalf("second new --commit: %v", err)
	}
	rt2, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after second init: %v", err)
	}

	if rt2.WardDraftID != draftID1 {
		t.Errorf("expected same draft ID on re-submit (idempotent), got %s vs %s",
			draftID1, rt2.WardDraftID)
	}
}

func TestE2E_NewCmd_HTTPMode_RecreatesExpiredDraft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	args := []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	}

	_, err := runNewCommit(t, args)
	if err != nil {
		t.Fatalf("first new --commit: %v", err)
	}
	rt1, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after first init: %v", err)
	}
	draftID1 := rt1.WardDraftID
	mock.setDraftStatus(draftID1, "expired")

	_, err = runNewCommit(t, args)
	if err != nil {
		t.Fatalf("second new --commit: %v", err)
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

// TestLive_NewCmd_HappyPath verifies the full CLI → Platform activate flow
// against a real platform deployment.
//
// Run with:
//
//	go test ./internal/e2e/ -v -count=1 -platform-url=https://dev.warded.me
func TestLive_NewCmd_HappyPath(t *testing.T) {
	t.Parallel()

	platformURL := livePlatformURL(t) // skips if env var absent
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + platformURL,
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
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

func TestE2E_NewCmd_HTTPMode_ExitsImmediatelyAfterDraft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "activate/draft") {
		t.Fatalf("expected activation URL in output, got:\n%s", out)
	}

	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil || runtime.WardDraftID == "" {
		t.Fatalf("expected draft ID to be persisted, got %#v", runtime)
	}
}
