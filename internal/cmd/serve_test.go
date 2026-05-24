package cmd

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
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
	_, err := runServeHeartbeat(context.Background(), store, platformAPI, runtime, "test", nil)
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

	_, err := runServeHeartbeat(context.Background(), store, platformAPI, runtime, "test", nil)
	if err == nil {
		t.Fatal("expected terminal credential_expired error")
	}
	if !strings.Contains(err.Error(), "credential expired") {
		t.Fatalf("expected credential expired error, got %v", err)
	}
}

type testAgentTokenCache struct {
	publicKeys  []domain.PlatformJWTPublicKey
	validTokens []ports.ValidAgentToken
}

func (c *testAgentTokenCache) UpdatePublicKeys(keys []domain.PlatformJWTPublicKey) {
	c.publicKeys = keys
}

func (c *testAgentTokenCache) UpdateValidTokens(tokens []ports.ValidAgentToken) {
	c.validTokens = tokens
}

func TestRunServeHeartbeatUpdatesAgentTokenCacheAndPersistsPublicKeys(t *testing.T) {
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
	cache := &testAgentTokenCache{}
	platformAPI := &serveTLSPlatformAPIStub{
		heartbeatResponse: &ports.HeartbeatResponse{
			Accepted:           true,
			WardStatus:         "active",
			NextHeartbeatAfter: 60,
			ExpiresAt:          time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			PlatformJWTPublicKeys: []domain.PlatformJWTPublicKey{
				{KID: "global-test", PublicKey: "public"},
			},
			ValidAgentTokens: []ports.ValidAgentToken{
				{JTI: "agtok_123", PrincipalID: "principal_123", TokenName: "CI"},
			},
		},
	}

	_, err := runServeHeartbeat(context.Background(), store, platformAPI, runtime, "test", cache)
	if err != nil {
		t.Fatalf("runServeHeartbeat returned error: %v", err)
	}
	if len(cache.publicKeys) != 1 || cache.publicKeys[0].KID != "global-test" {
		t.Fatalf("public keys not updated: %+v", cache.publicKeys)
	}
	if len(cache.validTokens) != 1 || cache.validTokens[0].JTI != "agtok_123" {
		t.Fatalf("valid tokens not updated: %+v", cache.validTokens)
	}
	saved, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if len(saved.PlatformJWTPublicKeys) != 1 || saved.PlatformJWTPublicKeys[0].KID != "global-test" {
		t.Fatalf("public keys not persisted: %+v", saved.PlatformJWTPublicKeys)
	}
}

func TestRunServeHeartbeatReplacesPublicKeysWithReturnedKeyset(t *testing.T) {
	t.Parallel()

	store := storage.NewJSONStore(t.TempDir())
	runtime := &domain.LocalWardRuntime{
		Site:       domain.SiteGlobal,
		WardID:     "ward_123",
		WardSecret: "wrd_123",
		WardStatus: domain.WardStatusActive,
		Domain:     "demo.warded.me",
		PlatformJWTPublicKeys: []domain.PlatformJWTPublicKey{
			{KID: "stale-key", PublicKey: "stale-public"},
		},
	}
	if err := store.SaveWardRuntime(context.Background(), *runtime); err != nil {
		t.Fatalf("save runtime: %v", err)
	}
	cache := &testAgentTokenCache{}
	platformAPI := &serveTLSPlatformAPIStub{
		heartbeatResponse: &ports.HeartbeatResponse{
			Accepted:           true,
			WardStatus:         "active",
			NextHeartbeatAfter: 60,
			PlatformJWTPublicKeys: []domain.PlatformJWTPublicKey{
				{KID: "global-old", PublicKey: "old-public"},
				{KID: "global-new", PublicKey: "new-public"},
			},
		},
	}

	_, err := runServeHeartbeat(context.Background(), store, platformAPI, runtime, "test", cache)
	if err != nil {
		t.Fatalf("runServeHeartbeat returned error: %v", err)
	}

	wantKIDs := []string{"global-old", "global-new"}
	if got := keyIDs(cache.publicKeys); !equalStrings(got, wantKIDs) {
		t.Fatalf("cache public keys = %v, want %v", got, wantKIDs)
	}
	saved, err := store.LoadWardRuntime(context.Background())
	if err != nil {
		t.Fatalf("load runtime: %v", err)
	}
	if got := keyIDs(saved.PlatformJWTPublicKeys); !equalStrings(got, wantKIDs) {
		t.Fatalf("persisted public keys = %v, want %v", got, wantKIDs)
	}
}

func TestServeStartedEnvelopeShape(t *testing.T) {
	t.Parallel()

	env := serveStartedEnvelope(&domain.LocalWardRuntime{
		Domain:     "demo.warded.me",
		ListenHost: "0.0.0.0",
		ListenPort: 443,
	})
	payload, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if got["ok"] != true || got["command"] != "serve" || got["event"] != "started" {
		t.Fatalf("expected serve started envelope, got: %v", got)
	}
	data, _ := got["data"].(map[string]any)
	if data["listen"] != "ipv4 0.0.0.0:443" || data["domain"] != "demo.warded.me" {
		t.Fatalf("unexpected started data: %v", data)
	}
	if _, hasError := got["error"]; hasError {
		t.Fatalf("started envelope must not contain error: %v", got)
	}
}

func keyIDs(keys []domain.PlatformJWTPublicKey) []string {
	ids := make([]string, 0, len(keys))
	for _, key := range keys {
		ids = append(ids, key.KID)
	}
	return ids
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
