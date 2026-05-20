package cmd

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/herewei/warded/internal/application"
	"github.com/herewei/warded/internal/ports"
)

// ── §0.1.2 Envelope JSON shape ───────────────────────────────────────────────

func TestEnvelope_SuccessOmitsErrorField(t *testing.T) {
	t.Parallel()
	e := Envelope{OK: true, Command: "doctor", Data: map[string]any{"key": "val"}}
	m := marshalEnvelope(t, e)
	if _, ok := m["error"]; ok {
		t.Fatal("success envelope must not contain 'error' field")
	}
}

func TestEnvelope_ErrorOmitsDataField(t *testing.T) {
	t.Parallel()
	e := Envelope{OK: false, Command: "new", Error: &ErrorDetail{Code: "internal_error"}}
	m := marshalEnvelope(t, e)
	if _, ok := m["data"]; ok {
		t.Fatal("error envelope must not contain 'data' field")
	}
}

func TestEnvelope_OKFieldAlwaysPresent(t *testing.T) {
	t.Parallel()
	for _, ok := range []bool{true, false} {
		e := Envelope{OK: ok, Command: "status"}
		if !ok {
			e.Error = &ErrorDetail{Code: "internal_error"}
		}
		m := marshalEnvelope(t, e)
		if _, present := m["ok"]; !present {
			t.Fatalf("ok field must always be present, envelope ok=%v", ok)
		}
	}
}

func TestEnvelope_CommandFieldAlwaysPresent(t *testing.T) {
	t.Parallel()
	for _, cmd := range []string{"new", "status", "doctor", "integrate", "renew-cert", "serve"} {
		e := Envelope{OK: true, Command: cmd}
		m := marshalEnvelope(t, e)
		if m["command"] != cmd {
			t.Errorf("expected command=%q, got %v", cmd, m["command"])
		}
	}
}

func TestEnvelope_ModeOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	e := Envelope{OK: true, Command: "doctor"}
	m := marshalEnvelope(t, e)
	if _, ok := m["mode"]; ok {
		t.Fatal("mode must be omitted when empty")
	}
}

func TestEnvelope_ModePresentWhenSet(t *testing.T) {
	t.Parallel()
	e := Envelope{OK: true, Command: "doctor", Mode: "baseline"}
	m := marshalEnvelope(t, e)
	if m["mode"] != "baseline" {
		t.Fatalf("expected mode=baseline, got %v", m["mode"])
	}
}

// §0.1.2 rule 8: request_id must not be filled with empty string; omit when absent.
func TestEnvelope_RequestIDOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	e := Envelope{OK: true, Command: "new"}
	m := marshalEnvelope(t, e)
	if v, ok := m["request_id"]; ok {
		t.Fatalf("request_id must be omitted when empty, got %v", v)
	}
}

func TestEnvelope_RequestIDPresentWhenSet(t *testing.T) {
	t.Parallel()
	e := Envelope{OK: true, Command: "new", RequestID: "req_abc123"}
	m := marshalEnvelope(t, e)
	if m["request_id"] != "req_abc123" {
		t.Fatalf("expected request_id=req_abc123, got %v", m["request_id"])
	}
}

func TestEnvelope_WarningsOmittedWhenNil(t *testing.T) {
	t.Parallel()
	e := Envelope{OK: true, Command: "doctor"}
	m := marshalEnvelope(t, e)
	if _, ok := m["warnings"]; ok {
		t.Fatal("warnings must be omitted when nil")
	}
}

func TestEnvelope_NextStepsOmittedWhenNil(t *testing.T) {
	t.Parallel()
	e := Envelope{OK: true, Command: "status"}
	m := marshalEnvelope(t, e)
	if _, ok := m["next_steps"]; ok {
		t.Fatal("next_steps must be omitted when nil")
	}
}

func TestEnvelope_WarningShape(t *testing.T) {
	t.Parallel()
	e := Envelope{
		OK:      true,
		Command: "doctor",
		Warnings: []Warning{
			{Code: "cert_expiring_soon", Message: "certificate expires in 5 days"},
		},
	}
	m := marshalEnvelope(t, e)
	warnings, ok := m["warnings"].([]any)
	if !ok || len(warnings) != 1 {
		t.Fatalf("expected warnings array with 1 item, got %v", m["warnings"])
	}
	w := warnings[0].(map[string]any)
	if w["code"] != "cert_expiring_soon" {
		t.Errorf("expected warning code=cert_expiring_soon, got %v", w["code"])
	}
}

