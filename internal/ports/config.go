package ports

import (
	"context"
	"time"

	"github.com/herewei/warded/internal/domain"
)

// RuntimeRecord is the persistent snapshot of a local ward runtime.
// It mirrors the on-disk wardFile layout and is the only type that
// LocalConfigStore reads and writes. Application code converts to/from
// domain.LocalWardRuntime via the mapping package.
type RuntimeRecord struct {
	Version                int                           `json:"version"`
	Site                   string                        `json:"site"`
	WardDraftID            string                        `json:"ward_draft_id"`
	WardDraftSecret        string                        `json:"ward_draft_secret,omitempty"`
	WardID                 string                        `json:"ward_id"`
	WardSecret             string                        `json:"ward_secret,omitempty"`
	JWTSigningSecret       string                        `json:"jwt_signing_secret,omitempty"`
	WardStatus             string                        `json:"ward_status"`
	Spec                   string                        `json:"spec"`
	BillingMode            string                        `json:"billing_mode"`
	ActivationMode         string                        `json:"activation_mode"`
	DomainType             string                        `json:"domain_type"`
	RequestedDomain        string                        `json:"requested_domain,omitempty"`
	Domain                 string                        `json:"domain"`
	UpstreamAddr           string                        `json:"upstream_addr"`
	UpstreamPort           int                           `json:"upstream_port"`
	UpstreamMode           string                        `json:"upstream_mode"`
	UpstreamCommand        string                        `json:"upstream_command"`
	ListenAddr             string                        `json:"listen_addr,omitempty"` // Deprecated
	ListenPort             int                           `json:"listen_port"`
	ListenHost             string                        `json:"listen_host"`
	IngressFamily          string                        `json:"ingress_family"`
	IngressMode            string                        `json:"ingress_mode,omitempty"`
	ServeTLS               bool                          `json:"serve_tls"`
	PublicPort             int                           `json:"public_port,omitempty"`
	TrustedProxyCIDRs      []string                      `json:"trusted_proxy_cidrs,omitempty"`
	TLSMode                string                        `json:"tls_mode"`
	LastPublicIP           string                        `json:"last_public_ip"`
	LastPublicIPReportedAt *time.Time                    `json:"last_public_ip_reported_at,omitempty"`
	ExpiresAt              *time.Time                    `json:"expires_at,omitempty"`
	ActivationURL          string                        `json:"activation_url"`
	LastCertRenewedAt      *time.Time                    `json:"last_cert_renewed_at,omitempty"`
	LastRefreshedAt        *time.Time                    `json:"last_refreshed_at,omitempty"`
	PlatformJWTPublicKeys  []domain.PlatformJWTPublicKey `json:"platform_jwt_public_keys,omitempty"`
	AuthWhitelist          []domain.AuthWhitelistRule    `json:"auth_whitelist,omitempty"`
	UpdatedAt              time.Time                     `json:"updated_at"`
}

type LocalConfigStore interface {
	LoadWardRuntime(ctx context.Context) (*RuntimeRecord, error)
	SaveWardRuntime(ctx context.Context, runtime RuntimeRecord) error
	LoadPendingRuntime(ctx context.Context) (*RuntimeRecord, error)
	SavePendingRuntime(ctx context.Context, runtime RuntimeRecord) error
	CommitPendingRuntime(ctx context.Context, runtime RuntimeRecord) error
	// ListWardRuntimes returns all local runtimes (pending-config, drafts, wards)
	// without modifying any store state. Safe to call before selecting a target.
	ListWardRuntimes(ctx context.Context) ([]RuntimeRecord, error)
	// LoadRuntimeByID loads the runtime identified by a ward_id or ward_draft_id
	// and primes the store so subsequent SaveWardRuntime calls target the same
	// directory (preserving the rename behavior on draft→ward promotion).
	LoadRuntimeByID(ctx context.Context, id string) (*RuntimeRecord, error)
}
