# VECTOR columns with the Go SDK

Machbase VECTOR values are finite FLOAT32 numbers, with a dimension from 1 to
65,536. The Go SDK exposes them as `api.Vector`, a named `[]float32` type.
The database does not turn a sentence into an embedding: run your embedding
model in the application, then bind its numeric result.

Native VECTOR metadata and binary values require Machbase CMI 4.0.5 or later.
The SDK rejects native VECTOR binds and append when the server advertises an
older protocol. VECTOR is distinct from the fixed-cardinality numeric ARRAY
type: ARRAY may have NULL elements; VECTOR may not.

## Insert and read a VECTOR

```go
package main

import (
    "context"
    "database/sql"
    "os"

    _ "github.com/machbase/neo-client/v2"
    "github.com/machbase/neo-client/v2/api"
)

func main() {
    ctx := context.Background()
    db, err := sql.Open("machbase", os.Getenv("MACHBASE_DSN"))
    if err != nil { panic(err) }
    defer db.Close()

    // Example schema: CREATE TRANSACTION TABLE DOCUMENTS
    //                 (ID INTEGER PRIMARY KEY, EMBEDDING VECTOR(3));
    embedding, err := api.NewVector(0.5, 0.25, -1)
    if err != nil { panic(err) }
    _, err = db.ExecContext(ctx,
        "INSERT INTO DOCUMENTS VALUES(?,?)", int32(1), embedding)
    if err != nil { panic(err) }

    var result api.Vector
    err = db.QueryRowContext(ctx,
        "SELECT EMBEDDING FROM DOCUMENTS WHERE ID=?", int32(1)).Scan(&result)
    if err != nil { panic(err) }
    _ = result // []float32{0.5, 0.25, -1}
}
```

The SDK also accepts `[]float32`, `[]float64`, or a numeric JSON array such as
`"[0.5,0.25,-1]"` when the prepared parameter is described as VECTOR. A typed
nil `api.Vector` or nil `*api.Vector` binds SQL NULL. A malformed dimension,
non-numeric element, NaN, Infinity, or value outside FLOAT32 range is rejected.
`api.ParseVector` parses JSON explicitly; `api.Vector.Scan` accepts VECTOR
results and JSON text. Scan a `database/sql` result into `*api.Vector` to
preserve numeric values. Dynamic expressions such as `TO_VECTOR(...)` can have
different row dimensions; the decoder uses the actual row payload length.

## APPEND

`Appender.Append` accepts `api.Vector` and `[]float32` for a VECTOR column. The
SDK validates the table column's declared dimension before sending the row.

```go
appender := &client.Appender{}
if err := appender.Connect(ctx, dsn, "DOCUMENTS", "ID", "EMBEDDING"); err != nil {
    panic(err)
}
if err := appender.Append(int32(2), api.Vector{1, 0, 0}); err != nil {
    panic(err)
}
_, _, err := appender.Close()
if err != nil { panic(err) }
```

Here `client` is the package imported as
`client "github.com/machbase/neo-client/v2"`, and `dsn` is the same connection
string used for `sql.Open`. The Go SDK handles the VECTOR wire payload;
`TO_VECTOR` is needed only when SQL receives a numeric JSON value as text.

To run the real-server integration test against a dedicated CMI 4.0.5 Standard
database, set `MACHBASE_VECTOR_TEST_DSN` and run
`go test -run '^TestVector.*Integration$' .`. The tests create and drop their
own `GO_VECTOR_4211` and `GO_VECTOR_LARGE_4211` tables. The large test also
checks a 17,000-dimensional value across CMI packet boundaries.
