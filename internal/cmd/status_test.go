package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

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
	if !strings.Contains(body, "Entry point: https://abcd.warded.cn") {
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
	if !strings.Contains(body, "Before activation, you can still change settings") {
		t.Fatalf("expected pending setup update hint in output, got: %s", body)
	}
}
