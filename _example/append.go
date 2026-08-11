//go:build example

package main

import (
	"flag"
	"fmt"
	"net"
	"strings"
	"time"

	client "github.com/machbase/neo-client"
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

	// dsn is the data source name for connecting to the database.
	dsn := strings.Join(fields, ";")

	appender := client.NewAppender(nil)
	// The columns are optional, if not specified, the columns will be retrieved from the table.
	// So, the below example is equivalent to `appender.Connect(dsn, "EXAMPLE")`
	if err := appender.Connect(dsn, "EXAMPLE", "NAME", "TIME", "VALUE"); err != nil {
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
