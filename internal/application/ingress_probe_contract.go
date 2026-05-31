package application

import (
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

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

func ApplyIngressProbeContractToDraftRequest(req *ports.CreateWardDraftRequest, contract IngressProbeContract, challenge string) {
	req.IngressMode = string(contract.IngressMode)
	req.ServeTLS = contract.IngressMode != domain.IngressModeBehindProxy
	req.ListenHost = contract.ListenHost
	req.ListenPort = contract.ListenPort
	req.IngressFamily = string(contract.IngressFamily)
	req.PublicPort = contract.PublicPort
	req.DomainType = string(contract.DomainType)
	req.RequestedDomain = contract.RequestedDomain
	req.ProbeChallenge = challenge
}
