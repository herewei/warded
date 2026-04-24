package domain

import "time"

type Site string

const (
	SiteCN     Site = "cn"
	SiteGlobal Site = "global"
)

type WardStatus string

const (
	WardStatusInitializing WardStatus = "initializing"
	WardStatusActive       WardStatus = "active"
	WardStatusExpired      WardStatus = "expired"
	WardStatusSuspended    WardStatus = "suspended"
	WardStatusDeleted      WardStatus = "deleted"
)

type DomainType string

const (
	DomainTypePlatformSubdomain DomainType = "platform_subdomain"
	DomainTypeCustomDomain      DomainType = "custom_domain"
)

type TLSMode string

const (
	TLSModePlatformWildcard TLSMode = "platform_wildcard"
	TLSModeLocalACME        TLSMode = "local_acme"
)

type Spec string

const (
	SpecStarter Spec = "starter"
	SpecPro     Spec = "pro"
)

type BillingMode string

const (
	BillingModeMonthly BillingMode = "monthly"
	BillingModeYearly  BillingMode = "yearly"
)

type ActivationMode string

const (
	ActivationModePaid  ActivationMode = "paid"
	ActivationModeTrial ActivationMode = "trial"
)

type LocalWardRuntime struct {
	Site                   Site
	WardDraftID            string
	WardDraftSecret        string
	WardID                 string
	WardSecret             string
	JWTSigningSecret       string
	WardStatus             WardStatus
	Spec                   Spec
	BillingMode            BillingMode
	ActivationMode         ActivationMode
	DomainType             DomainType
	RequestedDomain        string
	Domain                 string
	UpstreamPort           int
	ListenAddr             string
	TLSMode                TLSMode
	LastPublicIP           string
	LastPublicIPReportedAt time.Time
	ExpiresAt              time.Time
	LastCertRenewedAt      time.Time
	ActivationURL          string
	WebhookAllowPaths      []string
	UpdatedAt              time.Time
}

type LocalSession struct {
	SessionID   string
	PrincipalID string
	WardID      string
	ExpiresAt   time.Time
}
