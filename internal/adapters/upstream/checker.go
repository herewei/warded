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

func (c *Checker) Check(ctx context.Context, addr string) error {
	if addr == "" {
		return fmt.Errorf("upstream address is empty")
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}

	dialer := net.Dialer{Timeout: timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("upstream %s is not reachable: %w", addr, err)
	}
	_ = conn.Close()
	return nil
}
