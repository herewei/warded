package ports

import "context"

// ServeMonitor reports whether the local warded serve process is running.
// running=true means the service is up; detail is a human-readable status line.
type ServeMonitor interface {
	CheckServe(ctx context.Context) (running bool, detail string)
}

// ServeTLSMonitor inspects the local HTTPS listener and reports whether it is
// currently serving with a fallback self-signed certificate.
type ServeTLSMonitor interface {
	CheckServeTLS(ctx context.Context, addr string, serverName string) (fallback bool, detail string)
}
