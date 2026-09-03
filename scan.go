package client

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"math"
	"net"
	"reflect"
	"strconv"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

/**
# scan.go Type Mapping Matrix

This document summarizes the type-handling matrix in scan.go for:
- Scan(src, dst, loc)
- ScanNull(dst)
- Unbox(val)

## Legend

- Scan src: type is accepted as source in Scan.
- ScanNull dst: type is reset to NULL state in ScanNull.
- Unbox: type is unwrapped to raw value or nil in Unbox.
- Y: supported
- N: not supported

## Null Wrapper Matrix

| Type                 | Scan src | ScanNull dst | Unbox | Notes                                   |
| -------------------- | -------- | ------------ | ----- | --------------------------------------- |
| *sql.NullBool        | Y        | Y            | Y     | Routed to scanBool                      |
| *sql.Null[bool]      | Y        | Y            | Y     | Routed to scanBool                      |
| *sql.NullByte        | Y        | Y            | Y     | Routed to scanInt16                     |
| *sql.Null[uint8]     | Y        | Y            | Y     | Routed to scanInt16                     |
| *sql.Null[int]       | Y        | Y            | Y     | Routed to scanInt64                     |
| *sql.NullInt16       | Y        | Y            | Y     |                                         |
| *sql.Null[int16]     | Y        | Y            | Y     |                                         |
| *sql.Null[uint16]    | Y        | Y            | Y     |                                         |
| *sql.NullInt32       | Y        | Y            | Y     |                                         |
| *sql.Null[int32]     | Y        | Y            | Y     |                                         |
| *sql.Null[uint32]    | Y        | Y            | Y     |                                         |
| *sql.NullInt64       | Y        | Y            | Y     |                                         |
| *sql.Null[int64]     | Y        | Y            | Y     |                                         |
| *sql.Null[uint64]    | Y        | Y            | Y     |                                         |
| *sql.NullFloat64     | Y        | Y            | Y     |                                         |
| *sql.Null[float32]   | Y        | Y            | Y     |                                         |
| *sql.Null[float64]   | Y        | Y            | Y     |                                         |
| *sql.NullString      | Y        | Y            | Y     |                                         |
| *sql.Null[string]    | Y        | Y            | Y     |                                         |
| *sql.NullTime        | Y        | Y            | Y     |                                         |
| *sql.Null[time.Time] | Y        | Y            | Y     |                                         |
| *sql.Null[net.IP]    | Y        | Y            | Y     |                                         |
| *sql.Null[[]byte]    | Y        | Y            | Y     |                                         |
| *sql.Null[JSONString] | Y       | Y            | Y     |                                         |
| *sql.Null[Decimal]    | Y       | Y            | Y     | Source via recursive Scan(sv.V, dst, loc) |
| *sql.Null[any]       | N        | Y            | Y     | Reset V=nil, Valid=false                |

## Other NULL Reset Targets in ScanNull

| Type          | ScanNull dst | Notes        |
| ------------- | ------------ | ------------ |
| *[]byte       | Y            | Reset to nil |
| *driver.Value | Y            | Reset to nil |

## Non-Wrapper Source Types in Scan

| Source type accepted by Scan                 | Notes                          |
| -------------------------------------------- | ------------------------------ |
| int, *int, uint, *uint                       | Routed to scanInt32            |
| bool, *bool                                  | Routed to scanBool             |
| int16, *int16, uint16, *uint16               | Routed to scanInt16            |
| int32, *int32, uint32, *uint32               | Routed to scanInt32            |
| int64, *int64, uint64, *uint64               | Routed to scanInt64            |
| float32, *float32, float64, *float64         | Routed to scanFloat32/scanFloat64 |
| string, *string, JSONString, *JSONString     | Routed to scanString           |
| time.Time, *time.Time                        | Routed to scanDatetime         |
| []byte, *[]byte, sql.RawBytes, *sql.RawBytes | Routed to scanBytes            |
| net.IP, *net.IP                              | Routed to scanIP               |
| Decimal, *Decimal                            | Decimal-specific path          |

## Pointer Destinations (NULL as nil)

Any destination whose type is a pointer to a supported destination type is handled
generically, so a NULL column can be represented as a nil pointer.

| Destination                                          | Scan dst | ScanNull dst | Notes                                    |
| ---------------------------------------------------- | -------- | ------------ | ---------------------------------------- |
| **bool                                               | Y        | Y            | Allocated on value, reset to nil on NULL |
| **int, **int16/32/64, **uint, **uint16/32/64         | Y        | Y            |                                          |
| **float32, **float64                                 | Y        | Y            |                                          |
| **string, **JSONString                               | Y        | Y            |                                          |
| **time.Time                                          | Y        | Y            |                                          |
| **net.IP                                             | Y        | Y            |                                          |
| **Decimal                                            | Y        | Y            |                                          |

Implemented by scanIndirect in each scanXxx default branch and by the reflect-based
default branch of ScanNull. *[]byte already carries NULL as nil and is unchanged.

## Review Summary

After adding missing Scan src wrappers (excluding sql.Null[any]), ScanNull reset cases, and generic unboxing gaps, practical coverage is aligned for the listed wrappers.

One intentional asymmetry remains in Scan src:
- *sql.Null[any] is not accepted as a source type in top-level Scan switch.

**/

