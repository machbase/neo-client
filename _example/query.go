//go:build example

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net"
	"strings"

	_ "github.com/machbase/neo-client/v2"
)

var server = "127.0.0.1:5656"
var user = "sys"
var password = "manager"

func main() {
	flag.StringVar(&server, "s", server, "server address")
	flag.StringVar(&user, "u", user, "user")
	flag.StringVar(&password, "p", password, "password")
	flag.Parse()

	host, port, err := net.SplitHostPort(server)
	if err != nil {
		panic(err)
	}

	fields := []string{}
	fields = append(fields, "host="+host)
	fields = append(fields, "port="+port)
	fields = append(fields, "user="+user)
	fields = append(fields, "password="+password)
	// other options
	// fields = append(fields, "fetch_rows=777")
	// fields = append(fields, "statement_cache=off")
	// fields = append(fields, "io_metrics=true")
	// fields = append(fields, "alternative_servers=other_host:port")

	pool, err := sql.Open("machbase", fmt.Sprintf("%s", strings.Join(fields, "; ")))
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	ctx := context.Background()
	conn, err := pool.Conn(ctx)
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	rows, err := conn.QueryContext(ctx, `SELECT * FROM M$SYS_TABLES ORDER BY NAME`)
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
