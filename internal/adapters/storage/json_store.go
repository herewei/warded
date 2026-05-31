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

// ErrMultipleRuntimes is returned by LoadWardRuntime when more than one ward
// directory is found and the caller must select a specific one.
var ErrMultipleRuntimes = errors.New("multiple ward runtimes found")

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
		return nil, fmt.Errorf("%w under %s", ErrMultipleRuntimes, s.wardsBaseDir())
	}
}

func (s *JSONStore) SaveWardRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error {
	if filepath.Clean(s.wardDir) == filepath.Clean(s.pendingDir()) && runtime.WardID == "" {
		return s.saveToDir(ctx, s.pendingDir(), runtime)
	}
	targetDir := s.computeTargetDir(runtime)
	if s.wardDir != "" && filepath.Clean(s.wardDir) != filepath.Clean(targetDir) {
		if _, err := os.Stat(targetDir); err == nil {
			if sameRuntime, loadErr := s.targetDirContainsSameRuntime(ctx, targetDir, runtime); loadErr != nil {
				return loadErr
			} else if !sameRuntime {
				return fmt.Errorf("target ward runtime directory already exists: %s", targetDir)
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		} else {
			if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
				return err
			}
			if err := os.Rename(s.wardDir, targetDir); err != nil {
				return err
			}
		}
	}
	s.wardDir = targetDir
	return s.saveToDir(ctx, targetDir, runtime)
}

func (s *JSONStore) targetDirContainsSameRuntime(ctx context.Context, dir string, runtime domain.LocalWardRuntime) (bool, error) {
	existing, ok, err := s.loadFromDir(ctx, dir)
	if err != nil {
		return false, err
	}
	if !ok || existing == nil {
		return false, nil
	}
	if runtime.WardID != "" {
		return existing.WardID == runtime.WardID, nil
	}
	if runtime.WardDraftID != "" {
		return existing.WardDraftID == runtime.WardDraftID, nil
	}
	return existing.WardID == "" && existing.WardDraftID == "", nil
}

func (s *JSONStore) ListWardRuntimes(ctx context.Context) ([]domain.LocalWardRuntime, error) {
	var runtimes []domain.LocalWardRuntime
	if rt, ok, err := s.loadFromDir(ctx, s.pendingDir()); err != nil {
		return nil, err
	} else if ok {
		runtimes = append(runtimes, *rt)
	}
	dirs, err := s.scanWardDirs()
	if err != nil {
		return nil, err
	}
	for _, dir := range dirs {
		rt, ok, err := s.loadFromDir(ctx, dir)
		if err != nil {
			return nil, err
		}
		if ok {
			runtimes = append(runtimes, *rt)
		}
	}
	return runtimes, nil
}

func (s *JSONStore) LoadRuntimeByID(ctx context.Context, id string) (*domain.LocalWardRuntime, error) {
	if rt, ok, err := s.loadFromDir(ctx, s.pendingDir()); err != nil {
		return nil, err
	} else if ok && rt != nil && (rt.WardDraftID == id || rt.WardID == id) {
		s.wardDir = s.pendingDir()
		return rt, nil
	}

	dir := filepath.Join(s.wardsBaseDir(), id)
	rt, ok, err := s.loadFromDir(ctx, dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrNotFound
	}
	s.wardDir = dir
	return rt, nil
}

func (s *JSONStore) LoadPendingRuntime(ctx context.Context) (*domain.LocalWardRuntime, error) {
	runtime, ok, err := s.loadFromDir(ctx, s.pendingDir())
	if err != nil || !ok {
		return nil, err
	}
	return runtime, nil
}

func (s *JSONStore) SavePendingRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error {
	runtime.WardID = ""
	runtime.WardSecret = ""
	return s.saveToDir(ctx, s.pendingDir(), runtime)
}

