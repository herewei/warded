package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/sitepolicy"
)

// printWardHeader prints a consistent ward identity header.
//
// Format:
//
//	Ward: <label>
//	══════════════════════════════
func printWardHeader(w io.Writer, label string) {
	fmt.Fprintln(w)
	line := "Ward: " + label
	fmt.Fprintln(w, line)
	fmt.Fprintln(w, strings.Repeat("═", len(line)))
	fmt.Fprintln(w)
}

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

func resolvePlatformOrigin(site domain.Site, baseDomain, platformOrigin string) (string, error) {
	// Priority 1: explicit platform origin (for local dev/testing)
	if platformOrigin != "" {
		return sitepolicy.NormalizeBaseURL(platformOrigin), nil
	}

	// Priority 2: base domain override
	if baseDomain != "" {
		return buildHTTPSURLFromDomain(baseDomain)
	}

	// Priority 3: site default
	return sitepolicy.ForSite(site).PlatformBaseURL(), nil
}

func resolvePublicPlatformBaseURL(site domain.Site, baseDomain string) (string, error) {
	// Same as resolvePlatformOrigin but ignores platformOrigin flag
	// (used for generating user-facing URLs like activation links)
	if baseDomain != "" {
		return buildHTTPSURLFromDomain(baseDomain)
	}
	return sitepolicy.ForSite(site).PlatformBaseURL(), nil
}

func buildHTTPSURLFromDomain(raw string) (string, error) {
	domain := strings.TrimSpace(strings.TrimSuffix(raw, "/"))
	if domain == "" {
		return "", fmt.Errorf("base-domain cannot be empty")
	}
	if strings.Contains(domain, "://") {
		return "", fmt.Errorf("base-domain must not include a scheme; use host only, for example dev.warded.me")
	}
	if strings.Contains(domain, "/") {
		return "", fmt.Errorf("base-domain must not include a path; use host only, for example dev.warded.me")
	}
	return "https://" + domain, nil
}
