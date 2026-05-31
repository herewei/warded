package proxy

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"
)

// MultiServer dispatches one listener to multiple isolated ward proxy servers.
// It only shares the TCP/TLS listener; each ward keeps its own auth state,
// verifier, whitelist, upstream, and callback transaction store.
type MultiServer struct {
	entries  map[string]*Server
	tls      *tls.Config
	serveTLS bool
}

// NewMultiServer creates a shared-listener proxy from per-ward servers.
func NewMultiServer(servers []*Server) (*MultiServer, error) {
	if len(servers) == 0 {
		return nil, fmt.Errorf("multi proxy: at least one ward server is required")
	}
	entries := make(map[string]*Server, len(servers))
	for _, server := range servers {
		if server == nil {
			return nil, fmt.Errorf("multi proxy: nil ward server")
		}
		domain := normalizeHost(server.config.Domain)
		if domain == "" {
			return nil, fmt.Errorf("multi proxy: ward %s has no domain", server.config.WardID)
		}
		if _, exists := entries[domain]; exists {
			return nil, fmt.Errorf("multi proxy: duplicate domain %s", domain)
		}
		entries[domain] = server
	}
	serveTLS := servers[0].serveTLS()
	for _, server := range servers {
		if server.serveTLS() != serveTLS {
			return nil, fmt.Errorf("multi proxy: mixed TLS modes are not supported")
		}
	}
	return &MultiServer{
		entries:  entries,
		tls:      multiTLSConfig(entries),
		serveTLS: serveTLS,
	}, nil
}

func (m *MultiServer) Serve(ctx context.Context, listenAddr string) error {
	for _, server := range m.entries {
		server.startCleanupLoop(ctx)
	}
	if m.serveTLS && m.tls == nil {
		return fmt.Errorf("multi proxy: tls config is required")
	}

	srv := &http.Server{
		Addr:    listenAddr,
		Handler: m,
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		slog.Info("multi proxy shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("warded serve: listening", "addr", listenAddr, "tls_enabled", m.serveTLS, "wards", len(m.entries))
	listener, listenErr := net.Listen("tcp", listenAddr)
	if listenErr != nil {
		return listenErr
	}
	if !m.serveTLS {
		err := srv.Serve(listener)
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
	err := srv.Serve(tls.NewListener(listener, m.tls))
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (m *MultiServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := normalizeHost(r.Host)
	server := m.entries[host]
	if server == nil && r.TLS != nil {
		server = m.entries[normalizeHost(r.TLS.ServerName)]
	}
	if server == nil {
		slog.Warn("multi proxy: unknown host", "host", r.Host)
		http.Error(w, "unknown ward host", http.StatusNotFound)
		return
	}
	server.Handler().ServeHTTP(w, r)
}

func multiTLSConfig(entries map[string]*Server) *tls.Config {
	var fallback *tls.Config
	for _, server := range entries {
		if server.config.TLSConfig != nil {
			fallback = server.config.TLSConfig
			break
		}
	}
	if fallback == nil {
		return nil
	}

	cfg := fallback.Clone()
	cfg.GetCertificate = func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if hello != nil {
			if server := entries[normalizeHost(hello.ServerName)]; server != nil && server.config.TLSConfig != nil && server.config.TLSConfig.GetCertificate != nil {
				return server.config.TLSConfig.GetCertificate(hello)
			}
		}
		if fallback.GetCertificate != nil {
			return fallback.GetCertificate(hello)
		}
		if len(fallback.Certificates) > 0 {
			return &fallback.Certificates[0], nil
		}
		return nil, fmt.Errorf("multi proxy: no certificate available")
	}
	return cfg
}

func normalizeHost(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "[") {
		if idx := strings.LastIndex(value, "]"); idx >= 0 {
			return strings.Trim(value[:idx+1], "[]")
		}
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		return strings.Trim(host, "[]")
	}
	if idx := strings.LastIndex(value, ":"); idx > -1 && strings.Count(value, ":") == 1 {
		return value[:idx]
	}
	return strings.Trim(value, "[]")
}
