package application

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// OpenClawCLI abstracts interaction with the openclaw command-line tool.
type OpenClawCLI interface {
	Get(key string) (string, error)
	Set(key string, value string) error
	Validate() error
}

// openClawCLI implements OpenClawCLI by invoking the openclaw binary.
type openClawCLI struct {
	binPath string
}

// NewOpenClawCLI creates a new OpenClawCLI.
// If binPath is empty, it auto-resolves via exec.LookPath("openclaw").
func NewOpenClawCLI(binPath string) (OpenClawCLI, error) {
	if strings.TrimSpace(binPath) != "" {
		return &openClawCLI{binPath: binPath}, nil
	}
	found, err := exec.LookPath("openclaw")
	if err != nil {
		return nil, fmt.Errorf("openclaw command not found in PATH; specify --openclaw-path")
	}
	return &openClawCLI{binPath: found}, nil
}

func (c *openClawCLI) Get(key string) (string, error) {
	cmd := exec.Command(c.binPath, "config", "get", key)
	out, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			return "", fmt.Errorf("openclaw config get %s failed: %s", key, stderr)
		}
		return "", fmt.Errorf("openclaw config get %s: %w", key, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (c *openClawCLI) Set(key string, value string) error {
	cmd := exec.Command(c.binPath, "config", "set", key, value)
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			return fmt.Errorf("openclaw config set %s failed: %s", key, stderr)
		}
		return fmt.Errorf("openclaw config set %s: %w", key, err)
	}
	return nil
}

func (c *openClawCLI) Validate() error {
	cmd := exec.Command(c.binPath, "config", "validate")
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if strings.Contains(stderr, "Config invalid") {
				return fmt.Errorf("openclaw config invalid: %s", stderr)
			}
			return fmt.Errorf("openclaw config validate failed: %s", stderr)
		}
		return fmt.Errorf("openclaw config validate: %w", err)
	}
	return nil
}

// parseOpenClawAllowedOrigins parses the raw JSON array string returned by
// `openclaw config get gateway.controlUi.allowedOrigins`.
func parseOpenClawAllowedOrigins(raw string) ([]string, error) {
	if raw == "" {
		return nil, nil
	}
	var list []string
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, fmt.Errorf("parse allowedOrigins: %w", err)
	}
	return list, nil
}

// parseOpenClawPort parses the port string returned by openclaw config get.
func parseOpenClawPort(raw string) int {
	port, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || port <= 0 {
		return 18789
	}
	return port
}

// formatOpenClawAllowedOrigins formats a string slice as a JSON array for openclaw config set.
func formatOpenClawAllowedOrigins(origins []string) string {
	data, _ := json.Marshal(origins)
	return string(data)
}
