package application

import (
	"context"
	"fmt"
	"time"

	"github.com/herewei/warded/internal/application/mapping"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type DraftActivationService struct {
	ConfigStore ports.LocalConfigStore
	DraftAPI    ports.WardDraftAPI
	RuntimeAPI  ports.WardRuntimeAPI
}

func (s DraftActivationService) FinalizeIfConverted(ctx context.Context, prefetchedStatus ...*ports.GetWardDraftStatusResponse) (*domain.LocalWardRuntime, bool, error) {
	if s.ConfigStore == nil {
		return nil, false, fmt.Errorf("draft activation service: config store is required")
	}
	if s.DraftAPI == nil {
		return nil, false, fmt.Errorf("draft activation service: draft API is required")
	}
	if s.RuntimeAPI == nil {
		return nil, false, fmt.Errorf("draft activation service: runtime API is required")
	}

	record, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, false, err
	}
	if record == nil || record.WardID != "" || record.WardDraftID == "" || record.WardDraftSecret == "" {
		return nil, false, nil
	}
	runtime := mapping.DomainFromRuntimeRecord(record)

	var wardDraft *ports.GetWardDraftStatusResponse
	if len(prefetchedStatus) > 0 && prefetchedStatus[0] != nil {
		wardDraft = prefetchedStatus[0]
	} else {
		wardDraft, err = s.DraftAPI.GetWardDraftStatus(ctx, string(runtime.Site), draftSecretChallenge(runtime.WardDraftSecret), runtime.WardDraftID)
		if err != nil {
			if shouldCreateFreshDraft(err) {
				clearDraftState(runtime)
				runtime.UpdatedAt = time.Now().UTC()
				if saveErr := s.ConfigStore.SaveWardRuntime(ctx, mapping.RuntimeRecordFromDomain(runtime)); saveErr != nil {
					return nil, false, saveErr
				}
				return runtime, false, nil
			}
			return nil, false, err
		}
	}

	if wardDraft == nil || (wardDraft.Status != "converted_pending_claim" && wardDraft.Status != "claimed") {
		if wardDraft != nil {
			switch wardDraft.Status {
			case "expired", "failed":
				clearDraftState(runtime)
				runtime.UpdatedAt = time.Now().UTC()
				if saveErr := s.ConfigStore.SaveWardRuntime(ctx, mapping.RuntimeRecordFromDomain(runtime)); saveErr != nil {
					return nil, false, saveErr
				}
			}
		}
		return nil, false, nil
	}
	claimResp, err := s.DraftAPI.ClaimWardDraft(ctx, ports.ClaimWardDraftRequest{
		DraftSecret: runtime.WardDraftSecret,
		Site:        string(runtime.Site),
	}, runtime.WardDraftID)
	if err != nil {
		return nil, false, err
	}
	runtime, err = s.persistClaimedDraft(ctx, runtime, claimResp)
	if err != nil {
		return nil, false, err
	}
	return runtime, true, nil
}

func (s DraftActivationService) persistClaimedDraft(ctx context.Context, runtime *domain.LocalWardRuntime, claimed *ports.ClaimWardDraftResponse) (*domain.LocalWardRuntime, error) {
	if runtime == nil {
		return nil, fmt.Errorf("draft activation service: runtime is required")
	}
	if claimed == nil {
		return nil, fmt.Errorf("draft activation service: claim response is required")
	}
	if claimed.WardID == "" || claimed.WardSecret == "" {
		return nil, fmt.Errorf("draft activation service: claim response is missing ward credentials")
	}

	wardResp, err := s.RuntimeAPI.GetWard(ctx, string(runtime.Site), claimed.WardSecret, claimed.WardID)
	if err != nil {
		return nil, err
	}
	if err := mapping.ApplyClaimAndWardResponse(runtime, claimed, wardResp, time.Now().UTC()); err != nil {
		return nil, err
	}
	if err := s.ConfigStore.SaveWardRuntime(ctx, mapping.RuntimeRecordFromDomain(runtime)); err != nil {
		return nil, err
	}
	return runtime, nil
}
