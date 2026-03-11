package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/sitepolicy"
)

const (
	systemConfigDir   = "/var/lib/warded"
	fallbackConfigDir = ".warded"
)

var (
	runtimeGOOS       = runtime.GOOS
	osStat            = os.Stat
	userConfigDirFunc = os.UserConfigDir
)

func defaultConfigDir() string {
	if runtimeGOOS == "linux" {
		if info, err := osStat(systemConfigDir); err == nil && info.IsDir() {
			return systemConfigDir
		}
	}
	if dir, err := userConfigDirFunc(); err == nil && dir != "" {
		return filepath.Join(dir, "warded")
	}
	return fallbackConfigDir
}

func resolvePlatformOrigin(site domain.Site, baseDomain string, platformOrigin string) (string, error) {
	if platformOrigin != "" {
		return sitepolicy.NormalizeBaseURL(platformOrigin), nil
	}

	return resolvePublicPlatformBaseURL(site, baseDomain)
}

func resolvePublicPlatformBaseURL(site domain.Site, baseDomain string) (string, error) {
	if baseDomain != "" {
		normalizedBaseDomain, err := normalizeBaseDomain(baseDomain)
		if err != nil {
			return "", err
		}
		return "https://" + normalizedBaseDomain, nil
	}

	return sitepolicy.ForSite(site).PlatformBaseURL(), nil
}

func normalizeBaseDomain(raw string) (string, error) {
	baseDomain := strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	if baseDomain == "" {
		return "", fmt.Errorf("base-domain cannot be empty")
	}
	if strings.Contains(baseDomain, "://") {
		return "", fmt.Errorf("base-domain must not include a scheme; use host only, for example dev.warded.me")
	}
	if strings.Contains(baseDomain, "/") {
		return "", fmt.Errorf("base-domain must not include a path; use host only, for example dev.warded.me")
	}
	return baseDomain, nil
}
