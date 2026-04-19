package platformapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/herewei/warded/internal/ports"
)

const (
	maxLoggedBodyPreview = 512
	redactedValue        = "[REDACTED]"
)

type httpResult struct {
	StatusCode int
	Header     http.Header
	Body       []byte
	TraceID    string
}

type unexpectedStatusError struct {
	HTTPStatus int
	TraceID    string
	RequestID  string
	CFRay      string
	Message    string
}

func (e *unexpectedStatusError) Error() string {
	base := fmt.Sprintf("platform api: unexpected status %d", e.HTTPStatus)

	var meta []string
	if e.RequestID != "" {
		meta = append(meta, "request_id="+e.RequestID)
	}
	if e.TraceID != "" && e.TraceID != e.RequestID {
		meta = append(meta, "trace_id="+e.TraceID)
	}
	if e.CFRay != "" {
		meta = append(meta, "cf_ray="+e.CFRay)
	}
	if len(meta) > 0 {
		base += " (" + strings.Join(meta, ", ") + ")"
	}
	if e.Message != "" {
		base += ": " + e.Message
	}
	return base
}

// decodePlatformError reads an error response and returns either a structured
// *ports.PlatformError or a generic unexpected-status error with trace metadata.
func decodePlatformError(result *httpResult) error {
	var errBody struct {
		Error     string `json:"error"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if json.Unmarshal(result.Body, &errBody) == nil && errBody.Error != "" {
		requestID := strings.TrimSpace(errBody.RequestID)
		if requestID == "" {
			requestID = strings.TrimSpace(result.Header.Get("X-Request-Id"))
		}
		retryAfter := 0
		if ra := result.Header.Get("Retry-After"); ra != "" {
			if n, err := strconv.Atoi(ra); err == nil {
				retryAfter = n
			}
		}
		return &ports.PlatformError{
			Code:       errBody.Error,
			HTTPStatus: result.StatusCode,
			Message:    strings.TrimSpace(errBody.Message),
			RequestID:  requestID,
			RetryAfter: retryAfter,
		}
	}

	return &unexpectedStatusError{
		HTTPStatus: result.StatusCode,
		TraceID:    result.TraceID,
		RequestID:  strings.TrimSpace(result.Header.Get("X-Request-Id")),
		CFRay:      strings.TrimSpace(result.Header.Get("CF-Ray")),
		Message:    previewPayload(result.Body),
	}
}

type Client struct {
	baseURL    string
	httpClient *http.Client
	version    string
}

func NewClient(baseURL string, version ...string) *Client {
	v := "dev"
	if len(version) > 0 && version[0] != "" {
		v = version[0]
	}
	return &Client{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		version: v,
	}
}

func (c *Client) userAgent() string {
	return "warded-cli/" + c.version
}

func (c *Client) do(ctx context.Context, method, path, site, bearerToken string, body []byte) (*httpResult, error) {
	return c.doWithHeaders(ctx, method, path, site, bearerToken, body, nil)
}

func (c *Client) doWithHeaders(ctx context.Context, method, path, site, bearerToken string, body []byte, extraHeaders map[string]string) (*httpResult, error) {
	traceID := uuid.NewString()
	url := c.baseURL + path

	var reqBody io.Reader
	if len(body) > 0 {
		reqBody = bytes.NewReader(body)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("User-Agent", c.userAgent())
	httpReq.Header.Set("X-Request-Id", traceID)
	if len(body) > 0 {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if site != "" {
		httpReq.Header.Set("X-Warded-Site", site)
	}
	if bearerToken != "" {
		httpReq.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	for key, value := range extraHeaders {
		if strings.TrimSpace(key) == "" || value == "" {
			continue
		}
		httpReq.Header.Set(key, value)
	}

	slog.Debug(
		"platform api request",
		"trace_id", traceID,
		"method", method,
		"url", url,
		"site", site,
		"has_bearer", bearerToken != "",
		"body", previewPayload(body),
	)

	startedAt := time.Now()
	resp, err := c.httpClient.Do(httpReq)
	duration := time.Since(startedAt)
	if err != nil {
		slog.Debug(
			"platform api transport failure",
			"trace_id", traceID,
			"method", method,
			"url", url,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Debug(
			"platform api response read failed",
			"trace_id", traceID,
			"method", method,
			"url", url,
			"status", resp.StatusCode,
			"duration_ms", duration.Milliseconds(),
			"error", err,
		)
		return nil, err
	}

	slog.Debug(
		"platform api response",
		"trace_id", traceID,
		"method", method,
		"url", url,
		"status", resp.StatusCode,
		"duration_ms", duration.Milliseconds(),
		"content_type", strings.TrimSpace(resp.Header.Get("Content-Type")),
		"request_id", strings.TrimSpace(resp.Header.Get("X-Request-Id")),
		"cf_ray", strings.TrimSpace(resp.Header.Get("CF-Ray")),
		"server", strings.TrimSpace(resp.Header.Get("Server")),
		"body", previewPayload(respBody),
	)

	return &httpResult{
		StatusCode: resp.StatusCode,
		Header:     resp.Header.Clone(),
		Body:       respBody,
		TraceID:    traceID,
	}, nil
}

func previewPayload(body []byte) string {
	if len(body) == 0 {
		return ""
	}

	var decoded any
	if json.Unmarshal(body, &decoded) == nil {
		redacted := redactJSON(decoded)
		sanitized, err := json.Marshal(redacted)
		if err == nil {
			return truncatePreview(string(sanitized))
		}
	}

	return truncatePreview(strings.TrimSpace(string(body)))
}

func redactJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, nested := range typed {
			if isSensitiveJSONField(key) {
				out[key] = redactedValue
				continue
			}
			out[key] = redactJSON(nested)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, nested := range typed {
			out = append(out, redactJSON(nested))
		}
		return out
	default:
		return value
	}
}

func isSensitiveJSONField(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "authorization", "code", "token", "access_token", "refresh_token", "ward_draft_secret", "draft_secret", "draft_secret_challenge", "ward_secret", "tls_key", "tls_cert", "jwt_signing_secret":
		return true
	default:
		return false
	}
}

func truncatePreview(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if len(text) <= maxLoggedBodyPreview {
		return text
	}
	return text[:maxLoggedBodyPreview] + "...(truncated)"
}

func (c *Client) CreateWardDraft(ctx context.Context, req ports.CreateWardDraftRequest) (*ports.CreateWardDraftResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("platform api: marshal create ward draft request: %w", err)
	}

	result, err := c.do(ctx, http.MethodPost, "/api/v1/ward-drafts", req.Site, "", body)
	if err != nil {
		return nil, fmt.Errorf("platform api: create ward draft request failed: %w", err)
	}

	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, decodePlatformError(result)
	}

	var out ports.CreateWardDraftResponse
	if err := json.Unmarshal(result.Body, &out); err != nil {
		return nil, fmt.Errorf("platform api: decode create ward draft response: %w", err)
	}

	return &out, nil
}

func (c *Client) GetWardDraftStatus(ctx context.Context, site string, draftSecretChallenge string, wardDraftID string) (*ports.GetWardDraftStatusResponse, error) {
	result, err := c.doWithHeaders(ctx, http.MethodGet, "/api/v1/ward-drafts/"+wardDraftID+"/status", site, "", nil, map[string]string{
		"X-Warded-Draft-Challenge": draftSecretChallenge,
	})
	if err != nil {
		return nil, fmt.Errorf("platform api: get ward draft status request failed: %w", err)
	}

	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, decodePlatformError(result)
	}

	var out ports.GetWardDraftStatusResponse
	if err := json.Unmarshal(result.Body, &out); err != nil {
		return nil, fmt.Errorf("platform api: decode get ward draft status response: %w", err)
	}

	return &out, nil
}

func (c *Client) ClaimWardDraft(ctx context.Context, req ports.ClaimWardDraftRequest, wardDraftID string) (*ports.ClaimWardDraftResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("platform api: marshal claim ward draft request: %w", err)
	}
	result, err := c.do(ctx, http.MethodPost, "/api/v1/ward-drafts/"+wardDraftID+"/claim", req.Site, "", body)
	if err != nil {
		return nil, fmt.Errorf("platform api: claim ward draft request failed: %w", err)
	}
	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, decodePlatformError(result)
	}
	var out ports.ClaimWardDraftResponse
	if err := json.Unmarshal(result.Body, &out); err != nil {
		return nil, fmt.Errorf("platform api: decode claim ward draft response: %w", err)
	}
	return &out, nil
}

func (c *Client) ExchangeAuthCode(ctx context.Context, req ports.ExchangeAuthCodeRequest) (*ports.ExchangeAuthCodeResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("platform api: marshal exchange auth code request: %w", err)
	}

	result, err := c.do(ctx, http.MethodPost, "/api/v1/auth/exchange", req.Site, "", body)
	if err != nil {
		return nil, fmt.Errorf("platform api: exchange auth code request failed: %w", err)
	}

	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, decodePlatformError(result)
	}

	var out ports.ExchangeAuthCodeResponse
	if err := json.Unmarshal(result.Body, &out); err != nil {
		return nil, fmt.Errorf("platform api: decode exchange auth code response: %w", err)
	}

	return &out, nil
}

func (c *Client) GetWard(ctx context.Context, site string, bearerToken string, wardID string) (*ports.GetWardResponse, error) {
	result, err := c.do(ctx, http.MethodGet, "/api/v1/wards/"+wardID, site, bearerToken, nil)
	if err != nil {
		return nil, fmt.Errorf("platform api: get ward request failed: %w", err)
	}

	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, decodePlatformError(result)
	}

	var out ports.GetWardResponse
	if err := json.Unmarshal(result.Body, &out); err != nil {
		return nil, fmt.Errorf("platform api: decode get ward response: %w", err)
	}

	return &out, nil
}

func (c *Client) GetTLSMaterial(ctx context.Context, site string, bearerToken string, wardID string) (*ports.GetTLSMaterialResponse, error) {
	result, err := c.do(ctx, http.MethodPost, "/api/v1/wards/"+wardID+"/tls-material", site, bearerToken, nil)
	if err != nil {
		return nil, fmt.Errorf("platform api: get tls material request failed: %w", err)
	}

	if result.StatusCode < 200 || result.StatusCode >= 300 {
		return nil, decodePlatformError(result)
	}

	var out ports.GetTLSMaterialResponse
	if err := json.Unmarshal(result.Body, &out); err != nil {
		return nil, fmt.Errorf("platform api: decode get tls material response: %w", err)
	}
	return &out, nil
}
