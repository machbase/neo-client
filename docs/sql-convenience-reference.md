# SQL Convenience API Reference

This reference describes the convenience APIs added by `github.com/machbase/neo-client/v2` on top of Go's `database/sql` interfaces. These APIs do not replace the standard driver API. They help applications map query results to typed values and construct named query arguments.

## Scope

The package provides two independent features:

- Struct, scalar, and map result scanning from `*sql.Rows`.
- `sql.Named` argument construction from structs and `map[string]any` values.

All database value conversion is still performed by `database/sql` and the Machbase driver.

## Result Scanning

### Supported destination types

The generic scan functions accept these element types:

| Element type | Requirement |
| --- | --- |
| Struct | Fields are mapped to result columns by tags or an optional name mapper. |
| Pointer to a struct | The helper allocates one struct for each returned row. |
| Scalar | The query must return exactly one column. |
| `map[string]any` | Each result column becomes one map entry. |

For a struct field, use a pointer type such as `*float64` to receive SQL `NULL` as `nil`. `sql.Null[T]` fields are also supported by `database/sql`.

```go
type TagRecord struct {
	Name  string    `db:"NAME"`
	Time  time.Time `db:"TIME"`
	Value *float64  `db:"VALUE"`
}
```

### Row ownership

`ScanStruct`, `ScanRow`, `ScanRows`, `ScanOne`, `ScanAll`, `ScanEach`, and `NewCursor` never close the supplied rows. The caller remains responsible for `rows.Close()`.

`Get` and `Select` create their own rows and close them before returning.

```go
rows, err := db.QueryContext(ctx, query, args...)
if err != nil {
	return err
}
defer rows.Close()

records, err := client.ScanAll[TagRecord](rows)
```

### Mapping rules

The default primary tag is `db`; `json` is used only when a field has no `db` tag.

| Field declaration | Mapping result |
| --- | --- |
| `Value float64 \`db:"VALUE"\`` | Maps to `VALUE`. |
| `Value float64 \`json:"value"\`` | Maps to `value` through the fallback tag. |
| `Value float64 \`db:",omitempty"\`` | Maps to `Value`. |
| `Value float64 \`json:",omitempty"\`` | Maps to `Value`. |
| `Cache string \`db:"-"\`` | Excluded. |
| `Cache string` | Excluded unless a name mapper is configured. |

Column names and mapped names are compared case-insensitively. A column named `ID` therefore matches `db:"id"`.

Embedded structs with no explicit tag are flattened. A named nested struct is flattened with its mapped name as a dot-separated prefix.

```go
type Identity struct {
	ID int64 `db:"ID"`
}

type Record struct {
	Identity
	Meta struct {
		Source string `db:"SOURCE"`
	} `db:"META"`
}
// Record maps ID and META.SOURCE.
```

Structs that implement `sql.Scanner` are treated as one scalar destination rather than flattened.

### Strictness

Struct mapping is strict by default:

- Every result column must map to exactly one struct field.
- Every mapped struct field must have a matching result column.
- Duplicate column names, ignoring case, are rejected.
- Duplicate mapped field names, ignoring case, are rejected.

These checks prevent query changes, especially `SELECT *` changes, from silently leaving fields empty. Use `WithLaxColumns` or `WithLaxFields` only when the omitted side is intentional.

`map[string]any` does not use struct tags, but it also rejects duplicate result column names because a Go map cannot represent them without discarding a value.

### Scan options

Pass options to the row-oriented helpers and `NewCursor` after their required arguments.

| Option | Effect |
| --- | --- |
| `WithTagKey(key)` | Replaces the primary tag key, which defaults to `db`. |
| `WithFallbackTagKey(key)` | Replaces the fallback key, which defaults to `json`; use `""` to disable it. |
| `WithNameMapper(fn)` | Maps untagged fields with `fn`. |
| `WithLaxColumns()` | Ignores result columns with no mapped field. |
| `WithLaxFields()` | Allows mapped fields absent from the result set. |
| `WithMaxRows(n)` | Limits `ScanRows` and `ScanAll`; `0` disables the limit. |
| `WithCapacity(n)` | Sets initial slice capacity for `ScanRows` and `ScanAll`. |

`NameMapperIdentity()` maps a field name unchanged. `NameMapperSnake()` maps `DeviceID` to `device_id`.

```go
type Reading struct {
	DeviceID int64
	Value    float64
}

items, err := client.ScanAll[Reading](rows,
	client.WithNameMapper(client.NameMapperSnake()),
)
```

