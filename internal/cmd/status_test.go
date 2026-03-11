package cmd

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

func TestStatusCommandPrintsActivationMode(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:           domain.SiteGlobal,
		WardDraftID:    "draft_123",
		WardID:         "ward_123",
		WardSecret:     "wrd_secret",
		WardStatus:     domain.WardStatusActive,
		Spec:           domain.SpecStarter,
		BillingMode:    domain.BillingModeMonthly,
		ActivationMode: domain.ActivationModeTrial,
		DomainType:     domain.DomainTypePlatformSubdomain,
		Domain:         "demo.warded.me",
		UpstreamPort:   18789,
		ListenAddr:     ":443",
		UpdatedAt:      time.Now().UTC(),
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, "test")
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"status",
		"--local",
		"--config-dir", dir,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}

	body := out.String()
	if !strings.Contains(body, "billing_mode=monthly") {
		t.Fatalf("expected billing_mode in output, got: %s", body)
	}
	if !strings.Contains(body, "activation_mode=trial") {
		t.Fatalf("expected activation_mode in output, got: %s", body)
	}
}
