package ports

import "context"

type UpstreamChecker interface {
	Check(ctx context.Context, addr string) error
}
