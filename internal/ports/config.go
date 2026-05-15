package ports

import (
	"context"

	"github.com/herewei/warded/internal/domain"
)

type LocalConfigStore interface {
	LoadWardRuntime(ctx context.Context) (*domain.LocalWardRuntime, error)
	SaveWardRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error
	LoadPendingRuntime(ctx context.Context) (*domain.LocalWardRuntime, error)
	SavePendingRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error
	CommitPendingRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error
	// ListWardRuntimes returns all local runtimes (pending-config, drafts, wards)
	// without modifying any store state. Safe to call before selecting a target.
	ListWardRuntimes(ctx context.Context) ([]domain.LocalWardRuntime, error)
	// LoadRuntimeByID loads the runtime identified by a ward_id or ward_draft_id
	// and primes the store so subsequent SaveWardRuntime calls target the same
	// directory (preserving the rename behavior on draft→ward promotion).
	LoadRuntimeByID(ctx context.Context, id string) (*domain.LocalWardRuntime, error)
}
