package main

import (
	"context"
	"flag"
	"net"
	"strconv"
	"time"

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

	appender, err := conn.Appender(ctx, "EXAMPLE")
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
		if err := appender.Append(rec...); err != nil {
			panic(err)
		}
	}
	success, fail, err := appender.Close()
	if err != nil {
		panic(err)
	}
	println("Append finished. Success:", success, "Fail:", fail)
}
