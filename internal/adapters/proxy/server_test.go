package proxy

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

func TestServeLoginPageHonorsExplicitForwardedProtoHTTP(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{
		WardID:     "ward_test",
		Site:       domain.SiteGlobal,
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	})

	req := httptest.NewRequest(http.MethodGet, "http://demo.warded.me/", nil)
	req.Host = "demo.warded.me"
	req.Header.Set("X-Forwarded-Proto", "http")
	rec := httptest.NewRecorder()

	server.serveLoginPage(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "redirect_uri=http%3A%2F%2Fdemo.warded.me%2F_ward%2Fcallback") {
		t.Fatalf("expected http redirect_uri, got body: %s", body)
	}
}

func TestReverseProxyRewritesHostHeader(t *testing.T) {
	t.Parallel()

	var receivedHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	server := NewServer(ServerConfig{
		UpstreamAddr:  strings.TrimPrefix(upstream.URL, "http://"),
		WardStatus:    domain.WardStatusActive,
		AuthWhitelist: []domain.AuthWhitelistRule{{Type: "exact", Path: "/webhook"}},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/webhook", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	expectedHost := strings.TrimPrefix(upstream.URL, "http://")
	if receivedHost != expectedHost {
		t.Fatalf("expected Host %q, got %q", expectedHost, receivedHost)
	}
}

func TestReverseProxyAcceptsUpstreamURLWithScheme(t *testing.T) {
	t.Parallel()

	var receivedHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	server := NewServer(ServerConfig{
		UpstreamAddr:  upstream.URL,
		WardStatus:    domain.WardStatusActive,
		AuthWhitelist: []domain.AuthWhitelistRule{{Type: "exact", Path: "/webhook"}},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/webhook", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	expectedHost := strings.TrimPrefix(upstream.URL, "http://")
	if receivedHost != expectedHost {
		t.Fatalf("expected Host %q, got %q", expectedHost, receivedHost)
	}
}

func TestReverseProxyUsesSetHostWhenConfigured(t *testing.T) {
	t.Parallel()

	var receivedHost string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHost = r.Host
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	server := NewServer(ServerConfig{
		UpstreamAddr:  strings.TrimPrefix(upstream.URL, "http://"),
		SetHost:       "custom.host.example",
		WardStatus:    domain.WardStatusActive,
		AuthWhitelist: []domain.AuthWhitelistRule{{Type: "exact", Path: "/webhook"}},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/webhook", nil)
	req.Host = "example.com"
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	if receivedHost != "custom.host.example" {
		t.Fatalf("expected Host %q, got %q", "custom.host.example", receivedHost)
	}
}

func TestCleanupExpiredStateRemovesExpiredEntries(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{})
	now := time.Now().UTC()

	server.transactions["expired"] = loginTransaction{CreatedAt: now.Add(-11 * time.Minute)}
	server.transactions["active"] = loginTransaction{CreatedAt: now.Add(-2 * time.Minute)}
	server.revokedSessions["expired"] = revokedSession{ExpiresAt: now.Add(-time.Minute)}
	server.revokedSessions["active"] = revokedSession{ExpiresAt: now.Add(time.Minute)}

	server.cleanupExpiredState()

	if _, ok := server.transactions["expired"]; ok {
		t.Fatal("expected expired transaction to be deleted")
	}
	if _, ok := server.transactions["active"]; !ok {
		t.Fatal("expected active transaction to remain")
	}
	if _, ok := server.revokedSessions["expired"]; ok {
		t.Fatal("expected expired revoked session to be deleted")
	}
	if _, ok := server.revokedSessions["active"]; !ok {
		t.Fatal("expected active revoked session to remain")
	}
}

type acceptingAgentVerifier struct{}

func (acceptingAgentVerifier) Verify(string) (*ports.AgentBearerClaims, error) {
	return &ports.AgentBearerClaims{
		PrincipalID: "principal_agent",
		WardID:      "ward_agent",
		Aud:         "ward:ward_agent",
		JTI:         "agtok_123",
	}, nil
}

func TestHandleDefaultAcceptsAgentBearerToken(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("expected Authorization to be stripped, got %q", got)
		}
		if got := r.Header.Get("X-Warded-Principal-Id"); got != "principal_agent" {
			t.Errorf("unexpected principal header: %q", got)
		}
		if got := r.Header.Get("X-Warded-Auth-Type"); got != "ward_access_token" {
			t.Errorf("unexpected auth type: %q", got)
		}
		if got := r.Header.Get("X-Warded-Token-Jti"); got != "agtok_123" {
			t.Errorf("unexpected token jti: %q", got)
		}
		if got := r.Header.Get("X-Auth-Request-User"); got != "" {
			t.Errorf("expected spoofed auth request user to be stripped, got %q", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	server := NewServer(ServerConfig{
		WardID:        "ward_agent",
		Site:          domain.SiteGlobal,
		WardStatus:    domain.WardStatusActive,
		UpstreamAddr:  strings.TrimPrefix(upstream.URL, "http://"),
		AgentVerifier: acceptingAgentVerifier{},
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token_123")
	req.Header.Set("X-Auth-Request-User", "spoofed")
	req.Header.Set("X-Warded-Principal-Id", "spoofed")
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestCookieAuthStripsSpoofedIdentityHeaders(t *testing.T) {
	t.Parallel()

	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	verifier := acceptingJWTVerifier{
		claims: &ports.WardedClaims{
			PrincipalID: "principal_cookie",
			WardID:      "ward_test",
			Aud:         "ward:ward_test",
			SessionID:   "sess_abc",
		},
	}

	server := NewServer(ServerConfig{
		WardID:       "ward_test",
		Site:         domain.SiteGlobal,
		WardStatus:   domain.WardStatusActive,
		UpstreamAddr: strings.TrimPrefix(upstream.URL, "http://"),
		JWTVerifier:  verifier,
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/protected", nil)
	req.AddCookie(&http.Cookie{Name: "warded_session", Value: "valid_token"})
	req.Header.Set("X-Auth-Request-User", "spoofed_user")
	req.Header.Set("Remote-User", "spoofed_remote")
	req.Header.Set("X-Warded-Principal-Id", "spoofed_principal")
	req.Header.Set("X-Warded-Ward-Id", "spoofed_ward")
	req.Header.Set("X-Warded-Foo", "spoofed_foo")

	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}

	if got := receivedHeaders.Get("X-Forwarded-User"); got != "principal_cookie" {
		t.Errorf("expected X-Forwarded-User = principal_cookie, got %q", got)
	}
	if got := receivedHeaders.Get("X-Warded-Principal-Id"); got != "principal_cookie" {
		t.Errorf("expected X-Warded-Principal-Id = principal_cookie, got %q", got)
	}
	if got := receivedHeaders.Get("X-Warded-Ward-Id"); got != "ward_test" {
		t.Errorf("expected X-Warded-Ward-Id = ward_test, got %q", got)
	}
	if got := receivedHeaders.Get("X-Auth-Request-User"); got != "" {
		t.Errorf("expected spoofed X-Auth-Request-User stripped, got %q", got)
	}
	if got := receivedHeaders.Get("Remote-User"); got != "" {
		t.Errorf("expected spoofed Remote-User stripped, got %q", got)
	}
	if got := receivedHeaders.Get("X-Warded-Foo"); got != "" {
		t.Errorf("expected spoofed X-Warded-Foo stripped, got %q", got)
	}
}

type acceptingJWTVerifier struct {
	claims *ports.WardedClaims
}

func (v acceptingJWTVerifier) Verify(string) (*ports.WardedClaims, error) {
	return v.claims, nil
}

func TestHandleDefaultRejectsAgentBearerWithoutFallbackToLogin(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{
		WardID:     "ward_agent",
		Site:       domain.SiteGlobal,
		WardStatus: domain.WardStatusActive,
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer token_123")
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unexpected status: %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `"access_denied"`) {
		t.Fatalf("expected JSON access denied, got %s", rec.Body.String())
	}
}

type rejectingAuthExchangeAPI struct{}

func (rejectingAuthExchangeAPI) ExchangeAuthCode(_ context.Context, _ ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	panic("unexpected call")
}

func TestHandleCallbackRejectsMismatchedTransactionWardID(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{
		WardID:       "ward_current",
		Site:         domain.SiteGlobal,
		AuthExchange: rejectingAuthExchangeAPI{},
	})
	server.transactions["state_123"] = loginTransaction{
		WardID:    "ward_other",
		ReturnTo:  "/",
		CreatedAt: time.Now().UTC(),
	}

	req := httptest.NewRequest(http.MethodGet, "/_ward/callback?code=code_123&state=state_123", nil)
	rec := httptest.NewRecorder()

	server.handleCallback(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "invalid ward context") {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}
}

func TestServeRequiresTLSConfig(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{})
	err := server.Serve(context.Background(), "127.0.0.1:0")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "proxy: tls config is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestServeAcceptsTLSConfig(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{mustMakeTestTLSCertificate(t, "demo.warded.me")},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := server.Serve(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("Serve returned error: %v", err)
	}
}

func mustMakeTestTLSCertificate(t *testing.T, serverName string) tls.Certificate {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: serverName,
		},
		NotBefore:             time.Now().UTC().Add(-time.Hour),
		NotAfter:              time.Now().UTC().Add(24 * time.Hour),
		DNSNames:              []string{serverName},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}

	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("X509KeyPair: %v", err)
	}
	return cert
}

func TestWhitelistedExactPathBypassesAuth(t *testing.T) {
	t.Parallel()

	var receivedHeaders http.Header
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	server := NewServer(ServerConfig{
		WardID:       "ward_test",
		Site:         domain.SiteGlobal,
		WardStatus:   domain.WardStatusActive,
		UpstreamAddr: strings.TrimPrefix(upstream.URL, "http://"),
		AuthWhitelist: []domain.AuthWhitelistRule{
			{Type: "exact", Path: "/webhook/github"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/webhook/github", nil)
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	if receivedHeaders.Get("X-Forwarded-User") != "" {
		t.Fatalf("expected no identity headers on whitelisted path, got X-Forwarded-User")
	}
}

func TestWhitelistedPrefixPathBypassesAuth(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()

	server := NewServer(ServerConfig{
		WardID:       "ward_test",
		Site:         domain.SiteGlobal,
		WardStatus:   domain.WardStatusActive,
		UpstreamAddr: strings.TrimPrefix(upstream.URL, "http://"),
		AuthWhitelist: []domain.AuthWhitelistRule{
			{Type: "prefix", Path: "/callbacks/"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/callbacks/stripe/ok", nil)
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
}

func TestNonWhitelistedPathStillRequiresAuth(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{
		WardID:     "ward_test",
		Site:       domain.SiteGlobal,
		WardStatus: domain.WardStatusActive,
		AuthWhitelist: []domain.AuthWhitelistRule{
			{Type: "exact", Path: "/webhook/github"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/other", nil)
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for non-whitelisted path, got: %d", rec.Code)
	}
}

func TestExactWhitelistDoesNotMatchPrefix(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{
		WardID:     "ward_test",
		Site:       domain.SiteGlobal,
		WardStatus: domain.WardStatusActive,
		AuthWhitelist: []domain.AuthWhitelistRule{
			{Type: "exact", Path: "/callback"},
		},
	})

	req := httptest.NewRequest(http.MethodGet, "https://example.com/callback-admin", nil)
	rec := httptest.NewRecorder()
	server.handleDefault(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got: %d", rec.Code)
	}
}