`Get` and `Select` do not accept `ScanOption` values. When custom mapping or a non-default row limit is required, call `QueryContext` and use `ScanOne` or `ScanAll` directly.

### Materializing helpers

`ScanRows`, `ScanAll`, and `Select` retain every returned item in memory. Their default limit is 1000 rows. Exceeding the limit returns `ErrScanTooManyRows`.

| Function | Behavior |
| --- | --- |
| `ScanStruct(rows, &dest, opts...)` | Scans the current row; does not call `Next`. |
| `ScanRow(rows, &dest, opts...)` | Advances once and scans into `dest`; returns `sql.ErrNoRows` when empty. |
| `ScanRows(rows, &slice, opts...)` | Scans all remaining rows into a caller-owned slice. |
| `ScanOne[T](rows, opts...)` | Scans the next row into a new `T`; returns `sql.ErrNoRows` when empty. |
| `ScanAll[T](rows, opts...)` | Scans all remaining rows into a new `[]T`. |
| `Get[T](ctx, q, query, args...)` | Executes a query and scans its first row into `T`. |
| `Select[T](ctx, q, query, args...)` | Executes a query and scans all rows into `[]T`. |

`q` may be `*sql.DB`, `*sql.Conn`, or `*sql.Tx` because each implements `client.Queryer`.

### Streaming helpers

Use a streaming helper for result sets that are unbounded or may exceed the materialization limit.

| Function | Behavior |
| --- | --- |
| `ScanEach[T](rows, fn, opts...)` | Calls `fn` once per row. A non-nil callback error stops iteration and is returned unchanged. |
| `NewCursor[T](rows, opts...)` | Creates an iterator. Call `Next`, read `Value`, then check `Err` after the loop. |

```go
cursor, err := client.NewCursor[TagRecord](rows)
if err != nil {
	return err
}
for cursor.Next() {
	record := cursor.Value()
	process(record)
}
if err := cursor.Err(); err != nil {
	return err
}
```

### Sentinel errors

Use `errors.Is` to classify mapping and limit errors.

| Error | Meaning |
| --- | --- |
| `ErrScanDestNotPointer` | A non-generic destination was nil or not a pointer. |
| `ErrScanNoMappedField` | The target struct had no usable mapped fields. |
| `ErrScanNoMatchedField` | A result column had no mapped struct field. |
| `ErrScanNoMatchedColumn` | A mapped struct field had no result column. |
| `ErrScanDuplicateColumn` | A result has duplicate columns, or multiple fields map to one name. |
| `ErrScanTooManyRows` | A materializing helper exceeded `WithMaxRows`. |

`sql.ErrNoRows` is returned by `ScanRow`, `ScanOne`, and `Get` when no row is available.

## Named Arguments

### NamedArgs

`NamedArgs` converts a struct, pointer to a struct, or `map[string]any` to `[]any` containing `sql.Named` values.

```go
type Condition struct {
	Name string    `db:"name"`
	From time.Time `db:"from"`
	To   time.Time `db:"to"`
}

args, err := client.NamedArgs(Condition{
	Name: "sensor-1",
	From: begin,
	To:   end,
})
if err != nil {
	return err
}

rows, err := db.QueryContext(ctx, `
	SELECT NAME, TIME, VALUE FROM EXAMPLE
	WHERE NAME = :name AND TIME BETWEEN :from AND :to`, args...)
```

Struct sources follow the same field tag, fallback tag, embedded-field, and name-mapper rules as result scanning. `WithTagKey`, `WithFallbackTagKey`, and `WithNameMapper` may be passed to `NamedArgs`.

For `map[string]any`, map keys become argument names. The generated arguments are sorted by key for deterministic ordering. A nil source, a nil pointer, or any other source type returns an error.

`NamedArgs` does not parse, inspect, or rewrite SQL. It only supplies `sql.Named` values; the server resolves `:name` placeholders.

### Server capability

Named placeholders require server parameter-name metadata. Call `SupportsNamedParameters` before issuing named queries when the target server version is not known.

```go
supported, err := client.SupportsNamedParameters(ctx, db)
if err != nil {
	return err
}
if !supported {
	// Use positional ? markers with this server.
}
```

When named arguments are used against an unsupported server, the driver returns `ErrNamedParamsUnsupported`. Check it with `errors.Is` and use positional `?` parameters as the fallback.