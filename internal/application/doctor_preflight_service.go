package application

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/herewei/warded/internal/application/mapping"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

var ErrUnsupportedIngressDomainType = errors.New("unsupported ingress domain type")

type DoctorPreflightService struct {
	DataDirCheck    DataDirWritableChecker
	ListenResolver  DoctorListenResolver
	ListenCheck     ListenAddressChecker
	UpstreamCheck   ports.UpstreamChecker
	UpstreamStarter ports.UpstreamProcessManager
	DNSResolver     DNSResolver
	ChallengeGen    ProbeChallengeGenerator
	ProbeServer     ProbeServer
	IngressProbe    IngressProbeClientFactory
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
	StartProbeServer(ctx context.Context, addr string, serveTLS bool) (func(context.Context) error, error)
}

type IngressProbeClientFactory interface {
	NewIngressProbeAPI(site domain.Site, baseDomain, platformOrigin, version string) (ports.IngressProbeAPI, error)
}

type DoctorPreflightInput struct {
	DataDir           string
	Site              string
	ListenHost        string
	ListenV6Host      string
	ListenPort        int
	IngressMode       string
	PublicPort        int
	TrustedProxyCIDRs []string
	UpstreamAddr      string
	UpstreamMode      string
	UpstreamCommand   string
	DomainType        string
	RequestedDomain   string
	BaseDomain        string
	PlatformOrigin    string
	Version           string
	ListenChanged     bool
	ListenV6Changed   bool
	PortChanged       bool
}

type DoctorPreflightOutput struct {
	Site              domain.Site
	ListenHost        string
	ListenPort        int
	IngressFamily     domain.IngressFamily
	IngressMode       domain.IngressMode
	ServeTLS          bool
	PublicPort        int
	TrustedProxyCIDRs []string
	UpstreamAddr      string
	UpstreamMode      domain.UpstreamMode
	UpstreamCommand   string
	DomainType        domain.DomainType
	RequestedDomain   string
	ResolvedPublicIP  string
	ProbeReason       string
	ProbeRequestID    string
	PublicProbeURL    string
	Results           []CheckResult
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
	ingressMode, err := parsePreflightIngressMode(input.IngressMode)
	if err != nil {
		out.Results = append(out.Results, failPreflightValidation("ingress_mode", "ingress mode is invalid"))
		return out, err
	}
	out.IngressMode = ingressMode
	out.ServeTLS = ingressMode != domain.IngressModeBehindProxy
	if err := validateIngressDomainCombination(ingressMode, dt); err != nil {
		out.Results = append(out.Results, failPreflightValidation("ingress_mode", err.Error()))
		return out, err
	}

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
	publicPort := input.PublicPort
	if publicPort <= 0 {
		if ingressMode == domain.IngressModeStandalone {
			publicPort = listenPort
		} else {
			publicPort = 443
		}
	}
	out.PublicPort = publicPort
	out.TrustedProxyCIDRs = append([]string(nil), input.TrustedProxyCIDRs...)
	out.PublicProbeURL = preflightPublicProbeURL(out.RequestedDomain, publicPort)
	if err := validatePreflightIngressConfig(ingressMode, publicPort, listenPort, listenHost, out.TrustedProxyCIDRs); err != nil {
		out.appendFail("ingress_mode", err.Error())
		return out, err
	}
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
	upstreamMode := domain.UpstreamMode(strings.TrimSpace(input.UpstreamMode))
	if upstreamMode == "" {
		upstreamMode = domain.UpstreamModeDaemon
	}
	out.UpstreamMode = upstreamMode
	out.UpstreamCommand = strings.TrimSpace(input.UpstreamCommand)

	if upstreamMode == domain.UpstreamModeManaged {
		if out.UpstreamCommand == "" {
			err := fmt.Errorf("--upstream-command is required when --upstream-mode is managed")
			out.appendFail("upstream_reachable", err.Error())
			return out, err
		}
		if s.UpstreamStarter != nil {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			_, startErr := s.UpstreamStarter.EnsureRunning(ctx, upstreamAddr, out.UpstreamCommand)
			cancel()
			if startErr != nil {
				out.appendFail("upstream_reachable", fmt.Sprintf("failed to start managed upstream: %v", startErr))
				return out, startErr
			}
			defer func() {
				_ = s.UpstreamStarter.Shutdown(context.Background())
			}()
		}
		out.appendOK("upstream_reachable", fmt.Sprintf("managed upstream %s ready", upstreamAddr))
	} else {
		if err := s.UpstreamCheck.Check(ctx, upstreamAddr); err != nil {
			wrapped := fmt.Errorf("%w: %v", ErrUpstreamUnreachable, err)
			out.appendFail("upstream_reachable", fmt.Sprintf("upstream %s is not reachable", upstreamAddr))
			return out, wrapped
		}
		out.appendOK("upstream_reachable", fmt.Sprintf("upstream %s reachable", upstreamAddr))
	}

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
	stopProbe, err := s.ProbeServer.StartProbeServer(ctx, listenAddr, out.ServeTLS)
	if err != nil {
		out.appendFail("probe_handler", listenAddr)
		return out, err
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = stopProbe(shutdownCtx)
	}()
	out.appendOK("probe_handler", fmt.Sprintf("temporary probe handler started at %s", listenAddr))

	platform, err := s.IngressProbe.NewIngressProbeAPI(out.Site, input.BaseDomain, input.PlatformOrigin, input.Version)
	if err != nil {
		out.appendFail("ingress_probe", err.Error())
		return out, err
	}
	probeDetail := "requesting platform ingress probe"
	if out.PublicProbeURL != "" {
		probeDetail = fmt.Sprintf("requesting platform ingress probe for %s", out.PublicProbeURL)
	}
	resp, err := platform.CreateIngressProbe(ctx, mapping.BuildIngressProbeRequest(mapping.IngressProbeContract{
		Site:            out.Site,
		IngressMode:     out.IngressMode,
		ListenHost:      out.ListenHost,
		ListenPort:      out.ListenPort,
		IngressFamily:   out.IngressFamily,
		PublicPort:      out.PublicPort,
		DomainType:      out.DomainType,
		RequestedDomain: out.RequestedDomain,
	}, probeChallenge))
	if err != nil {
		out.appendFail("ingress_probe", probeDetail+": "+err.Error())
		return out, err
	}
	out.ResolvedPublicIP = resp.ResolvedPublicIP
	out.ProbeReason = resp.Reason
	out.ProbeRequestID = resp.RequestID
	if strings.TrimSpace(resp.ProbeURL) != "" {
		out.PublicProbeURL = resp.ProbeURL
		probeDetail = fmt.Sprintf("requesting platform ingress probe for %s", out.PublicProbeURL)
	}
	if resp.Result != "reachable" {
		err := &ports.PlatformError{Code: "ingress_unreachable", Reason: resp.Reason, HTTPStatus: 422, RequestID: resp.RequestID}
		out.appendFail("ingress_probe", preflightProbeDetail(probeDetail, resp.ResolvedPublicIP, resp.Reason, resp.RequestID))
		return out, err
	}
	out.appendOK("ingress_probe", preflightProbeDetail(probeDetail, resp.ResolvedPublicIP, resp.Reason, resp.RequestID))
	return out, nil
}

