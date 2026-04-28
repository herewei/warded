package e2e_test

// reactivate_test.go covers draft lifecycle scenarios:
//
//   - Idempotent reactivation (same draft ID reused)
//   - Expired draft recreation (fresh draft ID)
//   - Token unknown / credential expired recovery
//   - Already-activated ward handling
//   - Recommit with updated settings

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/adapters/storage"
)

// TestE2E_NewCmd_HTTPMode_IdempotentReactivate verifies that running new --commit
// twice with the same data dir reuses the same draft ID (idempotent).
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

// TestE2E_NewCmd_HTTPMode_RecreatesExpiredDraft verifies that when the existing
// draft is expired, a fresh draft is created.
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

// TestE2E_NewCmd_RecommitUpdatesUnactivatedDraft verifies that running new --commit
// on an existing unactivated draft updates settings without changing the draft ID.
func TestE2E_NewCmd_RecommitUpdatesUnactivatedDraft(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	listenPort := reserveActivationPort(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	firstOut, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--spec=pro",
		"--domain-type=platform_subdomain",
		"--domain=abcd.warded.me",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		fmt.Sprintf("--port=%d", listenPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("first new --commit: %v\noutput: %s", err, firstOut)
	}
	if !strings.Contains(firstOut, "✓ Setup created") {
		t.Fatalf("expected first commit to report setup created, got:\n%s", firstOut)
	}
	if !strings.Contains(firstOut, "https://abcd.warded.me") {
		t.Fatalf("expected first commit output to mention abcd.warded.me, got:\n%s", firstOut)
	}

	store := storage.NewJSONStore(dir)
	rt1, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after first commit: %v", err)
	}
	if rt1 == nil {
		t.Fatal("expected runtime after first commit")
	}
	firstDraftID := rt1.WardDraftID

	secondOut, err := runNewCommit(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=global",
		"--spec=pro",
		"--domain-type=platform_subdomain",
		"--domain=efgh.warded.me",
		fmt.Sprintf("--upstream-port=%d", upstreamPort),
		fmt.Sprintf("--port=%d", listenPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("second new --commit: %v\noutput: %s", err, secondOut)
	}
	if !strings.Contains(secondOut, "✓ Setup updated") {
		t.Fatalf("expected second commit to report setup updated, got:\n%s", secondOut)
	}
	mock.mu.Lock()
	lastRequestedDomain := mock.LastCreateRequestedDomain
	lastSpec := mock.LastCreateSpec
	mock.mu.Unlock()
	if lastRequestedDomain != "efgh.warded.me" || lastSpec != "pro" {
		t.Fatalf("expected second commit to send requested_domain=efgh.warded.me and spec=pro, got requested_domain=%q spec=%q", lastRequestedDomain, lastSpec)
	}
	if !strings.Contains(secondOut, "https://efgh.warded.me") {
		t.Fatalf("expected second commit output to mention efgh.warded.me, got:\n%s", secondOut)
	}

	rt2, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after second commit: %v", err)
	}
	if rt2 == nil {
		t.Fatal("expected runtime after second commit")
	}
	if rt2.WardDraftID != firstDraftID {
		t.Fatalf("expected recommit to reuse same draft id, got %s -> %s", firstDraftID, rt2.WardDraftID)
	}
	if rt2.RequestedDomain != "efgh.warded.me" {
		t.Fatalf("expected requested_domain to update to efgh.warded.me, got %s", rt2.RequestedDomain)
	}
	expectedListenAddr := fmt.Sprintf(":%d", listenPort)
	if rt2.ListenAddr != expectedListenAddr {
		t.Fatalf("expected listen addr to remain %s, got %s", expectedListenAddr, rt2.ListenAddr)
	}
}
