package application

import (
	"context"
	"fmt"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type DraftActivationService struct {
	ConfigStore ports.LocalConfigStore
	PlatformAPI ports.PlatformAPI
}

func (s DraftActivationService) FinalizeIfConverted(ctx context.Context) (*domain.LocalWardRuntime, bool, error) {
	if s.ConfigStore == nil {
		return nil, false, fmt.Errorf("draft activation service: config store is required")
	}
	if s.PlatformAPI == nil {
		return nil, false, fmt.Errorf("draft activation service: platform API is required")
	}

	runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, false, err
	}
	if runtime == nil || runtime.WardID != "" || runtime.WardDraftID == "" || runtime.WardDraftSecret == "" {
		return nil, false, nil
	}

	wardDraft, err := s.PlatformAPI.GetWardDraftStatus(ctx, string(runtime.Site), draftSecretChallenge(runtime.WardDraftSecret), runtime.WardDraftID)
	if err != nil {
		return nil, false, err
	}
	if wardDraft == nil || wardDraft.Status != "converted_pending_claim" && wardDraft.Status != "claimed" {
		return nil, false, nil
	}
	claimResp, err := s.PlatformAPI.ClaimWardDraft(ctx, ports.ClaimWardDraftRequest{
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

func (s DraftActivationService) WaitUntilActivated(ctx context.Context, interval time.Duration) (*domain.LocalWardRuntime, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("draft activation service: config store is required")
	}
	if s.PlatformAPI == nil {
		return nil, fmt.Errorf("draft activation service: platform API is required")
	}
	if interval <= 0 {
		interval = 3 * time.Second
	}

	for {
		runtime, err := s.ConfigStore.LoadWardRuntime(ctx)
		if err != nil {
			return nil, err
		}
		if runtime == nil || runtime.WardDraftID == "" || runtime.WardDraftSecret == "" {
			return nil, fmt.Errorf("draft activation service: no pending ward draft found for activation wait")
		}
		if runtime.WardID != "" && runtime.WardSecret != "" && runtime.WardStatus == domain.WardStatusActive {
			return runtime, nil
		}

		wardDraft, err := s.PlatformAPI.GetWardDraftStatus(ctx, string(runtime.Site), draftSecretChallenge(runtime.WardDraftSecret), runtime.WardDraftID)
		if err != nil {
			return nil, err
		}
		if wardDraft != nil {
			switch wardDraft.Status {
			case "converted_pending_claim", "claimed":
				claimResp, err := s.PlatformAPI.ClaimWardDraft(ctx, ports.ClaimWardDraftRequest{
					DraftSecret: runtime.WardDraftSecret,
					Site:        string(runtime.Site),
				}, runtime.WardDraftID)
				if err != nil {
					return nil, err
				}
				runtime, err = s.persistClaimedDraft(ctx, runtime, claimResp)
				if err != nil {
					return nil, err
				}
				return runtime, nil
			case "expired":
				return nil, fmt.Errorf("draft activation service: ward draft expired")
			case "failed":
				return nil, fmt.Errorf("draft activation service: ward draft activation failed")
			}
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
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

	wardResp, err := s.PlatformAPI.GetWard(ctx, string(runtime.Site), claimed.WardSecret, claimed.WardID)
	if err != nil {
		return nil, err
	}
	runtime.WardID = claimed.WardID
	runtime.WardSecret = claimed.WardSecret
	runtime.WardDraftSecret = ""
	runtime.WardStatus = domain.WardStatus(wardResp.Status)
	runtime.DomainType = domain.DomainType(wardResp.DomainType)
	runtime.Domain = wardResp.Domain
	runtime.UpstreamPort = wardResp.UpstreamPort
	runtime.BillingMode = domain.BillingMode(wardResp.BillingMode)
	runtime.ActivationMode = domain.ActivationMode(wardResp.ActivationMode)
	// Ensure Site is set from ward response if it was empty
	if runtime.Site == "" {
		runtime.Site = domain.Site(wardResp.Site)
	}
	runtime.TLSMode, err = tlsModeForDomainType(runtime.DomainType)
	if err != nil {
		return nil, fmt.Errorf("draft activation service: %w", err)
	}
	if expiresAt, err := time.Parse(time.RFC3339, wardResp.ExpiresAt); err == nil {
		runtime.ExpiresAt = expiresAt
	}
	runtime.ActivationURL = ""
	runtime.UpdatedAt = time.Now().UTC()
	if err := s.ConfigStore.SaveWardRuntime(ctx, *runtime); err != nil {
		return nil, err
	}
	return runtime, nil
}
