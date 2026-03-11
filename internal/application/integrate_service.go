package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

var (
	readFileFunc    = os.ReadFile
	writeFileFunc   = os.WriteFile
	renameFileFunc  = os.Rename
	mkdirAllFunc    = os.MkdirAll
	userHomeDirFunc = os.UserHomeDir
	nowFunc         = func() time.Time { return time.Now().UTC() }
)

type IntegrateService struct {
	ConfigStore ports.LocalConfigStore
}

type IntegrateInput struct {
	Agent      string
	ConfigFile string
	Domain     string
	Apply      bool
}

type IntegrateOutput struct {
	Agent          string
	ConfigFile     string
	RequiredOrigin string
	Status         string
	CurrentAllowed []string
	DesiredAllowed []string
	SuggestedPatch string
	Message        string
	BackupFile     string
	Updated        bool
}

func (s IntegrateService) Execute(ctx context.Context, input IntegrateInput) (*IntegrateOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("integrate service: config store is required")
	}
	agent := strings.TrimSpace(strings.ToLower(input.Agent))
	if agent != "openclaw" {
		return nil, fmt.Errorf("integrate service: unsupported agent: %s", input.Agent)
	}

	runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("integrate service: load ward runtime: %w", err)
	}
	if runtime == nil || runtime.WardID == "" || runtime.WardStatus != domain.WardStatusActive {
		return nil, fmt.Errorf("integrate service: ward is not active")
	}

	requiredOrigin, err := requiredOrigin(input.Domain, runtime.Domain)
	if err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}

	configFile, err := openClawConfigPath(input.ConfigFile)
	if err != nil {
		return nil, fmt.Errorf("integrate service: %w", err)
	}

	out := &IntegrateOutput{
		Agent:          agent,
		ConfigFile:     configFile,
		RequiredOrigin: requiredOrigin,
	}

	data, err := readFileFunc(configFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			out.Status = "config_not_found"
			out.SuggestedPatch = openClawAllowedOriginsPatch([]string{requiredOrigin})
			out.Message = "OpenClaw config file was not found."
			if input.Apply {
				return nil, fmt.Errorf("integrate service: config file not found: %s", configFile)
			}
			return out, nil
		}
		return nil, fmt.Errorf("integrate service: read config file: %w", err)
	}

	updated, currentAllowed, desiredAllowed, err := updateOpenClawAllowedOrigins(data, requiredOrigin)
	if err != nil {
		out.Status = "invalid_json"
		out.SuggestedPatch = openClawAllowedOriginsPatch([]string{requiredOrigin})
		out.Message = "OpenClaw config is not valid JSON."
		if input.Apply {
			return nil, fmt.Errorf("integrate service: invalid JSON in %s: %w", configFile, err)
		}
		return out, nil
	}

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

	backupFile := configFile + ".bak." + nowFunc().Format("20060102T150405Z")
	if err := writeFileFunc(backupFile, data, 0o600); err != nil {
		return nil, fmt.Errorf("integrate service: write backup: %w", err)
	}
	if err := mkdirAllFunc(filepath.Dir(configFile), 0o755); err != nil {
		return nil, fmt.Errorf("integrate service: ensure config directory: %w", err)
	}
	tmpPath := configFile + ".tmp"
	if err := writeFileFunc(tmpPath, append(updated, '\n'), 0o600); err != nil {
		return nil, fmt.Errorf("integrate service: write temp config: %w", err)
	}
	if err := renameFileFunc(tmpPath, configFile); err != nil {
		return nil, fmt.Errorf("integrate service: replace config: %w", err)
	}

	out.Status = "updated"
	out.Updated = true
	out.BackupFile = backupFile
	out.Message = "OpenClaw config updated."
	return out, nil
}

func openClawConfigPath(override string) (string, error) {
	if strings.TrimSpace(override) != "" {
		return override, nil
	}
	home, err := userHomeDirFunc()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".openclaw", "openclaw.json"), nil
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

func updateOpenClawAllowedOrigins(data []byte, required string) ([]byte, []string, []string, error) {
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, nil, nil, err
	}

	gateway := ensureMap(root, "gateway")
	controlUI := ensureMap(gateway, "controlUi")

	currentAllowed := stringSlice(controlUI["allowedOrigins"])
	desiredAllowed := appendUnique(currentAllowed, required)
	controlUI["allowedOrigins"] = desiredAllowed

	updated, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("marshal updated config: %w", err)
	}
	return updated, currentAllowed, desiredAllowed, nil
}

func ensureMap(parent map[string]any, key string) map[string]any {
	if current, ok := parent[key]; ok {
		if asMap, ok := current.(map[string]any); ok {
			return asMap
		}
	}
	created := map[string]any{}
	parent[key] = created
	return created
}

func stringSlice(value any) []string {
	list, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		text, ok := item.(string)
		if !ok {
			continue
		}
		trimmed := strings.TrimSpace(text)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
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
