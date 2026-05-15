package e2e_test

// new_listen_test.go covers listen flag validation and IPv6 configuration:
//
//   - Mutual exclusivity of --listen and --listen-v6
//   - IPv6 pending save and ingress_family derivation
//   - Old-form host:port rejection for --listen
//   - Family validation: IPv6 address in --listen, IPv4 address in --listen-v6
//   - IPv6 loopback actual bind (skipped if kernel has no IPv6 or non-Linux)

import (
	"context"
	"net"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

func TestE2E_NewCmd_RejectsMutuallyExclusiveListenFlags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := runNewRaw(t, []string{
		"--site=cn",
		"--listen=0.0.0.0",
		"--listen-v6=::",
		"--port=443",
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected error when --listen and --listen-v6 are both set")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Fatalf("expected 'mutually exclusive' in error, got: %v", err)
	}
}

func TestE2E_NewCmd_SavesIPv6PendingRuntime(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := runNewRaw(t, []string{
		"--site=cn",
		"--spec=pro",
		"--billing-mode=yearly",
		"--domain-type=custom_domain",
		"--domain=bot.example.com",
		"--upstream=127.0.0.1:18789",
		"--listen-v6=::",
		"--port=8443",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	store := storage.NewJSONStore(dir)
	rt, err := store.LoadPendingRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if rt == nil {
		t.Fatal("expected pending runtime")
	}
	if rt.IngressFamily != domain.IngressFamilyIPv6 {
		t.Errorf("ingress_family: got %q, want %q", rt.IngressFamily, domain.IngressFamilyIPv6)
	}
	if rt.ListenHost != "::" {
		t.Errorf("listen_host: got %q, want \"::\", ", rt.ListenHost)
	}
	if rt.ListenPort != 8443 {
		t.Errorf("listen_port: got %d, want 8443", rt.ListenPort)
	}
}

func TestE2E_NewCmd_DefaultsToIPv4WhenNoListenFlagGiven(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	port := reserveActivationPort(t)
	_, err := runNewRaw(t, []string{
		"--site=cn",
		"--spec=pro",
		"--billing-mode=yearly",
		"--domain-type=custom_domain",
		"--domain=bot.example.com",
		"--upstream=127.0.0.1:18789",
		"--port=" + strconv.Itoa(port),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}

	store := storage.NewJSONStore(dir)
	rt, err := store.LoadPendingRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if rt == nil {
		t.Fatal("expected pending runtime")
	}
	if rt.IngressFamily != domain.IngressFamilyIPv4 {
		t.Errorf("ingress_family: got %q, want %q", rt.IngressFamily, domain.IngressFamilyIPv4)
	}
	if rt.ListenHost != "0.0.0.0" {
		t.Errorf("listen_host: got %q, want \"0.0.0.0\"", rt.ListenHost)
	}
}

func TestE2E_NewCmd_RejectsPortInListenHost(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := runNewRaw(t, []string{
		"--site=cn",
		"--listen=0.0.0.0:443",
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected error for --listen with port suffix (old host:port form)")
	}
}

func TestE2E_NewCmd_RejectsIPv6AddressInListenFlag(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := runNewRaw(t, []string{
		"--site=cn",
		"--listen=::",
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected error for IPv6 address passed to --listen")
	}
	if !strings.Contains(err.Error(), "--listen") {
		t.Fatalf("expected error to mention --listen flag, got: %v", err)
	}
}

func TestE2E_NewCmd_RejectsIPv4AddressInListenV6Flag(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	_, err := runNewRaw(t, []string{
		"--site=cn",
		"--listen-v6=0.0.0.0",
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected error for IPv4 address passed to --listen-v6")
	}
	if !strings.Contains(err.Error(), "--listen-v6") {
		t.Fatalf("expected error to mention --listen-v6 flag, got: %v", err)
	}
}

func TestE2E_NewCmd_IPv6LoopbackPendingBind(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("IPv6 listener integration test runs on Linux only (ADR §4.5)")
	}
	ln, err := net.Listen("tcp6", "[::1]:0")
	if err != nil {
		t.Skipf("IPv6 not available on this machine: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	dir := t.TempDir()
	_, err = runNewRaw(t, []string{
		"--site=cn",
		"--spec=pro",
		"--billing-mode=yearly",
		"--domain-type=custom_domain",
		"--domain=bot.example.com",
		"--upstream=127.0.0.1:18789",
		"--listen-v6=::1",
		"--port=" + strconv.Itoa(port),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new with --listen-v6=::1: %v", err)
	}

	store := storage.NewJSONStore(dir)
	rt, err := store.LoadPendingRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if rt == nil {
		t.Fatal("expected pending runtime")
	}
	if rt.IngressFamily != domain.IngressFamilyIPv6 {
		t.Errorf("ingress_family: got %q, want ipv6", rt.IngressFamily)
	}
	if rt.ListenHost != "::1" {
		t.Errorf("listen_host: got %q, want \"::1\"", rt.ListenHost)
	}
	if rt.ListenPort != port {
		t.Errorf("listen_port: got %d, want %d", rt.ListenPort, port)
	}
}
