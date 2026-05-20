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
