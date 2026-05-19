package app

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"testing"
)

func TestEncryptWithPublicKeyDoesNotExposePlaintext(t *testing.T) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}
	publicKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: publicKeyDER,
	})

	ciphertext, contentHash, err := EncryptWithPublicKey(string(publicKeyPEM), "database-password=secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if ciphertext == "" || contentHash == "" {
		t.Fatal("expected ciphertext and content hash")
	}
	decoded, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		t.Fatalf("decode ciphertext: %v", err)
	}
	plaintext, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, privateKey, decoded, []byte("config-center"))
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(plaintext) != "database-password=secret" {
		t.Fatalf("unexpected plaintext %q", plaintext)
	}
	if ciphertext == "database-password=secret" {
		t.Fatal("ciphertext must not equal plaintext")
	}
}
