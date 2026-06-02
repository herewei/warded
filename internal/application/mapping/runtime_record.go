package mapping

import (
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

// RuntimeRecordFromDomain converts a domain.LocalWardRuntime to a ports.RuntimeRecord.
func RuntimeRecordFromDomain(runtime *domain.LocalWardRuntime) ports.RuntimeRecord {
	if runtime == nil {
		return ports.RuntimeRecord{}
	}
	var lastPublicIPReportedAt, expiresAt, lastCertRenewedAt, lastRefreshedAt *time.Time
	if !runtime.LastPublicIPReportedAt.IsZero() {
		t := runtime.LastPublicIPReportedAt
		lastPublicIPReportedAt = &t
	}
	if !runtime.ExpiresAt.IsZero() {
		t := runtime.ExpiresAt
		expiresAt = &t
	}
	if !runtime.LastCertRenewedAt.IsZero() {
		t := runtime.LastCertRenewedAt
		lastCertRenewedAt = &t
	}
	if !runtime.LastRefreshedAt.IsZero() {
		t := runtime.LastRefreshedAt
		lastRefreshedAt = &t
	}
	return ports.RuntimeRecord{
		Version:                1,
		Site:                   string(runtime.Site),
		WardDraftID:            runtime.WardDraftID,
		WardDraftSecret:        runtime.WardDraftSecret,
		WardID:                 runtime.WardID,
		WardSecret:             runtime.WardSecret,
		JWTSigningSecret:       runtime.JWTSigningSecret,
		WardStatus:             string(runtime.WardStatus),
		Spec:                   string(runtime.Spec),
		BillingMode:            string(runtime.BillingMode),
		ActivationMode:         string(runtime.ActivationMode),
		DomainType:             string(runtime.DomainType),
		RequestedDomain:        runtime.RequestedDomain,
		Domain:                 runtime.Domain,
		UpstreamAddr:           runtime.UpstreamAddr,
		UpstreamPort:           runtime.UpstreamPort,
		UpstreamMode:           string(runtime.UpstreamMode),
		UpstreamCommand:        runtime.UpstreamCommand,
		ListenPort:             runtime.ListenPort,
		ListenHost:             runtime.ListenHost,
		IngressFamily:          string(runtime.IngressFamily),
		IngressMode:            string(runtime.IngressMode),
		ServeTLS:               runtime.ServeTLS,
		PublicPort:             runtime.PublicPort,
		TrustedProxyCIDRs:      append([]string(nil), runtime.TrustedProxyCIDRs...),
		TLSMode:                string(runtime.TLSMode),
		LastPublicIP:           runtime.LastPublicIP,
		LastPublicIPReportedAt: lastPublicIPReportedAt,
		ExpiresAt:              expiresAt,
		ActivationURL:          runtime.ActivationURL,
		LastCertRenewedAt:      lastCertRenewedAt,
		LastRefreshedAt:        lastRefreshedAt,
		PlatformJWTPublicKeys:  append([]domain.PlatformJWTPublicKey(nil), runtime.PlatformJWTPublicKeys...),
		AuthWhitelist:          append([]domain.AuthWhitelistRule(nil), runtime.AuthWhitelist...),
		UpdatedAt:              runtime.UpdatedAt,
	}
}

// DomainFromRuntimeRecord converts a ports.RuntimeRecord to a domain.LocalWardRuntime.
func DomainFromRuntimeRecord(record *ports.RuntimeRecord) *domain.LocalWardRuntime {
	if record == nil {
		return nil
	}
	runtime := &domain.LocalWardRuntime{
		Site:              domain.Site(record.Site),
		WardDraftID:       record.WardDraftID,
		WardDraftSecret:   record.WardDraftSecret,
		WardID:            record.WardID,
		WardSecret:        record.WardSecret,
		JWTSigningSecret:  record.JWTSigningSecret,
		WardStatus:        domain.WardStatus(record.WardStatus),
		Spec:              domain.Spec(record.Spec),
		BillingMode:       domain.BillingMode(record.BillingMode),
		ActivationMode:    domain.ActivationMode(record.ActivationMode),
		DomainType:        domain.DomainType(record.DomainType),
		RequestedDomain:   record.RequestedDomain,
		Domain:            record.Domain,
		UpstreamAddr:      record.UpstreamAddr,
		UpstreamPort:      record.UpstreamPort,
		UpstreamMode:      domain.UpstreamMode(record.UpstreamMode),
		UpstreamCommand:   record.UpstreamCommand,
		ListenPort:        record.ListenPort,
		ListenHost:        record.ListenHost,
		IngressFamily:     domain.IngressFamily(record.IngressFamily),
		IngressMode:       domain.IngressMode(record.IngressMode),
		ServeTLS:          record.ServeTLS,
		PublicPort:        record.PublicPort,
		TrustedProxyCIDRs: append([]string(nil), record.TrustedProxyCIDRs...),
		TLSMode:           domain.TLSMode(record.TLSMode),
		LastPublicIP:      record.LastPublicIP,
		ActivationURL:     record.ActivationURL,
		UpdatedAt:         record.UpdatedAt,
	}
	if record.LastPublicIPReportedAt != nil {
		runtime.LastPublicIPReportedAt = *record.LastPublicIPReportedAt
	}
	if record.ExpiresAt != nil {
		runtime.ExpiresAt = *record.ExpiresAt
	}
	if record.LastCertRenewedAt != nil {
		runtime.LastCertRenewedAt = *record.LastCertRenewedAt
	}
	if record.LastRefreshedAt != nil {
		runtime.LastRefreshedAt = *record.LastRefreshedAt
	}
	if record.PlatformJWTPublicKeys != nil {
		runtime.PlatformJWTPublicKeys = append([]domain.PlatformJWTPublicKey(nil), record.PlatformJWTPublicKeys...)
	}
	if record.AuthWhitelist != nil {
		runtime.AuthWhitelist = append([]domain.AuthWhitelistRule(nil), record.AuthWhitelist...)
	}
	return runtime
}
