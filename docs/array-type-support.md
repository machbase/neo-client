# ARRAY type and sparse append

Client-server protocol 4.0.4 adds fixed-cardinality numeric ARRAY values. The client supports
INT16, UINT16, INT32, UINT32, INT64, UINT64, FLOAT, DOUBLE and DECIMAL arrays.
ARRAY positions in the public API are 0-based.

## Dense and sparse values

Use `api.NewArray` when most elements have values. Elements are normalized to
the element type's canonical Go type, so plain untyped literals work without
explicit casting:

```go
value, err := api.NewArray(api.SqlTypeInt32,
    10, nil, nil, 40)
```

Use `api.NewSparseArray` when only a few positions have values:

```go
value, err := api.NewSparseArray(api.SqlTypeInt32, 1024)
if err != nil {
    return err
}
value.Set(0, 10)
value.Set(1023, 40)
```

A nil `*api.Array` is a whole-ARRAY NULL. An empty sparse Array is a non-NULL
ARRAY whose elements are all NULL.

`api.Array` can be used as a prepared parameter. Direct Machbase rows can scan
the native value into `api.Array`; the standard `database/sql` result boundary
exposes the canonical string representation. Scanning that string into a
zero-value `api.Array` infers `INT64`, `UINT64`, exact `DECIMAL`, or `DOUBLE`
from its numeric spelling. Preconfigure the receiver with
`NewSparseArrayWithMeta` when the original FLOAT/DOUBLE/DECIMAL type metadata
must be preserved exactly; NaN and positive/negative Infinity are supported.

Integer min/max values reserved by Machbase as scalar NULL sentinels are not
valid non-NULL ARRAY elements. The client rejects those values for both whole
ARRAY input and indexed append targets. A typed nil pointer passed to
`Array.Set` is normalized to an element NULL.

More generally, `Array.Set` (and therefore `NewArray`) accepts any compatible
numeric or numeric-string primitive, not just literals matching the element
type exactly. A value that is out of range or cannot be converted to the
element type is rejected immediately by `Set`/`NewArray` instead of surfacing
later at append time. `Array.Get` always returns the canonical type,
regardless of the type originally passed to `Set`.

## Append with varying positions

Open the whole ARRAY column and pass a sparse value when positions vary by row:

```go
appender := &client.Appender{}
err := appender.Connect(ctx, dsn, "SENSOR", "ID", "VALUES_ARRAY")
err = appender.Append(int64(1), value)
```

The client chooses the smaller dense or sparse input representation. The
database always stores the canonical fixed-cardinality ARRAY representation.

`Append`'s per-column encoding already tolerates plain untyped literals (`int`)
for scalar and projected ARRAY element columns, and treats a `nil` argument as
NULL for that column, so `int64(1)` and `int32(...)` casts are not required.

## Append with fixed positions

When every row supplies the same positions, use element projection. This avoids
sending the remaining ARRAY slots:

```go
appender := &client.Appender{}
err := appender.Connect(
    ctx,
    dsn,
    "SENSOR",
    "ID",
    "VALUES_ARRAY[0]",
    "VALUES_ARRAY[1023]",
)
err = appender.Append(int64(1), int32(10), int32(40))
// equivalent without casts; a nil argument marks that element position NULL
err = appender.Append(1, 10, nil)
```

Omitting an ARRAY column produces a whole-ARRAY NULL. Opening at least one
element target produces a non-NULL ARRAY; unlisted positions are element NULLs.

## String ARRAY literals bound through `named()`/`SQL()`

`api.ParseArray` parses the sparse-aware ARRAY literal convenience syntax used
when a query parameter is a plain string, e.g. tql's `named("arr", "[1,2,3,4]")`:

```
[1,2,3,4]              // dense: position 0..3
[1=>1.0, 2=>2.1]        // sparse: idx=>value pairs, other positions are NULL
[1,2,null,4]            // dense with an explicit NULL element
```

Dense and sparse elements cannot be mixed in one literal; `ParseArray` rejects
that input. Cardinality is inferred from the highest referenced position when
the caller does not supply an explicit cardinality (e.g. from prepared
statement parameter metadata). This convenience syntax is only used for
string-typed bind parameters; it is unrelated to the canonical text format
produced/consumed by `Array.String()`/`Array.Scan()`.

## Column type and value representation of a `SELECT` result

`sql.ColumnType.DatabaseTypeName()` for an ARRAY column is one of
`INT16_ARRAY`, `UINT16_ARRAY`, `INT32_ARRAY`, `UINT32_ARRAY`, `INT64_ARRAY`,
`UINT64_ARRAY`, `FLOAT_ARRAY`, `DOUBLE_ARRAY`, `DECIMAL_ARRAY`; the
corresponding `api.DataType` (used by `neo-client.NewColumnWithType` and
`neo-server/spi.ColumnTypesToDataTypes`) is one of `int16_array`,
`uint16_array`, `int32_array`, `uint32_array`, `int64_array`, `uint64_array`,
`float_array`, `double_array`, `decimal_array`. A DECIMAL scalar column reports
`DECIMAL`/`decimal`.

When scanning a `SELECT` result row (e.g. `spi.MakeBuffer` + `client.Unbox`,
which is what tql's `SQL()` source uses), an ARRAY column value is unboxed into
a plain `[]any` of per-position values (`nil` for an element NULL), so a codec
like `JSON()` renders it as a real JSON array, e.g. `[1,2,3,4]` or
`[null,2,null,4]`. A whole-ARRAY NULL is reported as JSON `null`, not `[]`.
`DECIMAL` elements inside an ARRAY, and scalar DECIMAL column values, are kept
as their exact string representation (e.g. `"12.34"`) since `api.Decimal` has
unexported fields and would otherwise marshal to `{}`.

`SELECT` of a bare ARRAY literal constant without `FROM` (e.g.
`SELECT [1,2,3]`, `SELECT [1.1,2.2,3.3]`) is also supported by the engine and
reports the inferred element array type (`int32_array`, `double_array`, ...)
the same way as an ARRAY column.

## Compatibility

- ARRAY positions changed from 1-based to 0-based. Update `Set`, `Get`,
  `Entries`, and indexed append target callers before upgrading.
- Existing Appender calls and full-row ordering are unchanged.
- Existing `Connect`-then-`WithInputColumns` and explicit `_ARRIVAL_TIME`
  patterns remain supported. `Append` and `AppendLogTime` may be mixed in one
  projected LOG appender.
- Protocol 4.0.4 peers use ARRAY metadata, sparse values and element projection.
- Protocol 4.0.3 peers retain existing scalar append behavior.
- ARRAY values and indexed targets are rejected before sending to a protocol 4.0.3
  server.
