package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"log/slog"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

func TestDoctorCommandPrintsOpenClawIntegrationCheck(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".openclaw"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(home, ".openclaw", "openclaw.json"), []byte(`{"gateway":{"controlUi":{"allowedOrigins":["http://127.0.0.1:18789"]}}}`), 0o600); err != nil {
		t.Fatalf("write openclaw config: %v", err)
	}

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:           "ward_123",
		WardStatus:       domain.WardStatusActive,
		Domain:           "demo.warded.me",
		JWTSigningSecret: "jwt_secret",
		UpdatedAt:        time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, BuildInfo{Version: "test"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{"doctor", "--data-dir", dir})

	if err := root.Execute(); err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if !strings.Contains(out.String(), "openclaw_integration") {
		t.Fatalf("expected doctor output to include openclaw_integration, got %s", out.String())
	}
}
