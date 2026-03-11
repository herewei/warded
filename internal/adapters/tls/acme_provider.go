package tls

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/caddyserver/certmagic"
)

const (
	defaultACMEObtainTimeout = 2 * time.Minute
	acmeHTTPChallengeAddr    = ":80"
)

type ACMEProvider struct {
	tlsConfig *tls.Config
}

func NewACMEProvider(ctx context.Context, domain string, cacheDir string, obtainTimeout time.Duration) (*ACMEProvider, error) {
	if domain == "" {
		return nil, fmt.Errorf("local acme: domain is required")
	}
	if cacheDir == "" {
		return nil, fmt.Errorf("local acme: cache directory is required")
	}
	if obtainTimeout <= 0 {
		obtainTimeout = defaultACMEObtainTimeout
	}

	storage := &certmagic.FileStorage{Path: cacheDir}

	var cache *certmagic.Cache
	cache = certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(cert certmagic.Certificate) (*certmagic.Config, error) {
			return certmagic.New(cache, certmagic.Config{
				Storage: storage,
			}), nil
		},
	})

	magic := certmagic.New(cache, certmagic.Config{
		Storage: storage,
	})
	issuer := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
		CA:                        certmagic.LetsEncryptProductionCA,
		Agreed:                    true,
		DisableTLSALPNChallenge:   true,
		DisableHTTPChallenge:      false,
		DisableDistributedSolvers: true,
	})
	magic.Issuers = []certmagic.Issuer{issuer}

	challengeHandler := issuer.HTTPChallengeHandler(http.NotFoundHandler())
	challengeListener, err := net.Listen("tcp", acmeHTTPChallengeAddr)
	if err != nil {
		return nil, fmt.Errorf("local acme: listen on %s for HTTP-01 challenge: %w", acmeHTTPChallengeAddr, err)
	}

	challengeServer := &http.Server{
		Handler: challengeHandler,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	challengeErrCh := make(chan error, 1)
	go func() {
		err := challengeServer.Serve(challengeListener)
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			challengeErrCh <- err
			return
		}
		challengeErrCh <- nil
	}()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := challengeServer.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Warn("local acme: challenge server shutdown failed", "error", err)
		}
	}()

	manageCtx, cancel := context.WithTimeout(ctx, obtainTimeout)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- magic.ManageSync(manageCtx, []string{domain})
	}()

	select {
	case err := <-done:
		if err != nil {
			_ = challengeServer.Close()
			return nil, fmt.Errorf("local acme: obtain certificate for %s via HTTP-01: %w", domain, err)
		}
	case err := <-challengeErrCh:
		if err != nil {
			return nil, fmt.Errorf("local acme: HTTP-01 challenge server failed: %w", err)
		}
		return nil, fmt.Errorf("local acme: HTTP-01 challenge server exited unexpectedly")
	case <-manageCtx.Done():
		_ = challengeServer.Close()
		if errors.Is(manageCtx.Err(), context.DeadlineExceeded) {
			return nil, fmt.Errorf("local acme: timed out obtaining certificate for %s via HTTP-01 after %s; ensure TCP port 80 is reachable from the public internet", domain, obtainTimeout)
		}
		return nil, fmt.Errorf("local acme: certificate obtain canceled: %w", manageCtx.Err())
	}

	tlsConfig := magic.TLSConfig()
	tlsConfig.MinVersion = tls.VersionTLS12

	return &ACMEProvider{tlsConfig: tlsConfig}, nil
}

func (p *ACMEProvider) TLSConfig() *tls.Config {
	if p == nil {
		return nil
	}
	return p.tlsConfig
}
