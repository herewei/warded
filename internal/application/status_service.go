package application

import (
	"context"
	"fmt"
	"time"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type StatusService struct {
	ConfigStore ports.LocalConfigStore
	PlatformAPI ports.PlatformAPI
}

type StatusOutput struct {
	Runtime         *domain.LocalWardRuntime
	WardDraft       *ports.GetWardDraftStatusResponse
	Claimed         bool
	Refreshed       bool
	RefreshError    error
	LastRefreshedAt time.Time
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
	var refreshed bool
	var refreshErr error
	var lastRefreshedAt time.Time

	// 如果有未激活的 draft，检查状态并自动 claim
	if s.PlatformAPI != nil && runtime != nil && runtime.WardDraftID != "" && runtime.WardID == "" && runtime.WardDraftSecret != "" {
		wardDraft, err = s.PlatformAPI.GetWardDraftStatus(ctx, string(runtime.Site), draftSecretChallenge(runtime.WardDraftSecret), runtime.WardDraftID)
		if err != nil {
			// 草稿阶段平台不可达，记录错误但不失败
			refreshErr = err
		} else {
			refreshed = true
			lastRefreshedAt = time.Now().UTC()

			// 处理草稿状态
			if wardDraft != nil {
				switch wardDraft.Status {
				case "converted_pending_claim", "claimed":
					// 自动执行 claim（claim 内部会保存）
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
				case "expired", "failed":
					// 更新本地状态并保存
					runtime.WardStatus = domain.WardStatus(wardDraft.Status)
					runtime.LastRefreshedAt = lastRefreshedAt
					runtime.UpdatedAt = lastRefreshedAt
					if saveErr := s.ConfigStore.SaveWardRuntime(ctx, *runtime); saveErr != nil {
						return nil, saveErr
					}
				default:
					// pending_activation, activating 等非终态：只更新刷新时间并保存
					runtime.LastRefreshedAt = lastRefreshedAt
					runtime.UpdatedAt = lastRefreshedAt
					if saveErr := s.ConfigStore.SaveWardRuntime(ctx, *runtime); saveErr != nil {
						return nil, saveErr
					}
				}
			}
		}
	}

	// 已激活 ward：刷新平台状态
	if s.PlatformAPI != nil && runtime != nil && runtime.WardID != "" && runtime.WardSecret != "" {
		wardResp, err := s.PlatformAPI.GetWard(ctx, string(runtime.Site), runtime.WardSecret, runtime.WardID)
		if err != nil {
			// 平台不可达，记录错误但不失败，返回本地缓存
			refreshErr = err
		} else {
			refreshed = true
			lastRefreshedAt = time.Now().UTC()

			// 更新本地状态字段
			runtime.WardStatus = domain.WardStatus(wardResp.Status)
			runtime.Spec = domain.Spec(wardResp.Spec)
			runtime.BillingMode = domain.BillingMode(wardResp.BillingMode)
			runtime.ActivationMode = domain.ActivationMode(wardResp.ActivationMode)
			runtime.Domain = wardResp.Domain
			if wardResp.UpstreamAddr != "" {
				runtime.UpstreamAddr = wardResp.UpstreamAddr
			}
			runtime.UpstreamPort = wardResp.UpstreamPort
			if wardResp.ListenAddr != "" {
				runtime.ListenAddr = wardResp.ListenAddr
			}
			if expiresAt, err := time.Parse(time.RFC3339, wardResp.ExpiresAt); err == nil {
				runtime.ExpiresAt = expiresAt
			}
			runtime.LastRefreshedAt = lastRefreshedAt
			runtime.UpdatedAt = lastRefreshedAt

			if saveErr := s.ConfigStore.SaveWardRuntime(ctx, *runtime); saveErr != nil {
				return nil, saveErr
			}
		}
	}

	return &StatusOutput{
		Runtime:         runtime,
		WardDraft:       wardDraft,
		Claimed:         claimed,
		Refreshed:       refreshed,
		RefreshError:    refreshErr,
		LastRefreshedAt: lastRefreshedAt,
	}, nil
}