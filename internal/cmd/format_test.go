package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"testing"
)

// runCmd builds a fresh root command and executes it with the given args.
// The first arg is treated as the subcommand name; all remaining args are forwarded.
// Use prependArgs to inject global flags (like --format) before the subcommand.
func runCmd(t *testing.T, prependArgs []string, subcmd string, subcmdArgs []string) (string, error) {
	t.Helper()
	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, BuildInfo{Version: "test"})
	root.SilenceUsage = true
	root.SilenceErrors = true
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	var args []string
	args = append(args, prependArgs...)
	args = append(args, subcmd)
	args = append(args, subcmdArgs...)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

// parseJSONOutput parses the stdout of a command as a JSON object.
func parseJSONOutput(t *testing.T, output string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(output)), &m); err != nil {
		t.Fatalf("output is not valid JSON: %v\nraw output:\n%s", err, output)
	}
	return m
}

// ── §0.1.1 rule 1: default format is text ────────────────────────────────────

func TestFormat_DefaultIsText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, nil, "status", []string{"--local", "--data-dir=" + dir})
	// text output should not be valid top-level JSON
	var m map[string]any
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &m) == nil {
		t.Fatal("default format must be text (human readable), not JSON")
	}
}

// ── §0.1.1 rule 2-3: only text and json are valid; invalid must fail ─────────

func TestFormat_InvalidValueReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := runCmd(t, []string{"--format", "yaml"}, "status", []string{"--local", "--data-dir=" + dir})
	if err == nil {
		t.Fatal("expected error for --format yaml (unsupported value)")
	}
}

func TestFormat_InvalidValueXMLReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := runCmd(t, []string{"--format", "xml"}, "status", []string{"--local", "--data-dir=" + dir})
	if err == nil {
		t.Fatal("expected error for --format xml (unsupported value)")
	}
}

func TestFormat_InvalidValueEmptyStringReturnsError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := runCmd(t, []string{"--format", ""}, "status", []string{"--local", "--data-dir=" + dir})
	if err == nil {
		t.Fatal("expected error for --format '' (empty value)")
	}
}

// ── §0.1.1 rule 5: JSON mode stdout must be parseable JSON ───────────────────

func TestFormat_JSONProducesValidJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "status", []string{"--local", "--data-dir=" + dir})
	parseJSONOutput(t, out) // will fatalf if not valid JSON
}

// ── §0.1.2: envelope shape requirements ──────────────────────────────────────

func TestFormat_JSONAlwaysHasOKField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "status", []string{"--local", "--data-dir=" + dir})
	m := parseJSONOutput(t, out)
	if _, present := m["ok"]; !present {
		t.Fatal("JSON output must always contain 'ok' field")
	}
}

func TestFormat_JSONAlwaysHasCommandField_Status(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "status", []string{"--local", "--data-dir=" + dir})
	m := parseJSONOutput(t, out)
	if m["command"] != "status" {
		t.Fatalf("expected command=status, got %v", m["command"])
	}
}

func TestFormat_JSONAlwaysHasCommandField_Doctor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "doctor", []string{"--data-dir=" + dir})
	m := parseJSONOutput(t, out)
	if m["command"] != "doctor" {
		t.Fatalf("expected command=doctor, got %v", m["command"])
	}
}

func TestFormat_JSONAlwaysHasCommandField_New(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "new", []string{"--show", "--data-dir=" + dir})
	m := parseJSONOutput(t, out)
	if m["command"] != "new" {
		t.Fatalf("expected command=new, got %v", m["command"])
	}
}

// ── §0.1.1 rule 5: no non-JSON content in stdout ─────────────────────────────

func TestFormat_JSONStdoutIsSingleValidJSONObject(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "status", []string{"--local", "--data-dir=" + dir})
	trimmed := strings.TrimSpace(out)
	var m map[string]any
	if err := json.Unmarshal([]byte(trimmed), &m); err != nil {
		t.Fatalf("entire stdout must be a single valid JSON object\noutput: %q\nerr: %v", out, err)
	}
}

