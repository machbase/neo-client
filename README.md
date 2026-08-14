# neo-client

`neo-client` is the Go client module for Machbase Neo.

It provides:

- `neo-client`: the standard `database/sql` driver package
- `api`: shared interfaces, options, and helper types
- `machnet`: lower-level protocol and transport implementation used by `neo-client`

The examples in this module show how to connect to a Machbase Neo server, execute queries, append time-series records, and use the standard `database/sql` API.

## Requirements

- Go 1.22 or later
- A reachable Machbase Neo server
- A valid user account, such as `sys` / `manager` in a local development environment

The examples in this repository use the native TCP endpoint, usually `127.0.0.1:5656`.

## Install

```sh
go get github.com/machbase/neo-client/v2
```

## Package Layout

### `client`

This package provides a standard Go `database/sql` driver on top of the native TCP client. Use it when you want to integrate Machbase Neo with libraries or application code that already expect the standard `database/sql` interfaces.

### `api`

This package contains interfaces such as `Database`, `Conn`, `Rows`, and `Appender`, plus options like `api.WithPassword`, `api.WithFetchRows`, and `api.WithStatementCache`.

### `machnet`

This is the lower-level client implementation used internally by `machgo`. Most application code should not need to import it directly.

## Quick Start

### Connect and Query

The following example uses the standard `database/sql` package and reads from the `M$SYS_TABLES` system table.

```go
package main

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/machbase/neo-client/v2"
)

func main() {
	db, err := sql.Open("machbase", "server=tcp://sys:manager@127.0.0.1:5656")
	if err != nil {
		panic(err)
	}
	defer db.Close()

	ctx := context.Background()
	rows, err := db.QueryContext(ctx, `SELECT NAME, ID, TYPE FROM M$SYS_TABLES ORDER BY NAME`)
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

### Create Table and Insert Rows

The following example uses `database/sql` to create a tag table and insert records with `ExecContext`.

```sql
CREATE TAG TABLE IF NOT EXISTS example (
    name VARCHAR(100) PRIMARY KEY,
	time DATETIME BASE TIME,
    value DOUBLE
);
```

Then insert rows:

```go
package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/machbase/neo-client/v2"
)

func main() {
	dsn := "server=tcp://sys:manager@127.0.0.1:5656"

	db, err := sql.Open("machbase", dsn)
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
		rec := []any{
			"example-client",
			ts.Add(time.Duration(i)*time.Second),
			3.14*float64(i),
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
```

### Append Rows with Appender API

For high-throughput time-series ingestion, you can use the dedicated appender API shown in `_example/append.go`.

### Use the Standard `database/sql` Driver

If your application already uses `database/sql`, import `github.com/machbase/neo-client` for driver registration and connect with `sql.Open`.

```go
package main

import (
	"context"
	"database/sql"
	"fmt"

	_ "github.com/machbase/neo-client/v2"
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
- URL form such as `tcp://user:password@host:port/database?as=proxy&fetch_rows=100`
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
- `host`, `port`: explicit server fields (default `port=5656`)
- `user`, : login user and password
- `database`: database name
- `auth_mode`: authentication mode (`password` or `challenge`)
- `fetch_rows`, `fetchrows`: fetch batch size (default: `1000`)
- `statement_cache`, `statementcache`: `auto`, `on`, or `off` (default: `auto`)
- `io_metrics`, `iometrics`: `true` or `false`
- `alternative_servers`: one or more comma-separated server addresses such as `127.0.0.2:5656,backup.example.com:5657`

When `auth_key_file` or `auth_key_pem` is set and `auth_mode` is omitted, the driver treats it as challenge authentication implicitly.
- `auth_key_file`: private key file path for `auth_mode=challenge`
- `auth_key_pem`: inline private key PEM content for `auth_mode=challenge`
- `auth_sig_scheme`: challenge authentication signature scheme.

URL query parameters use the same option names. For example:

`tcp://sys:manager@127.0.0.1:5656/DATABASE_A?statement_cache=on&io_metrics=true`

Unknown keys are errors in key-value DSNs but are ignored in URL query strings.

The URL path also selects the initial database, for example `tcp://sys:manager@127.0.0.1:5656/DATABASE_A`. The driver selects the configured database on every new physical connection. If application code executes `USE` directly, the driver restores the configured database before that connection is reused from the pool.

The standard driver follows `database/sql` pooling through `sql.DB`. On servers that support transaction tables, `Begin`, `BeginTx`, `Commit`, and `Rollback` execute explicit transactions. Only the default isolation level is accepted; read-only and custom isolation options return an error. `LastInsertId` is not supported.

### Machbase 8.6.0 DECIMAL and Named Parameters

Machbase 8.6.0 provides exact DECIMAL values, nullable column information, and named parameters. Native code uses `api.Decimal` and `api.Named`:

```go
amount, err := api.ParseDecimal("1234567890.125", 30, 3)
if err != nil {
	panic(err)
}
result, err := conn.ExecContext(ctx,
	"INSERT INTO payments(id, amount) VALUES (:id, :amount)",
	api.Named("id", int32(1)),
	api.Named("amount", amount),
)
if err != nil {
	panic(err)
}
```

The `database/sql` driver accepts `sql.Named` and returns DECIMAL query values as exact strings. Parameter names are case-sensitive, a repeated marker reuses one supplied value, and named and positional arguments cannot be mixed.

When connected to Machbase 8.5.x, use positional `?` parameters with the table and data types supported by that server version. Named parameters and Machbase 8.6.0 data types are not available, and nullable column information may be unknown (`ColumnType.Nullable()` returns `ok=false`).

See [Machbase 8.6.0 Go client guide](docs/machbase-860-upgrade.md) for Transaction table edition limits, DECIMAL/NULL scan patterns, transaction and appender usage, prepared statements, and Machbase 8.5.x compatibility.

## Running the Included Examples

Runnable examples are included under `_example/`.

Run the query example:

```sh
go run ./_example/query.go -s 127.0.0.1:5656 -u sys -p manager
```

Run the append example:

```sh
go run ./_example/append.go -s 127.0.0.1:5656 -u sys -p manager
```

Run the insert example:

```sh
go run ./_example/insert.go -s 127.0.0.1:5656 -u sys -p manager
```

## Common DSN Options

- `server=tcp://user:password@host:port`: full server URL
- `database=DB_NAME` (or URL path): initial database for each physical connection
- `fetch_rows=777`: override fetch batch size
- `statement_cache=auto|on|off`: control statement reuse; defaults to `auto`
- `io_metrics=true|false`: enable or disable I/O metrics
- `alternative_servers=host1:port1,host2:port2`: one or more alternative server addresses, tried in order

## Notes

- Always close `Rows`, `Stmt`, `sql.Conn`, and `sql.DB` objects after use.
- `Appender.Close()` returns success and failure counts for the append session.
- On servers that support transaction tables, the standard driver supports explicit transactions. `LastInsertId` remains unsupported.

## See Also

- [_example/query.go](./_example/query.go)
- [_example/insert.go](./_example/insert.go)
- [_example/append.go](./_example/append.go)
- [_example/v860.go v8.6.x example](./_example/v860.go)
- [Machbase 8.6.0 Changes](./docs/machbase-860-upgrade.md)
