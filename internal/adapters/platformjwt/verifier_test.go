package platformjwt_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"testing"
	"time"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/herewei/warded/internal/adapters/platformjwt"
	"github.com/herewei/warded/internal/domain"
	"github.com/herewei/warded/internal/ports"
)

func TestVerifierRequiresValidJTI(t *testing.T) {
	t.Parallel()

	privateKey, publicKey := testRSAKeyPair(t)
	verifier := platformjwt.NewVerifier(domain.SiteGlobal, "ward_123", []domain.PlatformJWTPublicKey{
		{KID: "global-test", PublicKey: publicKey},
	})

	token := signAgentToken(t, privateKey, "global-test", "ward_123", "agtok_valid")
	if _, err := verifier.Verify(token); err == nil {
		t.Fatal("expected token to fail before heartbeat valid set update")
	}

	verifier.UpdateValidTokens([]ports.ValidAgentToken{{JTI: "agtok_valid", PrincipalID: "principal_123", TokenName: "CI"}})
	claims, err := verifier.Verify(token)
	if err != nil {
		t.Fatalf("Verify returned error: %v", err)
	}
	if claims.PrincipalID != "principal_123" || claims.JTI != "agtok_valid" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
	if claims.TokenName != "CI" {
		t.Fatalf("expected token name from valid set, got %q", claims.TokenName)
	}
}

func TestVerifierRejectsWrongAudience(t *testing.T) {
	t.Parallel()

	privateKey, publicKey := testRSAKeyPair(t)
	verifier := platformjwt.NewVerifier(domain.SiteGlobal, "ward_123", []domain.PlatformJWTPublicKey{
		{KID: "global-test", PublicKey: publicKey},
	})
	verifier.UpdateValidTokens([]ports.ValidAgentToken{{JTI: "agtok_valid", PrincipalID: "principal_123"}})

	token := signAgentToken(t, privateKey, "global-test", "ward_other", "agtok_valid")
	if _, err := verifier.Verify(token); err == nil {
		t.Fatal("expected wrong audience to fail")
	}
}

func TestVerifierRejectsPrincipalMismatchWithValidSet(t *testing.T) {
	t.Parallel()

	privateKey, publicKey := testRSAKeyPair(t)
	verifier := platformjwt.NewVerifier(domain.SiteGlobal, "ward_123", []domain.PlatformJWTPublicKey{
		{KID: "global-test", PublicKey: publicKey},
	})
	verifier.UpdateValidTokens([]ports.ValidAgentToken{{JTI: "agtok_valid", PrincipalID: "principal_other"}})

	token := signAgentToken(t, privateKey, "global-test", "ward_123", "agtok_valid")
	if _, err := verifier.Verify(token); err == nil {
		t.Fatal("expected principal mismatch to fail")
	}
}

func TestVerifierAcceptsMultiplePublicKeysAfterRotation(t *testing.T) {
	t.Parallel()

	oldPrivateKey, oldPublicKey := testRSAKeyPair(t)
	newPrivateKey, newPublicKey := testRSAKeyPair(t)
	verifier := platformjwt.NewVerifier(domain.SiteGlobal, "ward_123", []domain.PlatformJWTPublicKey{
		{KID: "global-old", PublicKey: oldPublicKey},
		{KID: "global-new", PublicKey: newPublicKey},
	})
	verifier.UpdateValidTokens([]ports.ValidAgentToken{
		{JTI: "agtok_old", PrincipalID: "principal_123", TokenName: "old"},
		{JTI: "agtok_new", PrincipalID: "principal_123", TokenName: "new"},
	})

	oldToken := signAgentToken(t, oldPrivateKey, "global-old", "ward_123", "agtok_old")
	newToken := signAgentToken(t, newPrivateKey, "global-new", "ward_123", "agtok_new")

	if _, err := verifier.Verify(oldToken); err != nil {
		t.Fatalf("old token should verify while old public key remains in keyset: %v", err)
	}
	if _, err := verifier.Verify(newToken); err != nil {
		t.Fatalf("new token should verify with active public key: %v", err)
	}
}

func TestVerifierReplacesPublicKeysOnUpdate(t *testing.T) {
	t.Parallel()

	oldPrivateKey, oldPublicKey := testRSAKeyPair(t)
	newPrivateKey, newPublicKey := testRSAKeyPair(t)
	verifier := platformjwt.NewVerifier(domain.SiteGlobal, "ward_123", []domain.PlatformJWTPublicKey{
		{KID: "global-old", PublicKey: oldPublicKey},
	})
	verifier.UpdateValidTokens([]ports.ValidAgentToken{
		{JTI: "agtok_old", PrincipalID: "principal_123"},
		{JTI: "agtok_new", PrincipalID: "principal_123"},
	})

	oldToken := signAgentToken(t, oldPrivateKey, "global-old", "ward_123", "agtok_old")
	newToken := signAgentToken(t, newPrivateKey, "global-new", "ward_123", "agtok_new")
	verifier.UpdatePublicKeys([]domain.PlatformJWTPublicKey{{KID: "global-new", PublicKey: newPublicKey}})

	if _, err := verifier.Verify(oldToken); err == nil {
		t.Fatal("old token should fail after old public key is removed from heartbeat keyset")
	}
	if _, err := verifier.Verify(newToken); err != nil {
		t.Fatalf("new token should verify after public key update: %v", err)
	}
}

func signAgentToken(t *testing.T, key *rsa.PrivateKey, kid string, wardID string, jti string) string {
	t.Helper()
	now := time.Now().UTC()
	claims := gojwt.MapClaims{
		"ver":             1,
		"iss":             "https://warded.me",
		"sub":             "principal_123",
		"aud":             "ward:" + wardID,
		"jti":             jti,
		"principal_id":    "principal_123",
		"ward_id":         wardID,
		"credential_type": "agent_bearer_token",
		"scope":           "agent",
		"token_name":      "CI",
		"iat":             now.Unix(),
		"nbf":             now.Unix(),
		"exp":             now.Add(time.Hour).Unix(),
	}
	token := gojwt.NewWithClaims(gojwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid
	signed, err := token.SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}

func testRSAKeyPair(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	publicPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})
	return privateKey, string(publicPEM)
}