func TestFormat_JSONNoEmojiInOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "status", []string{"--local", "--data-dir=" + dir})
	emojiPatterns := []string{"✓", "✗", "⚠", "═", "Ward:"}
	for _, pattern := range emojiPatterns {
		if strings.Contains(out, pattern) {
			t.Errorf("JSON output must not contain human-text pattern %q\noutput: %s", pattern, out)
		}
	}
}

func TestFormat_JSONNoNextStepsNarrativeText(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "status", []string{"--local", "--data-dir=" + dir})
	humanPhrases := []string{"Run `warded", "Next:", "Open the setup link"}
	for _, phrase := range humanPhrases {
		if strings.Contains(out, phrase) {
			t.Errorf("JSON output must not contain human narrative %q\noutput: %s", phrase, out)
		}
	}
}

// ── §0.1.1 rule 6: business errors produce JSON error envelope + non-zero exit ─

func TestFormat_JSONBusinessErrorNonZeroExit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// renew-cert with no ward runtime must fail
	_, err := runCmd(t, []string{"--format", "json"}, "renew-cert", []string{"--data-dir=" + dir})
	if err == nil {
		t.Fatal("expected non-zero exit for business error in JSON mode")
	}
}

func TestFormat_JSONBusinessErrorIsValidJSONOnStdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "renew-cert", []string{"--data-dir=" + dir})
	m := parseJSONOutput(t, out) // must be valid JSON
	if m["ok"] != false {
		t.Fatalf("expected ok=false for business error, got %v", m["ok"])
	}
}

func TestFormat_JSONBusinessErrorHasErrorField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "renew-cert", []string{"--data-dir=" + dir})
	m := parseJSONOutput(t, out)
	errObj, ok := m["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected 'error' field as object in JSON output, got: %v", m["error"])
	}
	if code, _ := errObj["code"].(string); code == "" {
		t.Fatalf("expected non-empty error.code, got: %v", errObj)
	}
}

func TestFormat_JSONBusinessErrorNoHumanTranslation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "renew-cert", []string{"--data-dir=" + dir})
	// JSON error should not contain human-translated phrases from explainNewErrorAddr patterns
	humanPhrases := []string{
		"Fix directory permissions",
		"Stop the conflicting process",
		"requires elevated privileges",
		"Run warded with permission",
	}
	for _, phrase := range humanPhrases {
		if strings.Contains(out, phrase) {
			t.Errorf("JSON error must not contain human-translated phrase %q\noutput: %s", phrase, out)
		}
	}
}

func TestFormat_JSONBusinessErrorSuppressesCobraErrorOnStderr(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	listenPort := reserveUnusedTCPPort(t)
	upstreamPort := reserveUnusedTCPPort(t)

	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, BuildInfo{Version: "test"})
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)
	root.SetOut(stdoutBuf)
	root.SetErr(stderrBuf)
	root.SetArgs([]string{
		"--format", "json",
		"doctor",
		"--preflight",
		"--site", "global",
		"--listen", "127.0.0.1",
		"--port", fmt.Sprintf("%d", listenPort),
		"--upstream", fmt.Sprintf("127.0.0.1:%d", upstreamPort),
		"--data-dir", dir,
	})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected upstream error")
	}
	parseJSONOutput(t, stdoutBuf.String())
	if strings.Contains(stderrBuf.String(), "Error:") {
		t.Fatalf("JSON mode must suppress cobra Error line on stderr, got: %q", stderrBuf.String())
	}
}

