package platformjwt

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
	"github.com/herewei/warded/internal/sitepolicy"
)

type Verifier struct {
	site   domain.Site
	wardID string
	issuer string

	mu          sync.RWMutex
	publicKeys  map[string]*rsa.PublicKey
	validTokens map[string]ports.ValidAgentToken
}

func NewVerifier(site domain.Site, wardID string, publicKeys []domain.PlatformJWTPublicKey) *Verifier {
	v := &Verifier{
		site:        site,
		wardID:      wardID,
		issuer:      sitepolicy.ForSite(site).PlatformBaseURL(),
		publicKeys:  make(map[string]*rsa.PublicKey),
		validTokens: make(map[string]ports.ValidAgentToken),
	}
	v.UpdatePublicKeys(publicKeys)
	return v
}

func (v *Verifier) UpdatePublicKeys(keys []domain.PlatformJWTPublicKey) {
	parsed := make(map[string]*rsa.PublicKey, len(keys))
	for _, key := range keys {
		publicKey, err := parseRSAPublicKeyPEM([]byte(key.PublicKey))
		if err != nil || key.KID == "" {
			continue
		}
		parsed[key.KID] = publicKey
	}
	v.mu.Lock()
	v.publicKeys = parsed
	v.mu.Unlock()
}

func (v *Verifier) UpdateValidTokens(tokens []ports.ValidAgentToken) {
	valid := make(map[string]ports.ValidAgentToken, len(tokens))
	for _, token := range tokens {
		if token.JTI != "" {
			valid[token.JTI] = token
		}
	}
	v.mu.Lock()
	v.validTokens = valid
	v.mu.Unlock()
}

func (v *Verifier) Verify(tokenString string) (*ports.AgentBearerClaims, error) {
	claims := &agentClaims{}
	parser := jwt.NewParser(
		jwt.WithIssuer(v.issuer),
		jwt.WithAudience("ward:"+v.wardID),
		jwt.WithLeeway(30*time.Second),
		jwt.WithIssuedAt(),
	)
	token, err := parser.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodRS256 {
			return nil, fmt.Errorf("platform jwt: unexpected signing method: %v", token.Header["alg"])
		}
		kid, _ := token.Header["kid"].(string)
		if kid == "" {
			return nil, fmt.Errorf("platform jwt: missing kid")
		}
		v.mu.RLock()
		key := v.publicKeys[kid]
		v.mu.RUnlock()
		if key == nil {
			return nil, fmt.Errorf("platform jwt: unknown kid %s", kid)
		}
		return key, nil
	})
	if err != nil {
		return nil, fmt.Errorf("platform jwt: parse: %w", err)
	}
	if token == nil || !token.Valid {
		return nil, fmt.Errorf("platform jwt: invalid token")
	}
	out := claims.toPorts()
	if out.Ver != 1 {
		return nil, fmt.Errorf("platform jwt: unsupported version %d", out.Ver)
	}
	if out.WardID != v.wardID {
		return nil, fmt.Errorf("platform jwt: ward_id mismatch")
	}
	if out.CredentialType != "agent_bearer_token" {
		return nil, fmt.Errorf("platform jwt: unexpected credential_type")
	}
	if out.Scope != "agent" {
		return nil, fmt.Errorf("platform jwt: unexpected scope")
	}
	if out.PrincipalID == "" || out.JTI == "" {
		return nil, fmt.Errorf("platform jwt: missing required claims")
	}
	v.mu.RLock()
	validToken, valid := v.validTokens[out.JTI]
	v.mu.RUnlock()
	if !valid {
		return nil, fmt.Errorf("platform jwt: token is not in valid set")
	}
	if validToken.PrincipalID != "" && validToken.PrincipalID != out.PrincipalID {
		return nil, fmt.Errorf("platform jwt: principal_id mismatch")
	}
	if validToken.TokenName != "" {
		out.TokenName = validToken.TokenName
	}
	return out, nil
}

type agentClaims struct {
	Ver            int    `json:"ver"`
	PrincipalID    string `json:"principal_id"`
	WardID         string `json:"ward_id"`
	CredentialType string `json:"credential_type"`
	Scope          string `json:"scope"`
	TokenName      string `json:"token_name"`
	jwt.RegisteredClaims
}

func (c *agentClaims) toPorts() *ports.AgentBearerClaims {
	aud := ""
	if len(c.Audience) > 0 {
		aud = c.Audience[0]
	}
	return &ports.AgentBearerClaims{
		Ver:            c.Ver,
		Iss:            c.Issuer,
		Sub:            c.Subject,
		Aud:            aud,
		JTI:            c.ID,
		PrincipalID:    c.PrincipalID,
		WardID:         c.WardID,
		CredentialType: c.CredentialType,
		Scope:          c.Scope,
		TokenName:      c.TokenName,
		Iat:            numericDateUnix(c.IssuedAt),
		Nbf:            numericDateUnix(c.NotBefore),
		Exp:            numericDateUnix(c.ExpiresAt),
	}
}

func numericDateUnix(value *jwt.NumericDate) int64 {
	if value == nil {
		return 0
	}
	return value.Unix()
}

func parseRSAPublicKeyPEM(value []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(value)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM")
	}
	parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err == nil {
		key, ok := parsed.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("not RSA public key")
		}
		return key, nil
	}
	key, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("invalid RSA public key: %w", err)
	}
	return key, nil
}
