package e2e_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

func TestE2E_NewCmd_SavesPendingWithoutPlatformCall(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewRaw(t, []string{
		"--platform-origin=" + mock.URL,
		"--site=cn",
		"--spec=pro",
		"--billing-mode=yearly",
		"--domain-type=custom_domain",
		"--domain=bot.example.com",
		"--upstream-port=18789",
		"--port=8443",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "Pending ward setup saved.") {
		t.Fatalf("expected pending-save output, got:\n%s", out)
	}

	mock.mu.Lock()
	calls := mock.Calls
	mock.mu.Unlock()
	if calls != 0 {
		t.Fatalf("expected no platform calls without --commit, got %d", calls)
	}

	store := storage.NewJSONStore(dir)
	runtime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if runtime == nil {
		t.Fatal("expected pending runtime")
	}
	if runtime.Site != domain.SiteCN {
		t.Fatalf("expected site cn, got %s", runtime.Site)
	}
	if runtime.Spec != domain.SpecPro {
		t.Fatalf("expected spec pro, got %s", runtime.Spec)
	}
	if runtime.BillingMode != domain.BillingModeYearly {
		t.Fatalf("expected yearly billing, got %s", runtime.BillingMode)
	}
	if runtime.DomainType != domain.DomainTypeCustomDomain {
		t.Fatalf("expected custom_domain, got %s", runtime.DomainType)
	}
	if runtime.RequestedDomain != "bot.example.com" {
		t.Fatalf("expected requested domain bot.example.com, got %q", runtime.RequestedDomain)
	}
	if runtime.UpstreamPort != 18789 {
		t.Fatalf("expected upstream port 18789, got %d", runtime.UpstreamPort)
	}
	if runtime.ListenAddr != ":8443" {
		t.Fatalf("expected listen addr :8443, got %q", runtime.ListenAddr)
	}
	if runtime.WardDraftID != "" {
		t.Fatalf("expected no draft id before commit, got %q", runtime.WardDraftID)
	}
	if runtime.ActivationURL != "" {
		t.Fatalf("expected no activation url before commit, got %q", runtime.ActivationURL)
	}
	if runtime.JWTSigningSecret == "" {
		t.Fatal("expected jwt signing secret to be generated")
	}
	if runtime.WardDraftSecret == "" {
		t.Fatal("expected ward draft secret to be generated")
	}

	pendingPath := filepath.Join(dir, ".pending", "ward.json")
	if _, err := os.Stat(pendingPath); err != nil {
		t.Fatalf("expected pending config at %s: %v", pendingPath, err)
	}
}

func TestE2E_NewCmd_MergesPendingFlags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	_, err := runNewRaw(t, []string{
		"--site=cn",
		"--spec=pro",
		"--billing-mode=yearly",
		"--domain-type=custom_domain",
		"--domain=first.example.com",
		"--upstream-port=18789",
		"--port=8443",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("first new: %v", err)
	}

	store := storage.NewJSONStore(dir)
	before, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load before merge: %v", err)
	}
	if before == nil {
		t.Fatal("expected pending runtime before merge")
	}
	originalJWT := before.JWTSigningSecret
	originalDraftSecret := before.WardDraftSecret

	_, err = runNewRaw(t, []string{
		"--domain=second.example.com",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("second new: %v", err)
	}

	after, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load after merge: %v", err)
	}
	if after == nil {
		t.Fatal("expected pending runtime after merge")
	}
	if after.RequestedDomain != "second.example.com" {
		t.Fatalf("expected domain override to apply, got %q", after.RequestedDomain)
	}
	if after.Site != domain.SiteCN {
		t.Fatalf("expected site to be preserved, got %s", after.Site)
	}
	if after.Spec != domain.SpecPro {
		t.Fatalf("expected spec to be preserved, got %s", after.Spec)
	}
	if after.BillingMode != domain.BillingModeYearly {
		t.Fatalf("expected billing mode to be preserved, got %s", after.BillingMode)
	}
	if after.UpstreamPort != 18789 {
		t.Fatalf("expected upstream port preserved, got %d", after.UpstreamPort)
	}
	if after.ListenAddr != ":8443" {
		t.Fatalf("expected listen addr preserved, got %q", after.ListenAddr)
	}
	if after.JWTSigningSecret != originalJWT {
		t.Fatal("expected jwt signing secret to be preserved across repeated new")
	}
	if after.WardDraftSecret != originalDraftSecret {
		t.Fatal("expected draft secret to be preserved across repeated new")
	}
}

func TestE2E_NewCmd_PreflightsExplicitPortWithoutCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := reserveActivationPort(t)
	ln, err := net.Listen("tcp", ":"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	_, err = runNewRaw(t, []string{
		"--site=cn",
		"--data-dir=" + dir,
		"--port=" + strconv.Itoa(port),
	})
	if err == nil {
		t.Fatal("expected new to fail when explicit port is already occupied")
	}
	if !strings.Contains(err.Error(), "port") || !strings.Contains(err.Error(), "in use") {
		t.Fatalf("unexpected error: %v", err)
	}

	store := storage.NewJSONStore(dir)
	runtime, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime after failed preflight: %v", err)
	}
	if runtime != nil {
		t.Fatalf("expected no pending runtime to be saved after failed port preflight, got %#v", runtime)
	}
}