func (s *JSONStore) CommitPendingRuntime(ctx context.Context, runtime domain.LocalWardRuntime) error {
	targetDir := s.computeTargetDir(runtime)
	pendingDir := s.pendingDir()
	if filepath.Clean(targetDir) == filepath.Clean(pendingDir) {
		return s.saveToDir(ctx, pendingDir, runtime)
	}
	if _, err := os.Stat(targetDir); err == nil {
		return fmt.Errorf("target ward runtime directory already exists: %s", targetDir)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if _, err := os.Stat(pendingDir); err == nil {
		if err := os.MkdirAll(filepath.Dir(targetDir), 0o755); err != nil {
			return err
		}
		if err := os.Rename(pendingDir, targetDir); err != nil {
			return err
		}
		s.wardDir = targetDir
		return s.saveToDir(ctx, targetDir, runtime)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	s.wardDir = targetDir
	return s.saveToDir(ctx, targetDir, runtime)
}

func (s *JSONStore) wardsBaseDir() string {
	return s.baseDir
}

func (s *JSONStore) pendingDir() string {
	return filepath.Join(s.wardsBaseDir(), pendingWardDir)
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
		if entry.Name() == pendingWardDir {
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
	if file.ListenAddr != "" {
		return nil, false, fmt.Errorf("deprecated config in %s: 'listen_addr' is no longer supported; re-run 'warded new' with --port and --listen or --listen-v6", path)
	}
	rt := &domain.LocalWardRuntime{
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
		UpstreamAddr:           file.UpstreamAddr,
		UpstreamPort:           file.UpstreamPort,
		ListenPort:             file.ListenPort,
		ListenHost:             file.ListenHost,
		IngressFamily:          domain.IngressFamily(file.IngressFamily),
		IngressMode:            domain.IngressMode(file.IngressMode),
		ServeTLS:               file.ServeTLS,
		PublicPort:             file.PublicPort,
		TrustedProxyCIDRs:      append([]string(nil), file.TrustedProxyCIDRs...),
		TLSMode:                domain.TLSMode(file.TLSMode),
		LastPublicIP:           file.LastPublicIP,
		LastPublicIPReportedAt: derefPtrTime(file.LastPublicIPReportedAt),
		ExpiresAt:              derefPtrTime(file.ExpiresAt),
		LastCertRenewedAt:      derefPtrTime(file.LastCertRenewedAt),
		LastRefreshedAt:        derefPtrTime(file.LastRefreshedAt),
		ActivationURL:          file.ActivationURL,
		PlatformJWTPublicKeys:  file.PlatformJWTPublicKeys,
		AuthWhitelist:          file.AuthWhitelist,
		UpdatedAt:              file.UpdatedAt,
	}
	if file.UpstreamMode != "" {
		rt.UpstreamMode = domain.UpstreamMode(file.UpstreamMode)
	} else {
		rt.UpstreamMode = domain.UpstreamModeDaemon
	}
	normalizeRuntimeIngressDefaults(rt)
	rt.UpstreamCommand = file.UpstreamCommand
	return rt, true, nil
}

func (s *JSONStore) saveToDir(_ context.Context, dir string, runtime domain.LocalWardRuntime) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	mode := string(runtime.UpstreamMode)
	if mode == "" {
		mode = string(domain.UpstreamModeDaemon)
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
		UpstreamAddr:           runtime.UpstreamAddr,
		UpstreamPort:           runtime.UpstreamPort,
		UpstreamMode:           mode,
		UpstreamCommand:        runtime.UpstreamCommand,
		ListenPort:             runtime.ListenPort,
		ListenHost:             runtime.ListenHost,
		IngressFamily:          string(runtime.IngressFamily),
		IngressMode:            string(effectiveIngressMode(&runtime)),
		ServeTLS:               effectiveServeTLS(&runtime),
		PublicPort:             effectivePublicPort(&runtime),
		TrustedProxyCIDRs:      runtime.TrustedProxyCIDRs,
		TLSMode:                string(runtime.TLSMode),
		LastPublicIP:           runtime.LastPublicIP,
		LastPublicIPReportedAt: ptrTime(runtime.LastPublicIPReportedAt),
		ExpiresAt:              ptrTime(runtime.ExpiresAt),
		LastCertRenewedAt:      ptrTime(runtime.LastCertRenewedAt),
		LastRefreshedAt:        ptrTime(runtime.LastRefreshedAt),
		ActivationURL:          runtime.ActivationURL,
		PlatformJWTPublicKeys:  runtime.PlatformJWTPublicKeys,
		AuthWhitelist:          runtime.AuthWhitelist,
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

func normalizeRuntimeIngressDefaults(rt *domain.LocalWardRuntime) {
	if rt == nil {
		return
	}
	if rt.IngressMode == "" {
		rt.IngressMode = domain.IngressModeStandalone
	}
	if rt.PublicPort == 0 {
		if rt.IngressMode == domain.IngressModeStandalone && rt.ListenPort > 0 {
			rt.PublicPort = rt.ListenPort
		} else {
			rt.PublicPort = 443
		}
	}
	rt.ServeTLS = rt.IngressMode != domain.IngressModeBehindProxy
	if rt.TrustedProxyCIDRs == nil {
		rt.TrustedProxyCIDRs = []string{}
	}
}

func effectiveIngressMode(rt *domain.LocalWardRuntime) domain.IngressMode {
	if rt == nil || rt.IngressMode == "" {
		return domain.IngressModeStandalone
	}
	return rt.IngressMode
}

func effectiveServeTLS(rt *domain.LocalWardRuntime) bool {
	return effectiveIngressMode(rt) != domain.IngressModeBehindProxy
}

func effectivePublicPort(rt *domain.LocalWardRuntime) int {
	if rt == nil {
		return 443
	}
	if rt.PublicPort > 0 {
		return rt.PublicPort
	}
	if effectiveIngressMode(rt) == domain.IngressModeStandalone && rt.ListenPort > 0 {
		return rt.ListenPort
	}
	return 443
}

type wardFile struct {
	Version                int                           `json:"version"`
	Site                   string                        `json:"site"`
	WardDraftID            string                        `json:"ward_draft_id"`
	WardDraftSecret        string                        `json:"ward_draft_secret,omitempty"`
	WardID                 string                        `json:"ward_id"`
	WardSecret             string                        `json:"ward_secret,omitempty"`
	JWTSigningSecret       string                        `json:"jwt_signing_secret,omitempty"`
	WardStatus             string                        `json:"ward_status"`
	Spec                   string                        `json:"spec"`
	BillingMode            string                        `json:"billing_mode"`
	ActivationMode         string                        `json:"activation_mode"`
	DomainType             string                        `json:"domain_type"`
	RequestedDomain        string                        `json:"requested_domain,omitempty"`
	Domain                 string                        `json:"domain"`
	UpstreamAddr           string                        `json:"upstream_addr"`
	UpstreamPort           int                           `json:"upstream_port"`
	UpstreamMode           string                        `json:"upstream_mode"`
	UpstreamCommand        string                        `json:"upstream_command"`
	ListenAddr             string                        `json:"listen_addr,omitempty"` // Deprecated
	ListenPort             int                           `json:"listen_port"`
	ListenHost             string                        `json:"listen_host"`
	IngressFamily          string                        `json:"ingress_family"`
	IngressMode            string                        `json:"ingress_mode,omitempty"`
	ServeTLS               bool                          `json:"serve_tls"`
	PublicPort             int                           `json:"public_port,omitempty"`
	TrustedProxyCIDRs      []string                      `json:"trusted_proxy_cidrs,omitempty"`
	TLSMode                string                        `json:"tls_mode"`
	LastPublicIP           string                        `json:"last_public_ip"`
	LastPublicIPReportedAt *time.Time                    `json:"last_public_ip_reported_at,omitempty"`
	ExpiresAt              *time.Time                    `json:"expires_at,omitempty"`
	ActivationURL          string                        `json:"activation_url"`
	LastCertRenewedAt      *time.Time                    `json:"last_cert_renewed_at,omitempty"`
	LastRefreshedAt        *time.Time                    `json:"last_refreshed_at,omitempty"`
	PlatformJWTPublicKeys  []domain.PlatformJWTPublicKey `json:"platform_jwt_public_keys,omitempty"`
	AuthWhitelist          []domain.AuthWhitelistRule    `json:"auth_whitelist,omitempty"`
	UpdatedAt              time.Time                     `json:"updated_at"`
}
