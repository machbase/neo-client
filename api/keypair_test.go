package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateAuthKeyPair(t *testing.T) {
	pair, err := GenerateAuthKeyPair()
	if err != nil {
		t.Fatalf("GenerateAuthKeyPair() error = %v", err)
	}
	if len(pair.PrivateKeyPEM) == 0 {
		t.Fatalf("empty private key PEM")
	}
	if len(pair.PublicKeyPEM) == 0 {
		t.Fatalf("empty public key PEM")
	}

	privBlock, _ := pem.Decode(pair.PrivateKeyPEM)
	if privBlock == nil || privBlock.Type != "EC PRIVATE KEY" {
		t.Fatalf("unexpected private key PEM block")
	}
	privKey, err := x509.ParseECPrivateKey(privBlock.Bytes)
	if err != nil {
		t.Fatalf("ParseECPrivateKey() error = %v", err)
	}
	if privKey.Curve != elliptic.P256() {
		t.Fatalf("unexpected curve: %T", privKey.Curve)
	}

	pubBlock, _ := pem.Decode(pair.PublicKeyPEM)
	if pubBlock == nil || pubBlock.Type != "PUBLIC KEY" {
		t.Fatalf("unexpected public key PEM block")
	}
	pubKey, err := x509.ParsePKIXPublicKey(pubBlock.Bytes)
	if err != nil {
		t.Fatalf("ParsePKIXPublicKey() error = %v", err)
	}
	if _, ok := pubKey.(*ecdsa.PublicKey); !ok {
		t.Fatalf("unexpected public key type: %T", pubKey)
	}
}

func TestAuthKeyPairWriteFiles(t *testing.T) {
	pair, err := GenerateAuthKeyPair()
	if err != nil {
		t.Fatalf("GenerateAuthKeyPair() error = %v", err)
	}

	dir := t.TempDir()
	privPath, pubPath, err := pair.WriteFiles(dir, "demo")
	if err != nil {
		t.Fatalf("WriteFiles() error = %v", err)
	}

	if privPath != filepath.Join(dir, "demo_private.pem") {
		t.Fatalf("unexpected private path: %s", privPath)
	}
	if pubPath != filepath.Join(dir, "demo_public.pem") {
		t.Fatalf("unexpected public path: %s", pubPath)
	}

	privBytes, err := os.ReadFile(privPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", privPath, err)
	}
	if string(privBytes) != string(pair.PrivateKeyPEM) {
		t.Fatalf("private key file content mismatch")
	}

	pubBytes, err := os.ReadFile(pubPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", pubPath, err)
	}
	if string(pubBytes) != string(pair.PublicKeyPEM) {
		t.Fatalf("public key file content mismatch")
	}
}