func Scan(src any, dst any, loc *time.Location) error {
	switch sv := src.(type) {
	case int:
		return scanInt32(int32(sv), dst)
	case *int:
		return scanInt32(int32(*sv), dst)
	case uint:
		return scanInt32(int32(sv), dst)
	case *uint:
		return scanInt32(int32(*sv), dst)
	case bool:
		return scanBool(sv, dst)
	case *bool:
		return scanBool(*sv, dst)
	case *sql.NullBool:
		if sv.Valid {
			return scanBool(sv.Bool, dst)
		}
	case *sql.Null[bool]:
		if sv.Valid {
			return scanBool(sv.V, dst)
		}
	case *sql.NullByte:
		if sv.Valid {
			return scanInt16(int16(sv.Byte), dst)
		}
	case *sql.Null[uint8]:
		if sv.Valid {
			return scanInt16(int16(sv.V), dst)
		}
	case *sql.Null[int]:
		if sv.Valid {
			return scanInt64(int64(sv.V), dst)
		}
	case int16:
		return scanInt16(sv, dst)
	case *int16:
		return scanInt16(*sv, dst)
	case uint16:
		return scanInt16(int16(sv), dst)
	case *uint16:
		return scanInt16(int16(*sv), dst)
	case *sql.NullInt16:
		if sv.Valid {
			return scanInt16(sv.Int16, dst)
		}
	case *sql.Null[int16]:
		if sv.Valid {
			return scanInt16(sv.V, dst)
		}
	case *sql.Null[uint16]:
		if sv.Valid {
			return scanInt16(int16(sv.V), dst)
		}
	case int32:
		return scanInt32(sv, dst)
	case *int32:
		return scanInt32(*sv, dst)
	case *sql.NullInt32:
		if sv.Valid {
			return scanInt32(sv.Int32, dst)
		}
	case *sql.Null[int32]:
		if sv.Valid {
			return scanInt32(sv.V, dst)
		}
	case uint32:
		return scanInt32(int32(sv), dst)
	case *uint32:
		return scanInt32(int32(*sv), dst)
	case *sql.Null[uint32]:
		if sv.Valid {
			return scanInt32(int32(sv.V), dst)
		}
	case int64:
		return scanInt64(sv, dst)
	case *int64:
		return scanInt64(*sv, dst)
	case *sql.NullInt64:
		if sv.Valid {
			return scanInt64(sv.Int64, dst)
		}
	case *sql.Null[int64]:
		if sv.Valid {
			return scanInt64(sv.V, dst)
		}
	case uint64:
		return scanInt64(int64(sv), dst)
	case *uint64:
		return scanInt64(int64(*sv), dst)
	case *sql.Null[uint64]:
		if sv.Valid {
			return scanInt64(int64(sv.V), dst)
		}
	case float64:
		return scanFloat64(sv, dst)
	case *float64:
		return scanFloat64(*sv, dst)
	case *sql.NullFloat64:
		if sv.Valid {
			return scanFloat64(sv.Float64, dst)
		}
	case *sql.Null[float64]:
		if sv.Valid {
			return scanFloat64(sv.V, dst)
		}
	case float32:
		return scanFloat32(sv, dst)
	case *float32:
		return scanFloat32(*sv, dst)
	case *sql.Null[float32]:
		if sv.Valid {
			return scanFloat32(sv.V, dst)
		}
	case string:
		return scanString(sv, dst)
	case *string:
		return scanString(*sv, dst)
	case *sql.NullString:
		if sv.Valid {
			return scanString(sv.String, dst)
		}
	case *sql.Null[string]:
		if sv.Valid {
			return scanString(sv.V, dst)
		}
	case api.JSONString:
		return scanString(string(sv), dst)
	case *api.JSONString:
		return scanString(string(*sv), dst)
	case *sql.Null[api.JSONString]:
		if sv.Valid {
			return scanString(string(sv.V), dst)
		}
	case time.Time:
		return scanDatetime(sv, dst, loc)
	case *time.Time:
		return scanDatetime(*sv, dst, loc)
	case *sql.NullTime:
		if sv.Valid {
			return scanDatetime(sv.Time, dst, loc)
		}
	case *sql.Null[time.Time]:
		if sv.Valid {
			return scanDatetime(sv.V, dst, loc)
		}
	case []byte:
		return scanBytes(sv, dst)
	case *[]byte:
		return scanBytes(*sv, dst)
	case sql.RawBytes:
		return scanBytes([]byte(sv), dst)
	case *sql.RawBytes:
		if sv != nil {
			return scanBytes([]byte(*sv), dst)
		}
	case *sql.Null[[]byte]:
		if sv.Valid {
			return scanBytes(sv.V, dst)
		}
	case net.IP:
		return scanIP(sv, dst)
	case *net.IP:
		return scanIP(*sv, dst)
	case *sql.Null[net.IP]:
		if sv.Valid {
			return scanIP(sv.V, dst)
		}
	case api.Decimal:
		switch d := dst.(type) {
		case *api.Decimal:
			*d = sv
			return nil
		case *string:
			*d = sv.String()
			return nil
		case *driver.Value:
			*d = sv.String()
			return nil
		case *sql.Null[api.Decimal]:
			d.V = sv
			d.Valid = true
			return nil
		default:
			if ok, err := scanIndirect(dst, func(d any) error { return Scan(sv, d, loc) }); ok {
				return err
			}
		}
	case *api.Decimal:
		if sv != nil {
			return Scan(*sv, dst, loc)
		}
	case *sql.Null[api.Decimal]:
		if sv.Valid {
			return Scan(sv.V, dst, loc)
		}
	case api.Array:
		switch d := dst.(type) {
		case *api.Array:
			*d = *sv.Clone()
			return nil
		case **api.Array:
			*d = sv.Clone()
			return nil
		case *string:
			*d = sv.String()
			return nil
		case *driver.Value:
			*d = sv.String()
			return nil
		}
	case *api.Array:
		if sv != nil {
			return Scan(*sv, dst, loc)
		}
	}
	return fmt.Errorf("cannot convert value from %T to %T", src, dst)
}

