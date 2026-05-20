package e2e_test

// format_json_test.go verifies the --format json contract (§0.1) end-to-end
// against the mock platform and real local command execution.
//
// Each test uses existing helpers (runStatus, runNewCommit, runDoctor, etc.)
// by injecting "--format json" into the args slice. Persistent flags may appear
// after the subcommand name in cobra, so this works without changing helpers.
//
// Tests are grouped by command.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// mustParseJSON fails the test if output is not a single valid JSON object.
func mustParseJSON(t *testing.T, output string) map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(output)
	var m map[string]any
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw output:\n%s", err, output)
	}
	return m
}

// assertEnvelopeShape checks that the envelope contains the required top-level fields.
func assertEnvelopeShape(t *testing.T, m map[string]any, wantCommand string, wantOK bool) {
	t.Helper()
	if _, present := m["ok"]; !present {
		t.Error("envelope must have 'ok' field")
	}
	if m["ok"] != wantOK {
		t.Errorf("expected ok=%v, got %v", wantOK, m["ok"])
	}
	if m["command"] != wantCommand {
		t.Errorf("expected command=%q, got %v", wantCommand, m["command"])
	}
	if wantOK {
		if _, hasErr := m["error"]; hasErr {
			t.Error("success envelope must not contain 'error' field")
		}
	} else {
		if _, hasData := m["data"]; hasData {
			t.Error("error envelope must not contain 'data' field")
		}
		if m["error"] == nil {
			t.Error("error envelope must contain 'error' field")
		}
	}
}

// assertNoSensitiveFields checks that the JSON output does not contain known sensitive field names.
func assertNoSensitiveFields(t *testing.T, output string) {
	t.Helper()
	sensitive := []string{
		`"ward_secret"`,
		`"draft_secret"`,
		`"ward_draft_secret"`,
		`"tls_key"`,
		`"private_key"`,
		`"jwt_signing_secret"`,
	}
	for _, field := range sensitive {
		if strings.Contains(output, field) {
			t.Errorf("JSON output must not expose sensitive field %s", field)
		}
	}
}

// ── warded status --format json ──────────────────────────────────────────────

// TestE2E_Format_JSON_Status_Unconfigured verifies that status with no ward
// returns a valid JSON envelope (ok=true with empty data or ok=false config_not_found).
func TestE2E_Format_JSON_Status_Unconfigured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, _ := runStatus(t, []string{"--format", "json", "--local", "--data-dir=" + dir})
	m := mustParseJSON(t, out)

	if _, present := m["ok"]; !present {
		t.Fatal("envelope must have 'ok' field")
	}
	if m["command"] != "status" {
		t.Fatalf("expected command=status, got %v", m["command"])
	}
	// Either ok=true (empty config is not an error) or ok=false with code=config_not_found
	if m["ok"] == false {
		errObj, _ := m["error"].(map[string]any)
		if errObj == nil {
			t.Fatal("ok=false must include 'error' object")
		}
		code, _ := errObj["code"].(string)
		if code != "config_not_found" {
			t.Errorf("expected error.code=config_not_found for unconfigured status, got %q", code)
		}
	}
}

// TestE2E_Format_JSON_Status_ActiveWard verifies JSON output for an active ward.
func TestE2E_Format_JSON_Status_ActiveWard(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:           domain.SiteGlobal,
		WardID:         "ward_abc",
		WardSecret:     "wrd_secret_sensitive",
		WardStatus:     domain.WardStatusActive,
		Spec:           domain.SpecStarter,
		BillingMode:    domain.BillingModeMonthly,
		ActivationMode: domain.ActivationModeTrial,
		Domain:         "abc.warded.me",
		UpstreamPort:   18789,
		UpstreamAddr:   "127.0.0.1:18789",
		ListenPort:     443,
		ListenHost:     "0.0.0.0",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runStatus(t, []string{"--format", "json", "--local", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "status", true)
	assertNoSensitiveFields(t, out)

	data, _ := m["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected 'data' object in success envelope")
	}
}

