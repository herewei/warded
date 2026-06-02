package mapping

import (
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

// ApplyTerminalWardStatus sets the runtime to a terminal status with refreshed timestamps.
func ApplyTerminalWardStatus(runtime *domain.LocalWardRuntime, status domain.WardStatus, now time.Time) {
	runtime.WardStatus = status
	runtime.LastRefreshedAt = now
	runtime.UpdatedAt = now
}

// ApplyGetWardResponseToStatus updates the runtime fields from a GetWard platform response.
// It handles the union of fields consumed by status_service.go and cmd/serve.go.
// It does not call store.Save.
func ApplyGetWardResponseToStatus(
	runtime *domain.LocalWardRuntime,
	resp *ports.GetWardResponse,
	now time.Time,
) {
	if resp.Status != "" {
		runtime.WardStatus = domain.WardStatus(resp.Status)
	}
	runtime.Spec = domain.Spec(resp.Spec)
	runtime.BillingMode = domain.BillingMode(resp.BillingMode)
	runtime.ActivationMode = domain.ActivationMode(resp.ActivationMode)

	upstreamMode := domain.UpstreamMode(resp.UpstreamMode)
	if resp.UpstreamMode == "" {
		upstreamMode = runtime.UpstreamMode
	}
	upstreamAddr := resp.UpstreamAddr
	if upstreamAddr == "" {
		upstreamAddr = runtime.UpstreamAddr
	}
	runtime.ApplyUpstreamSpec(domain.UpstreamSpec{
		Addr:    upstreamAddr,
		Port:    resp.UpstreamPort,
		Mode:    upstreamMode,
		Command: resp.UpstreamCommand,
	})

	ingressMode := domain.IngressMode(resp.IngressMode)
	if resp.IngressMode == "" {
		ingressMode = runtime.IngressMode
	}
	ingressFamily := domain.IngressFamily(resp.IngressFamily)
	if resp.IngressFamily == "" {
		ingressFamily = runtime.IngressFamily
	}
	listenPort := resp.ListenPort
	if listenPort <= 0 {
		listenPort = runtime.ListenPort
	}
	listenHost := resp.ListenHost
	if listenHost == "" {
		listenHost = runtime.ListenHost
	}
	publicPort := resp.PublicPort
	if publicPort <= 0 {
		publicPort = runtime.PublicPort
	}
	trustedProxyCIDRs := resp.TrustedProxyCIDRs
	if trustedProxyCIDRs == nil {
		trustedProxyCIDRs = runtime.TrustedProxyCIDRs
	}
	runtime.ApplyIngressSpec(domain.IngressSpec{
		Mode:              ingressMode,
		ServeTLS:          ingressMode != domain.IngressModeBehindProxy,
		ListenPort:        listenPort,
		ListenHost:        listenHost,
		Family:            ingressFamily,
		PublicPort:        publicPort,
		TrustedProxyCIDRs: trustedProxyCIDRs,
	})

	domainType := domain.DomainType(resp.DomainType)
	if domainType == "" {
		domainType = runtime.DomainType
	}
	domainValue := resp.Domain
	if domainValue == "" {
		domainValue = runtime.Domain
	}
	// GetWardResponse doesn't include TLSMode or RequestedDomain, so preserve existing values
	runtime.ApplyDomainSpec(domain.DomainSpec{
		Type:            domainType,
		Domain:          domainValue,
		TLSMode:         runtime.TLSMode,
		RequestedDomain: runtime.RequestedDomain,
	})
	if resp.ExpiresAt != "" {
		if expiresAt, err := time.Parse(time.RFC3339, resp.ExpiresAt); err == nil {
			runtime.ExpiresAt = expiresAt
		}
	}
	if resp.PlatformJWTPublicKeys != nil {
		runtime.PlatformJWTPublicKeys = resp.PlatformJWTPublicKeys
	}
	runtime.LastRefreshedAt = now
	runtime.UpdatedAt = now
}
