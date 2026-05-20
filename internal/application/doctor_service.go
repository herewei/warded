package application

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type DoctorService struct {
	ConfigStore     ports.LocalConfigStore
	ServeMonitor    ports.ServeMonitor
	ServeTLSMonitor ports.ServeTLSMonitor
}

var (
	dialTimeoutFunc = func(network, address string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout(network, address, timeout)
	}
	localIPv4AddrsFunc = localIPv4Addrs
)

type CheckState string

const (
	CheckOK   CheckState = "OK"
	CheckFAIL CheckState = "FAIL"
	CheckINFO CheckState = "INFO"
)

type CheckResult struct {
	Key    string
	Number int
	Name   string
	State  CheckState
	Detail string
}

type DoctorInput struct {
	Agent    string
	Baseline bool
}

type DoctorOutput struct {
	Runtime       *domain.LocalWardRuntime
	Results       []CheckResult
	AdoptPortHint int
	BaselineOK    bool
	NextSteps     []string
}

func (s DoctorService) Execute(ctx context.Context, input DoctorInput) (*DoctorOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("doctor service: config store is required")
	}

	if input.Baseline {
		agent := strings.TrimSpace(strings.ToLower(input.Agent))
		if agent == "" {
			agent = "openclaw"
		}
		if agent != "openclaw" {
			return nil, fmt.Errorf("doctor service: baseline diagnosis currently only supports --agent openclaw")
		}
		return s.executeOpenClawBaseline(ctx)
	}

	return s.executeLegacy(ctx)
}