func validateIngressDomainCombination(mode domain.IngressMode, domainType domain.DomainType) error {
	return ValidateIngressDomainCombination(mode, domainType)
}

func ValidateIngressDomainCombination(mode domain.IngressMode, domainType domain.DomainType) error {
	if mode == domain.IngressModeBehindProxy && domainType == domain.DomainTypePlatformSubdomain {
		return fmt.Errorf("%w: behind-proxy ingress only supports custom_domain; use --domain-type custom_domain with --domain", ErrUnsupportedIngressDomainType)
	}
	return nil
}

func preflightProbeDetail(base, resolvedPublicIP, reason, requestID string) string {
	parts := []string{base}
	if resolvedPublicIP != "" {
		parts = append(parts, "public_ip="+resolvedPublicIP)
	}
	if reason != "" {
		parts = append(parts, "reason="+reason)
	}
	if requestID != "" {
		parts = append(parts, "request_id="+requestID)
	}
	return strings.Join(parts, "; ")
}

func preflightPublicProbeURL(domain string, publicPort int) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	host := domain
	if publicPort > 0 && publicPort != 443 {
		host = net.JoinHostPort(domain, fmt.Sprintf("%d", publicPort))
	}
	return fmt.Sprintf("https://%s/_ward/probe", host)
}

func formatListenAddr(host string, port int, family domain.IngressFamily) string {
	if family == domain.IngressFamilyIPv6 {
		return fmt.Sprintf("[%s]:%d", host, port)
	}
	return fmt.Sprintf("%s:%d", host, port)
}

func parsePreflightIngressMode(value string) (domain.IngressMode, error) {
	switch strings.TrimSpace(value) {
	case "", string(domain.IngressModeStandalone):
		return domain.IngressModeStandalone, nil
	case string(domain.IngressModeBehindProxy), "behind-proxy":
		return domain.IngressModeBehindProxy, nil
	default:
		return "", fmt.Errorf("invalid --ingress-mode: %s (must be standalone or behind-proxy)", value)
	}
}

func validatePreflightIngressConfig(mode domain.IngressMode, publicPort, listenPort int, listenHost string, trustedProxyCIDRs []string) error {
	if publicPort <= 0 || publicPort > 65535 {
		return fmt.Errorf("invalid --public-port: must be between 1 and 65535")
	}
	switch mode {
	case domain.IngressModeStandalone:
		if publicPort != listenPort {
			return fmt.Errorf("standalone ingress requires public_port to equal listen_port")
		}
	case domain.IngressModeBehindProxy:
		if !preflightLoopbackHost(listenHost) && len(trustedProxyCIDRs) == 0 {
			return fmt.Errorf("behind-proxy ingress with non-loopback listen host requires at least one trusted proxy CIDR (--trusted-proxy-cidr)")
		}
		for _, value := range trustedProxyCIDRs {
			if _, _, err := net.ParseCIDR(strings.TrimSpace(value)); err != nil {
				return fmt.Errorf("invalid trusted proxy CIDR %q: %w", value, err)
			}
		}
	default:
		return fmt.Errorf("invalid ingress mode %q", mode)
	}
	return nil
}

func preflightLoopbackHost(host string) bool {
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
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
	host, portStr, err := net.SplitHostPort(input)
	if err != nil {
		return fmt.Errorf("invalid upstream address %q: %w", input, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("invalid upstream address %q: port must be a number between 1 and 65535", input)
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return fmt.Errorf("upstream host must not be 0.0.0.0, ::, or empty")
	}
	return nil
}
