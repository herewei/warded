package e2e_test

// live_test.go contains integration tests against a real platform deployment.
// These tests are skipped unless -platform-url flag is provided.
//
// Run with:
//
//	go test ./internal/e2e/ -v -count=1 -platform-url=https://dev.warded.me

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

// TestLive_NewCmd_HappyPath verifies the full CLI → Platform activate flow
// against a real platform deployment.
func TestLive_NewCmd_HappyPath(t *testing.T) {
	t.Parallel()

	platformURL := livePlatformURL(t) // skips if env var absent
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)

	out, err := runNewCommit(t, []string{
		"--platform-origin=" + platformURL,
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
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
