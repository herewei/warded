package cmd

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
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
	root := NewRootCommand(logLevel, BuildInfo{Version: "test"})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"status",
		"--local",
		"--data-dir", dir,
	})

	if err := root.Execute(); err != nil {
		t.Fatalf("status: %v", err)
	}

	body := out.String()
	if !strings.Contains(body, "Billing:") || !strings.Contains(body, "monthly") {
		t.Fatalf("expected Billing in output, got: %s", body)
	}
	if !strings.Contains(body, "Activation:") || !strings.Contains(body, "trial") {
		t.Fatalf("expected Activation in output, got: %s", body)
	}
}

func TestRenderStatusOutputPendingShowsSingleSetupStatus(t *testing.T) {
	t.Parallel()

	out := &application.StatusOutput{
		Runtime: &domain.LocalWardRuntime{
			Site:            domain.SiteCN,
			WardDraftID:     "d_123",
			WardStatus:      domain.WardStatusInitializing,
			RequestedDomain: "abcd.warded.cn",
			ActivationURL:   "https://warded.cn/activate/d_123",
			UpstreamPort:    18789,
			BillingMode:     domain.BillingModeMonthly,
		},
		WardDraft: &ports.GetWardDraftStatusResponse{
			WardDraftID: "d_123",
			Status:      "pending_activation",
			ExpiresAt:   "2026-04-23T17:50:19+08:00",
		},
	}

	var buf bytes.Buffer
	renderStatusOutput(&buf, out)
	body := buf.String()
	if !strings.Contains(body, "Entry point: https://abcd.warded.cn (pending)") {
		t.Fatalf("expected entry point in output, got: %s", body)
	}
	if !strings.Contains(body, "Setup:") || !strings.Contains(body, "pending activation") {
		t.Fatalf("expected setup status in output, got: %s", body)
	}
	if strings.Contains(body, "Draft Status:") || strings.Contains(body, "IDs:") || strings.Contains(body, "Draft:") {
		t.Fatalf("expected no internal draft/ID sections, got: %s", body)
	}
	if strings.Contains(body, "Status:     initializing") {
		t.Fatalf("expected local initializing status to be hidden for pending setup, got: %s", body)
	}
}
