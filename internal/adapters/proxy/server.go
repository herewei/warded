package proxy

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/herewei/warded/internal/sitepolicy"
)

// ServerConfig holds the runtime configuration for the proxy server.
type ServerConfig struct {
	WardID          string
	Site            domain.Site
	WardStatus      domain.WardStatus
	Domain          string
	UpstreamAddr    string
	UpstreamMode    domain.UpstreamMode
	UpstreamCommand string
	UpstreamManager ports.UpstreamProcessManager
	SetHost         string
	PlatformOrigin  string // optional override for platform API origin (dev/testing)
	ExpectedIssuer  string // public platform base URL used for JWT issuer validation
	AuthExchange    ports.AuthExchangeAPI
	JWTSigner       ports.JWTSigner
	JWTVerifier     ports.JWTVerifier
	AgentVerifier   ports.AgentTokenVerifier
	TLSConfig       *tls.Config

	AuthWhitelist []domain.AuthWhitelistRule
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
	upstreamAddr := config.UpstreamAddr
	if upstreamAddr == "" {
		upstreamAddr = "127.0.0.1:18789"
	}

	target := &url.URL{
		Scheme: "http",
		Host:   upstreamAddr,
	}

	rp := httputil.NewSingleHostReverseProxy(target)
	originalDirector := rp.Director
	rp.Director = func(req *http.Request) {
		originalDirector(req)
		if config.SetHost != "" {
			req.Host = config.SetHost
		} else {
			req.Host = target.Host
		}
	}

	if config.UpstreamMode == domain.UpstreamModeManaged && config.UpstreamManager != nil {
		rp.Transport = &upstreamRetryTransport{
			base: &http.Transport{
				Proxy:               nil,
				DisableKeepAlives:   true,
				MaxIdleConnsPerHost: -1,
			},
			manager: config.UpstreamManager,
			addr:    upstreamAddr,
			command: config.UpstreamCommand,
		}
	}

	return &Server{
		config:          config,
		transactions:    make(map[string]loginTransaction),
		revokedSessions: make(map[string]revokedSession),
		reverseProxy:    rp,
	}
}

// Handler returns the http.Handler for the proxy.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /_ward/probe", s.handleProbe)
	mux.HandleFunc("GET /_ward/callback", s.handleCallback)
	mux.HandleFunc("GET /_ward/logout", s.handleLogout)
	mux.HandleFunc("GET /_ward/healthz", s.handleHealthz)
	mux.HandleFunc("/", s.handleDefault)
	return mux
}