func TestEnvelope_NextStepCommandShape(t *testing.T) {
	t.Parallel()
	e := Envelope{
		OK:      true,
		Command: "status",
		NextSteps: []NextStep{
			{Kind: "command", Command: "warded", Args: []string{"new", "--site", "global"}},
		},
	}
	m := marshalEnvelope(t, e)
	steps, ok := m["next_steps"].([]any)
	if !ok || len(steps) != 1 {
		t.Fatalf("expected next_steps array with 1 item, got %v", m["next_steps"])
	}
	step := steps[0].(map[string]any)
	if step["kind"] != "command" {
		t.Errorf("expected kind=command, got %v", step["kind"])
	}
	if step["command"] != "warded" {
		t.Errorf("expected command=warded, got %v", step["command"])
	}
	args, _ := step["args"].([]any)
	if len(args) != 3 {
		t.Errorf("expected 3 args, got %v", args)
	}
}

// ── §0.1.2 rule 9: retry_after_seconds ───────────────────────────────────────
// Must be number or null; must NOT use 0 to represent unknown.

func TestErrorDetail_RetryAfterSecondsAlwaysPresentInJSON(t *testing.T) {
	t.Parallel()
	ed := ErrorDetail{Code: "ingress_unreachable"}
	b, _ := json.Marshal(ed)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["retry_after_seconds"]; !ok {
		t.Fatal("retry_after_seconds must always be present in ErrorDetail JSON (as null or number)")
	}
}

func TestErrorDetail_RetryAfterSecondsNullWhenUnknown(t *testing.T) {
	t.Parallel()
	ed := ErrorDetail{Code: "rate_limited", RetryAfterSeconds: nil}
	b, _ := json.Marshal(ed)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if v := m["retry_after_seconds"]; v != nil {
		t.Fatalf("retry_after_seconds must be null when unknown, got %v", v)
	}
}

func TestErrorDetail_RetryAfterSecondsNumberWhenKnown(t *testing.T) {
	t.Parallel()
	secs := 30
	ed := ErrorDetail{Code: "rate_limited", RetryAfterSeconds: &secs}
	b, _ := json.Marshal(ed)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["retry_after_seconds"] != float64(30) {
		t.Fatalf("expected retry_after_seconds=30, got %v", m["retry_after_seconds"])
	}
}

func TestErrorDetail_RetryAfterSecondsNotZeroForUnknown(t *testing.T) {
	t.Parallel()
	// Regression: must not serialize 0 when retry-after is unknown.
	ed := ErrorDetail{Code: "rate_limited", RetryAfterSeconds: nil}
	b, _ := json.Marshal(ed)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if m["retry_after_seconds"] == float64(0) {
		t.Fatal("retry_after_seconds must be null (not 0) when retry interval is unknown")
	}
}

func TestErrorDetail_HTTPStatusOmittedWhenZero(t *testing.T) {
	t.Parallel()
	ed := ErrorDetail{Code: "data_dir_not_writable"}
	b, _ := json.Marshal(ed)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if v, ok := m["http_status"]; ok && v != nil {
		t.Fatalf("http_status must be omitted for local errors, got %v", v)
	}
}

func TestErrorDetail_ReasonOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	ed := ErrorDetail{Code: "internal_error"}
	b, _ := json.Marshal(ed)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["reason"]; ok {
		t.Fatal("reason must be omitted when empty")
	}
}

// ── §0.1.4 classifyError ─────────────────────────────────────────────────────

// ── §0.1.4 platform error allowlist by category ──────────────────────────────
//
// Each group maps to a section of the CLI contract. Tests drive the allowlist
// so that unknown codes are never silently swallowed and known codes are never
// accidentally re-mapped to internal_error.

// Common codes: applicable across multiple platform endpoints.
func TestClassifyError_PlatformCommonCodes(t *testing.T) {
	t.Parallel()
	codes := []string{
		"not_found",
		"access_denied",
		"forbidden",
		"rate_limited",
		"site_not_supported",
	}
	for _, code := range codes {
		code := code
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			err := &ports.PlatformError{Code: code, HTTPStatus: 422}
			detail := classifyError(err)
			if detail.Code != code {
				t.Errorf("common platform code %q must pass through unchanged, got %q", code, detail.Code)
			}
		})
	}
}

