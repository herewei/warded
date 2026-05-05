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
	if !strings.Contains(body, "Ward: abcd.warded.cn (pending)") {
		t.Fatalf("expected pending header in output, got: %s", body)
	}
	if strings.Contains(body, "Entry point: https://abcd.warded.cn") {
		t.Fatalf("expected no duplicate entry point when header carries it, got: %s", body)
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

func TestRenderStatusOutputFailedDraftDoesNotShowPendingOrDuplicateEntryPoint(t *testing.T) {
	t.Parallel()

	out := &application.StatusOutput{
		Runtime: &domain.LocalWardRuntime{
			Site:            domain.SiteGlobal,
			WardDraftID:     "d_123",
			WardStatus:      "failed",
			RequestedDomain: "hnbkqixs.warded.me",
			ActivationURL:   "https://warded.me/activate/d_123",
			Spec:            domain.SpecStarter,
			UpstreamPort:    18789,
			BillingMode:     domain.BillingModeMonthly,
		},
		WardDraft: &ports.GetWardDraftStatusResponse{
			WardDraftID: "d_123",
			Status:      "failed",
			ExpiresAt:   "2026-05-04T10:00:04+08:00",
		},
	}

	var buf bytes.Buffer
	renderStatusOutput(&buf, out)
	body := buf.String()
	if !strings.Contains(body, "Ward: hnbkqixs.warded.me") {
		t.Fatalf("expected requested domain header, got: %s", body)
	}
	if strings.Contains(body, "(pending)") {
		t.Fatalf("expected failed draft header not to show pending, got: %s", body)
	}
	if strings.Contains(body, "Entry point: https://hnbkqixs.warded.me") {
		t.Fatalf("expected no duplicate entry point when header carries it, got: %s", body)
	}
	if strings.Contains(body, "Setup Link:") {
		t.Fatalf("expected no setup link for failed draft, got: %s", body)
	}
}
