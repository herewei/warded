package application

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/herewei/warded/internal/application/mapping"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type IntegrateService struct {
	ConfigStore ports.LocalConfigStore
	OpenClawCLI OpenClawCLI
}

type IntegrateInput struct {
	Agent           string
	OpenClawPath    string
	Domain          string
	Baseline        bool
	AdoptPublicPort int
	Apply           bool
	Repair          bool
}

type IntegrateOutput struct {
	Agent           string
	OpenClawPath    string
	Mode            string
	RequiredOrigin  string
	Status          string
	CurrentAllowed  []string
	DesiredAllowed  []string
	CurrentBind     string
	CurrentPort     int
	DesiredBind     string
	DesiredPort     int
	SuggestedPatch  string
	Message         string
	Updated         bool
	RestartRequired bool
}

func (s IntegrateService) Execute(ctx context.Context, input IntegrateInput) (*IntegrateOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("integrate service: config store is required")
	}
	if s.OpenClawCLI == nil {
		return nil, fmt.Errorf("integrate service: OpenClawCLI is required")
	}
	agent := strings.TrimSpace(strings.ToLower(input.Agent))
	if agent != "openclaw" {
		return nil, fmt.Errorf("integrate service: unsupported agent: %s", input.Agent)
	}
	if input.AdoptPublicPort > 0 && !input.Baseline {
		return nil, fmt.Errorf("integrate service: --adopt-public-port requires --baseline")
	}
	if input.Repair && !input.Baseline {
		return nil, fmt.Errorf("integrate service: --repair requires --baseline")
	}

	if input.Baseline {
		input.Repair = input.Repair || input.Apply
	}

	out := &IntegrateOutput{
		Agent:        agent,
		OpenClawPath: input.OpenClawPath,
	}
	if input.Baseline {
		out.Mode = "baseline"
		return s.executeOpenClawBaseline(input, out)
	}

	record, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("integrate service: load ward runtime: %w", err)
	}
	if record == nil || record.WardID == "" || record.WardStatus != string(domain.WardStatusActive) {
		return nil, fmt.Errorf("integrate service: ward is not active")
	}
	runtime := mapping.DomainFromRuntimeRecord(record)

	requiredOrigin, err := requiredOrigin(input.Domain, runtime.Domain)
	if err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}
	out.Mode = "allowed_origins"
	out.RequiredOrigin = requiredOrigin

	rawAllowed, err := s.OpenClawCLI.Get("gateway.controlUi.allowedOrigins")
	if err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}

	currentAllowed, err := parseOpenClawAllowedOrigins(rawAllowed)
	if err != nil {
		out.Status = "invalid_json"
		out.SuggestedPatch = openClawAllowedOriginsPatch([]string{requiredOrigin})
		out.Message = "OpenClaw config returned invalid JSON for allowedOrigins."
		if input.Apply {
			return nil, fmt.Errorf("integrate service: invalid allowedOrigins JSON: %w", err)
		}
		return out, nil
	}

	desiredAllowed := appendUnique(currentAllowed, requiredOrigin)

	out.CurrentAllowed = currentAllowed
	out.DesiredAllowed = desiredAllowed
	out.SuggestedPatch = openClawAllowedOriginsPatch(desiredAllowed)

	if len(currentAllowed) == len(desiredAllowed) {
		out.Status = "already_configured"
		out.Message = "Required origin is already present."
		return out, nil
	}

	out.Status = "patch_required"
	out.Message = "OpenClaw allowedOrigins is missing the ward origin."
	if !input.Apply {
		return out, nil
	}

	if err := s.OpenClawCLI.Set("gateway.controlUi.allowedOrigins", formatOpenClawAllowedOrigins(desiredAllowed)); err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}
	if err := s.OpenClawCLI.Validate(); err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}

	out.Status = "updated"
	out.Updated = true
	out.Message = "OpenClaw allowedOrigins updated."
	return out, nil
}