// TestE2E_Format_JSON_Status_ActiveWard_SensitiveFieldsAbsent verifies that
// ward_secret and other sensitive fields are never included in JSON output.
func TestE2E_Format_JSON_Status_ActiveWard_SensitiveFieldsAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_abc",
		WardSecret: "wrd_this_must_not_appear",
		WardStatus: domain.WardStatusActive,
		Domain:     "abc.warded.me",
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, _ := runStatus(t, []string{"--format", "json", "--local", "--data-dir=" + dir})
	assertNoSensitiveFields(t, out)
	if strings.Contains(out, "wrd_this_must_not_appear") {
		t.Error("JSON output must not include the ward_secret value")
	}
}

// TestE2E_Format_JSON_Status_PendingDraft verifies JSON output for a pending draft.
func TestE2E_Format_JSON_Status_PendingDraft(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:            domain.SiteCN,
		WardDraftID:     "d_123",
		WardDraftSecret: "wdd_sensitive",
		WardStatus:      domain.WardStatusInitializing,
		RequestedDomain: "pending.warded.cn",
		ActivationURL:   "https://warded.cn/activate/d_123",
		UpstreamPort:    18789,
		BillingMode:     domain.BillingModeMonthly,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runStatus(t, []string{"--format", "json", "--local", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("status: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "status", true)
	assertNoSensitiveFields(t, out)
	if strings.Contains(out, "wdd_sensitive") {
		t.Error("JSON output must not include the draft_secret value")
	}
}

// TestE2E_Format_JSON_Status_NoHumanText verifies no human-readable text leaks into JSON output.
func TestE2E_Format_JSON_Status_NoHumanText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runStatus(t, []string{"--format", "json", "--local", "--data-dir=" + dir})

	humanPatterns := []string{"Ward:", "Next:", "═", "✓", "✗", "Run `warded"}
	for _, pattern := range humanPatterns {
		if strings.Contains(out, pattern) {
			t.Errorf("JSON output must not contain human-text pattern %q\noutput: %s", pattern, out)
		}
	}
}

// ── warded doctor --format json ──────────────────────────────────────────────

// TestE2E_Format_JSON_Doctor_OutputShape verifies the doctor JSON envelope shape.
func TestE2E_Format_JSON_Doctor_OutputShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runDoctor(t, []string{"--format", "json", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("doctor: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "doctor", true)
}

// TestE2E_Format_JSON_Doctor_ChecksArrayShape verifies checks[] structure per §0.1.5.
func TestE2E_Format_JSON_Doctor_ChecksArrayShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runDoctor(t, []string{"--format", "json", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("doctor: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	data, _ := m["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected data object in doctor JSON output")
	}
	checks, _ := data["checks"].([]any)
	if len(checks) == 0 {
		t.Fatal("expected non-empty checks[] in doctor JSON output")
	}
	for i, c := range checks {
		check, _ := c.(map[string]any)
		if check == nil {
			t.Fatalf("checks[%d] must be an object, got %T", i, c)
		}
		if _, ok := check["number"]; !ok {
			t.Errorf("checks[%d] must have 'number' field", i)
		}
		if _, ok := check["key"]; !ok {
			t.Errorf("checks[%d] must have 'key' field", i)
		}
		status, _ := check["status"].(string)
		if status != "ok" && status != "fail" && status != "info" {
			t.Errorf("checks[%d].status must be ok/fail/info, got %q", i, status)
		}
		if _, ok := check["reason"]; !ok {
			// reason may be null/omitted for passing checks, that's OK
		}
	}
}

// TestE2E_Format_JSON_Doctor_BaselineModeField verifies mode=baseline is set.
func TestE2E_Format_JSON_Doctor_BaselineModeField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runDoctor(t, []string{"--format", "json", "--baseline", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("doctor --baseline: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	if m["mode"] != "baseline" {
		t.Errorf("expected mode=baseline for doctor --baseline, got %v", m["mode"])
	}
}

// TestE2E_Format_JSON_Doctor_NoHumanText verifies no human-text in JSON mode.
func TestE2E_Format_JSON_Doctor_NoHumanText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runDoctor(t, []string{"--format", "json", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("doctor: %v\noutput: %s", err, out)
	}

	humanPatterns := []string{"[OK  ]", "[FAIL]", "Summary:", "Ward:", "═"}
	for _, pattern := range humanPatterns {
		if strings.Contains(out, pattern) {
			t.Errorf("JSON mode must not contain human-text pattern %q\noutput: %s", pattern, out)
		}
	}
}

// ── warded new --show --format json ──────────────────────────────────────────

// TestE2E_Format_JSON_New_ShowNoPendingConfig verifies JSON when no pending config.
func TestE2E_Format_JSON_New_ShowNoPendingConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runNewRaw(t, []string{"--format", "json", "--show", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("new --show: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "new", true)
}

// TestE2E_Format_JSON_New_ShowPendingConfig verifies JSON with a pending config.
func TestE2E_Format_JSON_New_ShowPendingConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// Stage a pending config first
	upstreamPort := startMockUpstream(t)
	_, err := runNewRaw(t, []string{
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new (stage): %v", err)
	}

	out, err := runNewRaw(t, []string{"--format", "json", "--show", "--data-dir=" + dir})
	if err != nil {
		t.Fatalf("new --show: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "new", true)
	assertNoSensitiveFields(t, out)

	data, _ := m["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected data object in new --show JSON output")
	}
}

// ── warded new --commit --format json (mock platform) ─────────────────────────

// TestE2E_Format_JSON_NewCommit_SuccessShape verifies JSON envelope on commit success.
func TestE2E_Format_JSON_NewCommit_SuccessShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--format", "json",
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "new", true)
	assertNoSensitiveFields(t, out)

	data, _ := m["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected data object in new --commit JSON success")
	}
}

// TestE2E_Format_JSON_NewCommit_SuccessHasSetupLink verifies setup_link is in data.
func TestE2E_Format_JSON_NewCommit_SuccessHasSetupLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--format", "json",
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	data, _ := m["data"].(map[string]any)
	if data == nil {
		t.Fatal("expected data object")
	}
	// setup_link (or activation_url) must be present and non-empty
	setupLink, _ := data["setup_link"].(string)
	if setupLink == "" {
		t.Errorf("expected non-empty setup_link in data, got: %v", data)
	}
}

// TestE2E_Format_JSON_NewCommit_SuccessNoDraftIDInTextOutput verifies that
// draft_id is not present in text mode (§0.1.3 rule 5 regression guard).
func TestE2E_Format_JSON_NewCommit_SuccessNoDraftSecretInJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--format", "json",
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}
	assertNoSensitiveFields(t, out)
	// The ward_draft_secret (draft credential) must never appear in JSON output.
	runtime, err := storage.NewJSONStore(dir).LoadWardRuntime(context.Background())
	if err != nil || runtime == nil {
		t.Skip("could not load runtime for sensitive-value check")
	}
	if runtime.WardDraftSecret != "" && strings.Contains(out, runtime.WardDraftSecret) {
		t.Error("JSON output must not contain the ward_draft_secret value")
	}
}

// TestE2E_Format_JSON_NewCommit_PlatformErrorShape verifies JSON error envelope
// when the platform returns a structured error.
func TestE2E_Format_JSON_NewCommit_PlatformErrorIngressUnreachable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{IngressProbeStatus: "unreachable"})

	out, err := runNewCommit(t, []string{
		"--format", "json",
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected error for ingress_unreachable")
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "new", false)

	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "ingress_unreachable" {
		t.Errorf("expected error.code=ingress_unreachable, got %v", errObj["code"])
	}
	// Must NOT contain human explanation text
	humanPhrases := []string{"Check port", "firewall", "security group"}
	for _, phrase := range humanPhrases {
		if strings.Contains(out, phrase) {
			t.Errorf("JSON error must not contain human explanation %q\noutput: %s", phrase, out)
		}
	}
}

// TestE2E_Format_JSON_NewCommit_RateLimitedWithRetryAfter verifies retry_after_seconds.
func TestE2E_Format_JSON_NewCommit_RateLimitedWithRetryAfter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{RateLimited: true})

	out, err := runNewCommit(t, []string{
		"--format", "json",
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected error for rate_limited")
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "new", false)

	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "rate_limited" {
		t.Errorf("expected error.code=rate_limited, got %v", errObj["code"])
	}
	// The mock returns Retry-After: 30, so retry_after_seconds must be 30 (not 0 or null).
	retryAfter := errObj["retry_after_seconds"]
	if retryAfter == nil {
		t.Error("expected retry_after_seconds to be set (not null) when platform sends Retry-After header")
	}
	if retryAfter == float64(0) {
		t.Error("retry_after_seconds must not be 0; use null for unknown or actual seconds")
	}
}

// TestE2E_Format_JSON_NewCommit_PlatformError_CodePassThrough verifies that
// known platform error codes are forwarded verbatim (not translated).
func TestE2E_Format_JSON_NewCommit_PlatformError_CodePassThrough(t *testing.T) {
	t.Parallel()
	knownCodes := []struct {
		code       string
		httpStatus int
	}{
		{"domain_unavailable", 422},
		{"domain_reserved", 422},
		{"domain_dns_not_ready", 422},
		{"domain_public_ip_mismatch", 422},
	}
	for _, tc := range knownCodes {
		tc := tc
		t.Run(tc.code, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			upstreamPort := startMockUpstream(t)
			mock := newMockPlatform(t, mockPlatformOptions{
				CreateErrorStatus: tc.httpStatus,
				CreateErrorCode:   tc.code,
			})

			out, err := runNewCommit(t, []string{
				"--format", "json",
				"--platform-origin=" + mock.URL,
				"--site=global",
				"--spec=pro",
				"--domain-type=custom_domain",
				"--domain=robot.example.com",
				fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
				"--data-dir=" + dir,
			})
			if err == nil {
				t.Fatalf("expected error for platform code %s", tc.code)
			}

			m := mustParseJSON(t, out)
			errObj, _ := m["error"].(map[string]any)
			if errObj["code"] != tc.code {
				t.Errorf("expected error.code=%q to pass through, got %v", tc.code, errObj["code"])
			}
		})
	}
}

