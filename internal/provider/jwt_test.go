package provider

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"strings"
	"testing"
)

func TestParseRSAPrivateKey_PKCS8(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("marshal pkcs8: %v", err)
	}

	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})

	parsed, err := parseRSAPrivateKey(string(pemBlock))
	if err != nil {
		t.Fatalf("parseRSAPrivateKey failed: %v", err)
	}
	if parsed.N.Cmp(key.N) != 0 {
		t.Errorf("parsed key does not match original key")
	}
}

func TestParseRSAPrivateKey_PKCS1(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	der := x509.MarshalPKCS1PrivateKey(key)
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: der,
	})

	parsed, err := parseRSAPrivateKey(string(pemBlock))
	if err != nil {
		t.Fatalf("parseRSAPrivateKey failed: %v", err)
	}
	if parsed.N.Cmp(key.N) != 0 {
		t.Errorf("parsed key does not match original key")
	}
}

func TestParseRSAPrivateKey_EscapedNewlines(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemBlock := pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	})

	escapedPEM := strings.ReplaceAll(string(pemBlock), "\n", "\\n")
	parsed, err := parseRSAPrivateKey(escapedPEM)
	if err != nil {
		t.Fatalf("parseRSAPrivateKey with escaped newlines failed: %v", err)
	}
	if parsed.N.Cmp(key.N) != 0 {
		t.Errorf("parsed key does not match original key")
	}
}

func TestParseRSAPrivateKey_InvalidPEM(t *testing.T) {
	_, err := parseRSAPrivateKey("not-a-valid-pem")
	if err == nil {
		t.Fatal("expected error for invalid PEM, got nil")
	}
}

func TestCreateJWT(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	der, _ := x509.MarshalPKCS8PrivateKey(key)
	pemBlock := string(pem.EncodeToMemory(&pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: der,
	}))

	email := "service-account@project.iam.gserviceaccount.com"
	tok, err := createJWT(email, pemBlock)
	if err != nil {
		t.Fatalf("createJWT failed: %v", err)
	}

	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("JWT does not have 3 parts, got %d", len(parts))
	}
}