func TestFormat_JSONFlagParseErrorIsEnvelope(t *testing.T) {
	t.Parallel()
	out, err := runCmd(t, []string{"--format", "json"}, "status", []string{"--not-a-real-flag"})
	if err == nil {
		t.Fatal("expected flag parse error")
	}
	m := parseJSONOutput(t, out)
	if m["ok"] != false || m["command"] != "status" {
		t.Fatalf("expected status error envelope, got: %v", m)
	}
	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "invalid_argument" {
		t.Fatalf("expected error.code=invalid_argument, got: %v", errObj)
	}
}

func TestFormat_JSONArgsValidationErrorIsEnvelope(t *testing.T) {
	t.Parallel()
	out, err := runCmd(t, []string{"--format", "json"}, "status", []string{"one", "two"})
	if err == nil {
		t.Fatal("expected args validation error")
	}
	m := parseJSONOutput(t, out)
	if m["ok"] != false || m["command"] != "status" {
		t.Fatalf("expected status error envelope, got: %v", m)
	}
	errObj, _ := m["error"].(map[string]any)
	if errObj["code"] != "invalid_argument" {
		t.Fatalf("expected error.code=invalid_argument, got: %v", errObj)
	}
}

func reserveUnusedTCPPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve tcp port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// ── §0.1.3: sensitive fields must not appear in JSON output ──────────────────

func TestFormat_JSONNoSensitiveFieldsInOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "status", []string{"--local", "--data-dir=" + dir})
	sensitiveFieldNames := []string{
		`"ward_secret"`,
		`"draft_secret"`,
		`"ward_draft_secret"`,
		`"tls_key"`,
		`"private_key"`,
		`"jwt_signing_secret"`,
		`"authorization"`,
	}
	for _, field := range sensitiveFieldNames {
		if strings.Contains(out, field) {
			t.Errorf("JSON output must not expose sensitive field %s\noutput: %s", field, out)
		}
	}
}

// ── §0.1.1 rule 3: --verbose and --format are independent ────────────────────

func TestFormat_VerboseAndJSONAreIndependent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// --verbose --format json should still produce valid JSON on stdout
	// (verbose logs go to stderr, not mixed into stdout)
	logLevel := new(slog.LevelVar)
	root := NewRootCommand(logLevel, BuildInfo{Version: "test"})
	root.SilenceUsage = true
	root.SilenceErrors = true
	stdoutBuf := new(bytes.Buffer)
	stderrBuf := new(bytes.Buffer)
	root.SetOut(stdoutBuf)
	root.SetErr(stderrBuf)
	root.SetArgs([]string{"--format", "json", "--verbose", "status", "--local", "--data-dir=" + dir})
	_ = root.Execute()

	out := stdoutBuf.String()
	// stdout must be valid JSON
	parseJSONOutput(t, out)
	// verbose content should not be in the JSON stdout
	// (it goes to stderr which we've redirected to stderrBuf)
	var m map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err != nil {
		t.Fatalf("stdout with --verbose --format json must still be valid JSON: %v\noutput: %q", err, out)
	}
}

// ── §0.1.4: format=text still uses human translation (regression) ─────────────

func TestFormat_TextModePreservesHumanOutput(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "text"}, "status", []string{"--local", "--data-dir=" + dir})
	// text mode should contain human-readable content (Ward header)
	if !strings.Contains(out, "Ward:") && !strings.Contains(out, "No pending setup") {
		t.Errorf("--format text must produce human-readable output, got:\n%s", out)
	}
}

// ── error.code=config_not_found for commands requiring a configured ward ──────

func TestFormat_JSONRenewCertNoRuntimeReturnsConfigNotFound(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out, _ := runCmd(t, []string{"--format", "json"}, "renew-cert", []string{"--data-dir=" + dir})
	m := parseJSONOutput(t, out)
	errObj, _ := m["error"].(map[string]any)
	if errObj == nil {
		t.Fatalf("expected error object, got: %v", m)
	}
	code, _ := errObj["code"].(string)
	if code != "config_not_found" {
		t.Errorf("expected error.code=config_not_found when no ward configured, got %q", code)
	}
}