// TestE2E_Format_JSON_NewCommit_NoHumanTextOnSuccess verifies no human text in JSON success.
func TestE2E_Format_JSON_NewCommit_NoHumanTextOnSuccess(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runNewCommit(t, []string{
		"--format", "json",
		"--platform-origin=" + mock.URL,
		"--site=global",
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("new --commit: %v\noutput: %s", err, out)
	}

	humanPatterns := []string{
		"Open this link", "After opening", "✓", "✗", "Ward:", "═",
		"Setup updated", "Setup created",
	}
	for _, pattern := range humanPatterns {
		if strings.Contains(out, pattern) {
			t.Errorf("JSON success must not contain human-text pattern %q\noutput: %s", pattern, out)
		}
	}
}

// ── warded integrate --format json ───────────────────────────────────────────

// TestE2E_Format_JSON_Integrate_OutputShape verifies integrate JSON envelope shape.
func TestE2E_Format_JSON_Integrate_OutputShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runIntegrate(t, []string{
		"--format", "json",
		"--agent=openclaw",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("integrate: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "integrate", true)
	assertNoSensitiveFields(t, out)
}

// TestE2E_Format_JSON_Integrate_NoHumanText verifies no human text in JSON mode.
func TestE2E_Format_JSON_Integrate_NoHumanText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runIntegrate(t, []string{
		"--format", "json",
		"--agent=openclaw",
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("integrate: %v\noutput: %s", err, out)
	}

	humanPatterns := []string{"Agent:", "Config file:", "Status:", "Next:", "Suggested patch:"}
	for _, pattern := range humanPatterns {
		if strings.Contains(out, pattern) {
			t.Errorf("JSON mode must not contain human-text pattern %q\noutput: %s", pattern, out)
		}
	}
}

// ── warded renew-cert --format json ──────────────────────────────────────────

// TestE2E_Format_JSON_RenewCert_NoRuntime_ConfigNotFound verifies config_not_found
// is returned when there is no ward runtime.
func TestE2E_Format_JSON_RenewCert_NoRuntime_ConfigNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runCmd_renewCert(t, []string{"--format", "json", "--data-dir=" + dir})
	if err == nil {
		t.Fatal("expected error when no ward runtime")
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "renew-cert", false)

	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "config_not_found" {
		t.Errorf("expected error.code=config_not_found, got %v", errObj["code"])
	}
}

// TestE2E_Format_JSON_RenewCert_NoHumanText verifies no human text on error.
func TestE2E_Format_JSON_RenewCert_NoHumanText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, _ := runCmd_renewCert(t, []string{"--format", "json", "--data-dir=" + dir})

	humanPhrases := []string{"run 'warded new --commit'", "Certificate refreshed", "Domain:", "Valid until:"}
	for _, phrase := range humanPhrases {
		if strings.Contains(out, phrase) {
			t.Errorf("JSON mode must not contain human phrase %q\noutput: %s", phrase, out)
		}
	}
}

// ── cross-command: next_steps shape ──────────────────────────────────────────

// TestE2E_Format_JSON_NextStepsAreStructured verifies that when next_steps are
// present, each entry has a machine-readable 'kind' field (not just narrative text).
func TestE2E_Format_JSON_NextStepsAreStructured(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, _ := runStatus(t, []string{"--format", "json", "--local", "--data-dir=" + dir})
	m := mustParseJSON(t, out)

	steps, _ := m["next_steps"].([]any)
	for i, s := range steps {
		step, _ := s.(map[string]any)
		if step == nil {
			t.Errorf("next_steps[%d] must be an object", i)
			continue
		}
		if _, ok := step["kind"]; !ok {
			t.Errorf("next_steps[%d] must have 'kind' field, got: %v", i, step)
		}
		kind, _ := step["kind"].(string)
		if kind == "" {
			t.Errorf("next_steps[%d].kind must not be empty", i)
		}
	}
}

// ── warded doctor --preflight --format json ───────────────────────────────────

// TestE2E_Format_JSON_DoctorPreflight_ModePreflightSet verifies that
// doctor --preflight produces mode=preflight in the JSON envelope.
func TestE2E_Format_JSON_DoctorPreflight_ModePreflightSet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	listenPort := reserveActivationPort(t)
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runDoctor(t, []string{
		"--format", "json",
		"--preflight",
		"--site=global",
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--listen=127.0.0.1:%d", listenPort),
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("doctor --preflight: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "doctor", true)
	if m["mode"] != "preflight" {
		t.Errorf("expected mode=preflight, got %v", m["mode"])
	}
}

// TestE2E_Format_JSON_DoctorPreflight_NextStepsCommandShape verifies that
// the success next_steps[0] is `warded new ...` (without --commit).
// This is the core contract for preflight (§7.2 rule 9).
func TestE2E_Format_JSON_DoctorPreflight_NextStepsCommandShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	listenPort := reserveActivationPort(t)
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runDoctor(t, []string{
		"--format", "json",
		"--preflight",
		"--site=global",
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--listen=127.0.0.1:%d", listenPort),
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("doctor --preflight: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	steps, _ := m["next_steps"].([]any)
	if len(steps) == 0 {
		t.Fatal("doctor --preflight success must produce at least one next_step")
	}
	step, _ := steps[0].(map[string]any)
	if step == nil {
		t.Fatal("next_steps[0] must be a JSON object")
	}

	// kind must be "command"
	if step["kind"] != "command" {
		t.Errorf("expected next_steps[0].kind=command, got %v", step["kind"])
	}
	// command must be "warded"
	if step["command"] != "warded" {
		t.Errorf("expected next_steps[0].command=warded, got %v", step["command"])
	}
	// args must be an array whose first element is "new"
	args, _ := step["args"].([]any)
	if len(args) == 0 {
		t.Fatal("next_steps[0].args must be a non-empty array")
	}
	if args[0] != "new" {
		t.Errorf("expected next_steps[0].args[0]=new, got %v", args[0])
	}
	// args must NOT contain "--commit" (§7.2 rule 9: prohibit suggesting warded new --commit directly)
	for _, arg := range args {
		if arg == "--commit" {
			t.Error("next_steps[0].args must not contain --commit; preflight must suggest `warded new` (without --commit)")
		}
	}
	// args must not contain backtick-wrapped natural language sentences
	for _, arg := range args {
		argStr, _ := arg.(string)
		if strings.Contains(argStr, "`") || strings.Contains(argStr, "Run ") {
			t.Errorf("next_steps[0].args must not contain narrative text, got %q", argStr)
		}
	}
}

// TestE2E_Format_JSON_DoctorPreflight_DoesNotCreatePendingFile verifies that
// --preflight never writes .pending/ward.json (§7.2 rule 10).
func TestE2E_Format_JSON_DoctorPreflight_DoesNotCreatePendingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	listenPort := reserveActivationPort(t)
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	_, _ = runDoctor(t, []string{
		"--format", "json",
		"--preflight",
		"--site=global",
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--listen=127.0.0.1:%d", listenPort),
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})

	pendingFile := filepath.Join(dir, ".pending", "ward.json")
	if _, err := os.Stat(pendingFile); !os.IsNotExist(err) {
		t.Errorf("--preflight must not create %s, but file exists", pendingFile)
	}
}

// TestE2E_Format_JSON_DoctorPreflight_SiteMissingReturnsInvalidArgument verifies
// that --preflight without --site returns error.code=invalid_argument.
func TestE2E_Format_JSON_DoctorPreflight_SiteMissingReturnsInvalidArgument(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runDoctor(t, []string{
		"--format", "json",
		"--preflight",
		// deliberately omit --site
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected error: --preflight requires --site")
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "doctor", false)
	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "invalid_argument" {
		t.Errorf("expected error.code=invalid_argument for missing --site, got %v", errObj["code"])
	}
}

// TestE2E_Format_JSON_DoctorPreflight_ProbeFailureStableCode verifies that
// when the platform reports the ingress probe failed, the JSON output carries
// a stable error code with no NAT/firewall human text (§7.2 rule 12).
func TestE2E_Format_JSON_DoctorPreflight_ProbeFailureStableCode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	listenPort := reserveActivationPort(t)
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{
		IngressProbeErrorCode: "tcp_connect_failed",
	})

	out, err := runDoctor(t, []string{
		"--format", "json",
		"--preflight",
		"--site=global",
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--listen=127.0.0.1:%d", listenPort),
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err == nil {
		t.Fatal("expected error for probe failure")
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "doctor", false)

	errObj, _ := m["error"].(map[string]any)

	// error.code must be the stable top-level code
	if errObj["code"] != "ingress_unreachable" {
		t.Errorf("expected error.code=ingress_unreachable, got %v", errObj["code"])
	}
	// error.reason must carry the stable sub-reason from the platform
	if errObj["reason"] != "tcp_connect_failed" {
		t.Errorf("expected error.reason=tcp_connect_failed, got %v", errObj["reason"])
	}
	// neither field must be human-readable prose
	humanPhrases := []string{"NAT", "firewall", "security group", "public internet", "Check port"}
	for _, phrase := range humanPhrases {
		if strings.Contains(out, phrase) {
			t.Errorf("JSON must not contain human phrase %q in probe failure output:\n%s", phrase, out)
		}
	}
}

// ── warded serve --format json ────────────────────────────────────────────────

// TestE2E_Format_JSON_Serve_NoRuntime_ConfigNotFound verifies that serve with
// no local runtime outputs a single JSON object with ok=false and
// error.code=config_not_found (§0.1.6 startup failure path).
func TestE2E_Format_JSON_Serve_NoRuntime_ConfigNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, err := runServe(t, []string{"--format", "json", "--data-dir=" + dir})
	if err == nil {
		t.Fatal("expected error: serve with no ward runtime must fail")
	}

	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "serve", false)

	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "config_not_found" {
		t.Errorf("expected error.code=config_not_found when no ward is configured, got %v", errObj["code"])
	}
}

// TestE2E_Format_JSON_Serve_NoRuntime_StdoutIsSingleJSONObject verifies that
// even on startup failure, stdout is exactly one valid JSON object (§0.1.6).
func TestE2E_Format_JSON_Serve_NoRuntime_StdoutIsSingleJSONObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	out, _ := runServe(t, []string{"--format", "json", "--data-dir=" + dir})
	trimmed := strings.TrimSpace(out)
	// Must be a single JSON object — no trailing newline JSON lines, no mixed text.
	var m map[string]any
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		t.Fatalf("serve failure stdout must be a single valid JSON object\noutput: %q\nerr: %v", out, err)
	}
	if m["command"] != "serve" {
		t.Errorf("expected command=serve, got %v", m["command"])
	}
}

func TestE2E_Format_JSON_Serve_MultipleRuntimes_StdoutIsSingleJSONObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now().UTC()
	s1 := storage.NewJSONStore(dir)
	if err := s1.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		WardID:           "ward_multi_a",
		WardSecret:       "wrs_a",
		JWTSigningSecret: "jwt_a",
		Domain:           "alpha.warded.me",
		WardStatus:       domain.WardStatusActive,
		BillingMode:      domain.BillingModeMonthly,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("save ward a: %v", err)
	}
	s2 := storage.NewJSONStore(dir)
	if err := s2.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:             domain.SiteGlobal,
		WardID:           "ward_multi_b",
		WardSecret:       "wrs_b",
		JWTSigningSecret: "jwt_b",
		Domain:           "beta.warded.me",
		WardStatus:       domain.WardStatusActive,
		BillingMode:      domain.BillingModeMonthly,
		UpdatedAt:        now,
	}); err != nil {
		t.Fatalf("save ward b: %v", err)
	}

	out, err := runServe(t, []string{"--format", "json", "--data-dir=" + dir})
	if err == nil {
		t.Fatal("expected error for multiple local runtimes")
	}
	m := mustParseJSON(t, out)
	if m["ok"] != false || m["command"] != "serve" {
		t.Fatalf("expected serve error envelope, got: %v", m)
	}
	if strings.Contains(out, "Multiple local wards found") || strings.Contains(out, "Run `warded") {
		t.Fatalf("serve JSON output must not contain text runtime list, got: %s", out)
	}
	data, _ := m["data"].(map[string]any)
	runtimes, _ := data["runtimes"].([]any)
	if len(runtimes) != 2 {
		t.Fatalf("expected data.runtimes with 2 entries, got: %v", data)
	}
}

