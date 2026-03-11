package application

import (
	"context"
	"fmt"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type StatusService struct {
	ConfigStore ports.LocalConfigStore
	PlatformAPI ports.PlatformAPI
}

type StatusOutput struct {
	Runtime   *domain.LocalWardRuntime
	WardDraft *ports.GetWardDraftStatusResponse
}

func (s StatusService) Execute(ctx context.Context) (*StatusOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("status service: config store is required")
	}

	runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, err
	}

	var wardDraft *ports.GetWardDraftStatusResponse
	if s.PlatformAPI != nil && runtime != nil && runtime.WardDraftID != "" && runtime.WardID == "" && runtime.WardDraftSecret != "" {
		wardDraft, err = s.PlatformAPI.GetWardDraftStatus(ctx, string(runtime.Site), draftSecretChallenge(runtime.WardDraftSecret), runtime.WardDraftID)
		if err != nil {
			return nil, err
		}
	}

	return &StatusOutput{
		Runtime:   runtime,
		WardDraft: wardDraft,
	}, nil
}
