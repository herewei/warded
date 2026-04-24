package e2e_test

// Tests migrated from application/new_service_test.go.
// These validate the same CLI-visible behaviors through the E2E layer
// (cobra command + HTTP mock) instead of the application-layer mock.

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/adapters/storage"
)

// TestE2E_NewCmd_PersistsConfig verifies that new --commit persists all
// expected runtime fields to the local config store.
func TestE2E_NewCmd_PersistsConfig(t *testing.T) {
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
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
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

// TestE2E_NewCmd_CreatesFreshDraftWhenTokenUnknownToServer verifies that
// when the platform returns access_denied on draft status check, the CLI
// creates a fresh draft instead of failing.
func TestE2E_NewCmd_CreatesFreshDraftWhenTokenUnknownToServer(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	// First run: creates a draft
	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("first new --commit: %v", err)
	}
	rt1, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after first init: %v", err)
	}
	draftID1 := rt1.WardDraftID

	// Second run: simulate platform not recognizing the stored token
	mock2 := newMockPlatform(t, mockPlatformOptions{
		GetDraftStatusError:     "access_denied",
		GetDraftStatusHTTPError: 403,
	})

	_, err = runNewCommit(t, []string{
		"--platform-origin=" + mock2.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("second new --commit: %v", err)
	}
	rt2, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after second init: %v", err)
	}

	if rt2.WardDraftID == draftID1 {
		t.Fatalf("expected a fresh draft ID after access_denied, got %s", rt2.WardDraftID)
	}
}

// TestE2E_NewCmd_CreatesFreshDraftWhenCredentialExpired verifies that
// when the platform returns activation_link_expired on draft status check,
// the CLI creates a fresh draft.
func TestE2E_NewCmd_CreatesFreshDraftWhenCredentialExpired(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	// First run: creates a draft
	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("first new --commit: %v", err)
	}
	rt1, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after first init: %v", err)
	}
	draftID1 := rt1.WardDraftID

	// Second run: simulate expired activation link
	mock2 := newMockPlatform(t, mockPlatformOptions{
		GetDraftStatusError:     "activation_link_expired",
		GetDraftStatusHTTPError: 401,
	})

	_, err = runNewCommit(t, []string{
		"--platform-origin=" + mock2.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("second new --commit: %v", err)
	}
	rt2, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after second init: %v", err)
	}

	if rt2.WardDraftID == draftID1 {
		t.Fatalf("expected a fresh draft ID after activation_link_expired, got %s", rt2.WardDraftID)
	}
}

// TestE2E_NewCmd_DefaultUpstreamPort verifies that when --upstream-port is
// omitted and no openclaw.json is found, the CLI defaults to 18789 and
// fails because nothing is listening on that port.
func TestE2E_NewCmd_DefaultUpstreamPort(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := newMockPlatform(t, mockPlatformOptions{})

	// No --upstream-port flag; CLI will try to discover from openclaw.json
	// which doesn't exist, so it falls back to 18789. The upstream check
	// then fails because nothing is listening on 18789.
	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected new --commit to fail when default upstream port 18789 is unreachable")
	}
	if !strings.Contains(err.Error(), "upstream") && !strings.Contains(err.Error(), "not reachable") {
		t.Errorf("expected upstream unreachable error, got: %v", err)
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
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected new --commit to fail when platform returns 500")
	}
	if !strings.Contains(err.Error(), "platform error") && !strings.Contains(err.Error(), "internal_error") {
		t.Errorf("expected platform error, got: %v", err)
	}
}

// TestE2E_NewCmd_YearlyBillingMode verifies that --billing-mode=yearly is
// forwarded to the platform.
func TestE2E_NewCmd_YearlyBillingMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--billing-mode=yearly",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
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

// TestE2E_NewCmd_RejectsWardAlreadyActivated verifies that when a ward is
// already activated, running new --commit renders success without calling
// the platform API.
func TestE2E_NewCmd_RejectsWardAlreadyActivated(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	// First run: creates a draft
	_, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("first new --commit: %v", err)
	}

	mock.mu.Lock()
	callsAfterFirst := mock.Calls
	mock.mu.Unlock()

	// Manually mark the ward as activated in the runtime
	store := storage.NewJSONStore(dir)
	rt, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	rt.WardID = "ward_existing"
	rt.WardSecret = "wrd_existing"
	rt.WardStatus = "active"
	rt.Domain = "demo.warded.me"
	if err := store.SaveWardRuntime(context.Background(), *rt); err != nil {
		t.Fatalf("save activated runtime: %v", err)
	}

	// Second run: CLI renders success without calling platform
	out, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if !strings.Contains(out, "Protection is active") {
		t.Errorf("expected 'Protection is active' in output, got: %s", out)
	}

	// Platform must not have been called on second run — ward already activated
	// check happens before any I/O.
	mock.mu.Lock()
	callsAfterSecond := mock.Calls
	mock.mu.Unlock()
	if callsAfterSecond != callsAfterFirst {
		t.Errorf("expected no additional platform calls on second run, got %d → %d", callsAfterFirst, callsAfterSecond)
	}
}
