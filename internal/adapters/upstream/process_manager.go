package upstream

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"sync"
	"time"
)

// ProcessManager handles starting and stopping a managed upstream process.
type ProcessManager struct {
	mu       sync.Mutex
	ownedCmd *exec.Cmd
}

func NewProcessManager() *ProcessManager {
	return &ProcessManager{}
}

// EnsureRunning checks if addr is reachable via TCP. If not, it starts command
// and waits until addr becomes ready. Returns true if it started (owns) the
// process, false if the upstream was already reachable.
func (m *ProcessManager) EnsureRunning(ctx context.Context, addr string, command string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.ownedCmd != nil {
		if isUpstreamReady(ctx, addr) {
			return false, nil
		}
		_ = shutdownCmd(m.ownedCmd)
		m.ownedCmd = nil
	}
	if isUpstreamReady(ctx, addr) {
		return false, nil
	}

	cmd, err := startManagedProcess(command)
	if err != nil {
		return false, fmt.Errorf("start managed upstream: %w", err)
	}

	if err := waitForUpstreamReady(ctx, addr, cmd); err != nil {
		_ = shutdownCmd(cmd)
		return false, err
	}

	if m.ownedCmd != nil {
		_ = shutdownCmd(m.ownedCmd)
	}
	m.ownedCmd = cmd

	return true, nil
}

// Shutdown terminates any owned managed upstream process.
func (m *ProcessManager) Shutdown(_ context.Context) error {
	m.mu.Lock()
	cmd := m.ownedCmd
	m.ownedCmd = nil
	m.mu.Unlock()

	if cmd == nil {
		return nil
	}
	return shutdownCmd(cmd)
}

func isUpstreamReady(ctx context.Context, addr string) bool {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

func waitForUpstreamReady(ctx context.Context, addr string, cmd *exec.Cmd) error {
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("upstream %s did not become ready in time", addr)
		case <-ticker.C:
			if isUpstreamReady(ctx, addr) {
				return nil
			}
			if exited, _ := managedProcessExited(cmd); exited {
				return fmt.Errorf("managed upstream process exited before %s became ready", addr)
			}
		}
	}
}

