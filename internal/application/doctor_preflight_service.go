package application

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type DoctorPreflightService struct {
	DataDirCheck   DataDirWritableChecker
	ListenResolver DoctorListenResolver
	ListenCheck    ListenAddressChecker
	UpstreamCheck  ports.UpstreamChecker
	DNSResolver    DNSResolver
	ChallengeGen   ProbeChallengeGenerator
	ProbeServer    ProbeServer
	IngressProbe   IngressProbeClientFactory
}

type DataDirWritableChecker interface {
	EnsureWritable(path string) error
}

type DoctorListenResolver interface {
	ResolveListen(existing *domain.LocalWardRuntime, listenHost, listenV6Host string, listenPort int, listenChanged, listenV6Changed, portChanged bool) (string, int, domain.IngressFamily, error)
}

type ListenAddressChecker interface {
	EnsureAvailable(addr string) error
}

type DNSResolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

type ProbeChallengeGenerator interface {
	GenerateProbeChallenge() (string, error)
}

type ProbeServer interface {
	StartProbeServer(ctx context.Context, addr string) (func(context.Context) error, error)
}

type IngressProbeClientFactory interface {
	NewIngressProbeAPI(site domain.Site, baseDomain, platformOrigin, version string) (ports.IngressProbeAPI, error)
}

type DoctorPreflightInput struct {
	DataDir         string
	Site            string
	ListenHost      string
	ListenV6Host    string
	ListenPort      int
	UpstreamAddr    string
	DomainType      string
	RequestedDomain string
	BaseDomain      string
	PlatformOrigin  string
	Version         string
	ListenChanged   bool
	ListenV6Changed bool
	PortChanged     bool
}

type DoctorPreflightOutput struct {
	Site             domain.Site
	ListenHost       string
	ListenPort       int
	IngressFamily    domain.IngressFamily
	UpstreamAddr     string
	DomainType       domain.DomainType
	RequestedDomain  string
	ResolvedPublicIP string
	ProbeReason      string
	Results          []CheckResult
}

