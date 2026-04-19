package proxy

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/herewei/warded/internal/sitepolicy"
)

// ServerConfig holds the runtime configuration for the proxy server.
type ServerConfig struct {
	WardID       string
	Site         domain.Site
	WardStatus   domain.WardStatus
	Domain       string
	UpstreamPort int
	PlatformAPI  ports.PlatformAPI
	JWTSigner    ports.JWTSigner
	JWTVerifier  ports.JWTVerifier
	TLSConfig    *tls.Config

	WebhookAllowPaths []string
}

// loginTransaction stores state for an in-flight login redirect.
type loginTransaction struct {
	ReturnTo  string
	WardID    string
	CreatedAt time.Time
}

type revokedSession struct {
	ExpiresAt time.Time
}

// Server implements the identity-aware reverse proxy.
type Server struct {
	config ServerConfig

	mu              sync.RWMutex
	transactions    map[string]loginTransaction
	revokedSessions map[string]revokedSession

	reverseProxy *httputil.ReverseProxy
}

// NewServer creates a new proxy server.
func NewServer(config ServerConfig) *Server {
	upstreamPort := config.UpstreamPort
	if upstreamPort == 0 {
		upstreamPort = 18789
	}

	target := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("127.0.0.1:%d", upstreamPort),
	}

	return &Server{
		config:          config,
		transactions:    make(map[string]loginTransaction),
		revokedSessions: make(map[string]revokedSession),
		reverseProxy:    httputil.NewSingleHostReverseProxy(target),
	}
}

// Handler returns the http.Handler for the proxy.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_ward/probe", s.handleProbe)
	mux.HandleFunc("GET /_ward/callback", s.handleCallback)
	mux.HandleFunc("POST /_ward/logout", s.handleLogout)
	mux.HandleFunc("GET /_ward/healthz", s.handleHealthz)
	mux.HandleFunc("/", s.handleDefault)
	return mux
}

