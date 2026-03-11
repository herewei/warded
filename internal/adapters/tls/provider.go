package tls

import "crypto/tls"

type Provider interface {
	TLSConfig() *tls.Config
}