func (s DoctorPreflightService) Execute(ctx context.Context, input DoctorPreflightInput) (*DoctorPreflightOutput, error) {
	out := &DoctorPreflightOutput{}
	if s.DataDirCheck == nil {
		return out, fmt.Errorf("doctor preflight service: data-dir checker is required")
	}
	if s.ListenResolver == nil {
		return out, fmt.Errorf("doctor preflight service: listen resolver is required")
	}
	if s.ListenCheck == nil {
		return out, fmt.Errorf("doctor preflight service: listen checker is required")
	}
	if s.UpstreamCheck == nil {
		return out, fmt.Errorf("doctor preflight service: upstream checker is required")
	}
	if s.ChallengeGen == nil {
		return out, fmt.Errorf("doctor preflight service: challenge generator is required")
	}
	if s.ProbeServer == nil {
		return out, fmt.Errorf("doctor preflight service: probe server is required")
	}
	if s.IngressProbe == nil {
		return out, fmt.Errorf("doctor preflight service: ingress probe client factory is required")
	}

	siteValue := strings.TrimSpace(input.Site)
	if siteValue != string(domain.SiteCN) && siteValue != string(domain.SiteGlobal) {
		err := fmt.Errorf("--site is required: must be cn (warded.cn) or global (warded.me)")
		out.Results = append(out.Results, failPreflightValidation("site", "site is required"))
		return out, err
	}
	out.Site = domain.Site(siteValue)

	dt := domain.DomainType(strings.TrimSpace(input.DomainType))
	if dt == "" {
		dt = domain.DomainTypePlatformSubdomain
	}
	if dt != domain.DomainTypePlatformSubdomain && dt != domain.DomainTypeCustomDomain {
		err := fmt.Errorf("invalid --domain-type: %s (must be platform_subdomain or custom_domain)", input.DomainType)
		out.Results = append(out.Results, failPreflightValidation("domain_type", "domain type is invalid"))
		return out, err
	}
	if dt == domain.DomainTypeCustomDomain && strings.TrimSpace(input.RequestedDomain) == "" {
		err := fmt.Errorf("--domain is required when --domain-type custom_domain")
		out.Results = append(out.Results, failPreflightValidation("requested_domain", "custom domain is required"))
		return out, err
	}
	out.DomainType = dt
	out.RequestedDomain = strings.TrimSpace(input.RequestedDomain)

	if err := s.DataDirCheck.EnsureWritable(input.DataDir); err != nil {
		out.appendFail("data_dir_writable", input.DataDir)
		return out, err
	}
	out.appendOK("data_dir_writable", input.DataDir)

	listenHost, listenPort, ingressFamily, err := s.ListenResolver.ResolveListen(nil, input.ListenHost, input.ListenV6Host, input.ListenPort, input.ListenChanged, input.ListenV6Changed, input.PortChanged)
	if err != nil {
		out.appendFail("listen_available", err.Error())
		return out, err
	}
	out.ListenHost = listenHost
	out.ListenPort = listenPort
	out.IngressFamily = ingressFamily
	listenAddr := formatListenAddr(listenHost, listenPort, ingressFamily)
	if err := s.ListenCheck.EnsureAvailable(listenAddr); err != nil {
		out.appendFail("listen_available", listenAddr)
		return out, err
	}
	out.appendOK("listen_available", fmt.Sprintf("listen %s available", listenAddr))

	upstreamAddr := normalizeUpstreamAddr(input.UpstreamAddr)
	if err := validatePreflightUpstreamAddr(upstreamAddr); err != nil {
		out.appendFail("upstream_reachable", err.Error())
		return out, err
	}
	out.UpstreamAddr = upstreamAddr
	if err := s.UpstreamCheck.Check(ctx, upstreamAddr); err != nil {
		wrapped := fmt.Errorf("%w: %v", ErrUpstreamUnreachable, err)
		out.appendFail("upstream_reachable", fmt.Sprintf("upstream %s is not reachable", upstreamAddr))
		return out, wrapped
	}
	out.appendOK("upstream_reachable", fmt.Sprintf("upstream %s reachable", upstreamAddr))

	if dt == domain.DomainTypeCustomDomain {
		if s.DNSResolver == nil {
			return out, fmt.Errorf("doctor preflight service: DNS resolver is required")
		}
		if _, err := s.DNSResolver.LookupHost(ctx, out.RequestedDomain); err != nil {
			out.appendFail("dns_resolves", "DNS lookup failed")
			return out, fmt.Errorf("DNS lookup failed for %s: %w", out.RequestedDomain, err)
		}
		out.appendOK("dns_resolves", fmt.Sprintf("DNS resolved for %s", out.RequestedDomain))
	}

	probeChallenge, err := s.ChallengeGen.GenerateProbeChallenge()
	if err != nil {
		out.appendFail("probe_handler", err.Error())
		return out, err
	}
	stopProbe, err := s.ProbeServer.StartProbeServer(ctx, listenAddr)
	if err != nil {
		out.appendFail("probe_handler", listenAddr)
		return out, err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = stopProbe(shutdownCtx)
	}()
	out.appendOK("probe_handler", "temporary probe handler started")

	platform, err := s.IngressProbe.NewIngressProbeAPI(out.Site, input.BaseDomain, input.PlatformOrigin, input.Version)
	if err != nil {
		out.appendFail("ingress_probe", err.Error())
		return out, err
	}
	resp, err := platform.CreateIngressProbe(ctx, ports.IngressProbeRequest{
		Site:            string(out.Site),
		ListenHost:      out.ListenHost,
		ListenPort:      out.ListenPort,
		IngressFamily:   string(out.IngressFamily),
		DomainType:      string(out.DomainType),
		RequestedDomain: out.RequestedDomain,
		ProbeChallenge:  probeChallenge,
	})
	if err != nil {
		out.appendFail("ingress_probe", "could not reach this host from the public internet")
		return out, err
	}
	out.ResolvedPublicIP = resp.ResolvedPublicIP
	out.ProbeReason = resp.Reason
	if resp.Result != "reachable" {
		err := &ports.PlatformError{Code: "ingress_unreachable", Reason: resp.Reason, HTTPStatus: 422, RequestID: resp.RequestID}
		out.appendFail("ingress_probe", "could not reach this host from the public internet")
		return out, err
	}
	detail := "public internet can reach this host"
	if resp.ResolvedPublicIP != "" {
		detail = "public IP: " + resp.ResolvedPublicIP
	}
	out.appendOK("ingress_probe", detail)
	return out, nil
}

func formatListenAddr(host string, port int, family domain.IngressFamily) string {
	if family == domain.IngressFamilyIPv6 {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func (out *DoctorPreflightOutput) appendOK(key, detail string) {
	out.Results = append(out.Results, preflightCheck(len(out.Results)+1, key, CheckOK, detail))
}

func (out *DoctorPreflightOutput) appendFail(key, detail string) {
	out.Results = append(out.Results, preflightCheck(len(out.Results)+1, key, CheckFAIL, detail))
}

func failPreflightValidation(key, detail string) CheckResult {
	return preflightCheck(0, key, CheckFAIL, detail)
}

func preflightCheck(number int, key string, state CheckState, detail string) CheckResult {
	return CheckResult{Number: number, Key: key, Name: strings.ReplaceAll(key, "_", " "), State: state, Detail: detail}
}

func validatePreflightUpstreamAddr(input string) error {
	if input == "" {
		return nil
	}
	input = strings.TrimSpace(input)
	if port, err := strconv.Atoi(input); err == nil {
		if port <= 0 || port > 65535 {
			return fmt.Errorf("invalid upstream address %q: port must be a number between 1 and 65535", input)
		}
		return nil
	}
	if strings.HasPrefix(input, ":") {
		portStr := strings.TrimPrefix(input, ":")
		port, err := strconv.Atoi(portStr)
		if err != nil || port <= 0 || port > 65535 {
			return fmt.Errorf("invalid upstream address %q: port must be a number between 1 and 65535", input)
		}
		return nil
	}
	if !strings.Contains(input, ":") {
		return fmt.Errorf("invalid upstream address %q: must be in format host:port, :port, or just port number", input)
	}
	_, portStr, err := net.SplitHostPort(input)
	if err != nil {
		return fmt.Errorf("invalid upstream address %q: %w", input, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid upstream address %q: port must be a number between 1 and 65535", input)
	}
	return nil
}