// Ward-draft and preflight codes: returned by POST /api/v1/ward-drafts and
// POST /api/v1/ingress-probes.
func TestClassifyError_PlatformWardDraftAndPreflightCodes(t *testing.T) {
	t.Parallel()
	codes := []string{
		"ingress_unreachable",
		"domain_dns_not_ready",
		"domain_public_ip_mismatch",
		"public_ip_unavailable",
		"domain_policy_violation",
		"domain_reserved",
		"domain_unavailable",
		"domain_not_allowed",
		"trial_not_eligible",
		"draft_secret_conflict",
		"ward_not_active",
	}
	for _, code := range codes {
		code := code
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			err := &ports.PlatformError{Code: code, HTTPStatus: 422}
			detail := classifyError(err)
			if detail.Code != code {
				t.Errorf("ward-draft/preflight platform code %q must pass through unchanged, got %q", code, detail.Code)
			}
		})
	}
}

// Account and token codes: returned by auth and credential endpoints.
func TestClassifyError_PlatformAccountTokenCodes(t *testing.T) {
	t.Parallel()
	codes := []string{
		"credential_expired",
		"activation_link_expired",
		"auth_code_invalid",
		"auth_code_expired",
	}
	for _, code := range codes {
		code := code
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			err := &ports.PlatformError{Code: code, HTTPStatus: 401}
			detail := classifyError(err)
			if detail.Code != code {
				t.Errorf("account/token platform code %q must pass through unchanged, got %q", code, detail.Code)
			}
		})
	}
}

// PlatformError.Reason must propagate to ErrorDetail.Reason unchanged.
// This locks the data path: stable sub-reason codes (e.g. tcp_connect_failed from
// ingress probes) must reach the JSON output so agents can decide next steps.
func TestClassifyError_PlatformReasonPropagates(t *testing.T) {
	t.Parallel()
	err := &ports.PlatformError{Code: "ingress_unreachable", Reason: "tcp_connect_failed", HTTPStatus: 422}
	detail := classifyError(err)
	if detail.Code != "ingress_unreachable" {
		t.Errorf("expected code=ingress_unreachable, got %q", detail.Code)
	}
	if detail.Reason != "tcp_connect_failed" {
		t.Errorf("expected reason=tcp_connect_failed, got %q", detail.Reason)
	}
}

// Reason must be omitted (empty) when PlatformError carries no sub-reason.
func TestClassifyError_PlatformReasonOmittedWhenEmpty(t *testing.T) {
	t.Parallel()
	err := &ports.PlatformError{Code: "access_denied", HTTPStatus: 403}
	detail := classifyError(err)
	if detail.Reason != "" {
		t.Errorf("expected empty reason when PlatformError.Reason is unset, got %q", detail.Reason)
	}
}

// §0.1.1 rule 3: invalid --format value must produce stable error code invalid_format.
// classifyError must recognise ErrInvalidFormat and return code=invalid_format so
// the root command can output a stable JSON error rather than a raw cobra message.
func TestClassifyError_InvalidFormatSentinel(t *testing.T) {
	t.Parallel()
	detail := classifyError(ErrInvalidFormat)
	if detail.Code != "invalid_format" {
		t.Errorf("expected invalid_format for ErrInvalidFormat sentinel, got %q", detail.Code)
	}
}

// Unknown platform error codes must NOT pass through; must map to a safe fallback.
func TestClassifyError_PlatformUnknownCodeMappedToSafe(t *testing.T) {
	t.Parallel()
	unknownCodes := []string{
		"some_future_undocumented_code",
		"internal_server_error_verbose",
		"debug_trace_dump",
	}
	for _, code := range unknownCodes {
		code := code
		t.Run(code, func(t *testing.T) {
			t.Parallel()
			err := &ports.PlatformError{Code: code, HTTPStatus: 500}
			detail := classifyError(err)
			allowed := map[string]bool{"internal_error": true, "platform_response_invalid": true}
			if !allowed[detail.Code] {
				t.Errorf("unknown platform code %q must map to internal_error or platform_response_invalid, got %q", code, detail.Code)
			}
		})
	}
}

func TestClassifyError_PlatformHTTPStatusPreserved(t *testing.T) {
	t.Parallel()
	err := &ports.PlatformError{Code: "ingress_unreachable", HTTPStatus: 422, RequestID: "req_xyz"}
	detail := classifyError(err)
	if detail.HTTPStatus != 422 {
		t.Errorf("expected http_status=422, got %d", detail.HTTPStatus)
	}
	if detail.RequestID != "req_xyz" {
		t.Errorf("expected request_id=req_xyz, got %q", detail.RequestID)
	}
}

func TestErrorRequestID_PlatformError(t *testing.T) {
	t.Parallel()
	err := &ports.PlatformError{Code: "ingress_unreachable", HTTPStatus: 422, RequestID: "req_top"}
	if got := errorRequestID(err); got != "req_top" {
		t.Fatalf("expected request_id=req_top, got %q", got)
	}
}

