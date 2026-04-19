package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/herewei/warded/internal/domain"
)

func TestDefaultDataDirUsesSystemPathOnLinuxWhenPresent(t *testing.T) {
	originalGOOS := runtimeGOOS
	originalStat := osStat
	originalUserConfigDir := userDataDirFunc
	t.Cleanup(func() {
		runtimeGOOS = originalGOOS
		osStat = originalStat
		userDataDirFunc = originalUserConfigDir
	})

	runtimeGOOS = "linux"
	osStat = func(name string) (os.FileInfo, error) {
		if name != systemDataDir {
			t.Fatalf("unexpected stat path %q", name)
		}
		return os.Stat(t.TempDir())
	}
	userDataDirFunc = func() (string, error) {
		return "/Users/test/.config", nil
	}

	dir := defaultDataDir()
	if dir != systemDataDir {
		t.Fatalf("expected linux system data dir, got %q", dir)
	}
}

func TestDefaultDataDirFallsBackToUserConfigDir(t *testing.T) {
	originalGOOS := runtimeGOOS
	originalStat := osStat
	originalUserConfigDir := userDataDirFunc
	t.Cleanup(func() {
		runtimeGOOS = originalGOOS
		osStat = originalStat
		userDataDirFunc = originalUserConfigDir
	})

	runtimeGOOS = "darwin"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	userDataDirFunc = func() (string, error) {
		return "/Users/test/.config", nil
	}

	dir := defaultDataDir()
	want := filepath.Join("/Users/test/.config", "warded")
	if dir != want {
		t.Fatalf("expected user data dir %q, got %q", want, dir)
	}
}

func TestDefaultDataDirFallsBackToDotWarded(t *testing.T) {
	originalGOOS := runtimeGOOS
	originalStat := osStat
	originalUserConfigDir := userDataDirFunc
	t.Cleanup(func() {
		runtimeGOOS = originalGOOS
		osStat = originalStat
		userDataDirFunc = originalUserConfigDir
	})

	runtimeGOOS = "darwin"
	osStat = func(name string) (os.FileInfo, error) {
		return nil, os.ErrNotExist
	}
	userDataDirFunc = func() (string, error) {
		return "", errors.New("boom")
	}

	dir := defaultDataDir()
	if dir != fallbackDataDir {
		t.Fatalf("expected fallback dir %q, got %q", fallbackDataDir, dir)
	}
}

func TestResolvePlatformOriginUsesFlagOrigin(t *testing.T) {
	got, err := resolvePlatformOrigin(domain.SiteGlobal, "", "http://127.0.0.1:8080/")
	if err != nil {
		t.Fatalf("resolvePlatformOrigin() error = %v", err)
	}
	if got != "http://127.0.0.1:8080" {
		t.Fatalf("expected explicit origin override, got %q", got)
	}
}

func TestResolvePlatformOriginUsesBaseDomain(t *testing.T) {
	got, err := resolvePlatformOrigin(domain.SiteGlobal, "dev.warded.me", "")
	if err != nil {
		t.Fatalf("resolvePlatformOrigin() error = %v", err)
	}
	if got != "https://dev.warded.me" {
		t.Fatalf("expected derived https origin, got %q", got)
	}
}

func TestResolvePlatformOriginFallsBackToSiteDefault(t *testing.T) {
	got, err := resolvePlatformOrigin(domain.SiteGlobal, "", "")
	if err != nil {
		t.Fatalf("resolvePlatformOrigin() error = %v", err)
	}
	if got != "https://warded.me" {
		t.Fatalf("expected site default https origin, got %q", got)
	}
}

func TestResolvePlatformOriginRejectsSchemeInBaseDomain(t *testing.T) {
	_, err := resolvePlatformOrigin(domain.SiteGlobal, "https://dev.warded.me", "")
	if err == nil {
		t.Fatal("expected error")
	}
	if err.Error() != "base-domain must not include a scheme; use host only, for example dev.warded.me" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolvePublicPlatformBaseURLUsesBaseDomainOverride(t *testing.T) {
	got, err := resolvePublicPlatformBaseURL(domain.SiteGlobal, "preview.warded.me")
	if err != nil {
		t.Fatalf("resolvePublicPlatformBaseURL() error = %v", err)
	}
	if got != "https://preview.warded.me" {
		t.Fatalf("expected public base url override, got %q", got)
	}
}

func TestResolvePublicPlatformBaseURLUsesSiteDefault(t *testing.T) {
	got, err := resolvePublicPlatformBaseURL(domain.SiteCN, "")
	if err != nil {
		t.Fatalf("resolvePublicPlatformBaseURL() error = %v", err)
	}
	if got != "https://warded.cn" {
		t.Fatalf("expected site default public base url, got %q", got)
	}
}
