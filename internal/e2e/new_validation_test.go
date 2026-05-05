package e2e_test

// validation_test.go covers all fail-fast scenarios:
//
//   - Upstream port unreachable (no listener)
//   - Platform API unreachable (network failure)
//   - Invalid spec / domain type combinations
//   - Data directory not writable
//   - Platform-managed domain rejected for custom_domain type
//   - Platform API errors during draft creation
//   - Ingress probe failures
//   - Short domain name rejections for pro spec

import (
	"fmt"
	"strings"
	"testing"
)

// TestE2E_NewCmd_Preflight_UpstreamUnreachable verifies new --commit fails fast
// (before calling the platform) when the upstream port is not listening.
func TestE2E_NewCmd_Preflight_UpstreamUnreachable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := newMockPlatform(t, mockPlatformOptions{})

	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--upstream=127.0.0.1:59999", // nothing listening
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected new to fail when upstream is unreachable")
	}
	if !strings.Contains(err.Error(), "upstream port") && !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Platform must not have been called — upstream check happens first.
	mock.mu.Lock()
	calls := mock.Calls
	mock.mu.Unlock()
	if calls != 0 {
		t.Errorf("expected platform to not be called when upstream is unreachable, got %d call(s)", calls)
	}
}

// TestE2E_NewCmd_Preflight_PlatformUnreachable verifies new --commit fails when the
// platform API cannot be reached (network-level failure).
func TestE2E_NewCmd_Preflight_PlatformUnreachable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)

	_, err := runNewCommit(t, []string{
		"--platform-origin=http://127.0.0.1:59998", // nothing there
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected new to fail when platform is unreachable")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected connection refused, got: %v", err)
	}
}

// TestE2E_NewCmd_Preflight_InvalidSpecDomainCombination verifies that a bad
// spec/domain_type combination is rejected locally before any platform call.
func TestE2E_NewCmd_Preflight_InvalidSpecDomainCombination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--spec=starter",
		"--domain-type=custom_domain", // invalid for starter
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected new to fail on invalid spec/domain combination")
	}
	if !strings.Contains(err.Error(), "starter spec only supports platform_subdomain") {
		t.Errorf("unexpected error message: %v", err)
	}

	// Platform must not have been called — validation happens before any I/O.
	mock.mu.Lock()
	calls := mock.Calls
	mock.mu.Unlock()
	if calls != 0 {
		t.Errorf("expected no platform calls on validation error, got %d", calls)
	}
}

// TestE2E_NewCmd_Preflight_DataDirNotWritable verifies new --commit fails when the
// data directory is not writable. The platform call succeeds; the failure
// happens when trying to persist the result.
func TestE2E_NewCmd_Preflight_DataDirNotWritable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	restore := makeDataDirReadOnly(t, dir)
	defer restore()

	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected new to fail when data dir is not writable")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected permission denied, got: %v", err)
	}
}

// TestE2E_NewCmd_Preflight_CustomDomainWithPlatformSuffix verifies that
// custom_domain with a platform-managed suffix (warded.me/warded.cn) is
// rejected locally before any platform call.
func TestE2E_NewCmd_Preflight_CustomDomainWithPlatformSuffix(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	tests := []struct {
		name   string
		site   string
		domain string
	}{
		{
			name:   "global_site_warded_me_suffix",
			site:   "global",
			domain: "myrobot.warded.me",
		},
		{
			name:   "cn_site_warded_cn_suffix",
			site:   "cn",
			domain: "abcd.warded.cn",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := runNewCommit(t, []string{
				"--platform-origin=" + mock.URL,
				"--site=" + tc.site,
				"--spec=pro",
				"--domain-type=custom_domain",
				"--domain=" + tc.domain,
				fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
				"--data-dir=" + dir,
			})
			if err == nil {
				t.Fatal("expected new to fail on custom_domain with platform suffix")
			}
			if !strings.Contains(err.Error(), "platform-managed domain") {
				t.Errorf("unexpected error message: %v", err)
			}

			mock.mu.Lock()
			calls := mock.Calls
			mock.mu.Unlock()
			if calls != 0 {
				t.Errorf("expected no platform calls on validation error, got %d", calls)
			}
		})
	}
}

// TestE2E_NewCmd_PlatformAPICreateFails verifies that when the platform
// returns an error on draft creation, the CLI propagates it.
func TestE2E_NewCmd_PlatformAPICreateFails(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{
		CreateErrorStatus: 500,
		CreateErrorCode:   "internal_error",
	})

	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected new --commit to fail when platform returns 500")
	}
	if !strings.Contains(err.Error(), "platform error") && !strings.Contains(err.Error(), "internal_error") {
		t.Errorf("expected platform error, got: %v", err)
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
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
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


