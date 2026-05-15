package ports

import (
	"context"
	"fmt"
)

// ListenConfig describes a single-stack listener endpoint.
type ListenConfig struct {
	Host          string
	Port          int
	IngressFamily string // "ipv4" or "ipv6"
}

func (c ListenConfig) Addr() string {
	if c.IngressFamily == "ipv6" {
		return fmt.Sprintf("[%s]:%d", c.Host, c.Port)
	}
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

type AuthProxy interface {
	Serve(ctx context.Context, listenAddr string) error
}