func (s DoctorService) executeLegacy(ctx context.Context) (*DoctorOutput, error) {
	results := make([]CheckResult, 0, 6)

	runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, err
	}
	results = append(results, s.openClawBaselineResultLegacy())
	if runtime == nil {
		results = append(results, CheckResult{Key: "ward_runtime", Name: "ward_runtime", State: CheckFAIL, Detail: "ward.json not found"})
	} else {
		jwtOK := runtime.JWTSigningSecret != ""
		jwtDetail := "jwt_signing_secret is present"
		if !jwtOK {
			jwtDetail = "jwt_signing_secret is missing"
		}
		results = append(results, CheckResult{Key: "local_jwt", Name: "local_jwt", State: checkStateFromBool(jwtOK), Detail: jwtDetail})

		results = append(results, CheckResult{
			Key:    "ward_runtime",
			Name:   "ward_runtime",
			State:  CheckOK,
			Detail: fmt.Sprintf("ward_draft_id=%s ward_id=%s domain=%s", runtime.WardDraftID, runtime.WardID, runtime.Domain),
		})

		active := runtime.WardStatus == domain.WardStatusActive
		results = append(results, CheckResult{
			Key:    "ward_active",
			Name:   "ward_active",
			State:  checkStateFromBool(active),
			Detail: fmt.Sprintf("ward status is %s", runtime.WardStatus),
		})

		serveRunning := false
		if s.ServeMonitor != nil {
			running, detail := s.ServeMonitor.CheckServe(ctx)
			serveRunning = running
			results = append(results, CheckResult{
				Key:    "serve_running",
				Name:   "serve_running",
				State:  checkStateFromBool(running),
				Detail: detail,
			})
		}
		if s.ServeTLSMonitor != nil {
			tlsResult := CheckResult{
				Key:    "tls_platform_cert",
				Name:   "tls_platform_cert",
				State:  CheckFAIL,
				Detail: "skipped: serve is not running",
			}
			if serveRunning {
				addr := listenAddrFromRuntime(runtime)
				fallback, detail := s.ServeTLSMonitor.CheckServeTLS(ctx, addr, runtime.Domain)
				tlsResult.State = checkStateFromBool(!fallback)
				tlsResult.Detail = detail
			}
			results = append(results, tlsResult)
		}

		integrationResult := CheckResult{
			Key:    "openclaw_integration",
			Name:   "openclaw_integration",
			State:  CheckFAIL,
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
							integrationResult.State = CheckOK
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

	return &DoctorOutput{Runtime: runtime, Results: results}, nil
}

func (s DoctorService) executeOpenClawBaseline(ctx context.Context) (*DoctorOutput, error) {
	_ = ctx
	results := make([]CheckResult, 0, 4)
	var nextSteps []string
	adoptPortHint := 0

	configFile, err := openClawConfigPath("")
	if err != nil {
		results = append(results, CheckResult{
			Key: "config_exists", Number: 1, Name: "config file exists", State: CheckFAIL,
			Detail: fmt.Sprintf("failed to locate openclaw config: %v", err),
		})
		return &DoctorOutput{Results: results, BaselineOK: false, NextSteps: nextSteps}, nil
	}

	data, readErr := readFileFunc(configFile)

	configExists := !errors.Is(readErr, os.ErrNotExist) && readErr == nil
	results = append(results, CheckResult{
		Key: "config_exists", Number: 1, Name: "config file exists",
		State:  checkStateFromBool(configExists),
		Detail: configDetailForExists(configFile, readErr),
	})

	if !configExists {
		nextSteps = append(nextSteps, fmt.Sprintf("warded integrate --agent openclaw --baseline repair"))
		return &DoctorOutput{Results: results, BaselineOK: false, NextSteps: nextSteps}, nil
	}

	_, state, parseErr := parseOpenClawConfig(data)
	if parseErr != nil {
		results = append(results, CheckResult{
			Key: "gateway_bind_loopback", Number: 2, Name: "gateway.bind is loopback", State: CheckFAIL,
			Detail: fmt.Sprintf("invalid JSON in %s", configFile),
		})
		nextSteps = append(nextSteps, fmt.Sprintf("warded integrate --agent openclaw --baseline repair"))
		return &DoctorOutput{Results: results, BaselineOK: false, NextSteps: nextSteps}, nil
	}

	bindOK := state.Bind == "loopback"
	results = append(results, CheckResult{
		Key: "gateway_bind_loopback", Number: 2, Name: "gateway.bind is loopback",
		State:  checkStateFromBool(bindOK),
		Detail: fmt.Sprintf("gateway.bind=%s", safeConfigValue(state.Bind, "unset")),
	})

	probe := probeOpenClawPort(state.Port)
	nonLoopbackCheck := CheckResult{
		Key: "not_reachable_non_loopback", Number: 3, Name: "OpenClaw is not reachable on a non-loopback address",
	}
	if probe.NonLoopbackReachable {
		nonLoopbackCheck.State = CheckFAIL
		nonLoopbackCheck.Detail = fmt.Sprintf("still reachable on %s:%d", probe.NonLoopbackAddr, state.Port)
	} else {
		nonLoopbackCheck.State = CheckOK
		nonLoopbackCheck.Detail = "not reachable on non-loopback addresses"
	}
	results = append(results, nonLoopbackCheck)

	takeoverRequired := !bindOK || probe.NonLoopbackReachable
	takeoverCheck := CheckResult{
		Key: "old_public_port_takeover", Number: 4, Name: "old public port takeover",
	}
	if takeoverRequired {
		takeoverCheck.State = CheckINFO
		if probe.NonLoopbackReachable {
			takeoverCheck.Detail = fmt.Sprintf("recommended: adopt public port %d to release it for Warded", state.Port)
			adoptPortHint = state.Port
		} else {
			takeoverCheck.Detail = fmt.Sprintf("recommended: tighten gateway.bind to loopback")
		}
	} else {
		takeoverCheck.State = CheckINFO
		takeoverCheck.Detail = "not required"
	}
	results = append(results, takeoverCheck)

	for i := range results {
		results[i].Number = i + 1
	}

	passed := 0
	for _, r := range results {
		if r.State == CheckOK {
			passed++
		}
	}

	baselineOK := bindOK && !probe.NonLoopbackReachable

	if baselineOK {
		nextSteps = append(nextSteps, "baseline acceptable, proceed to warded new")
	} else {
		if probe.NonLoopbackReachable {
			nextSteps = append(nextSteps, fmt.Sprintf("warded integrate --agent openclaw --baseline repair --adopt-public-port %d", state.Port))
		} else {
			nextSteps = append(nextSteps, "warded integrate --agent openclaw --baseline repair")
		}
	}

	out := &DoctorOutput{
		Results:       results,
		BaselineOK:    baselineOK,
		NextSteps:     nextSteps,
		AdoptPortHint: adoptPortHint,
	}
	out.Results = results
	_ = passed
	return out, nil
}

func checkStateFromBool(ok bool) CheckState {
	if ok {
		return CheckOK
	}
	return CheckFAIL
}

func configDetailForExists(configFile string, readErr error) string {
	if readErr == nil {
		return fmt.Sprintf("found: %s", configFile)
	}
	if errors.Is(readErr, os.ErrNotExist) {
		return fmt.Sprintf("not found: %s", configFile)
	}
	return fmt.Sprintf("read error: %v", readErr)
}

func (s DoctorService) openClawBaselineResultLegacy() CheckResult {
	result := CheckResult{
		Key:    "openclaw_baseline",
		Name:   "openclaw_baseline",
		State:  CheckFAIL,
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
		result.State = CheckOK
		result.Detail = formatOpenClawBaselineSuccessDetail(state.Bind, state.Port, probe)
	case state.Bind == "loopback" && probe.NonLoopbackReachable:
		result.Detail = fmt.Sprintf("gateway.bind=%s port=%d but the service is still reachable on %s; repair the baseline before running warded new", state.Bind, state.Port, probe.NonLoopbackAddr)
	case probe.NonLoopbackReachable:
		result.Detail = fmt.Sprintf("gateway.bind=%s port=%d is reachable on %s; repair the baseline before running warded new", safeConfigValue(state.Bind, "unset"), state.Port, probe.NonLoopbackAddr)
	default:
		result.Detail = fmt.Sprintf("gateway.bind=%s port=%d is configured for direct exposure; non-loopback reachability is not confirmed, but repair the baseline before running warded new", safeConfigValue(state.Bind, "unset"), state.Port)
	}
	return result
}

func formatOpenClawBaselineSuccessDetail(bind string, port int, probe openClawPortProbe) string {
	if probe.LoopbackReachable {
		return fmt.Sprintf("gateway.bind=%s port=%d loopback=true", bind, port)
	}
	return fmt.Sprintf("gateway.bind=%s port=%d loopback=false", bind, port)
}

type openClawPortProbe struct {
	LoopbackReachable    bool
	NonLoopbackReachable bool
	NonLoopbackAddr      string
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