func (s IntegrateService) executeOpenClawBaseline(input IntegrateInput, out *IntegrateOutput) (*IntegrateOutput, error) {
	rawBind, err := s.OpenClawCLI.Get("gateway.bind")
	if err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}
	rawPort, err := s.OpenClawCLI.Get("gateway.port")
	if err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}

	state := openClawConfigState{
		Bind: strings.TrimSpace(rawBind),
		Port: parseOpenClawPort(rawPort),
	}
	out.CurrentBind = state.Bind
	out.CurrentPort = state.Port

	desiredBind := "loopback"
	desiredPort := state.Port
	if desiredPort <= 0 {
		desiredPort = 18789
	}
	if input.AdoptPublicPort > 0 {
		if state.Port != input.AdoptPublicPort {
			return nil, fmt.Errorf("integrate service: adopt-public-port %d does not match OpenClaw port %d", input.AdoptPublicPort, state.Port)
		}
		desiredPort = adoptedUpstreamPort(input.AdoptPublicPort)
	}
	out.DesiredBind = desiredBind
	out.DesiredPort = desiredPort
	out.SuggestedPatch = openClawBaselinePatch(desiredBind, desiredPort)

	if state.Bind == desiredBind && state.Port == desiredPort {
		out.Status = "baseline_already_configured"
		out.SuggestedPatch = ""
		out.Message = fmt.Sprintf("OpenClaw security baseline already uses bind=%s port=%d.", desiredBind, desiredPort)
		return out, nil
	}

	out.Status = "baseline_repair_required"
	if input.AdoptPublicPort > 0 {
		out.Message = fmt.Sprintf("OpenClaw should move from public port %d to local upstream port %d and bind to %s.", input.AdoptPublicPort, desiredPort, desiredBind)
	} else {
		out.Message = fmt.Sprintf("OpenClaw should bind to %s instead of %s.", desiredBind, safeConfigValue(state.Bind, "unset"))
	}
	if !input.Repair {
		return out, nil
	}

	if err := s.OpenClawCLI.Set("gateway.bind", desiredBind); err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}
	if err := s.OpenClawCLI.Set("gateway.port", fmt.Sprintf("%d", desiredPort)); err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}
	if err := s.OpenClawCLI.Validate(); err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}

	out.Updated = true
	if input.AdoptPublicPort > 0 {
		out.Status = "baseline_repaired_restart_required"
		out.RestartRequired = true
		out.Message = fmt.Sprintf("OpenClaw security baseline updated. Restart OpenClaw gateway to move it to port %d and release public port %d for Warded.", desiredPort, input.AdoptPublicPort)
		return out, nil
	}

	out.Status = "baseline_repaired"
	out.Message = "OpenClaw security baseline updated."
	return out, nil
}

func requiredOrigin(flagDomain string, runtimeDomain string) (string, error) {
	value := strings.TrimSpace(flagDomain)
	if value == "" {
		value = strings.TrimSpace(runtimeDomain)
	}
	if value == "" {
		return "", fmt.Errorf("ward domain is required")
	}
	if strings.Contains(value, "://") {
		parsed, err := url.Parse(value)
		if err != nil {
			return "", fmt.Errorf("invalid domain/origin: %w", err)
		}
		if parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" && parsed.Path != "/" || parsed.RawQuery != "" || parsed.Fragment != "" {
			return "", fmt.Errorf("invalid domain/origin: must be a bare origin")
		}
		return strings.TrimSuffix(parsed.Scheme+"://"+parsed.Host, "/"), nil
	}
	if strings.Contains(value, "/") {
		return "", fmt.Errorf("invalid domain/origin: must not contain a path")
	}
	return "https://" + value, nil
}

type openClawConfigState struct {
	Bind string
	Port int
}

func safeConfigValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func appendUnique(values []string, required string) []string {
	seen := make(map[string]struct{}, len(values)+1)
	out := make([]string, 0, len(values)+1)
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if _, ok := seen[required]; !ok {
		out = append(out, required)
	}
	return out
}

func openClawAllowedOriginsPatch(origins []string) string {
	payload := map[string]any{
		"gateway": map[string]any{
			"controlUi": map[string]any{
				"allowedOrigins": origins,
			},
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func openClawBaselinePatch(bind string, port int) string {
	payload := map[string]any{
		"gateway": map[string]any{
			"bind": bind,
			"port": port,
		},
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return ""
	}
	return string(data)
}

func adoptedUpstreamPort(publicPort int) int {
	if publicPort <= 0 {
		return 19789
	}
	if publicPort <= 64535 {
		return publicPort + 1000
	}
	return publicPort - 1000
}
