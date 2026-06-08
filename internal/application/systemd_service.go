package application

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type SystemdScope string

const (
	SystemdScopeUser   SystemdScope = "user"
	SystemdScopeSystem SystemdScope = "system"
)

type SystemdService struct {
	ConfigStore ports.LocalConfigStore
	Host        ports.SystemdHost
}

type SystemdInput struct {
	DataDir  string
	WardID   string
	UnitName string
	User     bool
	System   bool
	Now      bool
	DryRun   bool
}

type SystemdOutput struct {
	Scope         SystemdScope
	UnitName      string
	UnitPath      string
	UnitContent   string
	SystemctlPlan []string
	LogCommand    string
	LingerCommand string
	Warnings      []string
	RuntimeID     string
	RuntimeDomain string
	BinaryPath    string
	DataDir       string
	DryRun        bool
	Updated       bool
	Started       bool
}

func (s SystemdService) Execute(ctx context.Context, input SystemdInput) (*SystemdOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("integrate systemd: config store is required")
	}
	if s.Host == nil {
		return nil, fmt.Errorf("integrate systemd: systemd host is required")
	}
	if input.User && input.System {
		return nil, fmt.Errorf("integrate systemd: --user and --system are mutually exclusive")
	}
	unitName := strings.TrimSpace(input.UnitName)
	if unitName == "" {
		unitName = "warded.service"
	}
	if strings.ContainsAny(unitName, `/\`) {
		return nil, fmt.Errorf("integrate systemd: --unit-name must be a service name, not a path")
	}

	scope := SystemdScopeUser
	if input.System || (!input.User && s.Host.IsRoot()) {
		scope = SystemdScopeSystem
	}

	dataDir, err := filepath.Abs(strings.TrimSpace(input.DataDir))
	if err != nil {
		return nil, fmt.Errorf("integrate systemd: resolve data-dir: %w", err)
	}
	if dataDir == "" {
		return nil, fmt.Errorf("integrate systemd: --data-dir is required")
	}

	runtime, err := s.resolveRuntime(ctx, input.WardID)
	if err != nil {
		return nil, err
	}

	binaryPath, err := s.Host.ExecutablePath()
	if err != nil {
		return nil, fmt.Errorf("integrate systemd: resolve warded executable: %w", err)
	}
	if strings.TrimSpace(binaryPath) == "" || !filepath.IsAbs(binaryPath) {
		return nil, fmt.Errorf("integrate systemd: warded executable path must be absolute")
	}

	unitPath, err := s.unitPath(scope, unitName)
	if err != nil {
		return nil, err
	}
	content := renderSystemdUnit(scope, unitName, binaryPath, dataDir)
	plan := systemctlPlan(scope, unitName, input.Now)

	out := &SystemdOutput{
		Scope:         scope,
		UnitName:      unitName,
		UnitPath:      unitPath,
		UnitContent:   content,
		SystemctlPlan: plan,
		LogCommand:    journalCommand(scope, unitName),
		RuntimeID:     runtime.WardID,
		RuntimeDomain: runtime.Domain,
		BinaryPath:    binaryPath,
		DataDir:       dataDir,
		DryRun:        input.DryRun,
	}
	if scope == SystemdScopeUser {
		user := strings.TrimSpace(s.Host.CurrentUsername())
		if user != "" {
			out.LingerCommand = "sudo loginctl enable-linger " + user
		}
		if runtime.ListenPort > 0 && runtime.ListenPort < 1024 {
			out.Warnings = append(out.Warnings, "user-level systemd may not bind low ports; grant CAP_NET_BIND_SERVICE to the warded binary or use --system")
		}
	}

	if input.DryRun {
		return out, nil
	}
	if !s.Host.IsLinux() {
		return nil, fmt.Errorf("integrate systemd: only Linux systemd hosts are supported; use tmux, screen, or another process manager")
	}
	if !s.Host.HasSystemctl() {
		return nil, fmt.Errorf("integrate systemd: systemctl not found; use tmux, screen, or another process manager")
	}
	if err := s.Host.WriteUnit(ctx, unitPath, content); err != nil {
		return nil, fmt.Errorf("integrate systemd: write unit: %w", err)
	}
	out.Updated = true
	if err := s.Host.RunSystemctl(ctx, scope == SystemdScopeUser, "daemon-reload"); err != nil {
		return nil, err
	}
	if input.Now {
		if err := s.Host.RunSystemctl(ctx, scope == SystemdScopeUser, "enable", "--now", unitName); err != nil {
			return nil, err
		}
		out.Started = true
	}
	return out, nil
}

func (s SystemdService) resolveRuntime(ctx context.Context, wardID string) (*domain.LocalWardRuntime, error) {
	listOut, err := (StatusService{ConfigStore: s.ConfigStore}).ListRuntimes(ctx)
	if err != nil {
		return nil, fmt.Errorf("integrate systemd: list runtimes: %w", err)
	}
	var committed []RuntimeSummary
	for _, rt := range listOut.Runtimes {
		if rt.Kind == RuntimeKindWard {
			committed = append(committed, rt)
		}
	}
	if wardID == "" {
		switch len(committed) {
		case 0:
			return nil, fmt.Errorf("integrate systemd: no committed ward runtime found")
		case 1:
			return &committed[0].Runtime, nil
		default:
			return nil, fmt.Errorf("integrate systemd: multiple local wards found, use --ward-id <id> to select one")
		}
	}
	for _, rt := range committed {
		if rt.Runtime.WardID == wardID {
			return &rt.Runtime, nil
		}
	}
	return nil, fmt.Errorf("integrate systemd: no ward found with --ward-id %q", wardID)
}

func (s SystemdService) unitPath(scope SystemdScope, unitName string) (string, error) {
	if scope == SystemdScopeSystem {
		return filepath.Join("/etc/systemd/system", unitName), nil
	}
	home := strings.TrimSpace(s.Host.HomeDir())
	if home == "" {
		return "", fmt.Errorf("integrate systemd: HOME must be set for user-level systemd")
	}
	return filepath.Join(home, ".config", "systemd", "user", unitName), nil
}

func renderSystemdUnit(scope SystemdScope, unitName, binaryPath, dataDir string) string {
	var b strings.Builder
	if scope == SystemdScopeSystem {
		b.WriteString("[Unit]\n")
		b.WriteString("Description=Warded OpenClaw Protection Proxy\n")
		b.WriteString("After=network-online.target\n")
		b.WriteString("Wants=network-online.target\n\n")
		b.WriteString("[Service]\n")
		b.WriteString("Type=simple\n")
		b.WriteString("WorkingDirectory=" + dataDir + "\n")
		b.WriteString("ExecStart=" + quoteSystemdArg(binaryPath) + " serve --data-dir " + quoteSystemdArg(dataDir) + "\n")
		b.WriteString("Restart=always\n")
		b.WriteString("RestartSec=5\n")
		b.WriteString("AmbientCapabilities=CAP_NET_BIND_SERVICE\n")
		b.WriteString("CapabilityBoundingSet=CAP_NET_BIND_SERVICE\n")
		b.WriteString("NoNewPrivileges=true\n")
		b.WriteString("StandardOutput=journal\n")
		b.WriteString("StandardError=journal\n\n")
		b.WriteString("[Install]\n")
		b.WriteString("WantedBy=multi-user.target\n")
		return b.String()
	}
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Warded OpenClaw Protection Proxy (user)\n")
	b.WriteString("After=default.target\n\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=simple\n")
	b.WriteString("WorkingDirectory=" + dataDir + "\n")
	b.WriteString("ExecStart=" + quoteSystemdArg(binaryPath) + " serve --data-dir " + quoteSystemdArg(dataDir) + "\n")
	b.WriteString("Restart=always\n")
	b.WriteString("RestartSec=5\n")
	b.WriteString("StandardOutput=journal\n")
	b.WriteString("StandardError=journal\n\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=default.target\n")
	return b.String()
}

func quoteSystemdArg(value string) string {
	return strconv.Quote(value)
}

func systemctlPlan(scope SystemdScope, unitName string, now bool) []string {
	prefix := "systemctl"
	if scope == SystemdScopeUser {
		prefix = "systemctl --user"
	}
	plan := []string{prefix + " daemon-reload"}
	if now {
		plan = append(plan, prefix+" enable --now "+unitName)
	}
	return plan
}

func journalCommand(scope SystemdScope, unitName string) string {
	if scope == SystemdScopeUser {
		return "journalctl --user -u " + unitName + " -f"
	}
	return "journalctl -u " + unitName + " -f"
}
