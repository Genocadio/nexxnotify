package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidToken   = errors.New("invalid token format")
	ErrExpiredToken   = errors.New("token has expired")
	ErrTokenTooEarly  = errors.New("token not yet valid")
	ErrInvalidIssuer  = errors.New("invalid token issuer (expected nexxauth)")
	ErrInvalidAudience= errors.New("invalid token audience (expected nexxnotify)")
	ErrMissingJTI     = errors.New("missing jti (token id)")
	ErrReplayDetected = errors.New("token already used: replay rejected")
	ErrInvalidSignature = errors.New("invalid token signature")
)

// NonceCache stores seen JTIs (JWT IDs) to prevent replay attacks.
// Because tokens expire quickly (e.g. 60 seconds), nonces only need to be
// retained until their expiration time.
type NonceCache struct {
	mu   sync.Mutex
	seen map[string]int64 // jti -> expiration timestamp (unix)
}

func NewNonceCache() *NonceCache {
	c := &NonceCache{
		seen: make(map[string]int64),
	}
	return c
}

// CheckAndRecord returns ErrReplayDetected if the JTI was already seen.
// Otherwise, it records the JTI with its expiration timestamp.
func (c *NonceCache) CheckAndRecord(jti string, exp int64) error {
	if jti == "" {
		return ErrMissingJTI
	}
	now := time.Now().Unix()
	if exp < now {
		return ErrExpiredToken
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Periodic cleanup of expired entries if map grows
	if len(c.seen) > 200 {
		for k, v := range c.seen {
			if v < now {
				delete(c.seen, k)
			}
		}
	}

	if _, exists := c.seen[jti]; exists {
		return ErrReplayDetected
	}

	c.seen[jti] = exp
	return nil
}

type Claims struct {
	Issuer    string `json:"iss"`
	Audience  any    `json:"aud"`
	Subject   string `json:"sub"`
	JTI       string `json:"jti"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

type header struct {
	Alg string `json:"alg"`
	Typ string `json:"typ"`
}

type Verifier struct {
	ed25519Pub ed25519.PublicKey
	rsaPub     *rsa.PublicKey
	ecdsaPub   *ecdsa.PublicKey
	hmacSecret []byte
	nonces     *NonceCache
}

// NewVerifier initializes a token verifier from a public key (PEM / Base64)
// or a shared HMAC secret.
func NewVerifier(keyOrSecret string) (*Verifier, error) {
	clean := strings.TrimSpace(keyOrSecret)
	if clean == "" {
		return nil, errors.New("empty key or secret provided")
	}

	v := &Verifier{
		nonces: NewNonceCache(),
	}

	// 1. Try parsing as PEM
	if strings.Contains(clean, "-----BEGIN") {
		// Normalize newlines in case it was passed via environment variable with escaped newlines
		normalized := strings.ReplaceAll(clean, "\\n", "\n")
		block, _ := pem.Decode([]byte(normalized))
		if block != nil {
			pub, err := x509.ParsePKIXPublicKey(block.Bytes)
			if err != nil {
				return nil, fmt.Errorf("parse PKIX public key: %w", err)
			}
			switch k := pub.(type) {
			case ed25519.PublicKey:
				v.ed25519Pub = k
				return v, nil
			case *rsa.PublicKey:
				v.rsaPub = k
				return v, nil
			case *ecdsa.PublicKey:
				v.ecdsaPub = k
				return v, nil
			default:
				return nil, fmt.Errorf("unsupported public key type: %T", pub)
			}
		}
	}

	// 2. Try raw base64-encoded Ed25519 public key (32 bytes)
	if raw, err := base64.StdEncoding.DecodeString(clean); err == nil && len(raw) == ed25519.PublicKeySize {
		v.ed25519Pub = ed25519.PublicKey(raw)
		return v, nil
	}
	if raw, err := base64.RawURLEncoding.DecodeString(clean); err == nil && len(raw) == ed25519.PublicKeySize {
		v.ed25519Pub = ed25519.PublicKey(raw)
		return v, nil
	}

	// 3. Fallback: treat as HMAC-SHA256 shared secret
	v.hmacSecret = []byte(clean)
	return v, nil
}

// VerifyToken validates the signature, claims, and ensures the JTI has not been replayed.
func (v *Verifier) VerifyToken(tokenStr string) (*Claims, error) {
	parts := strings.Split(tokenStr, ".")
	if len(parts) != 3 {
		return nil, ErrInvalidToken
	}

	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, fmt.Errorf("decode header: %w", err)
	}

	var h header
	if err := json.Unmarshal(headerJSON, &h); err != nil {
		return nil, fmt.Errorf("unmarshal header: %w", err)
	}

	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("decode payload: %w", err)
	}

	var c Claims
	if err := json.Unmarshal(payloadJSON, &c); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, fmt.Errorf("decode signature: %w", err)
	}

	signingInput := []byte(parts[0] + "." + parts[1])

	// Verify cryptographic signature
	switch h.Alg {
	case "EdDSA":
		if v.ed25519Pub == nil {
			return nil, fmt.Errorf("EdDSA algorithm not configured on verifier")
		}
		if !ed25519.Verify(v.ed25519Pub, signingInput, sig) {
			return nil, ErrInvalidSignature
		}
	case "RS256":
		if v.rsaPub == nil {
			return nil, fmt.Errorf("RS256 algorithm not configured on verifier")
		}
		hashed := sha256.Sum256(signingInput)
		if err := rsa.VerifyPKCS1v15(v.rsaPub, crypto.SHA256, hashed[:], sig); err != nil {
			return nil, ErrInvalidSignature
		}
	case "HS256":
		if len(v.hmacSecret) == 0 {
			return nil, fmt.Errorf("HS256 algorithm not configured on verifier")
		}
		mac := hmac.New(sha256.New, v.hmacSecret)
		mac.Write(signingInput)
		expected := mac.Sum(nil)
		if !hmac.Equal(expected, sig) {
			return nil, ErrInvalidSignature
		}
	default:
		return nil, fmt.Errorf("unsupported JWT algorithm: %s", h.Alg)
	}

	now := time.Now().Unix()

	// Max allowable lifetime: reject tokens with expiration > 5 minutes in the future
	if c.ExpiresAt-c.IssuedAt > 300 {
		return nil, errors.New("token lifetime exceeds maximum permitted 5 minutes")
	}

	// Clock skew tolerance: 30 seconds
	if now > c.ExpiresAt+30 {
		return nil, ErrExpiredToken
	}
	if c.IssuedAt > now+30 {
		return nil, ErrTokenTooEarly
	}

	// Verify Issuer
	if c.Issuer != "nexxauth" {
		return nil, ErrInvalidIssuer
	}

	// Verify Audience
	if !matchesAudience(c.Audience, "nexxnotify") {
		return nil, ErrInvalidAudience
	}

	// Check JTI for replay attack prevention
	if err := v.nonces.CheckAndRecord(c.JTI, c.ExpiresAt); err != nil {
		return nil, err
	}

	return &c, nil
}

func matchesAudience(aud any, expected string) bool {
	switch a := aud.(type) {
	case string:
		return a == expected
	case []any:
		for _, item := range a {
			if s, ok := item.(string); ok && s == expected {
				return true
			}
		}
	case []string:
		for _, item := range a {
			if item == expected {
				return true
			}
		}
	}
	return false
}
