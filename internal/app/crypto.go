package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
)

func EncryptWithPublicKey(publicKeyPEM, plaintext string) (string, string, error) {
	publicKey, err := parseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return "", "", err
	}

	plainBytes := []byte(plaintext)
	if len(plainBytes) == 0 {
		return "", "", errors.New("config content cannot be empty")
	}

	hash := sha256.Sum256(plainBytes)
	label := []byte("config-center")
	hashForOAEP := sha256.New()
	maxChunkSize := publicKey.Size() - 2*hashForOAEP.Size() - 2
	if maxChunkSize <= 0 {
		return "", "", errors.New("rsa key is too small")
	}

	var encrypted []byte
	for start := 0; start < len(plainBytes); start += maxChunkSize {
		end := start + maxChunkSize
		if end > len(plainBytes) {
			end = len(plainBytes)
		}
		chunk, err := rsa.EncryptOAEP(sha256.New(), rand.Reader, publicKey, plainBytes[start:end], label)
		if err != nil {
			return "", "", fmt.Errorf("encrypt config chunk: %w", err)
		}
		encrypted = append(encrypted, chunk...)
	}

	return base64.StdEncoding.EncodeToString(encrypted), fmt.Sprintf("%x", hash[:]), nil
}

func ValidateRSAPublicKey(publicKeyPEM string) error {
	_, err := parseRSAPublicKey(publicKeyPEM)
	return err
}

func parseRSAPublicKey(publicKeyPEM string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, errors.New("rsa public key must be PEM encoded")
	}

	switch block.Type {
	case "PUBLIC KEY":
		key, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid PKIX rsa public key")
		}
		rsaKey, ok := key.(*rsa.PublicKey)
		if !ok {
			return nil, errors.New("public key is not RSA")
		}
		return rsaKey, nil
	case "RSA PUBLIC KEY":
		key, err := x509.ParsePKCS1PublicKey(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid PKCS1 rsa public key")
		}
		return key, nil
	default:
		return nil, errors.New("unsupported public key PEM block")
	}
}