// scanIndirect handles pointer destinations such as **float64 by allocating a new
// value, scanning into it, and storing its address. It reports whether pDst was a
// pointer-to-pointer destination.
func scanIndirect(pDst any, scan func(dst any) error) (bool, error) {
	rv := reflect.ValueOf(pDst)
	if rv.Kind() != reflect.Pointer || rv.IsNil() {
		return false, nil
	}
	elemType := rv.Type().Elem()
	if elemType.Kind() != reflect.Pointer {
		return false, nil
	}
	nv := reflect.New(elemType.Elem())
	if err := scan(nv.Interface()); err != nil {
		return true, err
	}
	rv.Elem().Set(nv)
	return true, nil
}

func ScanNull(dst any) bool {
	switch d := dst.(type) {
	case *sql.NullBool:
		d.Bool = false
		d.Valid = false
	case *sql.Null[bool]:
		d.V = false
		d.Valid = false
	case *sql.NullByte:
		d.Byte = 0
		d.Valid = false
	case *sql.Null[uint8]:
		d.V = 0
		d.Valid = false
	case *sql.Null[int]:
		d.V = 0
		d.Valid = false
	case *sql.NullInt16:
		d.Int16 = 0
		d.Valid = false
	case *sql.Null[int16]:
		d.V = 0
		d.Valid = false
	case *sql.Null[uint16]:
		d.V = 0
		d.Valid = false
	case *sql.NullInt32:
		d.Int32 = 0
		d.Valid = false
	case *sql.Null[int32]:
		d.V = 0
		d.Valid = false
	case *sql.Null[uint32]:
		d.V = 0
		d.Valid = false
	case *sql.NullInt64:
		d.Int64 = 0
		d.Valid = false
	case *sql.Null[int64]:
		d.V = 0
		d.Valid = false
	case *sql.Null[uint64]:
		d.V = 0
		d.Valid = false
	case *sql.Null[float32]:
		d.V = 0
		d.Valid = false
	case *sql.NullFloat64:
		d.Float64 = 0
		d.Valid = false
	case *sql.Null[float64]:
		d.V = 0
		d.Valid = false
	case *sql.NullString:
		d.String = ""
		d.Valid = false
	case *sql.Null[string]:
		d.V = ""
		d.Valid = false
	case *sql.NullTime:
		d.Time = time.Time{}
		d.Valid = false
	case *sql.Null[time.Time]:
		d.V = time.Time{}
		d.Valid = false
	case *sql.Null[net.IP]:
		d.V = nil
		d.Valid = false
	case *sql.Null[[]byte]:
		d.V = nil
		d.Valid = false
	case *sql.Null[api.JSONString]:
		d.V = ""
		d.Valid = false
	case *sql.Null[api.Decimal]:
		d.V = api.Decimal{}
		d.Valid = false
	case *api.Array:
		*d = api.Array{}
	case **api.Array:
		*d = nil
	case *sql.Null[any]:
		d.V = nil
		d.Valid = false
	case *[]byte:
		*d = nil
	case *driver.Value:
		*d = nil
	default:
		rv := reflect.ValueOf(dst)
		if rv.Kind() != reflect.Pointer || rv.IsNil() || rv.Type().Elem().Kind() != reflect.Pointer {
			return false
		}
		rv.Elem().SetZero()
	}
	return true
}

