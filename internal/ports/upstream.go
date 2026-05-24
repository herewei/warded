package ports

import "context"

type UpstreamChecker interface {
	Check(ctx context.Context, addr string) error
}

// UpstreamProcessManager handles lifecycle of a managed upstream process.
type UpstreamProcessManager interface {
	// EnsureRunning checks if addr is reachable. If not, it starts command
	// and waits until addr becomes ready. Returns true if it started (owns)
	// the process, false if it was already reachable.
	EnsureRunning(ctx context.Context, addr string, command string) (owned bool, err error)
	// Shutdown terminates any owned process.
	Shutdown(ctx context.Context) error
}
