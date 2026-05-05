package e2e_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/herewei/warded/internal/cmd"
)

// startMockUpstream starts a TCP listener to simulate an upstream service.
func startMockUpstream(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("failed to start mock upstream: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln.Addr().(*net.TCPAddr).Port
}

// makeDataDirReadOnly makes dir read-only and returns a restore function.
func makeDataDirReadOnly(t *testing.T, dir string) func() {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat data dir: %v", err)
	}
	origMode := info.Mode()
	if err := os.Chmod(dir, 0555); err != nil {
		t.Fatalf("make dir read-only: %v", err)
	}
	return func() { _ = os.Chmod(dir, origMode) }
}

// livePlatformURL returns the platform URL from the -platform-url flag or skips the test.
//
// Example:
//
//	go test ./internal/e2e/ -v -count=1 -platform-url=https://dev.warded.me
var livePlatformURLFlag = flag.String("platform-url", "", "live platform URL for e2e tests")

func livePlatformURL(t *testing.T) string {
	t.Helper()
	if *livePlatformURLFlag == "" {
		t.Skip("set -platform-url flag to run live e2e tests")
	}
	return *livePlatformURLFlag
}

// runNewCommit builds a fresh root command and executes "new --commit" with the given args.
// Returns combined stdout+stderr output and any error from Execute.
func runNewCommit(t *testing.T, args []string) (string, error) {
	t.Helper()
	hasPort := false
	hasCommit := false
	for _, arg := range args {
		if strings.HasPrefix(arg, "--listen=0.0.0.0:") {
			hasPort = true
		}
		if arg == "--commit" {
			hasCommit = true
		}
	}
	if !hasPort {
		args = append(args, fmt.Sprintf("--listen=0.0.0.0:%d", reserveActivationPort(t)))
	}
	if !hasCommit {
		args = append(args, "--commit")
	}
	logLevel := new(slog.LevelVar)
	root := cmd.NewRootCommand(logLevel, cmd.BuildInfo{Version: "test"})
	root.SilenceUsage = true  // suppress usage on error
	root.SilenceErrors = true // suppress "Error: ..." print; error is returned
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"new"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func runNewRaw(t *testing.T, args []string) (string, error) {
	t.Helper()
	logLevel := new(slog.LevelVar)
	root := cmd.NewRootCommand(logLevel, cmd.BuildInfo{Version: "test"})
	root.SilenceUsage = true
	root.SilenceErrors = true
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"new"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func runStatus(t *testing.T, args []string) (string, error) {
	t.Helper()
	logLevel := new(slog.LevelVar)
	root := cmd.NewRootCommand(logLevel, cmd.BuildInfo{Version: "test"})
	root.SilenceUsage = true
	root.SilenceErrors = true
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"status"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func runIntegrate(t *testing.T, args []string) (string, error) {
	t.Helper()
	logLevel := new(slog.LevelVar)
	root := cmd.NewRootCommand(logLevel, cmd.BuildInfo{Version: "test"})
	root.SilenceUsage = true
	root.SilenceErrors = true
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"integrate"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func runDoctor(t *testing.T, args []string) (string, error) {
	t.Helper()
	logLevel := new(slog.LevelVar)
	root := cmd.NewRootCommand(logLevel, cmd.BuildInfo{Version: "test"})
	root.SilenceUsage = true
	root.SilenceErrors = true
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"doctor"}, args...))
	err := root.Execute()
	return buf.String(), err
}

func reserveActivationPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve activation port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// mockPlatformOptions configures the local mock platform server.
type mockPlatformOptions struct {
	// IngressProbeStatus is returned in the lock draft response.
	// Defaults to "reachable".
	IngressProbeStatus string
	// AutoConvertAfterPolls converts the draft after the given number of GET polls.
	AutoConvertAfterPolls int
	// CreateErrorStatus forces POST /api/v1/ward-drafts to return this HTTP status
	// with an error body. 0 means no forced error.
	CreateErrorStatus int
	// CreateErrorCode is the error code returned when CreateErrorStatus > 0.
	// Defaults to "internal_error" when CreateErrorStatus is set but Code is empty.
	CreateErrorCode string
	// CreateErrorMessage is the human-readable message in the error body.
	CreateErrorMessage string
	// GetDraftStatusError forces GET /api/v1/ward-drafts/{id}/status to return
	// this error code with the given HTTP status.
	GetDraftStatusError     string
	GetDraftStatusHTTPError int
	// RateLimited causes POST to return 429 when true.
	RateLimited bool
}

// mockPlatform is a minimal httptest.Server implementing the platform API
// contract (POST /api/v1/ward-drafts). It is defined entirely within the cli
// module — no platform module imports required.
type mockPlatform struct {
	*httptest.Server

	mu                        sync.Mutex
	LastUA                    string // last User-Agent header received
	LastSite                  string // last X-Warded-Site header received
	LastCreateRequestedDomain string
	LastCreateSpec            string
	Calls                     int // total POST /api/v1/ward-drafts calls

	opts             mockPlatformOptions
	draftByChallenge map[string]string // challenge → draftID (for idempotency)
	draftStatus      map[string]string // draftID → status
	draftPolls       map[string]int    // draftID → GET count
	draftSecret      map[string]string // draftID → plaintext secret for claim
	draftRequested   map[string]string // draftID → requested domain
}

func newMockPlatform(t *testing.T, opts mockPlatformOptions) *mockPlatform {
	t.Helper()
	if opts.IngressProbeStatus == "" {
		opts.IngressProbeStatus = "reachable"
	}
	m := &mockPlatform{
		opts:             opts,
		draftByChallenge: make(map[string]string),
		draftStatus:      make(map[string]string),
		draftPolls:       make(map[string]int),
		draftSecret:      make(map[string]string),
		draftRequested:   make(map[string]string),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/v1/ward-drafts", m.handleCreateWardDraft)
	mux.HandleFunc("/api/v1/ward-drafts/", m.handleWardDraftRoutes)
	mux.HandleFunc("/api/v1/wards/", m.handleGetWard)
	m.Server = httptest.NewServer(mux)
	t.Cleanup(m.Server.Close)
	return m
}

func (m *mockPlatform) handleCreateWardDraft(w http.ResponseWriter, r *http.Request) {
	m.mu.Lock()
	m.LastUA = r.Header.Get("User-Agent")
	m.LastSite = r.Header.Get("X-Warded-Site")
	m.Calls++
	m.mu.Unlock()

	var req struct {
		Site                 string `json:"site"`
		Spec                 string `json:"spec"`
		DomainType           string `json:"domain_type"`
		RequestedDomain      string `json:"requested_domain"`
		DraftSecretChallenge string `json:"draft_secret_challenge"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	m.mu.Lock()
	m.LastCreateRequestedDomain = req.RequestedDomain
	m.LastCreateSpec = req.Spec
	m.mu.Unlock()

	site := r.Header.Get("X-Warded-Site")
	if site == "" {
		site = req.Site
	}

	// Per contract: when ingress_probe_status=unreachable, platform must return
	// HTTP 422 with error "ingress_unreachable" and must not create a draft.
	if m.opts.IngressProbeStatus == "unreachable" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": "ingress_unreachable",
		})
		return
	}

	// Forced error injection: return a custom error response.
	if m.opts.RateLimited {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   "rate_limited",
			"message": "too many requests",
		})
		return
	}
	if m.opts.CreateErrorStatus > 0 {
		errCode := m.opts.CreateErrorCode
		if errCode == "" {
			errCode = "internal_error"
		}
		errMsg := m.opts.CreateErrorMessage
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(m.opts.CreateErrorStatus)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":   errCode,
			"message": errMsg,
		})
		return
	}

	// Idempotency: reuse draft ID when the caller presents the same challenge,
	// unless that draft has already expired or failed.
	m.mu.Lock()
	draftID, seen := m.draftByChallenge[req.DraftSecretChallenge]
	if seen {
		status := m.draftStatus[draftID]
		if status == "expired" || status == "failed" {
			seen = false
		}
	}
	if !seen || req.DraftSecretChallenge == "" {
		draftID = fmt.Sprintf("draft_%d", time.Now().UnixNano())
	}
	m.draftByChallenge[req.DraftSecretChallenge] = draftID
	if _, ok := m.draftStatus[draftID]; !ok {
		m.draftStatus[draftID] = "pending_activation"
	}
	baseDomain := "warded.me"
	if site == "cn" {
		baseDomain = "warded.cn"
	}
	requestedDomain := m.draftRequested[draftID]
	switch {
	case req.DomainType == "custom_domain" && req.RequestedDomain != "":
		requestedDomain = req.RequestedDomain
	case req.RequestedDomain != "":
		requestedDomain = req.RequestedDomain
	case requestedDomain == "" && req.Spec == "starter":
		requestedDomain = fmt.Sprintf("k8m4xq9p.%s", baseDomain)
	case requestedDomain == "" && req.DomainType == "custom_domain":
		requestedDomain = "example.com"
	case requestedDomain == "":
		requestedDomain = fmt.Sprintf("k8m4xq9p.%s", baseDomain)
	}
	m.draftRequested[draftID] = requestedDomain
	m.mu.Unlock()

	domainCheck := "not_required"
	if req.DomainType == "custom_domain" {
		domainCheck = "available"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ward_draft_id":        draftID,
		"site":                 site,
		"status":               "pending_activation",
		"expires_at":           time.Now().Add(15 * time.Minute).Format(time.RFC3339),
		"activation_url":       fmt.Sprintf("https://%s/activate/%s", baseDomain, draftID),
		"requested_domain":     requestedDomain,
		"domain_check_status":  domainCheck,
		"resolved_public_ip":   "1.2.3.4",
		"ingress_probe_status": m.opts.IngressProbeStatus,
	})
}

func (m *mockPlatform) handleWardDraftRoutes(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/status"):
		m.handleGetWardDraftStatus(w, r)
	case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/claim"):
		m.handleClaimWardDraft(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (m *mockPlatform) handleGetWardDraftStatus(w http.ResponseWriter, r *http.Request) {
	draftID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/ward-drafts/"), "/status")
	challenge := r.Header.Get("X-Warded-Draft-Challenge")

	m.mu.Lock()
	expectedDraftID, seen := m.draftByChallenge[challenge]
	status := m.draftStatus[draftID]
	m.draftPolls[draftID]++
	if m.opts.AutoConvertAfterPolls > 0 && m.draftPolls[draftID] >= m.opts.AutoConvertAfterPolls {
		status = "converted_pending_claim"
		m.draftStatus[draftID] = status
	}
	getStatusErr := m.opts.GetDraftStatusError
	getStatusErrHTTP := m.opts.GetDraftStatusHTTPError
	m.mu.Unlock()

	// Forced error injection for GET status (e.g. access_denied, activation_link_expired).
	if getStatusErr != "" {
		httpCode := getStatusErrHTTP
		if httpCode == 0 {
			httpCode = http.StatusForbidden
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(httpCode)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": getStatusErr,
		})
		return
	}

	if !seen || expectedDraftID != draftID {
		http.Error(w, `{"error":"access_denied"}`, http.StatusForbidden)
		return
	}
	if status == "" {
		status = "pending_activation"
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ward_draft_id": draftID,
		"status":        status,
		"expires_at":    time.Now().Add(15 * time.Minute).Format(time.RFC3339),
	})
}

func (m *mockPlatform) handleClaimWardDraft(w http.ResponseWriter, r *http.Request) {
	draftID := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v1/ward-drafts/"), "/claim")
	var req struct {
		DraftSecret string `json:"draft_secret"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	challengeBytes := sha256.Sum256([]byte(req.DraftSecret))
	challenge := hex.EncodeToString(challengeBytes[:])

	m.mu.Lock()
	expectedDraftID, seen := m.draftByChallenge[challenge]
	status := m.draftStatus[draftID]
	m.draftSecret[draftID] = req.DraftSecret
	if status == "converted_pending_claim" {
		m.draftStatus[draftID] = "claimed"
		status = "claimed"
	}
	m.mu.Unlock()

	if !seen || expectedDraftID != draftID {
		http.Error(w, `{"error":"access_denied"}`, http.StatusForbidden)
		return
	}
	if status != "converted_pending_claim" && status != "claimed" {
		http.Error(w, `{"error":"invalid_state"}`, http.StatusConflict)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ward_id":         "ward_" + draftID,
		"ward_secret":     "wrd_" + draftID,
		"site":            "global",
		"status":          "active",
		"domain":          "demo.warded.me",
		"billing_mode":    "monthly",
		"activation_mode": "trial",
		"activated_at":    time.Now().UTC().Format(time.RFC3339),
		"expires_at":      time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339),
	})
}

func (m *mockPlatform) handleGetWard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}

	wardID := strings.TrimPrefix(r.URL.Path, "/api/v1/wards/")
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ward_id":            wardID,
		"owner_principal_id": "principal_mock",
		"site":               "global",
		"spec":               "starter",
		"billing_mode":       "monthly",
		"activation_mode":    "trial",
		"domain_type":        "platform_subdomain",
		"domain":             "demo.warded.me",
		"upstream_addr":      "127.0.0.1:18789",
		"upstream_port":      18789,
		"listen_addr":        "0.0.0.0:443",
		"listen_port":        443,
		"status":             "active",
		"activated_at":       time.Now().UTC().Format(time.RFC3339),
		"expires_at":         time.Now().UTC().Add(72 * time.Hour).Format(time.RFC3339),
	})
}

func (m *mockPlatform) setDraftStatus(draftID string, status string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.draftStatus[draftID] = status
}
