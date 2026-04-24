package sitepolicy

import (
	"testing"

	"github.com/herewei/warded/internal/domain"
)

func TestForSite_DerivesURLsFromBaseDomain(t *testing.T) {
	t.Parallel()

	policy := ForSite(domain.SiteGlobal)

	if policy.BaseDomain != "warded.me" {
		t.Fatalf("unexpected base domain: %s", policy.BaseDomain)
	}
	if got := policy.PlatformBaseURL(); got != "https://warded.me" {
		t.Fatalf("unexpected platform base url: %s", got)
	}
	if got := policy.InstallScriptURL(); got != "https://warded.me/install.sh" {
		t.Fatalf("unexpected install script url: %s", got)
	}
}

func TestAllowedBaseDomains(t *testing.T) {
	t.Parallel()

	global := ForSite(domain.SiteGlobal)
	if len(global.AllowedBaseDomains()) != 1 || global.AllowedBaseDomains()[0] != "warded.me" {
		t.Fatalf("unexpected allowed base domains for global: %v", global.AllowedBaseDomains())
	}

	cn := ForSite(domain.SiteCN)
	if len(cn.AllowedBaseDomains()) != 1 || cn.AllowedBaseDomains()[0] != "warded.cn" {
		t.Fatalf("unexpected allowed base domains for cn: %v", cn.AllowedBaseDomains())
	}
}
