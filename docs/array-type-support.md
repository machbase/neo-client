# ARRAY type and sparse append

CMI 4.0.4 adds fixed-cardinality numeric ARRAY values. The client supports
INT16, UINT16, INT32, UINT32, INT64, UINT64, FLOAT, DOUBLE and DECIMAL arrays.
ARRAY positions in the public API are 1-based.

## Dense and sparse values

Use `api.NewArray` when most elements have values:

```go
value, err := api.NewArray(api.SqlTypeInt32,
    []any{int32(10), nil, nil, int32(40)})
```

Use `api.NewSparseArray` when only a few positions have values:

```go
value, err := api.NewSparseArray(api.SqlTypeInt32, 1024)
if err != nil {
    return err
}
value.Set(1, int32(10))
value.Set(1024, int32(40))
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

## Append with varying positions

Open the whole ARRAY column and pass a sparse value when positions vary by row:

```go
appender := &client.Appender{}
err := appender.Connect(ctx, dsn, "SENSOR", "ID", "VALUES_ARRAY")
err = appender.Append(int64(1), value)
```

The client chooses the smaller dense or sparse CMI input representation. The
database always stores the canonical fixed-cardinality ARRAY representation.

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
    "VALUES_ARRAY[1]",
    "VALUES_ARRAY[1024]",
)
err = appender.Append(int64(1), int32(10), int32(40))
```

Omitting an ARRAY column produces a whole-ARRAY NULL. Opening at least one
element target produces a non-NULL ARRAY; unlisted positions are element NULLs.

## Compatibility

- Existing Appender calls and full-row ordering are unchanged.
- Existing `Connect`-then-`WithInputColumns` and explicit `_ARRIVAL_TIME`
  patterns remain supported. `Append` and `AppendLogTime` may be mixed in one
  projected LOG appender.
- CMI 4.0.4 peers use ARRAY metadata, sparse values and element projection.
- CMI 4.0.3 peers retain existing scalar append behavior.
- ARRAY values and indexed targets are rejected before sending to a CMI 4.0.3
  server.
