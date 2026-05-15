package e2e_test

// reactivate_test.go covers the current pending-only new flow:
//
//   - `warded new --commit` can submit a saved `.pending` runtime without
//     requiring the user to repeat configuration flags.
//   - Existing formal ward runtimes do not block preparing or committing a
//     separate pending setup.

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

func TestE2E_NewCmd_CommitUsesSavedPendingWithoutConfigFlags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	listenPort := reserveActivationPort(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewRaw(t, []string{
		"--site=global",
		"--spec=pro",
		"--domain-type=platform_subdomain",
		"--domain=abcd.warded.me",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--listen=0.0.0.0",
		fmt.Sprintf("--port=%d", listenPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("save pending: %v\noutput: %s", err, out)
	}

	out, err = runNewRaw(t, []string{
		"--platform-origin=" + mock.URL,
		"--data-dir=" + dir,
		"--commit",
	})
	if err != nil {
		t.Fatalf("commit saved pending: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "✓ Setup created") {
		t.Fatalf("expected commit to create setup, got:\n%s", out)
	}
	if !strings.Contains(out, "https://abcd.warded.me") {
		t.Fatalf("expected output to mention saved pending domain, got:\n%s", out)
	}

	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load committed runtime: %v", err)
	}
	if runtime == nil || runtime.WardDraftID == "" {
		t.Fatalf("expected committed draft runtime, got %#v", runtime)
	}
	if _, err := os.Stat(filepath.Join(dir, ".pending")); !os.IsNotExist(err) {
		t.Fatalf("expected pending runtime to be committed away, stat err=%v", err)
	}
}

func TestE2E_NewCmd_FormalWardDoesNotBlockPendingCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	listenPort := reserveActivationPort(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		WardID:           "ward_existing",
		WardSecret:       "wrd_existing",
		WardStatus:       domain.WardStatusActive,
		Domain:           "existing.warded.me",
		Spec:             domain.SpecStarter,
		BillingMode:      domain.BillingModeMonthly,
		DomainType:       domain.DomainTypePlatformSubdomain,
		UpstreamAddr:     fmt.Sprintf("127.0.0.1:%d", upstreamPort),
		ListenPort:       listenPort,
		ListenHost:       "0.0.0.0",
		IngressFamily:    domain.IngressFamilyIPv4,
		JWTSigningSecret: "jwt_existing",
		TLSMode:          domain.TLSModePlatformWildcard,
	}); err != nil {
		t.Fatalf("seed active runtime: %v", err)
	}

	out, err := runNewRaw(t, []string{
		"--site=global",
		"--spec=starter",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--listen=0.0.0.0",
		fmt.Sprintf("--port=%d", listenPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("save pending beside active runtime: %v\noutput: %s", err, out)
	}

	out, err = runNewRaw(t, []string{
		"--platform-origin=" + mock.URL,
		"--data-dir=" + dir,
		"--commit",
	})
	if err != nil {
		t.Fatalf("commit pending beside active runtime: %v\noutput: %s", err, out)
	}
	if !strings.Contains(out, "✓ Setup created") {
		t.Fatalf("expected commit to create setup, got:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "ward_existing", "ward.json")); err != nil {
		t.Fatalf("expected existing formal ward to remain untouched: %v", err)
	}
	mock.mu.Lock()
	calls := mock.Calls
	mock.mu.Unlock()
	if calls != 1 {
		t.Fatalf("expected exactly one platform create call, got %d", calls)
	}
}
