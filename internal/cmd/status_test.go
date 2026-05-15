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

func TestRenderRuntimeListShowsAllKindsAndNext(t *testing.T) {
	t.Parallel()

	runtimes := []application.RuntimeSummary{
		{Index: 1, Kind: application.RuntimeKindPendingConfig, Runtime: domain.LocalWardRuntime{RequestedDomain: "pending.warded.me"}},
		{Index: 2, Kind: application.RuntimeKindDraft, Runtime: domain.LocalWardRuntime{WardDraftID: "d_abc", RequestedDomain: "draft.warded.me", WardStatus: "failed"}},
		{Index: 3, Kind: application.RuntimeKindWard, Runtime: domain.LocalWardRuntime{WardID: "ward_xyz", Domain: "live.warded.me", WardStatus: "active"}},
	}

	var buf bytes.Buffer
	renderRuntimeList(&buf, runtimes, "/var/lib/warded")
	body := buf.String()

	if !strings.Contains(body, "Multiple local wards found") {
		t.Fatalf("expected header, got: %s", body)
	}
	if !strings.Contains(body, "pending-config") || !strings.Contains(body, "not submitted") {
		t.Fatalf("expected pending-config row, got: %s", body)
	}
	if !strings.Contains(body, "draft") || !strings.Contains(body, "d_abc") || !strings.Contains(body, "failed") {
		t.Fatalf("expected draft row, got: %s", body)
	}
	if !strings.Contains(body, "ward") || !strings.Contains(body, "ward_xyz") || !strings.Contains(body, "active") {
		t.Fatalf("expected ward row, got: %s", body)
	}
	if !strings.Contains(body, "warded status <index>") {
		t.Fatalf("expected Next hint, got: %s", body)
	}
}

func TestResolveStatusTargetByIndex(t *testing.T) {
	t.Parallel()

	runtimes := []application.RuntimeSummary{
		{Index: 1, Kind: application.RuntimeKindDraft, Runtime: domain.LocalWardRuntime{WardDraftID: "d_one"}},
		{Index: 2, Kind: application.RuntimeKindWard, Runtime: domain.LocalWardRuntime{WardID: "ward_two"}},
	}

	rt, err := resolveStatusTarget(runtimes, []string{"2"}, "", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.Runtime.WardID != "ward_two" {
		t.Fatalf("expected ward_two, got %+v", rt)
	}
}

func TestResolveStatusTargetByWardID(t *testing.T) {
	t.Parallel()

	runtimes := []application.RuntimeSummary{
		{Index: 1, Kind: application.RuntimeKindWard, Runtime: domain.LocalWardRuntime{WardID: "ward_abc", Domain: "abc.warded.me"}},
	}

	rt, err := resolveStatusTarget(runtimes, nil, "ward_abc", "", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rt.Runtime.WardID != "ward_abc" {
		t.Fatalf("expected ward_abc, got %+v", rt)
	}
}

func TestResolveStatusTargetByDomainAmbiguous(t *testing.T) {
	t.Parallel()

	runtimes := []application.RuntimeSummary{
		{Index: 1, Kind: application.RuntimeKindDraft, Runtime: domain.LocalWardRuntime{WardDraftID: "d_one", RequestedDomain: "foo.warded.me"}},
		{Index: 2, Kind: application.RuntimeKindWard, Runtime: domain.LocalWardRuntime{WardID: "ward_two", Domain: "foo.warded.me"}},
	}

	_, err := resolveStatusTarget(runtimes, nil, "", "", "foo.warded.me")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	if !strings.Contains(err.Error(), "matches multiple") {
		t.Fatalf("expected 'matches multiple' in error, got: %v", err)
	}
}

func TestResolveStatusTargetNotFound(t *testing.T) {
	t.Parallel()

	runtimes := []application.RuntimeSummary{
		{Index: 1, Kind: application.RuntimeKindWard, Runtime: domain.LocalWardRuntime{WardID: "ward_abc"}},
	}

	_, err := resolveStatusTarget(runtimes, nil, "ward_nope", "", "")
	if err == nil {
		t.Fatal("expected not-found error")
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
