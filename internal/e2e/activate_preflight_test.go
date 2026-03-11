package e2e_test

// Preflight tests validate activate's fail-fast behavior using the cobra command.
// These tests run entirely locally: no live platform required.

import (
	"fmt"
	"strings"
	"testing"
)

// TestE2E_ActivateCmd_Preflight_UpstreamUnreachable verifies activate fails fast
// (before calling the platform) when the upstream port is not listening.
func TestE2E_ActivateCmd_Preflight_UpstreamUnreachable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := newMockPlatform(t, mockPlatformOptions{})

	_, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		"--upstream-port=59999", // nothing listening
		"--config-dir=" + dir,
		"--no-wait",
	})
	if err == nil {
		t.Fatal("expected activate to fail when upstream is unreachable")
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

// TestE2E_ActivateCmd_Preflight_PlatformUnreachable verifies activate fails when the
// platform API cannot be reached (network-level failure).
func TestE2E_ActivateCmd_Preflight_PlatformUnreachable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)

	_, err := runActivate(t, []string{
		"--platform-origin=http://127.0.0.1:59998", // nothing there
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--config-dir=" + dir,
		"--no-wait",
	})
	if err == nil {
		t.Fatal("expected activate to fail when platform is unreachable")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("expected connection refused, got: %v", err)
	}
}

// TestE2E_ActivateCmd_Preflight_InvalidSpecDomainCombination verifies that a bad
// spec/domain_type combination is rejected locally before any platform call.
func TestE2E_ActivateCmd_Preflight_InvalidSpecDomainCombination(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	_, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		"--spec=starter",
		"--domain-type=custom_domain", // invalid for starter
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--config-dir=" + dir,
		"--no-wait",
	})
	if err == nil {
		t.Fatal("expected activate to fail on invalid spec/domain combination")
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

// TestE2E_ActivateCmd_Preflight_ConfigDirNotWritable verifies activate fails when the
// config directory is not writable. The platform call succeeds; the failure
// happens when trying to persist the result.
func TestE2E_ActivateCmd_Preflight_ConfigDirNotWritable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	restore := makeConfigDirReadOnly(t, dir)
	defer restore()

	_, err := runActivate(t, []string{
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--config-dir=" + dir,
		"--no-wait",
	})
	if err == nil {
		t.Fatal("expected activate to fail when config dir is not writable")
	}
	if !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected permission denied, got: %v", err)
	}
}
