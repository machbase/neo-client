# neo-client

`neo-client` is the Go client module for Machbase Neo.

It provides:

- `machgo`: the main client package used by applications
- `machbase`: the standard `database/sql` driver package
- `api`: shared interfaces, options, and helper types
- `machnet`: lower-level protocol and transport implementation used by `machgo`

The examples in this module show how to connect to a Machbase Neo server, execute queries, append time-series records, and use the standard `database/sql` API.

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

### `machbase`

This package provides a standard Go `database/sql` driver on top of the native TCP client. Use it when you want to integrate Machbase Neo with libraries or application code that already expect the standard `database/sql` interfaces.

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

### Use the Standard `database/sql` Driver

If your application already uses `database/sql`, import `github.com/machbase/neo-client` for driver registration and connect with `sql.Open`.

```go
package main

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/machbase/neo-client"
)

func main() {
	dsn := "server=tcp://sys:manager@127.0.0.1:5656;fetch_rows=777;statement_cache=off;io_metrics=true"

	db, err := sql.Open("machbase", dsn)
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

	cols, err := rows.Columns()
	if err != nil {
		panic(err)
	}
	fmt.Println("Columns:", cols)
	for rows.Next() {
		// scan values here
	}
	if err := rows.Err(); err != nil {
		panic(err)
	}
}
```

Supported DSN forms for the standard driver:

- Server value only:
	- `host`
	- `host:port`
	- URL form such as `tcp://user:password@host:port?as=proxy&fetch_rows=100`
- Key-value pairs separated by `;`:
	- `key=value;key=value;...`
	- example: `user=sys;password=manager;server=127.0.0.1:5656`

For key-value DSN syntax:

- Value may be quoted with `"..."` or `'...'`.
- `;` inside quoted values is treated as a literal character.
- Escapes are supported inside quoted values with backslash:
	- `\"` for `"` in double-quoted values
	- `\'` for `'` in single-quoted values
	- `\\` for `\`
- Unterminated or mismatched quotes return a parse error.

Examples:

- `user="sys as demo";password="12;34";server=127.0.0.1:5656;`
- `user='sys as demo';password='12;34';server=127.0.0.1:5656;`
- `password="a\\\"b";server=127.0.0.1:5656;`

Supported DSN keys include:

- `server`: server address such as `tcp://sys:manager@127.0.0.1:5656`
- `host`, `port`, `user`, `password`: explicit connection fields
- `auth_mode`: authentication mode (`password` or `challenge`)
- `auth_key_file`: private key file path for `auth_mode=challenge`
- `auth_key_pem`: inline private key PEM content for `auth_mode=challenge`

When `auth_key_file` or `auth_key_pem` is set and `auth_mode` is omitted, the driver treats it as challenge authentication implicitly.
- `fetch_rows`: fetch batch size
- `statement_cache`: `auto`, `on`, or `off`
- `io_metrics`: `true` or `false`
- `alternative_servers`: alternative server list such as `127.0.0.2:5656`

The standard driver follows `database/sql` pooling through `sql.DB`. On servers that support transaction tables, `Begin`, `BeginTx`, `Commit`, and `Rollback` execute explicit transactions. Only the default isolation level is accepted; read-only and custom isolation options return an error. `LastInsertId` is not supported.

### DECIMAL and Named Parameters

Machbase protocol 4.0.3 servers expose exact DECIMAL values, nullable/primary-key metadata, and named parameter occurrences. Native code uses `api.Decimal` and `api.Named`:

```go
amount, err := api.ParseDecimal("1234567890.125", 30, 3)
if err != nil {
	panic(err)
}
result := conn.Exec(ctx,
	"INSERT INTO payments(id, amount) VALUES (:id, :amount)",
	api.Named("id", int32(1)),
	api.Named("amount", amount),
)
if err := result.Err(); err != nil {
	panic(err)
}
```

The `database/sql` driver accepts `sql.Named` and returns DECIMAL query values as exact strings. Parameter names are case-sensitive, a repeated marker reuses one supplied value, and named and positional arguments cannot be mixed.

When connected to a pre-4.0.3 server, positional query, bind, fetch, and append behavior remains on the legacy protocol. Named arguments return an unsupported error, and nullable metadata is reported as unknown (`ColumnType.Nullable()` returns `ok=false`).

## Running the Included Examples

Four runnable examples are included under `_example/`.

Run the query example:

```sh
go run ./_example/query.go -s 127.0.0.1:5656 -u sys -p manager
```

Run the append example:

```sh
go run ./_example/append.go -s 127.0.0.1:5656 -u sys -p manager
```

Run the standard driver query example:

```sh
go run ./_example/driver_query.go -s 127.0.0.1:5656 -u sys -p manager
```

Run the standard driver insert example:

```sh
go run ./_example/driver_insert.go -s 127.0.0.1:5656 -u sys -p manager
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
- Use `machbase` when you need compatibility with `database/sql` and its connection pooling.
- The standard driver does not support explicit transactions or `LastInsertId`.

## See Also

- [_example/query.go](./_example/query.go)
- [_example/append.go](./_example/append.go)
- [_example/driver_query.go](./_example/driver_query.go)
- [_example/driver_insert.go](./_example/driver_insert.go)
