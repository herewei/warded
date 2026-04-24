package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/herewei/warded/internal/domain"
)

const (
	configVersion  = 1
	wardFileName   = "ward.json"
	pendingWardDir = ".pending"
)

var ErrNotFound = errors.New("local config not found")

type JSONStore struct {
	baseDir string
	wardDir string
}

func NewJSONStore(baseDir string) *JSONStore {
	return &JSONStore{baseDir: baseDir}
}

func (s *JSONStore) LoadWardRuntime(ctx context.Context) (*domain.LocalWardRuntime, error) {
	if s.wardDir != "" {
		runtime, ok, err := s.loadFromDir(ctx, s.wardDir)
		if err != nil {
			return nil, err
		}
		if ok {
			return runtime, nil
		}
		s.wardDir = ""
	}

	dirs, err := s.scanWardDirs()
	if err != nil {
		return nil, err
	}
	switch len(dirs) {
	case 0:
		return nil, nil
	case 1:
		s.wardDir = dirs[0]
		runtime, _, err := s.loadFromDir(ctx, dirs[0])
		return runtime, err
	default:
		return nil, fmt.Errorf("multiple ward runtimes found under %s", s.wardsBaseDir())
	}
}

func (s *JSONStore) SaveWardRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error {
	targetDir := s.computeTargetDir(runtime)
	if s.wardDir != "" && filepath.Clean(s.wardDir) != filepath.Clean(targetDir) {
		if _, err := os.Stat(targetDir); err == nil {
			return fmt.Errorf("target ward runtime directory already exists: %s", targetDir)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
			return err
		}
		if err := os.Rename(s.wardDir, targetDir); err != nil {
			return err
		}
	}
	s.wardDir = targetDir
	return s.saveToDir(ctx, targetDir, runtime)
}

func (s *JSONStore) wardsBaseDir() string {
	return s.baseDir
}

func (s *JSONStore) computeTargetDir(runtime domain.LocalWardRuntime) string {
	switch {
	case runtime.WardID != "":
		return filepath.Join(s.wardsBaseDir(), runtime.WardID)
	case runtime.WardDraftID != "":
		return filepath.Join(s.wardsBaseDir(), runtime.WardDraftID)
	case s.wardDir != "":
		return s.wardDir
	default:
		return filepath.Join(s.wardsBaseDir(), pendingWardDir)
	}
}

func (s *JSONStore) scanWardDirs() ([]string, error) {
	entries, err := os.ReadDir(s.wardsBaseDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var dirs []string
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(s.wardsBaseDir(), entry.Name())
		if _, err := os.Stat(filepath.Join(dir, wardFileName)); err == nil {
			dirs = append(dirs, dir)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

func (s *JSONStore) loadFromDir(_ context.Context, dir string) (*domain.LocalWardRuntime, bool, error) {
	path := filepath.Join(dir, wardFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var file wardFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, false, err
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
		RequestedDomain:        file.RequestedDomain,
		Domain:                 file.Domain,
		UpstreamPort:           file.UpstreamPort,
		ListenAddr:             file.ListenAddr,
		TLSMode:                domain.TLSMode(file.TLSMode),
		LastPublicIP:           file.LastPublicIP,
		LastPublicIPReportedAt: derefPtrTime(file.LastPublicIPReportedAt),
		ExpiresAt:              derefPtrTime(file.ExpiresAt),
		LastCertRenewedAt:      derefPtrTime(file.LastCertRenewedAt),
		ActivationURL:          file.ActivationURL,
		WebhookAllowPaths:      file.WebhookAllowPaths,
		UpdatedAt:              file.UpdatedAt,
	}, true, nil
}

func (s *JSONStore) saveToDir(_ context.Context, dir string, runtime domain.LocalWardRuntime) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(wardFile{
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
		RequestedDomain:        runtime.RequestedDomain,
		Domain:                 runtime.Domain,
		UpstreamPort:           runtime.UpstreamPort,
		ListenAddr:             runtime.ListenAddr,
		TLSMode:                string(runtime.TLSMode),
		LastPublicIP:           runtime.LastPublicIP,
		LastPublicIPReportedAt: ptrTime(runtime.LastPublicIPReportedAt),
		ExpiresAt:              ptrTime(runtime.ExpiresAt),
		LastCertRenewedAt:      ptrTime(runtime.LastCertRenewedAt),
		ActivationURL:          runtime.ActivationURL,
		WebhookAllowPaths:      runtime.WebhookAllowPaths,
		UpdatedAt:              runtime.UpdatedAt,
	}, "", "  ")
	if err != nil {
		return err
	}
	path := filepath.Join(dir, wardFileName)
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, append(data, '\n'), 0o600); err != nil {
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
	RequestedDomain        string     `json:"requested_domain,omitempty"`
	Domain                 string     `json:"domain"`
	UpstreamPort           int        `json:"upstream_port"`
	ListenAddr             string     `json:"listen_addr"`
	TLSMode                string     `json:"tls_mode"`
	LastPublicIP           string     `json:"last_public_ip"`
	LastPublicIPReportedAt *time.Time `json:"last_public_ip_reported_at,omitempty"`
	ExpiresAt              *time.Time `json:"expires_at,omitempty"`
	ActivationURL          string     `json:"activation_url"`
	LastCertRenewedAt      *time.Time `json:"last_cert_renewed_at,omitempty"`
	WebhookAllowPaths      []string   `json:"webhook_allow_paths"`
	UpdatedAt              time.Time  `json:"updated_at"`
}
