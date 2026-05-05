package ports

import "context"

type AuthProxy interface {
	Serve(ctx context.Context, listenAddr string) error
}
