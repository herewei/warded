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
	Claimed   bool
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
	var claimed bool

	// 如果有未激活的 draft，检查状态并自动 claim
	if s.PlatformAPI != nil && runtime != nil && runtime.WardDraftID != "" && runtime.WardID == "" && runtime.WardDraftSecret != "" {
		wardDraft, err = s.PlatformAPI.GetWardDraftStatus(ctx, string(runtime.Site), draftSecretChallenge(runtime.WardDraftSecret), runtime.WardDraftID)
		if err != nil {
			return nil, err
		}

		// 如果状态是 converted_pending_claim 或 claimed，自动执行 claim
		if wardDraft != nil && (wardDraft.Status == "converted_pending_claim" || wardDraft.Status == "claimed") {
			activationService := DraftActivationService{
				ConfigStore: s.ConfigStore,
				PlatformAPI: s.PlatformAPI,
			}
			updatedRuntime, finalized, err := activationService.FinalizeIfConverted(ctx, wardDraft)
			if err != nil {
				return nil, err
			}
			if finalized && updatedRuntime != nil {
				runtime = updatedRuntime
				claimed = true
				wardDraft = nil
			}
		}
	}

	return &StatusOutput{
		Runtime:   runtime,
		WardDraft: wardDraft,
		Claimed:   claimed,
	}, nil
}
