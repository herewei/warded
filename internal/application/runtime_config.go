package application

import (
	"fmt"

	"github.com/herewei/warded/internal/domain"
)

func tlsModeForDomainType(domainType domain.DomainType) (domain.TLSMode, error) {
	switch domainType {
	case domain.DomainTypePlatformSubdomain:
		return domain.TLSModePlatformWildcard, nil
	case domain.DomainTypeCustomDomain:
		return domain.TLSModeLocalACME, nil
	case "":
		return "", fmt.Errorf("domain_type is required to determine tls_mode")
	default:
		return "", fmt.Errorf("unsupported domain_type %q for tls_mode", domainType)
	}
}
