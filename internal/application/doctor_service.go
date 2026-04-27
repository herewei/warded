package application

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type DoctorService struct {
	ConfigStore     ports.LocalConfigStore
	ServeChecker    ports.ServeChecker
	ServeTLSChecker ports.ServeTLSChecker
}

var (
	dialTimeoutFunc   = func(network, address string, timeout time.Duration) (net.Conn, error) { return net.DialTimeout(network, address, timeout) }
	localIPv4AddrsFunc = localIPv4Addrs
)

type CheckResult struct {
	Name   string
	OK     bool
	Detail string
}

type DoctorOutput struct {
	Results []CheckResult
}

func (s DoctorService) Execute(ctx context.Context) (*DoctorOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("doctor service: config store is required")
	}

	results := make([]CheckResult, 0, 6)

	runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, err
	}
	results = append(results, s.openClawBaselineResult())
	if runtime == nil {
		results = append(results, CheckResult{Name: "ward_runtime", OK: false, Detail: "ward.json not found"})
	} else {
		jwtOK := runtime.JWTSigningSecret != ""
		jwtDetail := "jwt_signing_secret is present"
		if !jwtOK {
			jwtDetail = "jwt_signing_secret is missing"
		}
		results = append(results, CheckResult{Name: "local_jwt", OK: jwtOK, Detail: jwtDetail})

		results = append(results, CheckResult{
			Name:   "ward_runtime",
			OK:     true,
			Detail: fmt.Sprintf("ward_draft_id=%s ward_id=%s domain=%s", runtime.WardDraftID, runtime.WardID, runtime.Domain),
		})

		active := runtime.WardStatus == domain.WardStatusActive
		results = append(results, CheckResult{
			Name:   "ward_active",
			OK:     active,
			Detail: fmt.Sprintf("ward status is %s", runtime.WardStatus),
		})

		serveRunning := false
		if s.ServeChecker != nil {
			running, detail := s.ServeChecker.CheckServe(ctx)
			serveRunning = running
			results = append(results, CheckResult{
				Name:   "serve_running",
				OK:     running,
				Detail: detail,
			})
		}
		if s.ServeTLSChecker != nil {
			tlsResult := CheckResult{
				Name:   "tls_platform_cert",
				OK:     false,
				Detail: "skipped: serve is not running",
			}
			if serveRunning {
				addr := runtime.ListenAddr
				if addr == "" {
					addr = ":443"
				}
				fallback, detail := s.ServeTLSChecker.CheckServeTLS(ctx, addr, runtime.Domain)
				tlsResult.OK = !fallback
				tlsResult.Detail = detail
			}
			results = append(results, tlsResult)
		}

		integrationResult := CheckResult{
			Name:   "openclaw_integration",
			OK:     false,
			Detail: "skipped: ward is not active",
		}
		if active && runtime.Domain != "" {
			requiredOrigin, err := requiredOrigin("", runtime.Domain)
			if err != nil {
				integrationResult.Detail = fmt.Sprintf("failed to build required origin: %v", err)
			} else {
				configFile, err := openClawConfigPath("")
				if err != nil {
					integrationResult.Detail = fmt.Sprintf("failed to locate openclaw config: %v", err)
				} else {
					data, err := readFileFunc(configFile)
					switch {
					case errors.Is(err, os.ErrNotExist):
						integrationResult.Detail = fmt.Sprintf("config not found: %s", configFile)
					case err != nil:
						integrationResult.Detail = fmt.Sprintf("failed to read %s: %v", configFile, err)
					default:
						_, currentAllowed, desiredAllowed, err := updateOpenClawAllowedOrigins(data, requiredOrigin)
						if err != nil {
							integrationResult.Detail = fmt.Sprintf("invalid JSON in %s", configFile)
						} else if len(currentAllowed) == len(desiredAllowed) {
							integrationResult.OK = true
							integrationResult.Detail = fmt.Sprintf("allowedOrigins already includes %s", requiredOrigin)
						} else {
							integrationResult.Detail = fmt.Sprintf("allowedOrigins is missing %s", requiredOrigin)
						}
					}
				}
			}
		}
		results = append(results, integrationResult)
	}

	return &DoctorOutput{Results: results}, nil
}

func (s DoctorService) openClawBaselineResult() CheckResult {
	result := CheckResult{
		Name:   "openclaw_baseline",
		OK:     false,
		Detail: "OpenClaw security baseline could not be checked",
	}
	configFile, err := openClawConfigPath("")
	if err != nil {
		result.Detail = fmt.Sprintf("failed to locate openclaw config: %v", err)
		return result
	}
	data, err := readFileFunc(configFile)
	if errors.Is(err, os.ErrNotExist) {
		result.Detail = fmt.Sprintf("config not found: %s", configFile)
		return result
	}
	if err != nil {
		result.Detail = fmt.Sprintf("failed to read %s: %v", configFile, err)
		return result
	}
	_, state, err := parseOpenClawConfig(data)
	if err != nil {
		result.Detail = fmt.Sprintf("invalid JSON in %s", configFile)
		return result
	}
	probe := probeOpenClawPort(state.Port)
	switch {
	case state.Bind == "loopback" && !probe.NonLoopbackReachable:
		result.OK = true
		result.Detail = fmt.Sprintf("gateway.bind=%s port=%d loopback=%t non_loopback=%t", state.Bind, state.Port, probe.LoopbackReachable, probe.NonLoopbackReachable)
	case state.Bind == "loopback" && probe.NonLoopbackReachable:
		result.Detail = fmt.Sprintf("gateway.bind=%s port=%d but the service is still reachable on %s; repair the baseline before running warded new", state.Bind, state.Port, probe.NonLoopbackAddr)
	case probe.NonLoopbackReachable:
		result.Detail = fmt.Sprintf("gateway.bind=%s port=%d is reachable on %s; repair the baseline before running warded new", safeConfigValue(state.Bind, "unset"), state.Port, probe.NonLoopbackAddr)
	default:
		result.Detail = fmt.Sprintf("gateway.bind=%s port=%d is configured for direct exposure; non-loopback reachability is not confirmed, but repair the baseline before running warded new", safeConfigValue(state.Bind, "unset"), state.Port)
	}
	return result
}

type openClawPortProbe struct {
	LoopbackReachable   bool
	NonLoopbackReachable bool
	NonLoopbackAddr     string
}

func probeOpenClawPort(port int) openClawPortProbe {
	probe := openClawPortProbe{}
	if port <= 0 {
		return probe
	}
	probe.LoopbackReachable = canDial("127.0.0.1", port)
	for _, ip := range localIPv4AddrsFunc() {
		if ip == "" || ip == "127.0.0.1" {
			continue
		}
		if canDial(ip, port) {
			probe.NonLoopbackReachable = true
			probe.NonLoopbackAddr = ip
			break
		}
	}
	return probe
}

func canDial(host string, port int) bool {
	conn, err := dialTimeoutFunc("tcp", fmt.Sprintf("%s:%d", host, port), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func localIPv4Addrs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	out := make([]string, 0, 4)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil {
				continue
			}
			if v4 := ip.To4(); v4 != nil {
				out = append(out, v4.String())
			}
		}
	}
	return out
}
