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
	"strings"
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/storage"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type serveTLSPlatformAPIStub struct {
	response          *ports.GetTLSMaterialResponse
	err               error
	callCount         int
	heartbeatResponse *ports.HeartbeatResponse
	heartbeatErr      error
	heartbeatCount    int
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

func (s *serveTLSPlatformAPIStub) Heartbeat(context.Context, string, string, ports.HeartbeatRequest) (*ports.HeartbeatResponse, error) {
	s.heartbeatCount++
	if s.heartbeatErr != nil {
		return nil, s.heartbeatErr
	}
	if s.heartbeatResponse != nil {
		return s.heartbeatResponse, nil
	}
	panic("unexpected call")
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

func TestRunServeHeartbeatPersistsSuspendedAndStops(t *testing.T) {
	t.Parallel()

	store := storage.NewJSONStore(t.TempDir())
	runtime := &domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}
	if err := store.SaveWardRuntime(context.Background(), *runtime); err != nil {
		t.Fatalf("save runtime: %v", err)
	}

	platformAPI := &serveTLSPlatformAPIStub{
		heartbeatResponse: &ports.HeartbeatResponse{
			Accepted:   true,
			WardStatus: "suspended",
			ExpiresAt:  time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		},
	}
	_, err := runServeHeartbeat(context.Background(), store, platformAPI, runtime, "test")
	if err == nil {
		t.Fatal("expected terminal heartbeat error")
	}
	if platformAPI.heartbeatCount != 1 {
		t.Fatalf("expected one heartbeat, got %d", platformAPI.heartbeatCount)
	}

	saved, loadErr := store.LoadWardRuntime(context.Background())
	if loadErr != nil {
		t.Fatalf("load runtime: %v", loadErr)
	}
	if saved.WardStatus != domain.WardStatusSuspended {
		t.Fatalf("expected saved suspended status, got %s", saved.WardStatus)
	}
	if saved.LastRefreshedAt.IsZero() {
		t.Fatal("expected last_refreshed_at to be persisted")
	}
}

func TestRunServeHeartbeatStopsOnCredentialExpired(t *testing.T) {
	t.Parallel()

	store := storage.NewJSONStore(t.TempDir())
	runtime := &domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_old",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
	}
	platformAPI := &serveTLSPlatformAPIStub{
		heartbeatErr: &ports.PlatformError{Code: "credential_expired", HTTPStatus: 401},
	}

	_, err := runServeHeartbeat(context.Background(), store, platformAPI, runtime, "test")
	if err == nil {
		t.Fatal("expected terminal credential_expired error")
	}
	if !strings.Contains(err.Error(), "credential expired") {
		t.Fatalf("expected credential expired error, got %v", err)
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
