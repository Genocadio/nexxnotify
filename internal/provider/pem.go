package provider

import (
	"crypto"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

// parsePrivateKey parses a PEM-encoded private key.
func parsePrivateKey(pemBytes []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}

	// Try PKCS8 first (Firebase uses this format)
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err == nil {
		return key, nil
	}

	// Try PKCS1
	key2, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err == nil {
		return key2, nil
	}

	return nil, fmt.Errorf("unsupported private key format")
}
