package jwt_test

import (
	"testing"
	"time"

	"github.com/herewei/warded/internal/adapters/jwt"
	"github.com/herewei/warded/internal/ports"
)

const testSecret = "test-secret-32-bytes-long-enough"

func TestSignAndVerify(t *testing.T) {
	t.Parallel()

	signer := jwt.NewSigner(testSecret)
	verifier := jwt.NewVerifier(testSecret)

	token, err := signer.Sign(ports.WardedClaims{
		PrincipalID: "principal_123",
		WardID:      "ward_abc",
		SessionID:   "sess_xyz",
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if token == "" {
		t.Fatal("expected non-empty token")
	}

	claims, err := verifier.Verify(token)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}

	if claims.Ver != 1 {
		t.Errorf("ver=%d, want 1", claims.Ver)
	}
	if claims.Iss != "warded-proxy" {
		t.Errorf("iss=%s, want warded-proxy", claims.Iss)
	}
	if claims.Sub != "principal_123" {
		t.Errorf("sub=%s, want principal_123", claims.Sub)
	}
	if claims.Aud != "ward:ward_abc" {
		t.Errorf("aud=%s, want ward:ward_abc", claims.Aud)
	}
	if claims.PrincipalID != "principal_123" {
		t.Errorf("principal_id=%s, want principal_123", claims.PrincipalID)
	}
	if claims.WardID != "ward_abc" {
		t.Errorf("ward_id=%s, want ward_abc", claims.WardID)
	}
	if claims.SessionID != "sess_xyz" {
		t.Errorf("session_id=%s, want sess_xyz", claims.SessionID)
	}
	if claims.Jti == "" {
		t.Error("expected jti to be auto-generated")
	}

	// exp should be ~8h from now
	expTime := time.Unix(claims.Exp, 0)
	expected := time.Now().UTC().Add(8 * time.Hour)
	diff := expected.Sub(expTime).Abs()
	if diff > 5*time.Second {
		t.Errorf("exp diff too large: %v", diff)
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	t.Parallel()

	signer := jwt.NewSigner(testSecret)
	verifier := jwt.NewVerifier("wrong-secret")

	token, _ := signer.Sign(ports.WardedClaims{
		PrincipalID: "p1",
		WardID:      "w1",
		SessionID:   "s1",
	})

	_, err := verifier.Verify(token)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func TestVerify_ExpiredToken(t *testing.T) {
	t.Parallel()

	signer := jwt.NewSigner(testSecret)
	verifier := jwt.NewVerifier(testSecret)

	past := time.Now().UTC().Add(-10 * time.Hour)
	token, _ := signer.Sign(ports.WardedClaims{
		PrincipalID: "p1",
		WardID:      "w1",
		SessionID:   "s1",
		Iat:         past.Unix(),
		Nbf:         past.Unix(),
		Exp:         past.Add(1 * time.Hour).Unix(), // expired 9h ago
	})

	_, err := verifier.Verify(token)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestVerify_GarbageToken(t *testing.T) {
	t.Parallel()

	verifier := jwt.NewVerifier(testSecret)
	_, err := verifier.Verify("not.a.jwt")
	if err == nil {
		t.Fatal("expected error for garbage token")
	}
}
