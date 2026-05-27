//go:build example

// Command authkey_connect connects to Machbase with an auth private key.
//
// This example assumes the corresponding public key is already registered
// and activated on the server via SQL.
//
// Example:
//
//	go run -tags example ./_example/authkey_connect.go -s 127.0.0.1:5656 -u sys -k ./_example/demo_private.pem
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"strconv"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machgo"
)

func main() {
	server := "127.0.0.1:5656"
	user := "sys"
	keyFile := "./machbase_authkey_p256_private.pem"
	proxyUser := ""

	flag.StringVar(&server, "s", server, "server address")
	flag.StringVar(&user, "u", user, "user")
	flag.StringVar(&keyFile, "k", keyFile, "private key file path")
	flag.StringVar(&proxyUser, "as", proxyUser, "proxy user, connect as other user (this option only works when login user is sys)")
	flag.Parse()

	host, portStr, err := net.SplitHostPort(server)
	if err != nil {
		panic(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		panic(err)
	}

	db, err := machgo.NewDatabase(&machgo.Config{
		Host:         host,
		Port:         port,
		MaxOpenConn:  -1,
		MaxOpenQuery: -1,
	})
	if err != nil {
		panic(err)
	}
	defer db.Close()

	opts := []api.ConnectOption{api.WithAuthKeyFile(user, keyFile)}
	if proxyUser != "" {
		opts = append(opts, api.WithProxyUser(proxyUser))
	}
	ctx := context.Background()
	conn, err := db.Connect(ctx, opts...)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	row := conn.QueryRow(ctx, "select 1")
	if row.Err() != nil {
		panic(row.Err())
	}

	var v int64
	if err := row.Scan(&v); err != nil {
		panic(err)
	}
	fmt.Println("Auth key connect success, query result:", v)
}