func TestClassifyError_RateLimitedWithRetryAfter(t *testing.T) {
	t.Parallel()
	err := &ports.PlatformError{Code: "rate_limited", HTTPStatus: 429, RetryAfter: 30}
	detail := classifyError(err)
	if detail.Code != "rate_limited" {
		t.Errorf("expected rate_limited, got %q", detail.Code)
	}
	if detail.RetryAfterSeconds == nil || *detail.RetryAfterSeconds != 30 {
		t.Errorf("expected retry_after_seconds=30, got %v", detail.RetryAfterSeconds)
	}
}

// When platform returns 429 without Retry-After header, RetryAfter=0 in PlatformError.
// classifyError must produce null (nil pointer), not 0.
func TestClassifyError_RateLimitedWithoutRetryAfterProducesNull(t *testing.T) {
	t.Parallel()
	err := &ports.PlatformError{Code: "rate_limited", HTTPStatus: 429, RetryAfter: 0}
	detail := classifyError(err)
	if detail.RetryAfterSeconds != nil {
		t.Errorf("expected retry_after_seconds=null when RetryAfter=0, got %d", *detail.RetryAfterSeconds)
	}
}

func TestClassifyError_DataDirNotWritable(t *testing.T) {
	t.Parallel()
	detail := classifyError(application.ErrDataDirNotWritable)
	if detail.Code != "data_dir_not_writable" {
		t.Errorf("expected data_dir_not_writable, got %q", detail.Code)
	}
	if detail.HTTPStatus != 0 {
		t.Errorf("local error must have http_status=0 (omitted), got %d", detail.HTTPStatus)
	}
}

func TestClassifyError_ListenPortPermissionDenied(t *testing.T) {
	t.Parallel()
	detail := classifyError(application.ErrListenPortPermission)
	if detail.Code != "listen_port_permission_denied" {
		t.Errorf("expected listen_port_permission_denied, got %q", detail.Code)
	}
}

func TestClassifyError_ListenPortOccupied(t *testing.T) {
	t.Parallel()
	detail := classifyError(application.ErrListenPortOccupied)
	if detail.Code != "listen_port_occupied" {
		t.Errorf("expected listen_port_occupied, got %q", detail.Code)
	}
}

func TestClassifyError_UpstreamUnreachable(t *testing.T) {
	t.Parallel()
	detail := classifyError(application.ErrUpstreamUnreachable)
	if detail.Code != "upstream_unreachable" {
		t.Errorf("expected upstream_unreachable, got %q", detail.Code)
	}
}

func TestClassifyError_WrappedSentinelUnwrapped(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("new: preflight: %w", application.ErrListenPortOccupied)
	detail := classifyError(wrapped)
	if detail.Code != "listen_port_occupied" {
		t.Errorf("expected listen_port_occupied for wrapped sentinel, got %q", detail.Code)
	}
}

func TestClassifyError_UnknownErrorFallsToInternalError(t *testing.T) {
	t.Parallel()
	err := errors.New("something completely unexpected happened")
	detail := classifyError(err)
	if detail.Code != "internal_error" {
		t.Errorf("expected internal_error for unknown error, got %q", detail.Code)
	}
}

func TestClassifyError_CodeNeverEmpty(t *testing.T) {
	t.Parallel()
	cases := []error{
		errors.New("generic error"),
		fmt.Errorf("wrapped: %w", errors.New("inner")),
		&ports.PlatformError{Code: "", HTTPStatus: 500},
	}
	for _, err := range cases {
		detail := classifyError(err)
		if detail.Code == "" {
			t.Errorf("classifyError must never return empty code, got empty for err=%v", err)
		}
	}
}

// §0.1.3: sensitive data must never appear in classified error output.
func TestClassifyError_DoesNotLeakSensitiveMessage(t *testing.T) {
	t.Parallel()
	sensitiveErr := errors.New("database error: password=hunter2 token=sk-abc123")
	detail := classifyError(sensitiveErr)
	if detail.Code != "internal_error" {
		t.Errorf("expected internal_error, got %q", detail.Code)
	}
	// For internal_error, the raw message should not be exposed in Code or Reason.
	// Message field is allowed as optional debug summary per contract §0.1.2 rule 10,
	// but Code and Reason must not leak verbatim sensitive content.
	if detail.Reason == sensitiveErr.Error() {
		t.Error("classifyError must not expose raw error message as stable Reason field")
	}
}

// ── helpers ───────────────────────────────────────────────────────────────────

func marshalEnvelope(t *testing.T, e Envelope) map[string]any {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal(Envelope): %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	return m
}
