package domain

import "fmt"

func TLSModeForDomainType(domainType DomainType) (TLSMode, error) {
	switch domainType {
	case DomainTypePlatformSubdomain:
		return TLSModePlatformWildcard, nil
	case DomainTypeCustomDomain:
		return TLSModeLocalACME, nil
	case "":
		return "", fmt.Errorf("domain_type is required to determine tls_mode")
	default:
		return "", fmt.Errorf("unsupported domain_type %q for tls_mode", domainType)
	}
}
