package storage

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/herewei/warded/internal/domain"
)

const (
	configVersion = 1
	wardFileName  = "ward.json"
)

var ErrNotFound = errors.New("local config not found")

type JSONStore struct {
	baseDir string
}

func NewJSONStore(baseDir string) *JSONStore {
	return &JSONStore{baseDir: baseDir}
}

func (s *JSONStore) LoadWardRuntime(ctx context.Context) (*domain.LocalWardRuntime, error) {
	var file wardFile
	ok, err := s.load(ctx, wardFileName, &file)
	if err != nil || !ok {
		return nil, err
	}
	return &domain.LocalWardRuntime{
		Site:                   domain.Site(file.Site),
		WardDraftID:            file.WardDraftID,
		WardDraftSecret:        file.WardDraftSecret,
		WardID:                 file.WardID,
		WardSecret:             file.WardSecret,
		JWTSigningSecret:       file.JWTSigningSecret,
		WardStatus:             domain.WardStatus(file.WardStatus),
		Spec:                   domain.Spec(file.Spec),
		BillingMode:            domain.BillingMode(file.BillingMode),
		ActivationMode:         domain.ActivationMode(file.ActivationMode),
		DomainType:             domain.DomainType(file.DomainType),
		Domain:                 file.Domain,
		UpstreamPort:           file.UpstreamPort,
		ListenAddr:             file.ListenAddr,
		TLSMode:                domain.TLSMode(file.TLSMode),
		LastPublicIP:           file.LastPublicIP,
		LastPublicIPReportedAt: derefPtrTime(file.LastPublicIPReportedAt),
		ExpiresAt:              derefPtrTime(file.ExpiresAt),
		ActivationURL:          file.ActivationURL,
		WebhookAllowPaths:      file.WebhookAllowPaths,
		UpdatedAt:              file.UpdatedAt,
	}, nil
}

func (s *JSONStore) SaveWardRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error {
	return s.save(ctx, wardFileName, wardFile{
		Version:                configVersion,
		Site:                   string(runtime.Site),
		WardDraftID:            runtime.WardDraftID,
		WardDraftSecret:        runtime.WardDraftSecret,
		WardID:                 runtime.WardID,
		WardSecret:             runtime.WardSecret,
		JWTSigningSecret:       runtime.JWTSigningSecret,
		WardStatus:             string(runtime.WardStatus),
		Spec:                   string(runtime.Spec),
		BillingMode:            string(runtime.BillingMode),
		ActivationMode:         string(runtime.ActivationMode),
		DomainType:             string(runtime.DomainType),
		Domain:                 runtime.Domain,
		UpstreamPort:           runtime.UpstreamPort,
		ListenAddr:             runtime.ListenAddr,
		TLSMode:                string(runtime.TLSMode),
		LastPublicIP:           runtime.LastPublicIP,
		LastPublicIPReportedAt: ptrTime(runtime.LastPublicIPReportedAt),
		ExpiresAt:              ptrTime(runtime.ExpiresAt),
		ActivationURL:          runtime.ActivationURL,
		WebhookAllowPaths:      runtime.WebhookAllowPaths,
		UpdatedAt:              runtime.UpdatedAt,
	})
}

func (s *JSONStore) load(_ context.Context, name string, target any) (bool, error) {
	path := filepath.Join(s.baseDir, name)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, target); err != nil {
		return false, err
	}
	return true, nil
}

func (s *JSONStore) save(_ context.Context, name string, value any, modes ...os.FileMode) error {
	mode := os.FileMode(0o600)
	if len(modes) > 0 {
		mode = modes[0]
	}
	if err := os.MkdirAll(s.baseDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(s.baseDir, name)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), mode); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func ptrTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func derefPtrTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

type wardFile struct {
	Version                int        `json:"version"`
	Site                   string     `json:"site"`
	WardDraftID            string     `json:"ward_draft_id"`
	WardDraftSecret        string     `json:"ward_draft_secret,omitempty"`
	WardID                 string     `json:"ward_id"`
	WardSecret             string     `json:"ward_secret,omitempty"`
	JWTSigningSecret       string     `json:"jwt_signing_secret,omitempty"`
	WardStatus             string     `json:"ward_status"`
	Spec                   string     `json:"spec"`
	BillingMode            string     `json:"billing_mode"`
	ActivationMode         string     `json:"activation_mode"`
	DomainType             string     `json:"domain_type"`
	Domain                 string     `json:"domain"`
	UpstreamPort           int        `json:"upstream_port"`
	ListenAddr             string     `json:"listen_addr"`
	TLSMode                string     `json:"tls_mode"`
	LastPublicIP           string     `json:"last_public_ip"`
	LastPublicIPReportedAt *time.Time `json:"last_public_ip_reported_at,omitempty"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
	ActivationURL          string     `json:"activation_url"`
	WebhookAllowPaths      []string   `json:"webhook_allow_paths"`
	UpdatedAt              time.Time  `json:"updated_at"`
}
