package ports

import (
	"context"
	"fmt"
)

// PlatformError carries the structured error code returned by the platform API.
// Callers can use errors.As to inspect the Code field for precise branching.
type PlatformError struct {
	Code       string
	HTTPStatus int
	Message    string
	RequestID  string
	RetryAfter int // seconds, parsed from Retry-After header
}

func (e *PlatformError) Error() string {
	if e.RequestID != "" && e.Message != "" {
		return fmt.Sprintf("platform error: %s (HTTP %d, request_id=%s): %s", e.Code, e.HTTPStatus, e.RequestID, e.Message)
	}
	if e.RequestID != "" {
		return fmt.Sprintf("platform error: %s (HTTP %d, request_id=%s)", e.Code, e.HTTPStatus, e.RequestID)
	}
	if e.Message != "" {
		return fmt.Sprintf("platform error: %s (HTTP %d): %s", e.Code, e.HTTPStatus, e.Message)
	}
	return fmt.Sprintf("platform error: %s (HTTP %d)", e.Code, e.HTTPStatus)
}

type CreateWardDraftRequest struct {
	Site                 string `json:"site"`
	Mode                 string `json:"mode,omitempty"`
	TargetWardID         string `json:"target_ward_id,omitempty"`
	Spec                 string `json:"spec"`
	BillingMode          string `json:"billing_mode"`
	DomainType           string `json:"domain_type"`
	RequestedDomain      string `json:"requested_domain"`
	UpstreamPort         int    `json:"upstream_port"`
	ListenPort           int    `json:"listen_port"`
	ProbeChallenge       string `json:"probe_challenge,omitempty"`
	DraftSecretChallenge string `json:"draft_secret_challenge"`
}

type CreateWardDraftResponse struct {
	WardDraftID        string `json:"ward_draft_id"`
	Site               string `json:"site"`
	Status             string `json:"status"`
	ExpiresAt          string `json:"expires_at"`
	ActivationURL      string `json:"activation_url"`
	DomainCheckStatus  string `json:"domain_check_status"`
	ResolvedPublicIP   string `json:"resolved_public_ip"`
	IngressProbeStatus string `json:"ingress_probe_status"`
}

type GetWardDraftStatusResponse struct {
	WardDraftID string `json:"ward_draft_id"`
	Status      string `json:"status"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

type ClaimWardDraftRequest struct {
	DraftSecret string `json:"draft_secret"`
	Site        string `json:"-"`
}

type ClaimWardDraftResponse struct {
	WardID         string `json:"ward_id"`
	WardSecret     string `json:"ward_secret"`
	Site           string `json:"site"`
	Status         string `json:"status"`
	Domain         string `json:"domain"`
	BillingMode    string `json:"billing_mode"`
	ActivationMode string `json:"activation_mode"`
	ActivatedAt    string `json:"activated_at"`
	ExpiresAt      string `json:"expires_at"`
}

type GetWardResponse struct {
	WardID           string `json:"ward_id"`
	OwnerPrincipalID string `json:"owner_principal_id"`
	Site             string `json:"site"`
	Spec             string `json:"spec"`
	BillingMode      string `json:"billing_mode"`
	ActivationMode   string `json:"activation_mode"`
	DomainType       string `json:"domain_type"`
	Domain           string `json:"domain"`
	UpstreamPort     int    `json:"upstream_port"`
	Status           string `json:"status"`
	ActivatedAt      string `json:"activated_at"`
	ExpiresAt        string `json:"expires_at"`
}

type GetTLSMaterialResponse struct {
	TLSCert             string `json:"tls_cert"`
	TLSKey              string `json:"tls_key"`
	NotAfter            string `json:"not_after"`
	Version             string `json:"version"`
	RefreshAfterSeconds int    `json:"refresh_after_seconds"`
}

type ExchangeAuthCodeRequest struct {
	Code   string `json:"code"`
	Site   string `json:"site"`
	WardID string `json:"ward_id"`
}

type ExchangeAuthCodeResponse struct {
	PrincipalID string `json:"principal_id"`
	WardID      string `json:"ward_id"`
	SessionID   string `json:"session_id"`
	ExpiresAt   string `json:"expires_at"`
}

type PlatformAPI interface {
	CreateWardDraft(ctx context.Context, req CreateWardDraftRequest) (*CreateWardDraftResponse, error)
	GetWardDraftStatus(ctx context.Context, site string, draftSecretChallenge string, wardDraftID string) (*GetWardDraftStatusResponse, error)
	ClaimWardDraft(ctx context.Context, req ClaimWardDraftRequest, wardDraftID string) (*ClaimWardDraftResponse, error)
	GetWard(ctx context.Context, site string, bearerToken string, wardID string) (*GetWardResponse, error)
	GetTLSMaterial(ctx context.Context, site string, bearerToken string, wardID string) (*GetTLSMaterialResponse, error)
	ExchangeAuthCode(ctx context.Context, req ExchangeAuthCodeRequest) (*ExchangeAuthCodeResponse, error)
}
