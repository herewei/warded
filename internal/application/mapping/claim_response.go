package mapping

import (
	"fmt"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

// ApplyClaimAndWardResponse populates the runtime from a successful claim response
// and the follow-up GetWard response. It derives TLSMode and does not call store.Save.
func ApplyClaimAndWardResponse(
	runtime *domain.LocalWardRuntime,
	claimed *ports.ClaimWardDraftResponse,
	ward *ports.GetWardResponse,
	now time.Time,
) error {
	if runtime == nil {
		return fmt.Errorf("ApplyClaimAndWardResponse: runtime is required")
	}
	if claimed == nil || ward == nil {
		return fmt.Errorf("ApplyClaimAndWardResponse: claimed and ward responses are required")
	}
	if claimed.WardID == "" || claimed.WardSecret == "" {
		return fmt.Errorf("ApplyClaimAndWardResponse: claim response is missing ward credentials")
	}

	runtime.WardID = claimed.WardID
	runtime.WardSecret = claimed.WardSecret
	runtime.WardDraftSecret = ""
	runtime.WardStatus = domain.WardStatus(ward.Status)
	runtime.BillingMode = domain.BillingMode(ward.BillingMode)
	runtime.ActivationMode = domain.ActivationMode(ward.ActivationMode)
	if ward.PlatformJWTPublicKeys != nil {
		runtime.PlatformJWTPublicKeys = ward.PlatformJWTPublicKeys
	} else if claimed.PlatformJWTPublicKeys != nil {
		runtime.PlatformJWTPublicKeys = claimed.PlatformJWTPublicKeys
	}
	if runtime.Site == "" {
		runtime.Site = domain.Site(ward.Site)
	}

	var err error
	tlsMode, err := domain.TLSModeForDomainType(domain.DomainType(ward.DomainType))
	if err != nil {
		return fmt.Errorf("ApplyClaimAndWardResponse: %w", err)
	}
	runtime.ApplyDomainSpec(domain.DomainSpec{
		Type:            domain.DomainType(ward.DomainType),
		Domain:          ward.Domain,
		TLSMode:         tlsMode,
		RequestedDomain: runtime.RequestedDomain,
	})
	upstreamMode := domain.UpstreamMode(ward.UpstreamMode)
	if ward.UpstreamMode == "" {
		upstreamMode = runtime.UpstreamMode
	}
	runtime.ApplyUpstreamSpec(domain.UpstreamSpec{
		Addr:    ward.UpstreamAddr,
		Port:    ward.UpstreamPort,
		Mode:    upstreamMode,
		Command: ward.UpstreamCommand,
	})
	ingressMode := domain.IngressMode(ward.IngressMode)
	if ward.IngressMode == "" {
		ingressMode = runtime.IngressMode
	}
	ingressFamily := domain.IngressFamily(ward.IngressFamily)
	if ward.IngressFamily == "" {
		ingressFamily = runtime.IngressFamily
	}
	listenPort := ward.ListenPort
	if listenPort <= 0 {
		listenPort = runtime.ListenPort
	}
	listenHost := ward.ListenHost
	if listenHost == "" {
		listenHost = runtime.ListenHost
	}
	publicPort := ward.PublicPort
	if publicPort <= 0 {
		publicPort = runtime.PublicPort
	}
	trustedProxyCIDRs := ward.TrustedProxyCIDRs
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
	if expiresAt, err := time.Parse(time.RFC3339, ward.ExpiresAt); err == nil {
		runtime.ExpiresAt = expiresAt
	}
	runtime.ActivationURL = ""
	runtime.UpdatedAt = now
	return nil
}
