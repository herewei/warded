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

func TestIntegrateCommandPreview(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := storage.NewJSONStore(dataDir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	original := `{"gateway":{"controlUi":{"allowedOrigins":["http://127.0.0.1:18789"]}}}`
	if err := os.WriteFile(openClawConfig, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, "test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"integrate",
		"--agent", "openclaw",
		"--data-dir", dataDir,
		"--config-file", openClawConfig,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("integrate: %v", err)
	}

	body := out.String()
	if !strings.Contains(body, "Status: patch_required") {
		t.Fatalf("expected patch_required, got: %s", body)
	}
	data, err := os.ReadFile(openClawConfig)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if string(data) != original {
		t.Fatalf("preview mode should not modify file, got %s", string(data))
	}
}

func TestIntegrateCommandApply(t *testing.T) {
	t.Parallel()

	dataDir := t.TempDir()
	store := storage.NewJSONStore(dataDir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	openClawConfig := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(openClawConfig, []byte(`{"gateway":{"controlUi":{"allowedOrigins":["http://127.0.0.1:18789"]}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, "test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"integrate",
		"--agent", "openclaw",
		"--apply",
		"--data-dir", dataDir,
		"--config-file", openClawConfig,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("integrate: %v", err)
	}

	body := out.String()
	if !strings.Contains(body, "Status: updated") {
		t.Fatalf("expected updated status, got: %s", body)
	}
	data, err := os.ReadFile(openClawConfig)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if !strings.Contains(string(data), "https://demo.warded.me") {
		t.Fatalf("expected config to contain required origin, got %s", string(data))
	}
}
