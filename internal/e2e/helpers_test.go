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

// makeConfigDirReadOnly makes dir read-only and returns a restore function.
func makeConfigDirReadOnly(t *testing.T, dir string) func() {
	t.Helper()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat config dir: %v", err)
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

// runActivate builds a fresh root command and executes "activate" with the given args.
// Returns combined stdout+stderr output and any error from Execute.
func runActivate(t *testing.T, args []string) (string, error) {
	t.Helper()
	logLevel := new(slog.LevelVar)
	root := cmd.NewRootCommand(logLevel, "test")
	root.SilenceUsage = true  // suppress usage on error
	root.SilenceErrors = true // suppress "Error: ..." print; error is returned
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs(append([]string{"activate"}, args...))
	err := root.Execute()
	return buf.String(), err
}

// mockPlatformOptions configures the local mock platform server.
type mockPlatformOptions struct {
	// IngressProbeStatus is returned in the lock draft response.
	// Defaults to "reachable".
	IngressProbeStatus string
	// AutoConvertAfterPolls converts the draft after the given number of GET polls.
	AutoConvertAfterPolls int
}

// mockPlatform is a minimal httptest.Server implementing the platform API
// contract (POST /api/v1/ward-drafts). It is defined entirely within the cli
// module — no platform module imports required.
type mockPlatform struct {
	*httptest.Server

	mu       sync.Mutex
	LastUA   string // last User-Agent header received
	LastSite string // last X-Warded-Site header received
	Calls    int    // total POST /api/v1/ward-drafts calls

	opts             mockPlatformOptions
	draftByChallenge map[string]string // challenge → draftID (for idempotency)
	draftStatus      map[string]string // draftID → status
	draftPolls       map[string]int    // draftID → GET count
	draftSecret      map[string]string // draftID → plaintext secret for claim
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
		DomainType           string `json:"domain_type"`
		DraftSecretChallenge string `json:"draft_secret_challenge"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	site := r.Header.Get("X-Warded-Site")
	if site == "" {
		site = req.Site
	}

	// Idempotency: reuse draft ID when the caller presents the same challenge.
	m.mu.Lock()
	draftID, seen := m.draftByChallenge[req.DraftSecretChallenge]
	if !seen || req.DraftSecretChallenge == "" {
		draftID = fmt.Sprintf("draft_%d", time.Now().UnixNano())
	}
	m.draftByChallenge[req.DraftSecretChallenge] = draftID
	if _, ok := m.draftStatus[draftID]; !ok {
		m.draftStatus[draftID] = "pending_activation"
	}
	m.mu.Unlock()

	baseDomain := "warded.me"
	if site == "cn" {
		baseDomain = "warded.cn"
	}
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
	m.mu.Unlock()

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
		"upstream_port":      18789,
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
