package ports

import "context"

type UpstreamChecker interface {
	Check(ctx context.Context, port int) error
}

type ProxyRunner interface {
	Run(ctx context.Context, addr string) error
}

// ServeChecker reports whether the local warded serve process is running.
// running=true means the service is up; detail is a human-readable status line.
type ServeChecker interface {
	CheckServe(ctx context.Context) (running bool, detail string)
}

// ServeTLSChecker inspects the local HTTPS listener and reports whether it is
// currently serving with a fallback self-signed certificate.
type ServeTLSChecker interface {
	CheckServeTLS(ctx context.Context, addr string, serverName string) (fallback bool, detail string)
}
