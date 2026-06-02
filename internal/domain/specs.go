package domain

import (
	"fmt"
	"strings"
)

const DefaultUpstreamAddr = "127.0.0.1:18789"

// UpstreamSpec is a value-object view of the upstream configuration.
type UpstreamSpec struct {
	Addr    string
	Port    int
	Mode    UpstreamMode
	Command string
}

// EffectiveAddr returns the resolved upstream address, falling back to a
// loopback host derived from Port, then the platform default.
func (s UpstreamSpec) EffectiveAddr() string {
	if addr := strings.TrimSpace(s.Addr); addr != "" {
		return addr
	}
	if s.Port > 0 {
		return fmt.Sprintf("127.0.0.1:%d", s.Port)
	}
	return DefaultUpstreamAddr
}

// UpstreamSpec returns a read-only view of the upstream configuration.
func (r *LocalWardRuntime) UpstreamSpec() UpstreamSpec {
	if r == nil {
		return UpstreamSpec{}
	}
	return UpstreamSpec{
		Addr:    r.UpstreamAddr,
		Port:    r.UpstreamPort,
		Mode:    r.UpstreamMode,
		Command: r.UpstreamCommand,
	}
}

// ApplyUpstreamSpec writes the upstream configuration back to the runtime.
func (r *LocalWardRuntime) ApplyUpstreamSpec(spec UpstreamSpec) {
	if r == nil {
		return
	}
	r.UpstreamAddr = spec.Addr
	r.UpstreamPort = spec.Port
	r.UpstreamMode = spec.Mode
	r.UpstreamCommand = spec.Command
}

// IngressSpec is a value-object view of the ingress configuration.
type IngressSpec struct {
	Mode              IngressMode
	ServeTLS          bool
	ListenPort        int
	ListenHost        string
	Family            IngressFamily
	PublicPort        int
	TrustedProxyCIDRs []string
}

// EffectiveMode returns the ingress mode with the CLI default applied.
func (s IngressSpec) EffectiveMode() IngressMode {
	if s.Mode == "" {
		return IngressModeStandalone
	}
	return s.Mode
}

// EffectiveServeTLS returns whether the runtime should terminate TLS locally.
func (s IngressSpec) EffectiveServeTLS() bool {
	return s.EffectiveMode() != IngressModeBehindProxy
}

// EffectiveListenPort returns the listener port with the CLI default applied.
func (s IngressSpec) EffectiveListenPort() int {
	if s.ListenPort > 0 {
		return s.ListenPort
	}
	return 443
}

// EffectiveListenHost returns the listener host with the CLI default applied.
func (s IngressSpec) EffectiveListenHost() string {
	if host := strings.TrimSpace(s.ListenHost); host != "" {
		return host
	}
	return "0.0.0.0"
}

// EffectiveFamily returns the ingress address family with the CLI default applied.
func (s IngressSpec) EffectiveFamily() IngressFamily {
	if s.Family != "" {
		return s.Family
	}
	return IngressFamilyIPv4
}

// EffectivePublicPort returns the public port with ingress-mode defaults applied.
func (s IngressSpec) EffectivePublicPort() int {
	if s.PublicPort > 0 {
		return s.PublicPort
	}
	if s.EffectiveMode() == IngressModeStandalone {
		return s.EffectiveListenPort()
	}
	return 443
}

// WithEffectiveDefaults returns a copy with derived ingress defaults materialized.
func (s IngressSpec) WithEffectiveDefaults() IngressSpec {
	s.Mode = s.EffectiveMode()
	s.ListenPort = s.EffectiveListenPort()
	s.ListenHost = s.EffectiveListenHost()
	s.Family = s.EffectiveFamily()
	s.ServeTLS = s.EffectiveServeTLS()
	s.PublicPort = s.EffectivePublicPort()
	return s
}

// IngressSpec returns a read-only view of the ingress configuration.
func (r *LocalWardRuntime) IngressSpec() IngressSpec {
	if r == nil {
		return IngressSpec{}
	}
	return IngressSpec{
		Mode:              r.IngressMode,
		ServeTLS:          r.ServeTLS,
		ListenPort:        r.ListenPort,
		ListenHost:        r.ListenHost,
		Family:            r.IngressFamily,
		PublicPort:        r.PublicPort,
		TrustedProxyCIDRs: append([]string(nil), r.TrustedProxyCIDRs...),
	}
}

// ApplyIngressSpec writes the ingress configuration back to the runtime.
func (r *LocalWardRuntime) ApplyIngressSpec(spec IngressSpec) {
	if r == nil {
		return
	}
	r.IngressMode = spec.Mode
	r.ServeTLS = spec.ServeTLS
	r.ListenPort = spec.ListenPort
	r.ListenHost = spec.ListenHost
	r.IngressFamily = spec.Family
	r.PublicPort = spec.PublicPort
	r.TrustedProxyCIDRs = append([]string(nil), spec.TrustedProxyCIDRs...)
}

// DomainSpec is a value-object view of the domain configuration.
type DomainSpec struct {
	Type            DomainType
	RequestedDomain string
	Domain          string
	TLSMode         TLSMode
}

// DomainSpec returns a read-only view of the domain configuration.
func (r *LocalWardRuntime) DomainSpec() DomainSpec {
	if r == nil {
		return DomainSpec{}
	}
	return DomainSpec{
		Type:            r.DomainType,
		RequestedDomain: r.RequestedDomain,
		Domain:          r.Domain,
		TLSMode:         r.TLSMode,
	}
}

// ApplyDomainSpec writes the domain configuration back to the runtime.
func (r *LocalWardRuntime) ApplyDomainSpec(spec DomainSpec) {
	if r == nil {
		return
	}
	r.DomainType = spec.Type
	r.RequestedDomain = spec.RequestedDomain
	r.Domain = spec.Domain
	r.TLSMode = spec.TLSMode
}
