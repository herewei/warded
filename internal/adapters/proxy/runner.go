package proxy

import (
	"context"
)

// Runner implements ports.ProxyRunner by starting the proxy server.
type Runner struct {
	Config ServerConfig
}

// NewRunner creates a Runner with the given server config.
func NewRunner(config ServerConfig) *Runner {
	return &Runner{Config: config}
}

func (r *Runner) Run(ctx context.Context, addr string) error {
	srv := NewServer(r.Config)
	return srv.ListenAndServe(ctx, addr)
}