func scanInt16(src int16, pDst any) error {
	if src == math.MinInt16 {
		return errors.New("scan NULL INT16")
	}
	switch dst := pDst.(type) {
	case *int:
		*dst = int(src)
	case *uint:
		*dst = uint(src)
	case *int16:
		*dst = int16(src)
	case *uint16:
		*dst = uint16(src)
	case *int32:
		*dst = int32(src)
	case *uint32:
		*dst = uint32(src)
	case *int64:
		*dst = int64(src)
	case *uint64:
		*dst = uint64(src)
	case *string:
		*dst = strconv.Itoa(int(src))
	case *sql.NullInt16:
		dst.Valid = true
		dst.Int16 = src
	case *sql.Null[int]:
		dst.Valid = true
		dst.V = int(src)
	case *sql.Null[int16]:
		dst.Valid = true
		dst.V = src
	case *sql.Null[int32]:
		dst.Valid = true
		dst.V = int32(src)
	case *sql.Null[int64]:
		dst.Valid = true
		dst.V = int64(src)
	case *sql.NullInt32:
		dst.Valid = true
		dst.Int32 = int32(src)
	case *sql.NullInt64:
		dst.Valid = true
		dst.Int64 = int64(src)
	case *sql.Null[uint16]:
		dst.Valid = true
		dst.V = uint16(src)
	case *sql.Null[uint32]:
		dst.Valid = true
		dst.V = uint32(src)
	case *sql.Null[uint64]:
		dst.Valid = true
		dst.V = uint64(src)
	case *driver.Value:
		*dst = driver.Value(src)
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanInt16(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from INT16 to %T not supported", pDst)
	}
	return nil
}

func scanInt32(src int32, pDst any) error {
	if src == math.MinInt32 {
		return errors.New("scan NULL INT32")
	}
	switch dst := pDst.(type) {
	case *int:
		*dst = int(src)
	case *uint:
		*dst = uint(src)
	case *int16:
		*dst = int16(src)
	case *uint16:
		*dst = uint16(src)
	case *int32:
		*dst = int32(src)
	case *uint32:
		*dst = uint32(src)
	case *int64:
		*dst = int64(src)
	case *uint64:
		*dst = uint64(src)
	case *string:
		*dst = strconv.FormatInt(int64(src), 10)
	case *TableType:
		*dst = TableType(src)
	case *TableFlag:
		*dst = TableFlag(src)
	case *api.ColumnType:
		*dst = api.ColumnType(src)
	case *api.ColumnFlag:
		*dst = api.ColumnFlag(src)
	case *sql.NullInt32:
		dst.Valid = true
		dst.Int32 = src
	case *sql.Null[int]:
		dst.Valid = true
		dst.V = int(src)
	case *sql.Null[int32]:
		dst.Valid = true
		dst.V = src
	case *sql.Null[int64]:
		dst.Valid = true
		dst.V = int64(src)
	case *sql.NullInt64:
		dst.Valid = true
		dst.Int64 = int64(src)
	case *sql.Null[uint32]:
		dst.Valid = true
		dst.V = uint32(src)
	case *sql.Null[uint64]:
		dst.Valid = true
		dst.V = uint64(src)
	case *driver.Value:
		*dst = driver.Value(src)
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanInt32(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from INT32 to %T not supported", pDst)
	}
	return nil
}

func scanInt64(src int64, pDst any) error {
	if src == math.MinInt64 {
		return errors.New("scan NULL INT64")
	}
	switch dst := pDst.(type) {
	case *int:
		*dst = int(src)
	case *uint:
		*dst = uint(src)
	case *int16:
		*dst = int16(src)
	case *uint16:
		*dst = uint16(src)
	case *int32:
		*dst = int32(src)
	case *uint32:
		*dst = uint32(src)
	case *int64:
		*dst = int64(src)
	case *uint64:
		*dst = uint64(src)
	case *string:
		*dst = strconv.FormatInt(src, 10)
	case *time.Time:
		*dst = time.Unix(0, src)
	case *TableType:
		*dst = TableType(src)
	case *TableFlag:
		*dst = TableFlag(src)
	case *api.ColumnType:
		*dst = api.ColumnType(src)
	case *api.ColumnFlag:
		*dst = api.ColumnFlag(src)
	case *sql.NullInt64:
		dst.Valid = true
		dst.Int64 = src
	case *sql.Null[int]:
		dst.Valid = true
		dst.V = int(src)
	case *sql.Null[int64]:
		dst.Valid = true
		dst.V = src
	case *sql.Null[uint64]:
		dst.Valid = true
		dst.V = uint64(src)
	case *driver.Value:
		*dst = driver.Value(src)
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanInt64(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from INT64 to %T not supported", pDst)
	}
	return nil
}

func scanDatetime(src time.Time, pDst any, loc *time.Location) error {
	switch dst := pDst.(type) {
	case *int64:
		*dst = src.UnixNano()
	case *time.Time:
		*dst = src.In(loc)
	case *string:
		*dst = src.In(loc).Format(time.RFC3339)
	case *sql.NullTime:
		dst.Valid = true
		dst.Time = src
	case *sql.Null[time.Time]:
		dst.Valid = true
		dst.V = src
	case *driver.Value:
		*dst = driver.Value(src)
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanDatetime(src, d, loc) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from DATETIME to %T not supported", pDst)
	}
	return nil
}

func scanFloat32(src float32, pDst any) error {
	switch dst := pDst.(type) {
	case *float32:
		*dst = src
	case *float64:
		*dst = float64(src)
	case *string:
		*dst = strconv.FormatFloat(float64(src), 'f', -1, 32)
	case *sql.NullFloat64:
		dst.Valid = true
		dst.Float64 = float64(src)
	case *sql.Null[float32]:
		dst.Valid = true
		dst.V = src
	case *sql.Null[float64]:
		dst.Valid = true
		dst.V = float64(src)
	case *driver.Value:
		*dst = driver.Value(src)
	case *int:
		*dst = int(src)
	case *int32:
		*dst = int32(src)
	case *int64:
		*dst = int64(src)
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanFloat32(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from FLOAT32 to %T not supported", pDst)
	}
	return nil
}

func scanFloat64(src float64, pDst any) error {
	switch dst := pDst.(type) {
	case *float32:
		*dst = float32(src)
	case *float64:
		*dst = src
	case *string:
		*dst = strconv.FormatFloat(src, 'f', -1, 64)
	case *sql.NullFloat64:
		dst.Valid = true
		dst.Float64 = src
	case *sql.Null[float32]:
		dst.Valid = true
		dst.V = float32(src)
	case *sql.Null[float64]:
		dst.Valid = true
		dst.V = src
	case *driver.Value:
		*dst = driver.Value(src)
	case *int:
		*dst = int(src)
	case *int32:
		*dst = int32(src)
	case *int64:
		*dst = int64(src)
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanFloat64(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from FLOAT64 to %T not supported", pDst)
	}
	return nil
}

func scanString(src string, pDst any) error {
	switch dst := pDst.(type) {
	case *string:
		*dst = src
	case *[]uint8:
		*dst = []uint8(src)
	case *int:
		if i, err := strconv.ParseInt(src, 10, 32); err != nil {
			return err
		} else {
			*dst = int(i)
		}
	case *int32:
		if i, err := strconv.ParseInt(src, 10, 32); err != nil {
			return err
		} else {
			*dst = int32(i)
		}
	case *int64:
		if i, err := strconv.ParseInt(src, 10, 64); err != nil {
			return err
		} else {
			*dst = i
		}
	case *net.IP:
		if src == "" {
			return errors.New("scan NULL STRING")
		}
		*dst = net.ParseIP(src)
	case *sql.NullString:
		dst.Valid = true
		dst.String = src
	case *sql.Null[string]:
		dst.Valid = true
		dst.V = src
	case *driver.Value:
		*dst = driver.Value(src)
	case *api.JSONString:
		*dst = api.JSONString(src)
	case *sql.Null[api.JSONString]:
		dst.Valid = true
		dst.V = api.JSONString(src)
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanString(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from STRING to %T not supported", pDst)
	}
	return nil
}

func scanBytes(src []byte, pDst any) error {
	switch dst := pDst.(type) {
	case *[]uint8:
		*dst = src
	case *string:
		*dst = string(src)
	case *driver.Value:
		*dst = driver.Value(src)
	case *sql.Null[[]byte]:
		dst.Valid = true
		dst.V = src
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanBytes(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from BYTES to %T not supported", pDst)
	}
	return nil
}

func scanIP(src net.IP, pDst any) error {
	switch dst := pDst.(type) {
	case *net.IP:
		*dst = src
	case *string:
		*dst = src.String()
	case *driver.Value:
		*dst = driver.Value(src)
	case *sql.Null[net.IP]:
		dst.Valid = true
		dst.V = src
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanIP(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from IPv4 to %T not supported", pDst)
	}
	return nil
}

func scanBool(src bool, pDst any) error {
	switch dst := pDst.(type) {
	case *bool:
		*dst = src
	case *string:
		*dst = strconv.FormatBool(src)
	case *sql.NullBool:
		dst.Valid = true
		dst.Bool = src
	case *sql.Null[bool]:
		dst.Valid = true
		dst.V = src
	case *driver.Value:
		*dst = driver.Value(src)
	default:
		if ok, err := scanIndirect(pDst, func(d any) error { return scanBool(src, d) }); ok {
			return err
		}
		return fmt.Errorf("scan convert from BOOL to %T not supported", pDst)
	}
	return nil
}

func Unbox(val any) any {
	switch v := val.(type) {
	case *int:
		return *v
	case *uint:
		return *v
	case *int8:
		return *v
	case *uint8:
		return *v
	case *int16:
		return *v
	case *uint16:
		return *v
	case *int32:
		return *v
	case *uint32:
		return *v
	case *int64:
		return *v
	case *uint64:
		return *v
	case *float32:
		return *v
	case *float64:
		return *v
	case *string:
		return *v
	case *api.JSONString:
		return *v
	case *time.Time:
		return *v
	case *bool:
		return *v
	case *[]byte:
		return *v
	case *net.IP:
		return *v
	case *driver.Value:
		return *v
	case *sql.NullBool:
		if v.Valid {
			return v.Bool
		} else {
			return nil
		}
	case *sql.Null[bool]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.NullByte:
		if v.Valid {
			return v.Byte
		} else {
			return nil
		}
	case *sql.Null[uint8]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[int]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.NullInt16:
		if v.Valid {
			return v.Int16
		} else {
			return nil
		}
	case *sql.Null[int16]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[uint16]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.NullInt32:
		if v.Valid {
			return v.Int32
		} else {
			return nil
		}
	case *sql.Null[int32]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[uint32]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.NullInt64:
		if v.Valid {
			return v.Int64
		} else {
			return nil
		}
	case *sql.Null[int64]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[uint64]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.NullFloat64:
		if v.Valid {
			return v.Float64
		} else {
			return nil
		}
	case *sql.NullString:
		if v.Valid {
			return v.String
		} else {
			return nil
		}
	case *sql.Null[string]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.NullTime:
		if v.Valid {
			return v.Time
		} else {
			return nil
		}
	case *sql.Null[time.Time]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[net.IP]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[[]byte]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[float32]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[float64]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[api.JSONString]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[api.Decimal]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	case *sql.Null[any]:
		if v.Valid {
			return v.V
		} else {
			return nil
		}
	default:
		return val
	}
}

func NormalizeType(val any, loc *time.Location) any {
	raw := Unbox(val)
	if raw == nil {
		return nil
	}
	if loc == nil {
		loc = time.UTC
	}
	var dv driver.Value
	if err := Scan(raw, &dv, loc); err == nil {
		return dv
	}
	return raw
}

func NormalizeTypes(values []any, loc *time.Location) []any {
	for i, val := range values {
		values[i] = NormalizeType(val, loc)
	}
	return values
}
