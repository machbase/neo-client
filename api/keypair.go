package api

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const DefaultAuthKeyPrefix = "machbase_authkey_p256"

type AuthKeyPair struct {
	PrivateKeyPEM []byte
	PublicKeyPEM  []byte
}

func GenerateAuthKeyPair() (*AuthKeyPair, error) {
	return GenerateAuthKeyPairECDSA()
}

func GenerateAuthKeyPairECDSA() (*AuthKeyPair, error) {
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

func GenerateAuthKeyPairRSA() (*AuthKeyPair, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}

	privDER := x509.MarshalPKCS1PrivateKey(priv)
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		return nil, err
	}

	return &AuthKeyPair{
		PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: privDER}),
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

func AuthKeyPairFromPrivateKey(priv crypto.PrivateKey) (*AuthKeyPair, error) {
	privDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(crypto.PublicKey(priv.(crypto.Signer).Public()))
	if err != nil {
		return nil, err
	}

	return &AuthKeyPair{
		PrivateKeyPEM: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}),
		PublicKeyPEM:  pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}),
	}, nil
}

func (pair *AuthKeyPair) PublicKey() (crypto.PublicKey, error) {
	block, _ := pem.Decode(pair.PublicKeyPEM)
	if block == nil {
		return nil, errors.New("invalid public key block")
	}
	if block.Type != "PUBLIC KEY" {
		return nil, errors.New("invalid public key type")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid public key")
	}
	return key, nil
}

func (pair *AuthKeyPair) PrivateKey() (crypto.PrivateKey, error) {
	block, _ := pem.Decode(pair.PrivateKeyPEM)
	if block == nil {
		return nil, errors.New("invalid private key block")
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid private key")
		}
		return key, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid rsa private key")
		}
		return key, nil
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, errors.New("invalid ec private key")
		}
		return key, nil
	default:
		return nil, errors.New("invalid private key")
	}
}

func LoadPrivateKeyFromFile(keyFile string) (crypto.PrivateKey, error) {
	privateKeyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("invalid AUTH_KEY")
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, nil
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, nil
	default:
		return nil, fmt.Errorf("invalid auth key type %s", block.Type)
	}
}

func LoadPrivateKeyFromPEM(privateKeyPEM []byte) (crypto.PrivateKey, error) {
	block, _ := pem.Decode(privateKeyPEM)
	if block == nil {
		return nil, fmt.Errorf("invalid AUTH_KEY")
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, nil
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		return key, nil
	default:
		return nil, fmt.Errorf("invalid auth key type %s", block.Type)
	}
}
