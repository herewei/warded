package ports

type WardedClaims struct {
	Ver         int    `json:"ver"`
	Iss         string `json:"iss"`
	Sub         string `json:"sub"`
	Aud         string `json:"aud"`
	Jti         string `json:"jti"`
	PrincipalID string `json:"principal_id"`
	WardID      string `json:"ward_id"`
	SessionID   string `json:"session_id"`
	Iat         int64  `json:"iat"`
	Nbf         int64  `json:"nbf"`
	Exp         int64  `json:"exp"`
}

type JWTSigner interface {
	Sign(claims WardedClaims) (string, error)
}

type JWTVerifier interface {
	Verify(tokenString string) (*WardedClaims, error)
}

type AgentBearerClaims struct {
	Ver            int    `json:"ver"`
	Iss            string `json:"iss"`
	Sub            string `json:"sub"`
	Aud            string `json:"aud"`
	JTI            string `json:"jti"`
	PrincipalID    string `json:"principal_id"`
	WardID         string `json:"ward_id"`
	CredentialType string `json:"credential_type"`
	Scope          string `json:"scope"`
	TokenName      string `json:"token_name"`
	Iat            int64  `json:"iat"`
	Nbf            int64  `json:"nbf"`
	Exp            int64  `json:"exp"`
}

type AgentTokenVerifier interface {
	Verify(tokenString string) (*AgentBearerClaims, error)
}
