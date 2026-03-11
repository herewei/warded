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
	"fmt"
	"log/slog"
	"math/big"
	"sync"
	"time"

	"github.com/herewei/warded/internal/ports"
)

const (
	defaultRefreshAfter       = time.Hour
	minRefreshAfter           = 5 * time.Minute
	refreshAfterSuccessWindow = 24 * time.Hour
	refreshBeforeExpiry       = 14 * 24 * time.Hour
	fallbackValidity          = 30 * 24 * time.Hour
	maxRefreshFailures        = 5
	refreshFailureCooldownMul = 6
)

type FetchFunc func(ctx context.Context) (*ports.GetTLSMaterialResponse, error)

type PlatformCertProvider struct {
	ctx    context.Context
	fetch  FetchFunc
	domain string

	mu                   sync.RWMutex
	platformCert         *tls.Certificate
	platformCertNotAfter time.Time
	fallbackCert         *tls.Certificate
	lastSuccessAt        time.Time
	nextAllowedRefreshAt time.Time
	refreshInFlight      bool
	consecutiveFailures  int
	version              string
}

func NewPlatformCertProvider(
	ctx context.Context,
	domain string,
	initialCert *tls.Certificate,
	initialNotAfter time.Time,
	initialVersion string,
	initialRefreshAfter time.Duration,
	fetch FetchFunc,
) (*PlatformCertProvider, error) {
	if fetch == nil {
		return nil, fmt.Errorf("platform cert provider: fetch function is required")
	}
	if domain == "" {
		return nil, fmt.Errorf("platform cert provider: domain is required")
	}
	fallbackCert, err := generateFallbackCertificate(domain, time.Now().UTC())
	if err != nil {
		return nil, fmt.Errorf("platform cert provider: generate fallback certificate: %w", err)
	}

	now := time.Now().UTC()
	p := &PlatformCertProvider{
		ctx:          ctx,
		fetch:        fetch,
		domain:       domain,
		fallbackCert: fallbackCert,
	}
	if initialCert != nil {
		p.platformCert = initialCert
		p.platformCertNotAfter = initialNotAfter.UTC()
		p.lastSuccessAt = now
		p.nextAllowedRefreshAt = now.Add(normalizeRefreshAfter(initialRefreshAfter))
		p.version = initialVersion
	} else {
		p.nextAllowedRefreshAt = now
	}
	return p, nil
}

func (p *PlatformCertProvider) TLSConfig() *tls.Config {
	return &tls.Config{
		MinVersion: tls.VersionTLS12,
		GetCertificate: func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
			now := time.Now().UTC()
			if p.shouldRefresh(now) {
				p.startRefresh()
			}

			p.mu.RLock()
			defer p.mu.RUnlock()
			cert := p.currentCertificateLocked(now)
			if cert == nil {
				return nil, fmt.Errorf("platform cert provider: no certificate loaded")
			}
			return cert, nil
		},
	}
}

func (p *PlatformCertProvider) UsingFallbackCertificate() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.usingFallbackLocked(time.Now().UTC())
}

func (p *PlatformCertProvider) shouldRefresh(now time.Time) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.refreshInFlight || now.Before(p.nextAllowedRefreshAt) {
		return false
	}
	if p.usingFallbackLocked(now) {
		return true
	}
	if p.lastSuccessAt.IsZero() || now.Sub(p.lastSuccessAt) >= refreshAfterSuccessWindow {
		return true
	}
	if !p.platformCertNotAfter.IsZero() && p.platformCertNotAfter.Sub(now) <= refreshBeforeExpiry {
		return true
	}
	return false
}

func (p *PlatformCertProvider) usingFallbackLocked(now time.Time) bool {
	return p.currentCertificateLocked(now) == p.fallbackCert
}

func (p *PlatformCertProvider) currentCertificateLocked(now time.Time) *tls.Certificate {
	if p.platformCert != nil {
		if p.platformCertNotAfter.IsZero() || now.Before(p.platformCertNotAfter) {
			return p.platformCert
		}
	}
	return p.fallbackCert
}

func (p *PlatformCertProvider) startRefresh() {
	p.mu.Lock()
	if p.refreshInFlight {
		p.mu.Unlock()
		return
	}
	p.refreshInFlight = true
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			p.refreshInFlight = false
			p.mu.Unlock()
		}()

		refreshCtx, cancel := context.WithTimeout(p.ctx, 15*time.Second)
		defer cancel()
		if err := p.refresh(refreshCtx); err != nil {
			slog.Warn("tls material refresh failed", "domain", p.domain, "error", err)
		}
	}()
}

func (p *PlatformCertProvider) refresh(ctx context.Context) error {
	resp, err := p.fetch(ctx)
	if err != nil {
		p.markRefreshFailure(defaultRefreshAfter)
		return fmt.Errorf("platform cert provider: fetch tls material: %w", err)
	}
	if resp == nil {
		p.markRefreshFailure(defaultRefreshAfter)
		return fmt.Errorf("platform cert provider: empty tls material response")
	}

	cert, err := tls.X509KeyPair([]byte(resp.TLSCert), []byte(resp.TLSKey))
	if err != nil {
		p.markRefreshFailure(refreshAfterFromResponse(resp))
		return fmt.Errorf("platform cert provider: parse tls material: %w", err)
	}

	notAfter := time.Time{}
	if resp.NotAfter != "" {
		notAfter, _ = time.Parse(time.RFC3339, resp.NotAfter)
	}
	now := time.Now().UTC()

	p.mu.Lock()
	p.platformCert = &cert
	p.platformCertNotAfter = notAfter.UTC()
	p.lastSuccessAt = now
	p.nextAllowedRefreshAt = now.Add(refreshAfterFromResponse(resp))
	p.consecutiveFailures = 0
	p.version = resp.Version
	p.mu.Unlock()
	return nil
}

func (p *PlatformCertProvider) markRefreshFailure(refreshAfter time.Duration) {
	now := time.Now().UTC()
	refreshAfter = normalizeRefreshAfter(refreshAfter)

	p.mu.Lock()
	defer p.mu.Unlock()

	p.consecutiveFailures++
	if p.consecutiveFailures >= maxRefreshFailures {
		p.nextAllowedRefreshAt = now.Add(refreshAfter * refreshFailureCooldownMul)
		return
	}
	p.nextAllowedRefreshAt = now.Add(refreshAfter)
}

func refreshAfterFromResponse(resp *ports.GetTLSMaterialResponse) time.Duration {
	if resp == nil {
		return defaultRefreshAfter
	}
	return normalizeRefreshAfter(time.Duration(resp.RefreshAfterSeconds) * time.Second)
}

func normalizeRefreshAfter(v time.Duration) time.Duration {
	if v <= 0 {
		return defaultRefreshAfter
	}
	if v < minRefreshAfter {
		return minRefreshAfter
	}
	return v
}

func generateFallbackCertificate(serverName string, now time.Time) (*tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			CommonName:   "warded-fallback",
			Organization: []string{"Warded Fallback"},
		},
		Issuer: pkix.Name{
			CommonName:   "warded-fallback",
			Organization: []string{"Warded Fallback"},
		},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(fallbackValidity),
		DNSNames:              []string{serverName},
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &cert, nil
}
