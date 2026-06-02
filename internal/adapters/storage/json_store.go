package storage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
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

func (s *JSONStore) LoadWardRuntime(ctx context.Context) (*ports.RuntimeRecord, error) {
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

func (s *JSONStore) SaveWardRuntime(ctx context.Context, runtime ports.RuntimeRecord) error {
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

func (s *JSONStore) targetDirContainsSameRuntime(ctx context.Context, dir string, runtime ports.RuntimeRecord) (bool, error) {
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

func (s *JSONStore) ListWardRuntimes(ctx context.Context) ([]ports.RuntimeRecord, error) {
	var runtimes []ports.RuntimeRecord
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

func (s *JSONStore) LoadRuntimeByID(ctx context.Context, id string) (*ports.RuntimeRecord, error) {
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

func (s *JSONStore) LoadPendingRuntime(ctx context.Context) (*ports.RuntimeRecord, error) {
	runtime, ok, err := s.loadFromDir(ctx, s.pendingDir())
	if err != nil || !ok {
		return nil, err
	}
	return runtime, nil
}

func (s *JSONStore) SavePendingRuntime(ctx context.Context, runtime ports.RuntimeRecord) error {
	runtime.WardID = ""
	runtime.WardSecret = ""
	return s.saveToDir(ctx, s.pendingDir(), runtime)
}

func (s *JSONStore) CommitPendingRuntime(ctx context.Context, runtime ports.RuntimeRecord) error {
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

func (s *JSONStore) computeTargetDir(runtime ports.RuntimeRecord) string {
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

func (s *JSONStore) loadFromDir(_ context.Context, dir string) (*ports.RuntimeRecord, bool, error) {
	path := filepath.Join(dir, wardFileName)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, err
	}
	var record ports.RuntimeRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, false, err
	}
	if record.ListenAddr != "" {
		return nil, false, fmt.Errorf("deprecated config in %s: 'listen_addr' is no longer supported; re-run 'warded new' with --port and --listen or --listen-v6", path)
	}
	if record.UpstreamMode == "" {
		record.UpstreamMode = string(domain.UpstreamModeDaemon)
	}
	if record.IngressMode == "" {
		record.IngressMode = string(domain.IngressModeStandalone)
	}
	if record.PublicPort == 0 {
		if record.IngressMode == string(domain.IngressModeStandalone) && record.ListenPort > 0 {
			record.PublicPort = record.ListenPort
		} else {
			record.PublicPort = 443
		}
	}
	record.ServeTLS = record.IngressMode != string(domain.IngressModeBehindProxy)
	if record.TrustedProxyCIDRs == nil {
		record.TrustedProxyCIDRs = []string{}
	}
	return &record, true, nil
}

func (s *JSONStore) saveToDir(_ context.Context, dir string, runtime ports.RuntimeRecord) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if runtime.UpstreamMode == "" {
		runtime.UpstreamMode = string(domain.UpstreamModeDaemon)
	}
	if runtime.IngressMode == "" {
		runtime.IngressMode = string(domain.IngressModeStandalone)
	}
	if runtime.PublicPort == 0 {
		if runtime.IngressMode == string(domain.IngressModeStandalone) && runtime.ListenPort > 0 {
			runtime.PublicPort = runtime.ListenPort
		} else {
			runtime.PublicPort = 443
		}
	}
	runtime.ServeTLS = runtime.IngressMode != string(domain.IngressModeBehindProxy)
	if runtime.TrustedProxyCIDRs == nil {
		runtime.TrustedProxyCIDRs = []string{}
	}
	data, err := json.MarshalIndent(runtime, "", "  ")
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
