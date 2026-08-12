package machnet

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/machbase/neo-client/api"
)

func readPrivateKeyPEM(t *testing.T, path string) []byte {
	t.Helper()
	pemBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s) error = %v", path, err)
	}
	return pemBytes
}

func writeRSAPrivateKeyPEM(t *testing.T, dir string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	path := filepath.Join(dir, "rsa_key.pem")
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func writeECPrivateKeyPEM(t *testing.T, dir string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	path := filepath.Join(dir, "ec_key.pem")
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func writeECP384PrivateKeyPEM(t *testing.T, dir string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatalf("ecdsa.GenerateKey() error = %v", err)
	}
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalECPrivateKey() error = %v", err)
	}
	path := filepath.Join(dir, "ec_p384_key.pem")
	block := &pem.Block{Type: "EC PRIVATE KEY", Bytes: der}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func writeRSA3072PrivateKeyPEM(t *testing.T, dir string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatalf("rsa.GenerateKey() error = %v", err)
	}
	path := filepath.Join(dir, "rsa_3072_key.pem")
	block := &pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
	return path
}

func TestFinalizeAuthConnectOptions(t *testing.T) {
	t.Run("defaults to password", func(t *testing.T) {
		mode, scheme, err := finalizeAuthConnectOptions("", nil, "")
		if err != nil {
			t.Fatalf("finalizeAuthConnectOptions() error = %v", err)
		}
		if mode != authModePassword || scheme != "" {
			t.Fatalf("unexpected mode/scheme: %q %q", mode, scheme)
		}
	})

	t.Run("challenge requires key file", func(t *testing.T) {
		_, _, err := finalizeAuthConnectOptions("CHALLENGE", nil, "")
		if err == nil {
			t.Fatalf("expected error")
		}
	})

	t.Run("auto challenge detects rsa scheme", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := writeRSAPrivateKeyPEM(t, dir)
		key, err := api.LoadPrivateKeyFromFile(keyFile)
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", keyFile, err)
		}
		mode, scheme, err := finalizeAuthConnectOptions("", key, "")
		if err != nil {
			t.Fatalf("finalizeAuthConnectOptions() error = %v", err)
		}
		if mode != authModeChallenge || scheme != authSigSchemeRSAPKCS1V15 {
			t.Fatalf("unexpected mode/scheme: %q %q", mode, scheme)
		}
	})
}

func TestSignAuthNonce(t *testing.T) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("rand.Read() error = %v", err)
	}

	t.Run("rsa pkcs1", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := writeRSAPrivateKeyPEM(t, dir)
		sig, err := signAuthNonce(keyFile, authSigSchemeRSAPKCS1V15, nonce)
		if err != nil {
			t.Fatalf("signAuthNonce() error = %v", err)
		}
		if len(sig) == 0 {
			t.Fatalf("empty signature")
		}
	})

	t.Run("rsa pss", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := writeRSAPrivateKeyPEM(t, dir)
		sig, err := signAuthNonce(keyFile, authSigSchemeRSAPSS, nonce)
		if err != nil {
			t.Fatalf("signAuthNonce() error = %v", err)
		}
		if len(sig) == 0 {
			t.Fatalf("empty signature")
		}
	})

	t.Run("ecdsa", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := writeECPrivateKeyPEM(t, dir)
		sig, err := signAuthNonce(keyFile, authSigSchemeECDSA, nonce)
		if err != nil {
			t.Fatalf("signAuthNonce() error = %v", err)
		}
		if len(sig) == 0 {
			t.Fatalf("empty signature")
		}
	})

	t.Run("pem string input", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := writeRSAPrivateKeyPEM(t, dir)
		privateKeyPEM := readPrivateKeyPEM(t, keyFile)
		sig, err := signAuthNoncePEM(privateKeyPEM, authSigSchemeRSAPKCS1V15, nonce)
		if err != nil {
			t.Fatalf("signAuthNoncePEM() error = %v", err)
		}
		if len(sig) == 0 {
			t.Fatalf("empty signature")
		}
	})

	t.Run("ecdsa p384", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := writeECP384PrivateKeyPEM(t, dir)
		sig, err := signAuthNonce(keyFile, authSigSchemeECDSA, nonce)
		if err != nil || len(sig) == 0 {
			t.Fatalf("signAuthNonce() error = %v", err)
		}
	})

	t.Run("rsa 3072", func(t *testing.T) {
		dir := t.TempDir()
		keyFile := writeRSA3072PrivateKeyPEM(t, dir)
		sig, err := signAuthNonce(keyFile, authSigSchemeRSAPKCS1V15, nonce)
		if err != nil || len(sig) == 0 {
			t.Fatalf("signAuthNonce() error = %v", err)
		}
	})
}
