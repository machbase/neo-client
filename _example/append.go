//go:build example

package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	client "github.com/machbase/neo-client/v2"
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

	// dsn is the data source name for connecting to the database.
	dsn := fmt.Sprintf("server=tcp://%s:%s@%s:%s", user, password, host, port)
	// other options
	// dsn = dsn + ";alternative_servers=192.168.1.200:5656"

	ctx := context.Background()

	appender := &client.Appender{}
	// The columns are optional, if not specified, the columns will be retrieved from the table.
	// So, the below example is equivalent to `appender.Connect(ctx, dsn, "EXAMPLE")`
	if err := appender.Connect(ctx, dsn, "EXAMPLE", "NAME", "TIME", "VALUE"); err != nil {
		panic(err)
	}
	defer func() {
		successCount, failCount, err := appender.Close()
		if err != nil {
			panic(err)
		}
		println("Append finished. Success:", successCount, "Fail:", failCount)
	}()

	cols, _ := appender.Columns()
	typs, _ := appender.ColumnTypes()
	println("Columns:", strings.Join(cols, ", "))
	println("Column Types:", strings.Join(typs, ", "))

	ts := time.Now()
	for i := 0; i < 10; i++ {
		// NAME, TIME, VALUE
		rec := []any{
			"example-client",
			ts.Add(time.Second * time.Duration(i)),
			3.14 * float64(i),
		}
		if err := appender.Append(rec...); err != nil {
			panic(err)
		}
	}
}