// Serve starts the auth proxy on the given listen address.
// It blocks until ctx is cancelled.
func (s *Server) Serve(ctx context.Context, listenAddr string) error {
	s.startCleanupLoop(ctx)
	if s.config.TLSConfig == nil {
		return fmt.Errorf("proxy: tls config is required")
	}

	srv := &http.Server{
		Addr:    listenAddr,
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

	slog.Info("warded serve: listening", "addr", listenAddr, "tls_enabled", s.config.TLSConfig != nil)
	listener, listenErr := net.Listen("tcp", listenAddr)
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

// handleDefault handles all non-internal routes: ward status, whitelist bypass, auth middleware, then reverse proxy.
func (s *Server) handleDefault(w http.ResponseWriter, r *http.Request) {
	// Check ward status first — whitelist does not bypass inactive ward
	if s.config.WardStatus != domain.WardStatusActive {
		http.Error(w, "service unavailable: ward is not active", http.StatusForbidden)
		return
	}

	// Check if path is whitelisted
	if s.isWhitelisted(r.URL.Path) {
		s.ensureManagedUpstream(r.Context())
		cleanInjectedIdentityHeaders(r.Header)
		s.reverseProxy.ServeHTTP(w, r)
		return
	}

	// Auth middleware: validate JWT cookie
	if bearerToken := extractBearerToken(r); bearerToken != "" {
		s.handleAgentBearer(w, r, bearerToken)
		return
	}

	cookie, err := r.Cookie("warded_session")
	if err != nil || cookie.Value == "" {
		s.serveLoginPage(w, r)
		return
	}

	claims, err := s.config.JWTVerifier.Verify(cookie.Value)
	if err != nil {
		attrs := baseAuditAttrs("session_cookie_auth", "rejected", "invalid_jwt", r)
		attrs = append(attrs,
			"error", err,
			"site", string(s.config.Site),
			"ward_id", s.config.WardID,
		)
		slog.Warn("proxy: session cookie rejected", attrs...)
		s.serveLoginPage(w, r)
		return
	}

	if claims.WardID != s.config.WardID {
		attrs := baseAuditAttrs("session_cookie_auth", "rejected", "ward_id_mismatch", r)
		attrs = append(attrs,
			"site", string(s.config.Site),
			"ward_id", s.config.WardID,
			"token_ward_id", claims.WardID,
			"principal_id", claims.PrincipalID,
		)
		slog.Warn("proxy: session cookie rejected", attrs...)
		s.serveLoginPage(w, r)
		return
	}

	expectedAud := "ward:" + s.config.WardID
	if claims.Aud != expectedAud {
		attrs := baseAuditAttrs("session_cookie_auth", "rejected", "audience_mismatch", r)
		attrs = append(attrs,
			"site", string(s.config.Site),
			"ward_id", s.config.WardID,
			"token_aud", claims.Aud,
			"expected_aud", expectedAud,
			"principal_id", claims.PrincipalID,
		)
		slog.Warn("proxy: session cookie rejected", attrs...)
		s.serveLoginPage(w, r)
		return
	}

	s.mu.RLock()
	revokedEntry, revoked := s.revokedSessions[claims.SessionID]
	s.mu.RUnlock()
	if revoked && time.Now().UTC().Before(revokedEntry.ExpiresAt) {
		attrs := baseAuditAttrs("session_cookie_auth", "rejected", "session_revoked", r)
		attrs = append(attrs,
			"site", string(s.config.Site),
			"ward_id", s.config.WardID,
			"session_id", claims.SessionID,
			"principal_id", claims.PrincipalID,
		)
		slog.Warn("proxy: session cookie rejected", attrs...)
		s.serveLoginPage(w, r)
		return
	}

	// Auth passed: strip any client-spoofed identity headers, then inject trusted ones
	cleanInjectedIdentityHeaders(r.Header)
	r.Header.Set("X-Forwarded-User", claims.PrincipalID)
	r.Header.Set("X-Warded-Principal-Id", claims.PrincipalID)
	r.Header.Set("X-Warded-Ward-Id", claims.WardID)
	s.ensureManagedUpstream(r.Context())
	s.reverseProxy.ServeHTTP(w, r)
}

func (s *Server) handleAgentBearer(w http.ResponseWriter, r *http.Request, bearerToken string) {
	expectedIssuer := s.config.ExpectedIssuer
	if expectedIssuer == "" {
		expectedIssuer = sitepolicy.ForSite(s.config.Site).PlatformBaseURL()
	}
	expectedAudience := "ward:" + s.config.WardID

	if s.config.AgentVerifier == nil {
		attrs := baseAuditAttrs("agent_bearer_auth", "rejected", "verifier_not_configured", r)
		attrs = append(attrs,
			"site", string(s.config.Site),
			"ward_id", s.config.WardID,
			"expected_issuer", expectedIssuer,
			"expected_audience", expectedAudience,
		)
		slog.Warn("proxy: agent bearer token rejected", attrs...)
		writeBearerUnauthorized(w)
		return
	}

	claims, err := s.config.AgentVerifier.Verify(bearerToken)
	if err != nil {
		tokenLen, tokenKID, tokenAlg := parseTokenMeta(bearerToken)
		attrs := baseAuditAttrs("agent_bearer_auth", "rejected", "invalid_token", r)
		attrs = append(attrs,
			"error", err,
			"site", string(s.config.Site),
			"ward_id", s.config.WardID,
			"expected_issuer", expectedIssuer,
			"expected_audience", expectedAudience,
			"token_length", tokenLen,
			"token_kid", tokenKID,
			"token_alg", tokenAlg,
		)
		slog.Error("proxy: agent bearer token rejected", attrs...)
		writeBearerUnauthorized(w)
		return
	}

	attrs := baseAuditAttrs("agent_bearer_auth", "accepted", "valid_token", r)
	attrs = append(attrs,
		"site", string(s.config.Site),
		"ward_id", s.config.WardID,
		"principal_id", claims.PrincipalID,
		"jti", claims.JTI,
		"token_name", claims.TokenName,
	)
	slog.Info("proxy: agent bearer token accepted", attrs...)
	cleanInjectedIdentityHeaders(r.Header)
	r.Header.Del("Authorization")
	r.Header.Set("X-Forwarded-User", claims.PrincipalID)
	r.Header.Set("X-Warded-Principal-Id", claims.PrincipalID)
	r.Header.Set("X-Warded-Ward-Id", claims.WardID)
	r.Header.Set("X-Warded-Auth-Type", "ward_access_token")
	r.Header.Set("X-Warded-Token-Jti", claims.JTI)
	if claims.TokenName != "" {
		r.Header.Set("X-Warded-Credential-Name", claims.TokenName)
	}
	s.ensureManagedUpstream(r.Context())
	s.reverseProxy.ServeHTTP(w, r)
}

func cleanInjectedIdentityHeaders(h http.Header) {
	for key := range h {
		canonical := http.CanonicalHeaderKey(key)
		if canonical == "X-Forwarded-User" ||
			canonical == "X-Auth-Request-User" ||
			canonical == "Remote-User" ||
			strings.HasPrefix(canonical, "X-Warded-") {
			delete(h, key)
		}
	}
}

func extractBearerToken(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(value, "Bearer ") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(value, "Bearer "))
}

func writeBearerUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"access_denied"}`))
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
	if s.config.PlatformOrigin != "" {
		platformBaseURL = strings.TrimSuffix(s.config.PlatformOrigin, "/")
	}
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

	exchangeResp, err := s.config.AuthExchange.ExchangeAuthCode(r.Context(), ports.ExchangeAuthCodeRequest{
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

func (s *Server) isWhitelisted(path string) bool {
	for _, rule := range s.config.AuthWhitelist {
		switch rule.Type {
		case "exact":
			if path == rule.Path {
				return true
			}
		case "prefix":
			if strings.HasPrefix(path, rule.Path) {
				return true
			}
		}
	}
	return false
}

// upstreamRetryTransport wraps an http.RoundTripper and attempts to start the
// managed upstream process on dial failures, then retries the request once.
type upstreamRetryTransport struct {
	base    http.RoundTripper
	manager ports.UpstreamProcessManager
	addr    string
	command string
}

func (s *Server) ensureManagedUpstream(ctx context.Context) {
	if s.config.UpstreamMode != domain.UpstreamModeManaged || s.config.UpstreamManager == nil {
		return
	}
	addr := s.config.UpstreamAddr
	if addr == "" {
		addr = "127.0.0.1:18789"
	}
	runCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, _ = s.config.UpstreamManager.EnsureRunning(runCtx, addr, s.config.UpstreamCommand)
}

func (t *upstreamRetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	if err != nil && t.manager != nil && isUpstreamConnectionError(err) {
		startCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		_, startErr := t.manager.EnsureRunning(startCtx, t.addr, t.command)
		cancel()
		if startErr == nil {
			resp, err = t.base.RoundTrip(req)
		}
	}
	return resp, err
}

func isUpstreamConnectionError(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return opErr.Op == "dial" || opErr.Op == "read"
	}
	return errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.ETIMEDOUT)
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

func remoteAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func forwardedFor(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		if idx := strings.Index(ip, ","); idx != -1 {
			ip = strings.TrimSpace(ip[:idx])
		}
		return ip
	}
	return ""
}

func realIP(r *http.Request) string {
	ip := r.Header.Get("X-Real-Ip")
	if ip != "" {
		return ip
	}
	return ""
}

func parseTokenMeta(token string) (length int, kid, alg string) {
	length = len(token)
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return
	}
	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return
	}
	var header struct {
		KID string `json:"kid"`
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return
	}
	return length, header.KID, header.Alg
}

func baseAuditAttrs(eventType, outcome, reason string, r *http.Request) []any {
	return []any{
		"event_type", eventType,
		"outcome", outcome,
		"reason", reason,
		"client_ip", remoteAddr(r),
		"x_forwarded_for", forwardedFor(r),
		"x_real_ip", realIP(r),
		"method", r.Method,
		"path", r.URL.Path,
		"user_agent", r.UserAgent(),
	}
}
