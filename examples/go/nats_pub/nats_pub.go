package main

import (
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/nats-io/nats.go"
)

// Before run this program,
//  1. Add bridge in machbase-neo server
//     bridge add -t nats my_nats server=127.0.0.1:4222 name=hello
//  2. Add subscriber
//     subscriber add hello-nats my_nats test.topic db/write/EXAMPLE:csv;
//  3. Start subscriber
//     subscriber start hello-nats
//  4. Run
//     go run nats_pub.go -server nats://<ip>:<port>
func main() {
	optServer := flag.String("server", "nats://127.0.0.1:4222", "nats server address")
	flag.Parse()

	opts := nats.GetDefaultOptions()
	opts.Servers = []string{*optServer}
	conn, err := opts.Connect()
	if err != nil {
		panic(err)
	}
	defer conn.Close()
	lines := []string{}
	tick := time.Now()
	for i := 0; i < 10; i++ {
		line := fmt.Sprintf("hello-nats,%d,1.2345", tick.Add(time.Duration(i)).UnixNano())
		lines = append(lines, line)
	}
	reqData := []byte(strings.Join(lines, "\n"))

	// A) request-respond model
	if rsp, err := conn.Request("test.topic", reqData, 100*time.Millisecond); err != nil {
		panic(err)
	} else {
		fmt.Println("RESP:", string(rsp.Data))
	}
	// B) fire-and-forget model
	//
	// if err := conn.Publish("test.topic", reqData); err != nil {
	// 	panic(err)
	// }
}
