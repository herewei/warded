package upstream

import (
	"context"
	"fmt"
	"net"
	"time"
)

type Checker struct {
	Timeout time.Duration
}

func NewChecker() *Checker {
	return &Checker{Timeout: 2 * time.Second}
}

func (c *Checker) Check(ctx context.Context, port int) error {
	if port <= 0 {
		return fmt.Errorf("invalid upstream port: %d", port)
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("upstream port %d is not reachable on localhost: %w", port, err)
	}
	_ = conn.Close()
	return nil
}
