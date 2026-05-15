package e2e_test

// happy_path_test.go covers the primary user scenarios:
//
//   - First-time ward creation and activation
//   - Custom domain setup with DNS hints
//   - Site-specific behaviour (CN vs global)
//   - Configuration persistence and output validation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/adapters/storage"
)

// TestE2E_NewCmd_PersistsConfig verifies that new --commit persists all
// expected runtime fields to the local config store on first creation.
func TestE2E_NewCmd_PersistsConfig(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}

	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected ward runtime to be persisted")
	}
	if runtime.WardDraftID == "" {
		t.Error("expected persisted ward runtime to have ward draft ID")
	}
	if runtime.WardDraftSecret == "" {
		t.Error("expected persisted ward runtime to have ward_draft_secret")
	}
	if runtime.JWTSigningSecret == "" {
		t.Error("expected persisted ward runtime to have jwt_signing_secret")
	}
	if runtime.LastPublicIP == "" {
		t.Error("expected persisted ward runtime to have public IP")
	}
	if runtime.UpstreamPort != upstreamPort {
		t.Errorf("expected persisted upstream_port=%d, got %d", upstreamPort, runtime.UpstreamPort)
	}
	if runtime.ActivationURL == "" {
		t.Error("expected persisted activation_url")
	}
	if runtime.TLSMode != "platform_wildcard" {
		t.Errorf("expected persisted tls_mode=platform_wildcard, got %s", runtime.TLSMode)
	}
}

// TestE2E_NewCmd_PersistsLocalACMETLSModeForCustomDomain verifies that
// custom_domain results in tls_mode=local_acme.
func TestE2E_NewCmd_PersistsLocalACMETLSModeForCustomDomain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--spec=pro",
		"--domain-type=custom_domain",
		"--domain=robot.example.com",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v", err)
	}

	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected runtime to be persisted")
	}
	if runtime.TLSMode != "local_acme" {
		t.Fatalf("expected tls_mode=local_acme, got %s", runtime.TLSMode)
	}
}

// TestE2E_NewCmd_YearlyBillingMode verifies that --billing-mode=yearly is
// forwarded to the platform and persisted.
func TestE2E_NewCmd_YearlyBillingMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--billing-mode=yearly",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v", err)
	}

	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime.BillingMode != "yearly" {
		t.Errorf("expected billing_mode=yearly, got %s", runtime.BillingMode)
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
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
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
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "example.com") {
		t.Errorf("expected DNS hint mentioning example.com, got:\n%s", out)
	}
}

// TestE2E_NewCmd_HTTPMode_ActivationURLUsesBaseDomain verifies that when
// --base-domain is specified, the activation URL uses that domain.
func TestE2E_NewCmd_HTTPMode_ActivationURLUsesBaseDomain(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--base-domain=preview.warded.me",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
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

// TestLive_NewCmd_DefaultOrigin verifies that the platform URL
// is derived from site policy when --platform-origin is not passed.
// This is a live integration test that requires a real platform connection.
// Run with: go test ./internal/e2e/ -v -count=1 -platform-url=https://warded.me
func TestLive_NewCmd_DefaultOrigin(t *testing.T) {
	t.Parallel()

	platformURL := livePlatformURL(t) // skips if -platform-url flag not provided
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)

	// Extract base domain from platform URL to test URL derivation via --base-domain
	// This allows testing default URL derivation behavior while still connecting to
	// the live platform specified by -platform-url flag.
	baseDomain := strings.TrimPrefix(platformURL, "https://")
	baseDomain = strings.TrimPrefix(baseDomain, "http://")

	out, err := runNewCommit(t, []string{
		"--site=global",
		"--base-domain=" + baseDomain,
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, baseDomain) {
		t.Errorf("expected output to contain %s, got:\n%s", baseDomain, out)
	}
}

// TestE2E_NewCmd_HTTPMode_ExitsImmediatelyAfterDraft verifies that new --commit
// exits immediately after creating the draft, outputting the activation URL.
func TestE2E_NewCmd_HTTPMode_ExitsImmediatelyAfterDraft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
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
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
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
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
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