// ListenAndServe starts the proxy on the given address.
// It blocks until ctx is cancelled.
func (s *Server) ListenAndServe(ctx context.Context, addr string) error {
	s.startCleanupLoop(ctx)
	if s.config.TLSConfig == nil {
		return fmt.Errorf("proxy: tls config is required")
	}

	srv := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
		BaseContext: func(_ net.Listener) context.Context {
			return ctx
		},
	}

	go func() {
		<-ctx.Done()
		slog.Info("proxy shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	slog.Info("proxy starting", "addr", addr, "tls_enabled", s.config.TLSConfig != nil)
	listener, listenErr := net.Listen("tcp", addr)
	if listenErr != nil {
		return listenErr
	}
	tlsListener := tls.NewListener(listener, s.config.TLSConfig)
	err := srv.Serve(tlsListener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// handleDefault handles all non-internal routes: webhook bypass, auth middleware, then reverse proxy.
func (s *Server) handleDefault(w http.ResponseWriter, r *http.Request) {
	// Check if path is a webhook bypass
	if s.isWebhookPath(r.URL.Path) {
		s.reverseProxy.ServeHTTP(w, r)
		return
	}

	// Check ward status
	if s.config.WardStatus != domain.WardStatusActive {
		http.Error(w, "service unavailable: ward is not active", http.StatusForbidden)
		return
	}

	// Auth middleware: validate JWT cookie
	cookie, err := r.Cookie("warded_session")
	if err != nil || cookie.Value == "" {
		s.serveLoginPage(w, r)
		return
	}

	claims, err := s.config.JWTVerifier.Verify(cookie.Value)
	if err != nil {
		slog.Debug("proxy: invalid JWT", "error", err)
		s.serveLoginPage(w, r)
		return
	}

	if claims.WardID != s.config.WardID {
		slog.Debug("proxy: ward_id mismatch", "token_ward_id", claims.WardID, "expected", s.config.WardID)
		s.serveLoginPage(w, r)
		return
	}

	expectedAud := "ward:" + s.config.WardID
	if claims.Aud != expectedAud {
		slog.Debug("proxy: aud mismatch", "token_aud", claims.Aud, "expected", expectedAud)
		s.serveLoginPage(w, r)
		return
	}

	s.mu.RLock()
	revokedEntry, revoked := s.revokedSessions[claims.SessionID]
	s.mu.RUnlock()
	if revoked && time.Now().UTC().Before(revokedEntry.ExpiresAt) {
		slog.Debug("proxy: session revoked", "session_id", claims.SessionID)
		s.serveLoginPage(w, r)
		return
	}

	// Auth passed: inject identity headers and reverse proxy
	r.Header.Set("X-Forwarded-User", claims.PrincipalID)
	r.Header.Set("X-Warded-Principal-Id", claims.PrincipalID)
	r.Header.Set("X-Warded-Ward-Id", claims.WardID)
	s.reverseProxy.ServeHTTP(w, r)
}

func (s *Server) handleProbe(w http.ResponseWriter, r *http.Request) {
	challenge := strings.TrimSpace(r.URL.Query().Get("challenge"))
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if challenge == "" {
		http.Error(w, "missing challenge", http.StatusBadRequest)
		return
	}
	_, _ = w.Write([]byte("warded-probe-ok:" + challenge))
}

// serveLoginPage returns a local HTML page indicating the session has expired,
// with a login button that redirects to the platform. This prevents bot/scanner
// traffic from being forwarded to the platform automatically.
func (s *Server) serveLoginPage(w http.ResponseWriter, r *http.Request) {
	// DEBUG: Log the WardID being used for login URL generation
	slog.Debug("serveLoginPage: WardID in config", "ward_id", s.config.WardID, "domain", s.config.Domain)

	state, err := generateState()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	returnTo := r.URL.RequestURI()
	if returnTo == "" {
		returnTo = "/"
	}

	s.mu.Lock()
	s.transactions[state] = loginTransaction{
		ReturnTo:  returnTo,
		WardID:    s.config.WardID,
		CreatedAt: time.Now().UTC(),
	}
	s.mu.Unlock()

	platformBaseURL := sitepolicy.ForSite(s.config.Site).PlatformBaseURL()
	host := r.Host
	if host == "" {
		host = s.config.Domain
	}
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" {
		scheme = "http"
	}

	redirectURI := fmt.Sprintf("%s://%s/_ward/callback", scheme, host)

	params := url.Values{}
	params.Set("ward_id", s.config.WardID)
	params.Set("state", state)
	params.Set("redirect_uri", redirectURI)
	params.Set("return_to", returnTo)

	loginURL := fmt.Sprintf("%s/auth/signin?%s", platformBaseURL, params.Encode())

	// DEBUG: Log the final login URL being generated
	slog.Debug("serveLoginPage: generated login URL", "login_url", loginURL, "ward_id_param", s.config.WardID)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	fmt.Fprintf(w, loginPageHTML, loginURL)
}

const loginPageHTML = `<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>Warded - Session expired</title>
<style>body{font-family:system-ui,sans-serif;display:flex;align-items:center;justify-content:center;min-height:100vh;margin:0;background:#f5f5f5}
.card{text-align:center;padding:2rem;background:#fff;border-radius:8px;box-shadow:0 2px 8px rgba(0,0,0,.1)}
a.btn{display:inline-block;margin-top:1rem;padding:.75rem 2rem;background:#2563eb;color:#fff;text-decoration:none;border-radius:6px;font-size:1rem}
a.btn:hover{background:#1d4ed8}</style></head>
<body><div class="card"><h2>Session expired</h2><p>Sign in again to continue.</p><a class="btn" href="%s">Sign in</a></div></body></html>`

// handleCallback implements GET /_ward/callback.
func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if code == "" || state == "" {
		http.Error(w, "missing code or state", http.StatusBadRequest)
		return
	}

	s.mu.Lock()
	txn, ok := s.transactions[state]
	if ok {
		delete(s.transactions, state)
	}
	s.mu.Unlock()

	if !ok {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}

	if time.Since(txn.CreatedAt) > 10*time.Minute {
		http.Error(w, "state expired", http.StatusBadRequest)
		return
	}
	if txn.WardID != s.config.WardID {
		http.Error(w, "invalid ward context", http.StatusBadRequest)
		return
	}

	exchangeResp, err := s.config.PlatformAPI.ExchangeAuthCode(r.Context(), ports.ExchangeAuthCodeRequest{
		Code:   code,
		Site:   string(s.config.Site),
		WardID: s.config.WardID,
	})
	if err != nil {
		slog.Error("callback: exchange auth code failed", "error", err)
		http.Error(w, "auth code exchange failed", http.StatusUnauthorized)
		return
	}

	token, err := s.config.JWTSigner.Sign(ports.WardedClaims{
		PrincipalID: exchangeResp.PrincipalID,
		WardID:      exchangeResp.WardID,
		SessionID:   exchangeResp.SessionID,
	})
	if err != nil {
		slog.Error("callback: sign JWT failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "warded_session",
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	returnTo := txn.ReturnTo
	if returnTo == "" {
		returnTo = "/"
	}

	http.Redirect(w, r, returnTo, http.StatusFound)
}

// handleLogout implements POST /_ward/logout.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie("warded_session")
	if err == nil && cookie.Value != "" {
		claims, err := s.config.JWTVerifier.Verify(cookie.Value)
		if err == nil && claims.SessionID != "" {
			s.mu.Lock()
			s.revokedSessions[claims.SessionID] = revokedSession{
				ExpiresAt: time.Unix(claims.Exp, 0).UTC(),
			}
			s.mu.Unlock()
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     "warded_session",
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
	})

	w.WriteHeader(http.StatusNoContent)
}

// handleHealthz implements GET /_ward/healthz.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) isWebhookPath(path string) bool {
	for _, p := range s.config.WebhookAllowPaths {
		if path == p || (len(p) > 0 && p[len(p)-1] == '*' && len(path) >= len(p)-1 && path[:len(p)-1] == p[:len(p)-1]) {
			return true
		}
	}
	return false
}

func (s *Server) startCleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.cleanupExpiredState()
			}
		}
	}()
}

func (s *Server) cleanupExpiredState() {
	now := time.Now().UTC()

	s.mu.Lock()
	defer s.mu.Unlock()

	for state, txn := range s.transactions {
		if now.Sub(txn.CreatedAt) > 10*time.Minute {
			delete(s.transactions, state)
		}
	}

	for sessionID, revoked := range s.revokedSessions {
		if !now.Before(revoked.ExpiresAt) {
			delete(s.revokedSessions, sessionID)
		}
	}
}

func generateState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "st_" + hex.EncodeToString(b), nil
}