func TestE2E_Format_JSON_Serve_MissingJWTSecret_ConfigCorrupted(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	store := storage.NewJSONStore(dir)
	if err := store.SaveWardRuntime(context.Background(), domain.LocalWardRuntime{
		Site:        domain.SiteGlobal,
		WardID:      "ward_missing_jwt",
		WardSecret:  "wrs_secret",
		Domain:      "missing-jwt.warded.me",
		WardStatus:  domain.WardStatusActive,
		BillingMode: domain.BillingModeMonthly,
	}); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	out, err := runServe(t, []string{"--format", "json", "--data-dir=" + dir})
	if err == nil {
		t.Fatal("expected error for missing JWT signing secret")
	}
	m := mustParseJSON(t, out)
	assertEnvelopeShape(t, m, "serve", false)
	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "config_corrupted" {
		t.Errorf("expected error.code=config_corrupted, got %v", errObj["code"])
	}
}

// ── Finding 5: next_steps stronger assertions (preflight success path) ────────

// TestE2E_Format_JSON_NextSteps_CommandStepHasCommandAndArgs verifies that
// for a scenario with guaranteed next_steps (doctor --preflight success), every
// command-kind step has non-empty command and args fields, and args is an array
// of strings (not narrative sentences).
func TestE2E_Format_JSON_NextSteps_CommandStepHasCommandAndArgs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	listenPort := reserveActivationPort(t)
	upstreamPort := startMockUpstream(t)
	mock := newMockPlatform(t, mockPlatformOptions{})

	out, err := runDoctor(t, []string{
		"--format", "json",
		"--preflight",
		"--site=global",
		"--platform-origin=" + mock.URL,
		fmt.Sprintf("--listen=127.0.0.1:%d", listenPort),
		fmt.Sprintf("--upstream=127.0.0.1:%d", upstreamPort),
		"--data-dir=" + dir,
	})
	if err != nil {
		t.Fatalf("doctor --preflight: %v\noutput: %s", err, out)
	}

	m := mustParseJSON(t, out)
	steps, _ := m["next_steps"].([]any)
	if len(steps) == 0 {
		t.Fatal("doctor --preflight success must produce next_steps")
	}

	for i, s := range steps {
		step, _ := s.(map[string]any)
		if step == nil {
			t.Errorf("next_steps[%d] must be an object", i)
			continue
		}
		kind, _ := step["kind"].(string)
		if kind != "command" {
			continue // only validate command-kind steps
		}
		// command must be "warded" (the binary name)
		if step["command"] != "warded" {
			t.Errorf("next_steps[%d].command must be 'warded', got %v", i, step["command"])
		}
		// args must be an array
		args, ok := step["args"].([]any)
		if !ok || len(args) == 0 {
			t.Errorf("next_steps[%d].args must be a non-empty array, got %v", i, step["args"])
			continue
		}
		// args[0] must be a subcommand name (single word, no spaces)
		subCmd, _ := args[0].(string)
		if subCmd == "" || strings.Contains(subCmd, " ") {
			t.Errorf("next_steps[%d].args[0] must be a subcommand name (no spaces), got %q", i, subCmd)
		}
		// no arg should be a backtick-wrapped natural language sentence
		for j, arg := range args {
			argStr, _ := arg.(string)
			if strings.Contains(argStr, "`") {
				t.Errorf("next_steps[%d].args[%d] must not contain backticks (no narrative text), got %q", i, j, argStr)
			}
		}
	}
}
