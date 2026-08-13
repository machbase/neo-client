//go:build example

package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

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
	fields = append(fields, fmt.Sprintf("server=tcp://%s:%s@%s:%s", user, password, host, port))
	// other options
	// fields = append(fields, "fetch_rows=777")
	// fields = append(fields, "statement_cache=off")
	// fields = append(fields, "io_metrics=true")
	// fields = append(fields, "alternative_servers=127.0.0.2:5656")

	db, err := sql.Open("machbase", strings.Join(fields, ";"))
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()

	_, err = db.ExecContext(ctx, `CREATE TAG TABLE IF NOT EXISTS EXAMPLE (
		NAME   VARCHAR(100)  PRIMARY KEY,
		TIME   DATETIME      BASE TIME,
		VALUE  DOUBLE
	)`)
	if err != nil {
		panic(err)
	}
	ts := time.Now()
	for i := 0; i < 10; i++ {
		// NAME, TIME, VALUE
		rec := []any{
			"example-client",
			ts.Add(time.Second * time.Duration(i)),
			3.14 * float64(i),
		}
		result, err := db.ExecContext(ctx, `INSERT INTO EXAMPLE VALUES (?, ?, ?)`, rec...)
		if err != nil {
			panic(err)
		}
		affected, err := result.RowsAffected()
		if err != nil {
			panic(err)
		}
		fmt.Println("Rows affected:", affected)
	}
}
