//go:build example

// Command authkey_gen generates a Machbase auth key pair (ECDSA P-256).
//
// It writes two PEM files:
//
//   - <name>_private.pem: private key for client authentication
//   - <name>_public.pem: public key to register on the server
//
// Use this command before auth key registration on the server.
//
// Example:
//
//	go run -tags example ./_example/authkey_gen.go -out ./_example -name demo
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func main() {
	outDir := "./"
	prefix := "machbase_authkey_p256"

	flag.StringVar(&outDir, "out", outDir, "output directory")
	flag.StringVar(&prefix, "name", prefix, "output file name prefix")
	flag.Parse()

	if err := os.MkdirAll(outDir, 0o700); err != nil {
		panic(err)
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}

	privDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		panic(err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		panic(err)
	}

	privPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privDER})
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})

	privPath := filepath.Join(outDir, prefix+"_private.pem")
	pubPath := filepath.Join(outDir, prefix+"_public.pem")

	if err := os.WriteFile(privPath, privPEM, 0o600); err != nil {
		panic(err)
	}
	if err := os.WriteFile(pubPath, pubPEM, 0o644); err != nil {
		panic(err)
	}

	absPrivPath, _ := filepath.Abs(privPath)
	absPubPath, _ := filepath.Abs(pubPath)

	fmt.Println("Auth key pair generated.")
	fmt.Println("Private key:", absPrivPath)
	fmt.Println("Public key:", absPubPath)
	fmt.Println("Use the public key file content when you register the key on server side.")
}
