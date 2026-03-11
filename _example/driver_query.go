//go:build example

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"

	_ "github.com/machbase/neo-client/machbase"
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

	fields := []string{}
	fields = append(fields, fmt.Sprintf("server=tcp://%s:%s@%s:%d", user, password, host, port))
	fields = append(fields, "fetch_rows=777")
	fields = append(fields, "statement_cache=off")
	fields = append(fields, "io_metrics=true")
	//fields = append(fields, "alternative_servers=127.0.0.2:5656")

	db, err := sql.Open("machbase", strings.Join(fields, ";"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()

	rows, err := db.QueryContext(ctx, `SELECT * FROM M$SYS_TABLES ORDER BY NAME`)
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
