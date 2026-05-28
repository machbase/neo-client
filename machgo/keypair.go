package machgo

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"

	"github.com/machbase/neo-client/machnet"
)

const DefaultAuthKeyPrefix = "machbase_authkey_p256"

type AuthKeyPair struct {
	PrivateKeyPEM []byte
	PublicKeyPEM  []byte
}

func GenerateAuthKeyPair() (*AuthKeyPair, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, err
	}

	return &AuthKeyPair{
		PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER}),
		PublicKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}),
	}, nil
}

func (pair *AuthKeyPair) WriteFiles(outDir string, prefix string) (privatePath string, publicPath string, err error) {
	if pair == nil {
		return "", "", fmt.Errorf("auth key pair is nil")
	}
	if outDir == "" {
		outDir = "."
	}
	if prefix == "" {
		prefix = DefaultAuthKeyPrefix
	}

	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return "", "", err
	}

	privatePath = filepath.Join(outDir, prefix+"_private.pem")
	publicPath = filepath.Join(outDir, prefix+"_public.pem")

	if err := os.WriteFile(privatePath, pair.PrivateKeyPEM, 0o600); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(publicPath, pair.PublicKeyPEM, 0o644); err != nil {
		return "", "", err
	}

	return privatePath, publicPath, nil
}

func LoadPrivateKeyFromFile(keyFile string) (crypto.PrivateKey, error) {
	return machnet.LoadPrivateKeyFromFile(keyFile)
}
