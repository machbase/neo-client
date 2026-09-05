# Transaction Helper Reference

This reference describes `client.Tx` and `client.TxConn`, the closure-based
transaction helpers provided by `github.com/machbase/neo-client/v2` on top of
the standard `database/sql` transaction API.

## Server Support

Transactions (`BEGIN` / `COMMIT` / `ROLLBACK`) are only supported on regular
tables created with `CREATE TABLE` (transaction tables, available since
Machbase 8.6.0 standard edition).

TAG tables do **not** support transactions. Any transactional DML on a TAG
table fails with `MACHCLI-ERR-2362, This statement is not supported`.

Transaction options are not supported:

- Isolation levels other than the default are rejected with an error.
- Read-only transactions are rejected with an error.

## API

```go
func Tx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error
func TxConn(ctx context.Context, conn *sql.Conn, fn func(tx *sql.Tx) error) error
```

Both helpers share the same semantics:

| Outcome of `fn` | Result |
| --- | --- |
| returns `nil` | The transaction is committed. A commit failure is returned. |
| returns an error | The transaction is rolled back and the error is returned **as-is**, so `errors.Is` / `errors.As` keep working. If the rollback itself also fails, the rollback error is attached to the original error. |
| panics | The transaction is rolled back and the panic is re-raised with the original value. |
| `BeginTx` fails | The error is returned and `fn` is never called. |

`Tx` begins the transaction on the connection pool (`*sql.DB`). `TxConn`
begins it on a specific connection (`*sql.Conn`), which is useful when the
caller already holds a connection acquired via `db.Conn(ctx)` and needs the
transaction to run on that exact connection.

## Usage

### Commit

```go
err := client.Tx(ctx, db, func(tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO EXAMPLE_TX VALUES (?, ?, ?)`, name, ts, value); err != nil {
		return err
	}
	return nil // COMMIT
})
```

### Intentional rollback with a sentinel error

Returning an error from the closure is the idiomatic way to abort a
transaction. Use a sentinel error so the caller can distinguish an intentional
abort from a real failure:

```go
var errAbort = errors.New("abort transaction")

err := client.Tx(ctx, db, func(tx *sql.Tx) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM EXAMPLE_TX WHERE NAME = ?`, name); err != nil {
		return err
	}
	return errAbort // ROLLBACK
})
if errors.Is(err, errAbort) {
	// intentional rollback
}
```

### Panic safety

A panic inside the closure never leaks an open transaction: the helper rolls
back and re-panics with the original value.

```go
func() {
	defer func() {
		if p := recover(); p != nil {
			// the transaction was already rolled back
		}
	}()
	_ = client.Tx(ctx, db, func(tx *sql.Tx) error {
		// ...
		panic("boom")
	})
}()
```

### Transaction on a specific connection

```go
conn, err := db.Conn(ctx)
if err != nil {
	return err
}
defer conn.Close()

err = client.TxConn(ctx, conn, func(tx *sql.Tx) error {
	// statements run on the connection held by conn
	return nil
})
```

## Design Notes

The closure shape follows the de-facto standard used by other Go database
libraries: pgx (`pgx.BeginFunc`), GORM (`db.Transaction`), ent (`WithTx`), and
cockroach-go (`crdb.ExecuteTx`) all converge on `fn func(tx) error` with
identical commit/rollback/panic semantics.

The helper does not accept `*sql.TxOptions`: the machbase driver rejects
non-default options at `BeginTx` time, so accepting options would invite
runtime errors. This keeps the signature honest.

Automatic retry (as in cockroach-go's `ExecuteTx`) is intentionally omitted:
machbase has no retryable error class equivalent to CockroachDB's 40001.

## See Also

- Runnable example: [_example/transaction.go](../_example/transaction.go)
- Integration tests: `TestClientTxHelper` in
  `neo-server/spi/sql_test.go`
- Machbase 8.6.0 transaction tables:
  [machbase-860-upgrade.md](machbase-860-upgrade.md)
