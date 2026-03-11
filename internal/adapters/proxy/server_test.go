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

type rejectingPlatformAPI struct{}

func (rejectingPlatformAPI) CreateWardDraft(_ context.Context, _ ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
	panic("unexpected call")
}
func (rejectingPlatformAPI) GetWardDraftStatus(_ context.Context, _ string, _ string, _ string) (*ports.GetWardDraftStatusResponse, error) {
	panic("unexpected call")
}
func (rejectingPlatformAPI) ClaimWardDraft(_ context.Context, _ ports.ClaimWardDraftRequest, _ string) (*ports.ClaimWardDraftResponse, error) {
	panic("unexpected call")
}
func (rejectingPlatformAPI) GetWard(_ context.Context, _ string, _ string, _ string) (*ports.GetWardResponse, error) {
	panic("unexpected call")
}
func (rejectingPlatformAPI) GetTLSMaterial(_ context.Context, _ string, _ string, _ string) (*ports.GetTLSMaterialResponse, error) {
	panic("unexpected call")
}
func (rejectingPlatformAPI) ExchangeAuthCode(_ context.Context, _ ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	panic("unexpected call")
}

func TestHandleCallbackRejectsMismatchedTransactionWardID(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{
		WardID:      "ward_current",
		Site:        domain.SiteGlobal,
		PlatformAPI: rejectingPlatformAPI{},
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

func TestListenAndServeRequiresTLSConfig(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{})
	err := server.ListenAndServe(context.Background(), "127.0.0.1:0")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "proxy: tls config is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestListenAndServeAcceptsTLSConfig(t *testing.T) {
	t.Parallel()

	server := NewServer(ServerConfig{
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{mustMakeTestTLSCertificate(t, "demo.warded.me")},
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := server.ListenAndServe(ctx, "127.0.0.1:0"); err != nil {
		t.Fatalf("ListenAndServe returned error: %v", err)
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
