package auth

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"testing"
	"time"
)

func generateTestEd25519Token(t *testing.T, privKey ed25519.PrivateKey, jti string, expDelta time.Duration, iss, aud string) string {
	header := map[string]string{"alg": "EdDSA", "typ": "JWT"}
	hJSON, _ := json.Marshal(header)
	hB64 := base64.RawURLEncoding.EncodeToString(hJSON)

	now := time.Now()
	claims := map[string]any{
		"iss": iss,
		"aud": aud,
		"jti": jti,
		"iat": now.Unix(),
		"exp": now.Add(expDelta).Unix(),
	}
	cJSON, _ := json.Marshal(claims)
	cB64 := base64.RawURLEncoding.EncodeToString(cJSON)

	signingInput := []byte(hB64 + "." + cB64)
	sig := ed25519.Sign(privKey, signingInput)
	sigB64 := base64.RawURLEncoding.EncodeToString(sig)

	return hB64 + "." + cB64 + "." + sigB64
}

func TestVerifierEd25519AndReplayPrevention(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	pubDER, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		t.Fatalf("marshal pub key: %v", err)
	}
	pubPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubDER,
	}))

	v, err := NewVerifier(pubPEM)
	if err != nil {
		t.Fatalf("NewVerifier failed: %v", err)
	}

	token := generateTestEd25519Token(t, priv, "token-1", 60*time.Second, "nexxauth", "nexxnotify")

	// First verification must succeed
	claims, err := v.VerifyToken(token)
	if err != nil {
		t.Fatalf("expected valid token, got: %v", err)
	}
	if claims.JTI != "token-1" {
		t.Errorf("expected jti token-1, got %s", claims.JTI)
	}

	// Second verification of the SAME token must be REJECTED (Replay attack prevention!)
	_, err = v.VerifyToken(token)
	if err != ErrReplayDetected {
		t.Fatalf("expected ErrReplayDetected, got: %v", err)
	}

	// A different token with different JTI must succeed
	token2 := generateTestEd25519Token(t, priv, "token-2", 60*time.Second, "nexxauth", "nexxnotify")
	claims2, err := v.VerifyToken(token2)
	if err != nil {
		t.Fatalf("expected valid token2, got: %v", err)
	}
	if claims2.JTI != "token-2" {
		t.Errorf("expected jti token-2, got %s", claims2.JTI)
	}

	// Expired token must be rejected
	expiredToken := generateTestEd25519Token(t, priv, "token-expired", -100*time.Second, "nexxauth", "nexxnotify")
	_, err = v.VerifyToken(expiredToken)
	if err != ErrExpiredToken {
		t.Fatalf("expected ErrExpiredToken, got: %v", err)
	}

	// Wrong issuer must be rejected
	badIssToken := generateTestEd25519Token(t, priv, "token-bad-iss", 60*time.Second, "attacker", "nexxnotify")
	_, err = v.VerifyToken(badIssToken)
	if err != ErrInvalidIssuer {
		t.Fatalf("expected ErrInvalidIssuer, got: %v", err)
	}
}
