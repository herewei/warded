package jwt

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"

	"github.com/herewei/warded/internal/ports"
)

const (
	issuer  = "warded-proxy"
	version = 1
	ttl     = 8 * time.Hour
)

type claims struct {
	Ver         int    `json:"ver"`
	PrincipalID string `json:"principal_id"`
	WardID      string `json:"ward_id"`
	SessionID   string `json:"session_id"`
	gojwt.RegisteredClaims
}

type Signer struct {
	secret []byte
}

func NewSigner(secret string) *Signer {
	return &Signer{secret: []byte(secret)}
}

func (s *Signer) Sign(c ports.WardedClaims) (string, error) {
	now := time.Now().UTC()

	jti := c.Jti
	if jti == "" {
		b := make([]byte, 12)
		if _, err := rand.Read(b); err != nil {
			return "", fmt.Errorf("jwt: generate jti: %w", err)
		}
		jti = "jwt_" + hex.EncodeToString(b)
	}

	iat := now
	if c.Iat != 0 {
		iat = time.Unix(c.Iat, 0)
	}
	nbf := iat
	if c.Nbf != 0 {
		nbf = time.Unix(c.Nbf, 0)
	}
	exp := iat.Add(ttl)
	if c.Exp != 0 {
		exp = time.Unix(c.Exp, 0)
	}

	token := gojwt.NewWithClaims(gojwt.SigningMethodHS256, claims{
		Ver:         version,
		PrincipalID: c.PrincipalID,
		WardID:      c.WardID,
		SessionID:   c.SessionID,
		RegisteredClaims: gojwt.RegisteredClaims{
			Issuer:    issuer,
			Subject:   c.PrincipalID,
			Audience:  gojwt.ClaimStrings{"ward:" + c.WardID},
			ID:        jti,
			IssuedAt:  gojwt.NewNumericDate(iat),
			NotBefore: gojwt.NewNumericDate(nbf),
			ExpiresAt: gojwt.NewNumericDate(exp),
		},
	})

	return token.SignedString(s.secret)
}

type Verifier struct {
	secret []byte
}

func NewVerifier(secret string) *Verifier {
	return &Verifier{secret: []byte(secret)}
}

func (v *Verifier) Verify(tokenString string) (*ports.WardedClaims, error) {
	token, err := gojwt.ParseWithClaims(tokenString, &claims{}, func(token *gojwt.Token) (any, error) {
		if _, ok := token.Method.(*gojwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("jwt: unexpected signing method: %v", token.Header["alg"])
		}
		return v.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("jwt: parse: %w", err)
	}

	c, ok := token.Claims.(*claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("jwt: invalid token")
	}

	if c.Ver != version {
		return nil, fmt.Errorf("jwt: unsupported version %d", c.Ver)
	}
	if c.Issuer != issuer {
		return nil, fmt.Errorf("jwt: unexpected issuer %s", c.Issuer)
	}
	if c.PrincipalID == "" || c.WardID == "" || c.SessionID == "" {
		return nil, fmt.Errorf("jwt: missing required claims")
	}

	aud := ""
	if len(c.Audience) > 0 {
		aud = c.Audience[0]
	}

	return &ports.WardedClaims{
		Ver:         c.Ver,
		Iss:         c.Issuer,
		Sub:         c.Subject,
		Aud:         aud,
		Jti:         c.ID,
		PrincipalID: c.PrincipalID,
		WardID:      c.WardID,
		SessionID:   c.SessionID,
		Iat:         c.IssuedAt.Unix(),
		Nbf:         c.NotBefore.Unix(),
		Exp:         c.ExpiresAt.Unix(),
	}, nil
}
