package platformapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/herewei/warded/internal/ports"
)

func TestClientCreateWardDraft(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/ward-drafts" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Warded-Site"); got != "global" {
			t.Fatalf("unexpected site header: %s", got)
		}
		if got := r.Header.Get("X-Request-Id"); got == "" {
			t.Fatal("expected X-Request-Id header to be set")
		}

		var req ports.CreateWardDraftRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.DraftSecretChallenge == "" {
			t.Fatalf("expected draft_secret_challenge in JSON body: %#v", req)
		}
		if req.UpstreamPort != 18789 {
			t.Fatalf("unexpected request body: %#v", req)
		}

		_ = json.NewEncoder(w).Encode(ports.CreateWardDraftResponse{
			WardDraftID:       "draft_123",
			Site:              "global",
			Status:            "pending_activation",
			ExpiresAt:         "2026-03-13T12:00:00Z",
			ActivationURL:     "/activate/draft_123",
			DomainCheckStatus: "not_required",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.CreateWardDraft(context.Background(), ports.CreateWardDraftRequest{
		Site:                 "global",
		Spec:                 "starter",
		BillingMode:          "monthly",
		DomainType:           "platform_subdomain",
		RequestedDomain:      "",
		UpstreamPort:         18789,
		DraftSecretChallenge: "challenge_123",
	})
	if err != nil {
		t.Fatalf("CreateWardDraft returned error: %v", err)
	}
	if resp.WardDraftID != "draft_123" || resp.DomainCheckStatus != "not_required" {
		t.Fatalf("unexpected response: %#v", resp)
	}
	if resp.ActivationURL != "/activate/draft_123" {
		t.Fatalf("expected activation_url, got %#v", resp)
	}
}

func TestClientGetWardDraftStatus(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/ward-drafts/draft_123/status" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Warded-Draft-Challenge"); got != "challenge_123" {
			t.Fatalf("unexpected challenge header: %s", got)
		}
		if got := r.Header.Get("X-Warded-Site"); got != "global" {
			t.Fatalf("unexpected site header: %s", got)
		}
		if got := r.Header.Get("X-Request-Id"); got == "" {
			t.Fatal("expected X-Request-Id header to be set")
		}

		_ = json.NewEncoder(w).Encode(ports.GetWardDraftStatusResponse{
			WardDraftID: "draft_123",
			Status:      "pending_activation",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.GetWardDraftStatus(context.Background(), "global", "challenge_123", "draft_123")
	if err != nil {
		t.Fatalf("GetWardDraftStatus returned error: %v", err)
	}
	if resp.WardDraftID != "draft_123" || resp.Status != "pending_activation" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestClientExchangeAuthCodeSetsSiteHeader(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/auth/exchange" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("X-Warded-Site"); got != "global" {
			t.Fatalf("unexpected site header: %s", got)
		}
		if got := r.Header.Get("X-Request-Id"); got == "" {
			t.Fatal("expected X-Request-Id header to be set")
		}

		_ = json.NewEncoder(w).Encode(ports.ExchangeAuthCodeResponse{
			PrincipalID: "principal_123",
			WardID:      "ward_123",
			SessionID:   "sess_123",
			ExpiresAt:   "2026-03-14T00:00:00Z",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.ExchangeAuthCode(context.Background(), ports.ExchangeAuthCodeRequest{
		Code:   "code_123",
		Site:   "global",
		WardID: "ward_123",
	})
	if err != nil {
		t.Fatalf("ExchangeAuthCode returned error: %v", err)
	}
	if resp.SessionID != "sess_123" {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestClientGetTLSMaterial(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/api/v1/wards/ward_123/tls-material" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer wrd_123" {
			t.Fatalf("unexpected authorization header: %s", got)
		}
		if got := r.Header.Get("X-Warded-Site"); got != "global" {
			t.Fatalf("unexpected site header: %s", got)
		}
		if got := r.Header.Get("X-Request-Id"); got == "" {
			t.Fatal("expected X-Request-Id header to be set")
		}

		_ = json.NewEncoder(w).Encode(ports.GetTLSMaterialResponse{
			TLSCert:             "CERT",
			TLSKey:              "KEY",
			NotAfter:            "2026-04-01T00:00:00Z",
			Version:             "v1",
			RefreshAfterSeconds: 3600,
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	resp, err := client.GetTLSMaterial(context.Background(), "global", "wrd_123", "ward_123")
	if err != nil {
		t.Fatalf("GetTLSMaterial returned error: %v", err)
	}
	if resp.TLSCert != "CERT" || resp.TLSKey != "KEY" || resp.Version != "v1" || resp.RefreshAfterSeconds != 3600 {
		t.Fatalf("unexpected response: %#v", resp)
	}
}

func TestClientCreateWardDraftSurfacesPlatformMessage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":      "internal_error",
			"message":    "database operation failed",
			"request_id": "req_test123",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.CreateWardDraft(context.Background(), ports.CreateWardDraftRequest{
		Site:         "global",
		Spec:         "starter",
		BillingMode:  "monthly",
		DomainType:   "platform_subdomain",
		UpstreamPort: 18789,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	platformErr, ok := err.(*ports.PlatformError)
	if !ok {
		t.Fatalf("expected *ports.PlatformError, got %T (%v)", err, err)
	}
	if platformErr.Message != "database operation failed" {
		t.Fatalf("unexpected platform message: %#v", platformErr)
	}
	if platformErr.RequestID != "req_test123" {
		t.Fatalf("unexpected request id: %#v", platformErr)
	}
	if got := platformErr.Error(); got != "platform error: internal_error (HTTP 500, request_id=req_test123): database operation failed" {
		t.Fatalf("unexpected error string: %s", got)
	}
}

func TestClientCreateWardDraftFallsBackToResponseRequestID(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Request-Id", "req_header456")
		w.WriteHeader(http.StatusBadGateway)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"error":   "upstream_unavailable",
			"message": "origin temporarily unavailable",
		})
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.CreateWardDraft(context.Background(), ports.CreateWardDraftRequest{
		Site:         "global",
		Spec:         "starter",
		BillingMode:  "monthly",
		DomainType:   "platform_subdomain",
		UpstreamPort: 18789,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	platformErr, ok := err.(*ports.PlatformError)
	if !ok {
		t.Fatalf("expected *ports.PlatformError, got %T (%v)", err, err)
	}
	if platformErr.RequestID != "req_header456" {
		t.Fatalf("expected response header request id fallback, got %#v", platformErr)
	}
}

func TestClientCreateWardDraftUnexpectedStatusIncludesTraceDetails(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("CF-Ray", "abc123-sha")
		http.Error(w, "error code: 526", 526)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	_, err := client.CreateWardDraft(context.Background(), ports.CreateWardDraftRequest{
		Site:         "global",
		Spec:         "starter",
		BillingMode:  "monthly",
		DomainType:   "platform_subdomain",
		UpstreamPort: 18789,
	})
	if err == nil {
		t.Fatal("expected error")
	}

	msg := err.Error()
	if !strings.Contains(msg, "unexpected status 526") {
		t.Fatalf("expected status in error, got: %s", msg)
	}
	if !strings.Contains(msg, "trace_id=") {
		t.Fatalf("expected trace_id in error, got: %s", msg)
	}
	if !strings.Contains(msg, "cf_ray=abc123-sha") {
		t.Fatalf("expected cf_ray in error, got: %s", msg)
	}
	if !strings.Contains(msg, "error code: 526") {
		t.Fatalf("expected body preview in error, got: %s", msg)
	}
}

func TestPreviewPayloadRedactsSensitiveFields(t *testing.T) {
	t.Parallel()

	preview := previewPayload([]byte(`{"code":"auth_123","ward_secret":"wrd_secret","nested":{"tls_key":"KEY","ok":"value"}}`))
	if strings.Contains(preview, "auth_123") {
		t.Fatalf("expected auth code to be redacted, got %s", preview)
	}
	if strings.Contains(preview, "wrd_secret") {
		t.Fatalf("expected ward secret to be redacted, got %s", preview)
	}
	if strings.Contains(preview, "\"KEY\"") {
		t.Fatalf("expected tls key to be redacted, got %s", preview)
	}
	if !strings.Contains(preview, redactedValue) {
		t.Fatalf("expected redaction marker, got %s", preview)
	}
	if !strings.Contains(preview, "\"ok\":\"value\"") {
		t.Fatalf("expected non-sensitive fields to remain visible, got %s", preview)
	}
}

func TestPreviewPayloadTruncatesLongPayload(t *testing.T) {
	t.Parallel()

	preview := previewPayload([]byte(strings.Repeat("a", maxLoggedBodyPreview+64)))
	if !strings.HasSuffix(preview, "...(truncated)") {
		t.Fatalf("expected truncated suffix, got %s", preview)
	}
	if len(preview) <= maxLoggedBodyPreview {
		t.Fatalf("expected preview to include content plus truncation suffix, got len=%d", len(preview))
	}
}
