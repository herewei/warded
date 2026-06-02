package mapping

import (
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

// ApplyCreateDraftResponseParams carries the parameters needed to apply a CreateWardDraftResponse.
type ApplyCreateDraftResponseParams struct {
	Runtime           *domain.LocalWardRuntime
	Response          *ports.CreateWardDraftResponse
	Site              domain.Site
	Spec              domain.Spec
	BillingMode       domain.BillingMode
	DomainType        domain.DomainType
	UpstreamAddr      string
	UpstreamPort      int
	UpstreamMode      domain.UpstreamMode
	UpstreamCommand   string
	IngressMode       domain.IngressMode
	ServeTLS          bool
	ListenPort        int
	ListenHost        string
	IngressFamily     domain.IngressFamily
	PublicPort        int
	TrustedProxyCIDRs []string
	ActivationURL     string
	TLSMode           domain.TLSMode
	Now               time.Time
}

// ApplyCreateDraftResponse updates the runtime fields after a successful
// create-or-update draft call. It does not call store.Save.
func ApplyCreateDraftResponse(params ApplyCreateDraftResponseParams) {
	if params.Runtime.WardDraftID == "" {
		params.Runtime.WardDraftID = params.Response.WardDraftID
		params.Runtime.WardStatus = domain.WardStatusInitializing
	}
	params.Runtime.Spec = params.Spec
	params.Runtime.BillingMode = params.BillingMode
	params.Runtime.ApplyDomainSpec(domain.DomainSpec{
		Type:            params.DomainType,
		RequestedDomain: params.Response.RequestedDomain,
		TLSMode:         params.TLSMode,
	})
	params.Runtime.ApplyUpstreamSpec(domain.UpstreamSpec{
		Addr:    params.UpstreamAddr,
		Port:    params.UpstreamPort,
		Mode:    params.UpstreamMode,
		Command: params.UpstreamCommand,
	})
	params.Runtime.ApplyIngressSpec(domain.IngressSpec{
		Mode:              params.IngressMode,
		ServeTLS:          params.ServeTLS,
		ListenPort:        params.ListenPort,
		ListenHost:        params.ListenHost,
		Family:            params.IngressFamily,
		PublicPort:        params.PublicPort,
		TrustedProxyCIDRs: params.TrustedProxyCIDRs,
	})
	params.Runtime.ActivationURL = params.ActivationURL
	params.Runtime.LastPublicIP = params.Response.ResolvedPublicIP
	params.Runtime.Site = params.Site
	params.Runtime.UpdatedAt = params.Now
}
