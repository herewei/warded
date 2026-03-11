package cmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type serveTLSPlatformAPIStub struct {
	response  *ports.GetTLSMaterialResponse
	err       error
	callCount int
}

func (s *serveTLSPlatformAPIStub) CreateWardDraft(context.Context, ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
	panic("unexpected call")
}

func (s *serveTLSPlatformAPIStub) GetWardDraftStatus(context.Context, string, string, string) (*ports.GetWardDraftStatusResponse, error) {
	panic("unexpected call")
}

func (s *serveTLSPlatformAPIStub) ClaimWardDraft(context.Context, ports.ClaimWardDraftRequest, string) (*ports.ClaimWardDraftResponse, error) {
	panic("unexpected call")
}

func (s *serveTLSPlatformAPIStub) GetWard(context.Context, string, string, string) (*ports.GetWardResponse, error) {
	panic("unexpected call")
}

func (s *serveTLSPlatformAPIStub) GetTLSMaterial(context.Context, string, string, string) (*ports.GetTLSMaterialResponse, error) {
	s.callCount++
	if s.err != nil {
		return nil, s.err
	}
	return s.response, nil
}

func (s *serveTLSPlatformAPIStub) ExchangeAuthCode(context.Context, ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	panic("unexpected call")
}

func TestNewServeTLSProviderFetchesAndLoadsPlatformCertificateAtStartup(t *testing.T) {
	t.Parallel()

	certPEM, keyPEM := generateTestCertificate(t, "demo.warded.me")
	platformAPI := &serveTLSPlatformAPIStub{
		response: &ports.GetTLSMaterialResponse{
			TLSCert: certPEM,
			TLSKey:  keyPEM,
			Version: "v1",
		},
	}
	runtime := &domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		Domain:     "demo.warded.me",
		TLSMode:    domain.TLSModePlatformWildcard,
	}

	provider, err := newServeTLSProvider(context.Background(), runtime, t.TempDir(), platformAPI)
	if err != nil {
		t.Fatalf("newServeTLSProvider returned error: %v", err)
	}
	if platformAPI.callCount != 1 {
		t.Fatalf("expected exactly one startup fetch, got %d", platformAPI.callCount)
	}

	cfg := provider.TLSConfig()
	cert, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate returned error: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatalf("expected loaded certificate, got %#v", cert)
	}
}

func TestNewServeTLSProviderReturnsPlatformFetchError(t *testing.T) {
	t.Parallel()

	runtime := &domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		Domain:     "demo.warded.me",
		TLSMode:    domain.TLSModePlatformWildcard,
	}
	platformAPI := &serveTLSPlatformAPIStub{
		err: context.DeadlineExceeded,
	}

	provider, err := newServeTLSProvider(context.Background(), runtime, t.TempDir(), platformAPI)
	if err != nil {
		t.Fatalf("expected fallback provider, got error: %v", err)
	}
	cfg := provider.TLSConfig()
	cert, certErr := cfg.GetCertificate(nil)
	if certErr != nil {
		t.Fatalf("GetCertificate returned error: %v", certErr)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatalf("expected fallback certificate, got %#v", cert)
	}
}

func TestNewServeTLSProviderRejectsInvalidPlatformCertificate(t *testing.T) {
	t.Parallel()

	runtime := &domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		Domain:     "demo.warded.me",
		TLSMode:    domain.TLSModePlatformWildcard,
	}
	platformAPI := &serveTLSPlatformAPIStub{
		response: &ports.GetTLSMaterialResponse{
			TLSCert: "invalid-cert",
			TLSKey:  "invalid-key",
		},
	}

	provider, err := newServeTLSProvider(context.Background(), runtime, t.TempDir(), platformAPI)
	if err != nil {
		t.Fatalf("expected fallback provider, got error: %v", err)
	}
	cfg := provider.TLSConfig()
	cert, certErr := cfg.GetCertificate(nil)
	if certErr != nil {
		t.Fatalf("GetCertificate returned error: %v", certErr)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatalf("expected fallback certificate, got %#v", cert)
	}
}

func generateTestCertificate(t *testing.T, serverName string) (string, string) {
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

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	return string(certPEM), string(keyPEM)
}
