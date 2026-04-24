package sitepolicy

import (
	"fmt"
	"strings"

	"github.com/herewei/warded/internal/domain"
)

type Policy struct {
	Site       domain.Site
	BaseDomain string
}

func ForSite(site domain.Site) Policy {
	switch site {
	case domain.SiteCN:
		return Policy{
			Site:       domain.SiteCN,
			BaseDomain: "warded.cn",
		}
	default:
		return Policy{
			Site:       domain.SiteGlobal,
			BaseDomain: "warded.me",
		}
	}
}

func NormalizeBaseURL(raw string) string {
	return strings.TrimSuffix(raw, "/")
}

func (p Policy) PlatformBaseURL() string {
	if p.BaseDomain == "" {
		return ""
	}
	return fmt.Sprintf("https://%s", p.BaseDomain)
}

func (p Policy) InstallScriptURL() string {
	if p.BaseDomain == "" {
		return ""
	}
	return fmt.Sprintf("https://%s/install.sh", p.BaseDomain)
}

func (p Policy) AllowedBaseDomains() []string {
	switch p.Site {
	case domain.SiteCN:
		return []string{"warded.cn"}
	default:
		return []string{"warded.me"}
	}
}

func (p Policy) IsConfigured() bool {
	return p.BaseDomain != ""
}
