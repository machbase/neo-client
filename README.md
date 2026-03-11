# neo-client

`neo-client` is the Go client module for Machbase Neo.

It provides:

- `machgo`: the main client package used by applications
- `api`: shared interfaces, options, and helper types
- `machnet`: lower-level protocol and transport implementation used by `machgo`

The examples in this module show how to connect to a Machbase Neo server, execute queries, and append time-series records.

## Requirements

- Go 1.22 or later
- A reachable Machbase Neo server
- A valid user account, such as `sys` / `manager` in a local development environment

The examples in this repository use the native TCP endpoint, usually `127.0.0.1:5656`.

## Install

```sh
go get github.com/machbase/neo-client
```

## Package Layout

### `machgo`

Use this package for normal application code. It exposes the database handle, connection management, query execution, prepared statements, and append operations.

### `api`

This package contains interfaces such as `Database`, `Conn`, `Rows`, and `Appender`, plus options like `api.WithPassword`, `api.WithFetchRows`, and `api.WithStatementCache`.

### `machnet`

This is the lower-level client implementation used internally by `machgo`. Most application code should not need to import it directly.

## Quick Start

### Connect and Query

The following example connects to a server and reads from the `M$SYS_TABLES` system table.

```go
package main

import (
	"context"
	"fmt"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machgo"
)

func main() {
	db, err := machgo.NewDatabase(&machgo.Config{
		Host:        "127.0.0.1",
		Port:        5656,
		MaxOpenConn: -1,
	})
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Connect(ctx, api.WithPassword("sys", "manager"))
	if err != nil {
		panic(err)
	}
	defer conn.Close()

	rows, err := conn.Query(ctx, `SELECT NAME, ID, TYPE FROM M$SYS_TABLES ORDER BY NAME`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			name string
			id   int64
			typ  int
		)
		if err := rows.Scan(&name, &id, &typ); err != nil {
			panic(err)
		}
		fmt.Println(name, id, typ)
	}

	if err := rows.Err(); err != nil {
		panic(err)
	}
}
```

### Append Rows

The append example assumes a tag table named `EXAMPLE` exists with `NAME`, `TIME`, and `VALUE` columns.

```sql
CREATE TAG TABLE IF NOT EXISTS example (
    name VARCHAR(100) PRIMARY KEY,
    time DATETIME BASETIME,
    value DOUBLE
);
```

Then append rows with `conn.Appender`:

```go
package main

import (
	"context"
	"time"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machgo"
)

func main() {
	db, err := machgo.NewDatabase(&machgo.Config{
		Host:        "127.0.0.1",
		Port:        5656,
		MaxOpenConn: -1,
	})
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()
	conn, err := db.Connect(ctx, api.WithPassword("sys", "manager"))
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
		if err := appender.Append(
			"example-client",
			ts.Add(time.Duration(i)*time.Second),
			3.14*float64(i),
		); err != nil {
			panic(err)
		}
	}

	success, fail, err := appender.Close()
	if err != nil {
		panic(err)
	}

	println("Append finished.", "success:", success, "fail:", fail)
}
```

## Running the Included Examples

Two runnable examples are included under `_example/`.

Run the query example:

```sh
go run ./_example/query.go -s 127.0.0.1:5656 -u sys -p manager
```

Run the append example:

```sh
go run ./_example/append.go -s 127.0.0.1:5656 -u sys -p manager
```

## Common Connection Options

- `api.WithPassword(user, password)`: connect with explicit credentials
- `api.WithFetchRows(n)`: override the default fetch batch size for a connection
- `api.WithStatementCache(mode)`: control query statement reuse
- `api.WithIOMetrics(true)`: enable I/O metrics collection on the connection

## Notes

- Always close `Rows`, `Stmt`, `Appender`, and `Conn` objects after use.
- `Appender.Close()` returns success and failure counts for the append session.
- For regular application usage, prefer `machgo` over importing `machnet` directly.

## See Also

- [_example/query.go](./_example/query.go)
- [_example/append.go](./_example/append.go)
