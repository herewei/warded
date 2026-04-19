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
	systemDataDir   = "/var/lib/warded"
	fallbackDataDir = ".warded"
)

var (
	runtimeGOOS     = runtime.GOOS
	osStat          = os.Stat
	userDataDirFunc = os.UserConfigDir
)

func defaultDataDir() string {
	if runtimeGOOS == "linux" {
		if info, err := osStat(systemDataDir); err == nil && info.IsDir() {
			return systemDataDir
		}
	}
	if dir, err := userDataDirFunc(); err == nil && dir != "" {
		return filepath.Join(dir, "warded")
	}
	return fallbackDataDir
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
