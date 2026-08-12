package api

import (
	"context"
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
	"strings"
	"time"
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

type RegisteredAuthKey struct {
	KeyID     int
	User      string
	PubKey    string
	Comment   string
	Activated int
}

func GetRegisteredAuthKey(ctx context.Context, conn Conn, user string, pubKey []byte) (RegisteredAuthKey, error) {
	//var pubKeyStr = strings.TrimSpace(string(pubKey))
	var ret RegisteredAuthKey
	row := conn.QueryRow(ctx, `SELECT 
			KEY_ID, 
			USER_NAME,
			PUBKEY,
			COMMENT,
			ACTIVATED 
		FROM
			V$USER_AUTH_KEYS
		WHERE
			USER_NAME=? 
		AND PUBKEY=?
		ORDER BY
			KEY_ID DESC LIMIT 1`,
		strings.ToUpper(user), strings.TrimSpace(string(pubKey)))
	if row.Err() != nil {
		return ret, row.Err()
	}
	if err := row.Scan(&ret.KeyID, &ret.User, &ret.PubKey, &ret.Comment, &ret.Activated); err != nil {
		return ret, err
	}
	return ret, nil
}

// RegisterAuthKey registers the public key as an auth key of the user, and returns the key ID.
func RegisterAuthKey(ctx context.Context, sysConn Conn, user string, pubKey []byte, comment string) (int, error) {
	user = strings.ToUpper(user)
	pubKeyStr := strings.TrimSpace(string(pubKey))
	comment = strings.ReplaceAll(comment, `'`, `''`)

	validBefore := time.Now().Add(24 * time.Hour * 365 * 30).Format("2006-01-02")
	result := sysConn.Exec(ctx,
		fmt.Sprintf("ALTER USER %s ADD AUTH KEY (KEY = '%s', VALID_BEFORE = '%s', COMMENT = '%s')",
			user, pubKeyStr, validBefore, comment),
	)
	if result.Err() != nil {
		return 0, result.Err()
	}

	reg, err := GetRegisteredAuthKey(ctx, sysConn, user, pubKey)
	if err != nil {
		return 0, err
	}
	if reg.KeyID == 0 {
		return 0, fmt.Errorf("failed to get registered auth key")
	}
	if reg.Activated != 1 {
		result := sysConn.Exec(ctx, fmt.Sprintf("ALTER USER %s ACTIVATE AUTH KEY ID %d", user, reg.KeyID))
		if result.Err() != nil {
			return 0, result.Err()
		}
	}
	return reg.KeyID, nil
}
