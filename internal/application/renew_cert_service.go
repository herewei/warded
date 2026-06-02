package application

import (
	"context"
	"fmt"
	"time"

	"github.com/herewei/warded/internal/application/mapping"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

type RenewCertService struct {
	ConfigStore ports.LocalConfigStore
	TLSAPI      ports.TLSMaterialAPI
}

type RenewCertOutput struct {
	Domain        string
	NotAfter      time.Time
	DaysRemaining int
	LastRenewedAt time.Time
}

func (s RenewCertService) Execute(ctx context.Context) (*RenewCertOutput, error) {
	if s.ConfigStore == nil {
		return nil, fmt.Errorf("renew-cert: config store is required")
	}
	if s.TLSAPI == nil {
		return nil, fmt.Errorf("renew-cert: TLS material API is required")
	}

	record, err := s.ConfigStore.LoadWardRuntime(ctx)
	if err != nil {
		return nil, fmt.Errorf("renew-cert: load ward runtime: %w", err)
	}
	if record == nil {
		return nil, fmt.Errorf("renew-cert: no ward runtime found — run 'warded new --commit' first")
	}
	runtime := mapping.DomainFromRuntimeRecord(record)
	if runtime.WardID == "" || runtime.WardSecret == "" {
		return nil, fmt.Errorf("renew-cert: ward is not activated")
	}
	if runtime.TLSMode != domain.TLSModePlatformWildcard {
		return nil, fmt.Errorf("renew-cert: TLS certificate is managed automatically by ACME for custom domains — no manual renewal needed")
	}

	resp, err := s.TLSAPI.GetTLSMaterial(ctx, string(runtime.Site), runtime.WardSecret, runtime.WardID)
	if err != nil {
		return nil, fmt.Errorf("renew-cert: fetch TLS material: %w", err)
	}

	var notAfter time.Time
	if resp.NotAfter != "" {
		notAfter, _ = time.Parse(time.RFC3339, resp.NotAfter)
	}

	now := time.Now().UTC()
	runtime.LastCertRenewedAt = now
	runtime.UpdatedAt = now
	if err := s.ConfigStore.SaveWardRuntime(ctx, mapping.RuntimeRecordFromDomain(runtime)); err != nil {
		return nil, fmt.Errorf("renew-cert: save ward runtime: %w", err)
	}

	daysRemaining := 0
	if !notAfter.IsZero() {
		daysRemaining = int(time.Until(notAfter).Hours() / 24)
	}

	return &RenewCertOutput{
		Domain:        runtime.Domain,
		NotAfter:      notAfter,
		DaysRemaining: daysRemaining,
		LastRenewedAt: now,
	}, nil
}
