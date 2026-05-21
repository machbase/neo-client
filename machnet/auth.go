package machnet

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	authModePassword  = "PASSWORD"
	authModeChallenge = "CHALLENGE"

	authSigSchemeECDSA       = "ECDSA"
	authSigSchemeRSAPKCS1V15 = "RSA_PKCS1_V15"
	authSigSchemeRSAPSS      = "RSA_PSS"
)

func canonicalizeAuthMode(mode string) (string, error) {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return "", nil
	}
	if strings.EqualFold(mode, authModePassword) {
		return authModePassword, nil
	}
	if strings.EqualFold(mode, authModeChallenge) {
		return authModeChallenge, nil
	}
	return "", errors.New("AUTH_MODE must be PASSWORD or CHALLENGE")
}

func canonicalizeAuthSigScheme(scheme string) (string, error) {
	scheme = strings.TrimSpace(scheme)
	if scheme == "" {
		return "", nil
	}
	if strings.EqualFold(scheme, authSigSchemeECDSA) {
		return authSigSchemeECDSA, nil
	}
	if strings.EqualFold(scheme, authSigSchemeRSAPSS) {
		return authSigSchemeRSAPSS, nil
	}
	if strings.EqualFold(scheme, authSigSchemeRSAPKCS1V15) || strings.EqualFold(scheme, "RSA") {
		return authSigSchemeRSAPKCS1V15, nil
	}
	return "", errors.New("AUTH_SIG_SCHEME must be ECDSA, RSA_PKCS1_V15, or RSA_PSS")
}

func finalizeAuthConnectOptions(mode string, keyFile string, sigScheme string) (string, string, error) {
	canonMode, err := canonicalizeAuthMode(mode)
	if err != nil {
		return "", "", err
	}
	keyFile = strings.TrimSpace(keyFile)
	if canonMode == "" {
		if keyFile != "" {
			canonMode = authModeChallenge
		} else {
			canonMode = authModePassword
			return canonMode, "", nil
		}
	}
	if canonMode == authModePassword {
		return canonMode, "", nil
	}
	if keyFile == "" {
		return "", "", errors.New("AUTH_KEY_FILE is required for AUTH_MODE=CHALLENGE")
	}
	canonScheme, err := canonicalizeAuthSigScheme(sigScheme)
	if err != nil {
		return "", "", err
	}
	if canonScheme != "" {
		return canonMode, canonScheme, nil
	}
	detected, err := detectAuthSigSchemeFromKeyFile(keyFile)
	if err != nil {
		return "", "", err
	}
	return canonMode, detected, nil
}

func detectAuthSigSchemeFromKeyFile(keyFile string) (string, error) {
	key, err := loadPrivateKey(keyFile)
	if err != nil {
		return "", err
	}
	if err := validateSupportedAuthKey(key); err != nil {
		return "", err
	}
	switch key.(type) {
	case *ecdsa.PrivateKey:
		return authSigSchemeECDSA, nil
	case *rsa.PrivateKey:
		return authSigSchemeRSAPKCS1V15, nil
	default:
		return "", errors.New("AUTH_SIG_SCHEME is required when key algorithm cannot be inferred")
	}
}

func signAuthNonce(keyFile string, sigScheme string, nonce []byte) ([]byte, error) {
	key, err := loadPrivateKey(keyFile)
	if err != nil {
		return nil, err
	}
	if err := validateSupportedAuthKey(key); err != nil {
		return nil, err
	}
	scheme, err := canonicalizeAuthSigScheme(sigScheme)
	if err != nil {
		return nil, err
	}
	if scheme == "" {
		scheme, err = detectAuthSigSchemeFromKeyFile(keyFile)
		if err != nil {
			return nil, err
		}
	}
	h := sha256.Sum256(nonce)
	switch scheme {
	case authSigSchemeECDSA:
		ecdsaKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("AUTH_KEY_FILE is not an EC private key")
		}
		return ecdsa.SignASN1(rand.Reader, ecdsaKey, h[:])
	case authSigSchemeRSAPKCS1V15:
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("AUTH_KEY_FILE is not an RSA private key")
		}
		return rsa.SignPKCS1v15(rand.Reader, rsaKey, crypto.SHA256, h[:])
	case authSigSchemeRSAPSS:
		rsaKey, ok := key.(*rsa.PrivateKey)
		if !ok {
			return nil, errors.New("AUTH_KEY_FILE is not an RSA private key")
		}
		return rsa.SignPSS(rand.Reader, rsaKey, crypto.SHA256, h[:], &rsa.PSSOptions{Hash: crypto.SHA256, SaltLength: rsa.PSSSaltLengthEqualsHash})
	default:
		return nil, errors.New("AUTH_SIG_SCHEME must be ECDSA, RSA_PKCS1_V15, or RSA_PSS")
	}
}

func validateSupportedAuthKey(key crypto.PrivateKey) error {
	switch k := key.(type) {
	case *ecdsa.PrivateKey:
		if k.Curve == nil || k.Curve.Params() == nil || k.Curve.Params().BitSize != 256 {
			return errors.New("AUTH_KEY_FILE must be ECDSA P-256 or RSA 2048 private key")
		}
		return nil
	case *rsa.PrivateKey:
		if k.N == nil || k.N.BitLen() != 2048 {
			return errors.New("AUTH_KEY_FILE must be ECDSA P-256 or RSA 2048 private key")
		}
		return nil
	default:
		return errors.New("AUTH_KEY_FILE must be ECDSA P-256 or RSA 2048 private key")
	}
}

func loadPrivateKey(keyFile string) (crypto.PrivateKey, error) {
	pemBytes, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read AUTH_KEY_FILE<%s>", keyFile)
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid AUTH_KEY_FILE<%s>", keyFile)
	}
	switch block.Type {
	case "PRIVATE KEY":
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid AUTH_KEY_FILE<%s>", keyFile)
		}
		return key, nil
	case "RSA PRIVATE KEY":
		key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid AUTH_KEY_FILE<%s>", keyFile)
		}
		return key, nil
	case "EC PRIVATE KEY":
		key, err := x509.ParseECPrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("invalid AUTH_KEY_FILE<%s>", keyFile)
		}
		return key, nil
	default:
		return nil, fmt.Errorf("invalid AUTH_KEY_FILE<%s>", keyFile)
	}
}

func readChallengeFields(units map[uint32][]MarshalUnit) ([]byte, uint32, error) {
	nonceUnit, ok := firstUnit(units, cmiCAuthNonceID)
	if !ok || nonceUnit.typ != cmiBinaryType {
		return nil, 0, errors.New("connect response missing auth nonce")
	}
	if len(nonceUnit.data) != 32 {
		return nil, 0, errors.New("invalid auth nonce length")
	}
	validUnit, ok := firstUnit(units, cmiCAuthValidMsID)
	if !ok {
		return nil, 0, errors.New("connect response missing auth validity")
	}
	validMs, ok := readUIntLE(validUnit.data)
	if !ok || validMs == 0 {
		return nil, 0, errors.New("invalid auth validity")
	}
	nonce := make([]byte, len(nonceUnit.data))
	copy(nonce, nonceUnit.data)
	return nonce, uint32(validMs), nil
}
