package tls

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
	"sync/atomic"
	"testing"
	"time"

	"github.com/herewei/warded/internal/ports"
)

func TestPlatformCertProviderServesInitialPlatformCertificate(t *testing.T) {
	t.Parallel()

	certPEM, keyPEM := generateTestCertificate(t, "demo.warded.me", "platform")
	initialCert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatalf("X509KeyPair returned error: %v", err)
	}

	provider, err := NewPlatformCertProvider(context.Background(), "demo.warded.me", &initialCert, time.Now().UTC().Add(24*time.Hour), "v1", time.Hour, func(context.Context) (*ports.GetTLSMaterialResponse, error) {
		return &ports.GetTLSMaterialResponse{}, nil
	})
	if err != nil {
		t.Fatalf("NewPlatformCertProvider returned error: %v", err)
	}

	cfg := provider.TLSConfig()
	cert, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate returned error: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatalf("expected loaded certificate, got %#v", cert)
	}
	if provider.UsingFallbackCertificate() {
		t.Fatal("expected platform certificate, got fallback")
	}
}

func TestPlatformCertProviderFallsBackWhenNoInitialPlatformCertificate(t *testing.T) {
	t.Parallel()

	provider, err := NewPlatformCertProvider(context.Background(), "demo.warded.me", nil, time.Time{}, "", 0, func(context.Context) (*ports.GetTLSMaterialResponse, error) {
		return nil, context.DeadlineExceeded
	})
	if err != nil {
		t.Fatalf("NewPlatformCertProvider returned error: %v", err)
	}

	cfg := provider.TLSConfig()
	cert, err := cfg.GetCertificate(nil)
	if err != nil {
		t.Fatalf("GetCertificate returned error: %v", err)
	}
	if cert == nil || len(cert.Certificate) == 0 {
		t.Fatalf("expected fallback certificate, got %#v", cert)
	}
	if !provider.UsingFallbackCertificate() {
		t.Fatal("expected fallback certificate to be active")
	}
}

func TestPlatformCertProviderPassiveRefreshReplacesFallbackCertificate(t *testing.T) {
	t.Parallel()

	refreshedCertPEM, refreshedKeyPEM := generateTestCertificate(t, "demo.warded.me", "platform")

	var fetchCalls atomic.Int32
	provider, err := NewPlatformCertProvider(context.Background(), "demo.warded.me", nil, time.Time{}, "", 0, func(context.Context) (*ports.GetTLSMaterialResponse, error) {
		fetchCalls.Add(1)
		return &ports.GetTLSMaterialResponse{
			TLSCert:             refreshedCertPEM,
			TLSKey:              refreshedKeyPEM,
			NotAfter:            time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339),
			Version:             "v2",
			RefreshAfterSeconds: 3600,
		}, nil
	})
	if err != nil {
		t.Fatalf("NewPlatformCertProvider returned error: %v", err)
	}

	cfg := provider.TLSConfig()
	if _, err := cfg.GetCertificate(nil); err != nil {
		t.Fatalf("GetCertificate returned error: %v", err)
	}

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if _, err := cfg.GetCertificate(nil); err != nil {
			t.Fatalf("GetCertificate returned error: %v", err)
		}
		if !provider.UsingFallbackCertificate() {
			if fetchCalls.Load() == 0 {
				t.Fatal("expected refresh fetch to be called")
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatal("expected fallback certificate to be replaced")
}

func TestPlatformCertProviderNormalizesRefreshHintFloor(t *testing.T) {
	t.Parallel()

	if got := refreshAfterFromResponse(&ports.GetTLSMaterialResponse{RefreshAfterSeconds: 0}); got != defaultRefreshAfter {
		t.Fatalf("expected default refresh after, got %s", got)
	}
	if got := refreshAfterFromResponse(&ports.GetTLSMaterialResponse{RefreshAfterSeconds: 1}); got != minRefreshAfter {
		t.Fatalf("expected min refresh after, got %s", got)
	}
}

func generateTestCertificate(t *testing.T, serverName string, cn string) (string, string) {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			CommonName: cn,
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
