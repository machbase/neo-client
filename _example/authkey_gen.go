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
	"flag"
	"fmt"
	"path/filepath"

	"github.com/machbase/neo-client/v2/api"
)

func main() {
	outDir := "./"
	prefix := api.DefaultAuthKeyPrefix

	flag.StringVar(&outDir, "out", outDir, "output directory")
	flag.StringVar(&prefix, "name", prefix, "output file name prefix")
	flag.Parse()

	pair, err := api.GenerateAuthKeyPair()
	if err != nil {
		panic(err)
	}

	privPath, pubPath, err := pair.WriteFiles(outDir, prefix)
	if err != nil {
		panic(err)
	}

	absPrivPath, _ := filepath.Abs(privPath)
	absPubPath, _ := filepath.Abs(pubPath)

	fmt.Println("Auth key pair generated.")
	fmt.Println("Private key:", absPrivPath)
	fmt.Println("Public key:", absPubPath)
	fmt.Println("Use the public key file content when you register the key on server side.")
}
