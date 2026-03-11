//go:build example

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

var server = "127.0.0.1:5656"
var user = "sys"
var password = "manager"

func main() {
	flag.StringVar(&server, "s", server, "server address")
	flag.StringVar(&user, "u", user, "user")
	flag.StringVar(&password, "p", password, "password")
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

	ctx := context.Background()
	conn, err := db.Connect(ctx, api.WithPassword(user, password))
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	rows, err := conn.Query(ctx, `SELECT * FROM M$SYS_TABLES ORDER BY NAME`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	columns, err := rows.Columns()
	if err != nil {
		panic(err)
	}
	fmt.Println("Columns:", columns)
	var (
		name        string
		typ         int
		dbId        int64
		id          int64
		userId      int
		columnCount int
		flag        int
	)
	for rows.Next() {
		if err := rows.Scan(&name, &typ, &dbId, &id, &userId, &columnCount, &flag); err != nil {
			panic(err)
		}
		fmt.Println(name, typ, dbId, id, userId, columnCount, flag)
	}
}
