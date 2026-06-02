package mapping

import (
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

// IngressProbeContract carries the parameters needed to build an ingress probe request
// and to populate ingress-related fields in a draft request.
type IngressProbeContract struct {
	Site            domain.Site
	IngressMode     domain.IngressMode
	ListenHost      string
	ListenPort      int
	IngressFamily   domain.IngressFamily
	PublicPort      int
	DomainType      domain.DomainType
	RequestedDomain string
}

// CreateWardDraftRequestParams carries the parameters needed to build a CreateWardDraftRequest.
type CreateWardDraftRequestParams struct {
	Site                 domain.Site
	Spec                 domain.Spec
	BillingMode          domain.BillingMode
	UpstreamAddr         string
	UpstreamPort         int
	UpstreamMode         domain.UpstreamMode
	UpstreamCommand      string
	TrustedProxyCIDRs    []string
	DraftSecretChallenge string
	Ingress              IngressProbeContract
	Challenge            string
}

// BuildIngressProbeRequest maps an IngressProbeContract to a platform IngressProbeRequest.
func BuildIngressProbeRequest(contract IngressProbeContract, challenge string) ports.IngressProbeRequest {
	return ports.IngressProbeRequest{
		Site:            string(contract.Site),
		IngressMode:     string(contract.IngressMode),
		ListenHost:      contract.ListenHost,
		ListenPort:      contract.ListenPort,
		IngressFamily:   string(contract.IngressFamily),
		PublicPort:      contract.PublicPort,
		DomainType:      string(contract.DomainType),
		RequestedDomain: contract.RequestedDomain,
		ProbeChallenge:  challenge,
	}
}

// BuildCreateWardDraftRequest builds a CreateWardDraftRequest from service inputs,
// runtime state, and the ingress probe contract.
func BuildCreateWardDraftRequest(params CreateWardDraftRequestParams) ports.CreateWardDraftRequest {
	req := ports.CreateWardDraftRequest{
		Site:                 string(params.Site),
		Mode:                 "new",
		Spec:                 string(params.Spec),
		BillingMode:          string(params.BillingMode),
		UpstreamAddr:         params.UpstreamAddr,
		UpstreamPort:         params.UpstreamPort,
		UpstreamMode:         string(params.UpstreamMode),
		UpstreamCommand:      params.UpstreamCommand,
		TrustedProxyCIDRs:    append([]string(nil), params.TrustedProxyCIDRs...),
		DraftSecretChallenge: params.DraftSecretChallenge,
	}
	req.IngressMode = string(params.Ingress.IngressMode)
	req.ServeTLS = params.Ingress.IngressMode != domain.IngressModeBehindProxy
	req.ListenHost = params.Ingress.ListenHost
	req.ListenPort = params.Ingress.ListenPort
	req.IngressFamily = string(params.Ingress.IngressFamily)
	req.PublicPort = params.Ingress.PublicPort
	req.DomainType = string(params.Ingress.DomainType)
	req.RequestedDomain = params.Ingress.RequestedDomain
	req.ProbeChallenge = params.Challenge
	return req
}
