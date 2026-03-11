package ports

import (
	"context"

	"github.com/herewei/warded/internal/domain"
)

type LocalConfigStore interface {
	LoadWardRuntime(ctx context.Context) (*domain.LocalWardRuntime, error)
	SaveWardRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error
}
