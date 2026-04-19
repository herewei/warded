package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

func TestIntegrateService_Execute_PreviewPatchRequired(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	configFile := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(configFile, []byte(`{"gateway":{"bind":"lan","controlUi":{"allowedOrigins":["http://127.0.0.1:18789"]}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	svc := IntegrateService{ConfigStore: store}
	out, err := svc.Execute(context.Background(), IntegrateInput{
		Agent:      "openclaw",
		ConfigFile: configFile,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.Status != "patch_required" {
		t.Fatalf("expected patch_required, got %q", out.Status)
	}
	if !strings.Contains(out.SuggestedPatch, "https://demo.warded.me") {
		t.Fatalf("expected suggested patch to include required origin, got %s", out.SuggestedPatch)
	}
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read config: %v", err)
	}
	if strings.Contains(string(data), "https://demo.warded.me") {
		t.Fatalf("preview mode should not modify file, got %s", string(data))
	}
}

func TestIntegrateService_Execute_ApplyUpdatesAndBacksUp(t *testing.T) {
	t.Parallel()

	originalNow := nowFunc
	nowFunc = func() time.Time { return time.Date(2026, time.April, 11, 12, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFunc = originalNow })

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	dataDir := t.TempDir()
	configFile := filepath.Join(dataDir, "openclaw.json")
	original := `{"gateway":{"bind":"lan","controlUi":{"allowedOrigins":["http://127.0.0.1:18789"]}}}`
	if err := os.WriteFile(configFile, []byte(original), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	svc := IntegrateService{ConfigStore: store}
	out, err := svc.Execute(context.Background(), IntegrateInput{
		Agent:      "openclaw",
		ConfigFile: configFile,
		Apply:      true,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.Status != "updated" || !out.Updated {
		t.Fatalf("expected updated result, got %#v", out)
	}
	if out.BackupFile == "" {
		t.Fatalf("expected backup file, got %#v", out)
	}
	backupData, err := os.ReadFile(out.BackupFile)
	if err != nil {
		t.Fatalf("read backup: %v", err)
	}
	if string(backupData) != original {
		t.Fatalf("unexpected backup contents: %s", string(backupData))
	}
	updatedData, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatalf("read updated config: %v", err)
	}
	if !strings.Contains(string(updatedData), "https://demo.warded.me") {
		t.Fatalf("expected updated config to include required origin, got %s", string(updatedData))
	}
}

func TestIntegrateService_Execute_AlreadyConfigured(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		WardID:     "ward_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
		UpdatedAt:  time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	configFile := filepath.Join(t.TempDir(), "openclaw.json")
	if err := os.WriteFile(configFile, []byte(`{"gateway":{"controlUi":{"allowedOrigins":["https://demo.warded.me"]}}}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	svc := IntegrateService{ConfigStore: store}
	out, err := svc.Execute(context.Background(), IntegrateInput{
		Agent:      "openclaw",
		ConfigFile: configFile,
	})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if out.Status != "already_configured" {
		t.Fatalf("expected already_configured, got %q", out.Status)
	}
}
