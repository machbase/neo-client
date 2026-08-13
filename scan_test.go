package client

import (
	"database/sql"
	"net"
	"testing"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

type unsupportedValue struct {
	A int
}

func TestScanToGenericNullTargets(t *testing.T) {
	loc := time.UTC

	var nf64 sql.Null[float64]
	if err := Scan(float32(1.25), &nf64, loc); err != nil {
		t.Fatalf("Scan(float32 -> sql.Null[float64]) error = %v", err)
	}
	if !nf64.Valid || nf64.V != 1.25 {
		t.Fatalf("Scan(float32 -> sql.Null[float64]) = %+v", nf64)
	}

	var ns sql.Null[string]
	if err := Scan("hello", &ns, loc); err != nil {
		t.Fatalf("Scan(string -> sql.Null[string]) error = %v", err)
	}
	if !ns.Valid || ns.V != "hello" {
		t.Fatalf("Scan(string -> sql.Null[string]) = %+v", ns)
	}

	now := time.Date(2026, 8, 7, 1, 2, 3, 4, time.UTC)
	var nt sql.Null[time.Time]
	if err := Scan(now, &nt, loc); err != nil {
		t.Fatalf("Scan(time.Time -> sql.Null[time.Time]) error = %v", err)
	}
	if !nt.Valid || !nt.V.Equal(now) {
		t.Fatalf("Scan(time.Time -> sql.Null[time.Time]) = %+v", nt)
	}

	var nb sql.Null[[]byte]
	if err := Scan([]byte("abc"), &nb, loc); err != nil {
		t.Fatalf("Scan([]byte -> sql.Null[[]byte]) error = %v", err)
	}
	if !nb.Valid || string(nb.V) != "abc" {
		t.Fatalf("Scan([]byte -> sql.Null[[]byte]) = %+v", nb)
	}

	var nip sql.Null[net.IP]
	ip := net.ParseIP("127.0.0.1")
	if err := Scan(ip, &nip, loc); err != nil {
		t.Fatalf("Scan(net.IP -> sql.Null[net.IP]) error = %v", err)
	}
	if !nip.Valid || !nip.V.Equal(ip) {
		t.Fatalf("Scan(net.IP -> sql.Null[net.IP]) = %+v", nip)
	}
}

func TestScanFromGenericNullSources(t *testing.T) {
	loc := time.UTC

	srcFloat := sql.Null[float64]{V: 3.5, Valid: true}
	var f64 float64
	if err := Scan(&srcFloat, &f64, loc); err != nil {
		t.Fatalf("Scan(sql.Null[float64] -> float64) error = %v", err)
	}
	if f64 != 3.5 {
		t.Fatalf("Scan(sql.Null[float64] -> float64) = %v", f64)
	}

	srcString := sql.Null[string]{V: "42", Valid: true}
	var i32 int32
	if err := Scan(&srcString, &i32, loc); err != nil {
		t.Fatalf("Scan(sql.Null[string] -> int32) error = %v", err)
	}
	if i32 != 42 {
		t.Fatalf("Scan(sql.Null[string] -> int32) = %v", i32)
	}

	srcTime := sql.Null[time.Time]{V: time.Date(2026, 8, 7, 7, 8, 9, 10, time.UTC), Valid: true}
	var ts string
	if err := Scan(&srcTime, &ts, loc); err != nil {
		t.Fatalf("Scan(sql.Null[time.Time] -> string) error = %v", err)
	}
	if ts == "" {
		t.Fatal("Scan(sql.Null[time.Time] -> string) produced empty string")
	}

	srcInt := sql.Null[int]{V: 21, Valid: true}
	var i64 int64
	if err := Scan(&srcInt, &i64, loc); err != nil {
		t.Fatalf("Scan(sql.Null[int] -> int64) error = %v", err)
	}
	if i64 != 21 {
		t.Fatalf("Scan(sql.Null[int] -> int64) = %v", i64)
	}

	srcU8 := sql.Null[uint8]{V: 8, Valid: true}
	var i32b int32
	if err := Scan(&srcU8, &i32b, loc); err != nil {
		t.Fatalf("Scan(sql.Null[uint8] -> int32) error = %v", err)
	}
	if i32b != 8 {
		t.Fatalf("Scan(sql.Null[uint8] -> int32) = %v", i32b)
	}

	srcByte := sql.NullByte{Byte: 9, Valid: true}
	var i16 int16
	if err := Scan(&srcByte, &i16, loc); err != nil {
		t.Fatalf("Scan(sql.NullByte -> int16) error = %v", err)
	}
	if i16 != 9 {
		t.Fatalf("Scan(sql.NullByte -> int16) = %v", i16)
	}

	srcBool := sql.NullBool{Bool: true, Valid: true}
	var b bool
	if err := Scan(&srcBool, &b, loc); err != nil {
		t.Fatalf("Scan(sql.NullBool -> bool) error = %v", err)
	}
	if !b {
		t.Fatalf("Scan(sql.NullBool -> bool) = %v", b)
	}

	srcGBool := sql.Null[bool]{V: false, Valid: true}
	var bs string
	if err := Scan(&srcGBool, &bs, loc); err != nil {
		t.Fatalf("Scan(sql.Null[bool] -> string) error = %v", err)
	}
	if bs != "false" {
		t.Fatalf("Scan(sql.Null[bool] -> string) = %q", bs)
	}
}

func TestScanFromRawBytesSources(t *testing.T) {
	loc := time.UTC

	raw := sql.RawBytes("abc")

	var s string
	if err := Scan(raw, &s, loc); err != nil {
		t.Fatalf("Scan(sql.RawBytes -> string) error = %v", err)
	}
	if s != "abc" {
		t.Fatalf("Scan(sql.RawBytes -> string) = %q", s)
	}

	var b []byte
	if err := Scan(raw, &b, loc); err != nil {
		t.Fatalf("Scan(sql.RawBytes -> []byte) error = %v", err)
	}
	if string(b) != "abc" {
		t.Fatalf("Scan(sql.RawBytes -> []byte) = %q", string(b))
	}

	var nb sql.Null[[]byte]
	if err := Scan(raw, &nb, loc); err != nil {
		t.Fatalf("Scan(sql.RawBytes -> sql.Null[[]byte]) error = %v", err)
	}
	if !nb.Valid || string(nb.V) != "abc" {
		t.Fatalf("Scan(sql.RawBytes -> sql.Null[[]byte]) = %+v", nb)
	}

	rawPtr := sql.RawBytes("xyz")
	if err := Scan(&rawPtr, &s, loc); err != nil {
		t.Fatalf("Scan(*sql.RawBytes -> string) error = %v", err)
	}
	if s != "xyz" {
		t.Fatalf("Scan(*sql.RawBytes -> string) = %q", s)
	}
}

func TestNormalizeValue(t *testing.T) {
	now := time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC)

	if got := NormalizeType(&sql.NullString{String: "ok", Valid: true}, time.UTC); got != "ok" {
		t.Fatalf("NormalizeValue(null string) = %#v", got)
	}
	if got := NormalizeType(&sql.NullString{}, time.UTC); got != nil {
		t.Fatalf("NormalizeValue(invalid null string) = %#v", got)
	}
	if got, ok := NormalizeType(&sql.NullTime{Time: now, Valid: true}, nil).(time.Time); !ok || !got.Equal(now) {
		t.Fatalf("NormalizeValue(null time) = %#v", got)
	}
	if got, ok := NormalizeType(unsupportedValue{A: 1}, time.UTC).(unsupportedValue); !ok || got.A != 1 {
		t.Fatalf("NormalizeValue(fallback) = %#v", got)
	}
}

func TestNormalizeValues(t *testing.T) {
	vals := []any{
		sql.RawBytes("raw"),
		[]byte("bin"),
		&sql.NullInt64{Int64: 7, Valid: true},
		&sql.NullString{},
	}
	out := NormalizeTypes(vals, time.UTC)

	if got, ok := out[0].([]byte); !ok || string(got) != "raw" {
		t.Fatalf("NormalizeValues(raw) = %#v", out[0])
	}
	if got, ok := out[1].([]byte); !ok || string(got) != "bin" {
		t.Fatalf("NormalizeValues(bytes) = %#v", out[1])
	}
	if got, ok := out[2].(int64); !ok || got != 7 {
		t.Fatalf("NormalizeValues(int64) = %#v", out[2])
	}
	if out[3] != nil {
		t.Fatalf("NormalizeValues(invalid null) = %#v", out[3])
	}
}

func TestUnboxGenericNullWrappers(t *testing.T) {
	nBool := sql.Null[bool]{V: true, Valid: true}
	if got := Unbox(&nBool); got != true {
		t.Fatalf("Unbox(sql.Null[bool]) = %#v", got)
	}

	nInt := sql.Null[int]{V: 11, Valid: true}
	if got := Unbox(&nInt); got != 11 {
		t.Fatalf("Unbox(sql.Null[int]) = %#v", got)
	}

	nI16 := sql.Null[int16]{V: 12, Valid: true}
	if got := Unbox(&nI16); got != int16(12) {
		t.Fatalf("Unbox(sql.Null[int16]) = %#v", got)
	}

	nI32 := sql.Null[int32]{V: 13, Valid: true}
	if got := Unbox(&nI32); got != int32(13) {
		t.Fatalf("Unbox(sql.Null[int32]) = %#v", got)
	}

	nI64 := sql.Null[int64]{V: 14, Valid: true}
	if got := Unbox(&nI64); got != int64(14) {
		t.Fatalf("Unbox(sql.Null[int64]) = %#v", got)
	}

	nStr := sql.Null[string]{V: "ok", Valid: true}
	if got := Unbox(&nStr); got != "ok" {
		t.Fatalf("Unbox(sql.Null[string]) = %#v", got)
	}

	nTime := sql.Null[time.Time]{V: time.Date(2026, 8, 10, 1, 2, 3, 4, time.UTC), Valid: true}
	if got, ok := Unbox(&nTime).(time.Time); !ok || !got.Equal(nTime.V) {
		t.Fatalf("Unbox(sql.Null[time.Time]) = %#v", got)
	}

	dec, err := api.ParseDecimal("12.34", 10, 2)
	if err != nil {
		t.Fatalf("ParseDecimal error = %v", err)
	}
	nDecimal := sql.Null[api.Decimal]{V: dec, Valid: true}
	if got, ok := Unbox(&nDecimal).(api.Decimal); !ok || got.String() != "12.34" {
		t.Fatalf("Unbox(sql.Null[api.Decimal]) = %#v", got)
	}

	nInvalid := sql.Null[int]{V: 99, Valid: false}
	if got := Unbox(&nInvalid); got != nil {
		t.Fatalf("Unbox(invalid sql.Null[int]) = %#v", got)
	}
}

func TestScanNull(t *testing.T) {
	t.Run("unsigned and generic sql null wrappers", func(t *testing.T) {
		nBool := sql.Null[bool]{V: true, Valid: true}
		if !ScanNull(&nBool) {
			t.Fatal("ScanNull(sql.Null[bool]) = false")
		}
		if nBool.Valid || nBool.V {
			t.Fatalf("ScanNull(sql.Null[bool]) = %+v", nBool)
		}

		nByte := sql.NullByte{Byte: 7, Valid: true}
		if !ScanNull(&nByte) {
			t.Fatal("ScanNull(sql.NullByte) = false")
		}
		if nByte.Valid || nByte.Byte != 0 {
			t.Fatalf("ScanNull(sql.NullByte) = %+v", nByte)
		}

		nU8 := sql.Null[uint8]{V: 7, Valid: true}
		if !ScanNull(&nU8) {
			t.Fatal("ScanNull(sql.Null[uint8]) = false")
		}
		if nU8.Valid || nU8.V != 0 {
			t.Fatalf("ScanNull(sql.Null[uint8]) = %+v", nU8)
		}

		nU16 := sql.Null[uint16]{V: 16, Valid: true}
		if !ScanNull(&nU16) {
			t.Fatal("ScanNull(sql.Null[uint16]) = false")
		}
		if nU16.Valid || nU16.V != 0 {
			t.Fatalf("ScanNull(sql.Null[uint16]) = %+v", nU16)
		}

		nU32 := sql.Null[uint32]{V: 32, Valid: true}
		if !ScanNull(&nU32) {
			t.Fatal("ScanNull(sql.Null[uint32]) = false")
		}
		if nU32.Valid || nU32.V != 0 {
			t.Fatalf("ScanNull(sql.Null[uint32]) = %+v", nU32)
		}

		nU64 := sql.Null[uint64]{V: 64, Valid: true}
		if !ScanNull(&nU64) {
			t.Fatal("ScanNull(sql.Null[uint64]) = false")
		}
		if nU64.Valid || nU64.V != 0 {
			t.Fatalf("ScanNull(sql.Null[uint64]) = %+v", nU64)
		}

		nAny := sql.Null[any]{V: "x", Valid: true}
		if !ScanNull(&nAny) {
			t.Fatal("ScanNull(sql.Null[any]) = false")
		}
		if nAny.Valid || nAny.V != nil {
			t.Fatalf("ScanNull(sql.Null[any]) = %+v", nAny)
		}
	})

	t.Run("unsupported destination", func(t *testing.T) {
		var plain int
		if ScanNull(&plain) {
			t.Fatal("ScanNull(*int) should be false")
		}
	})
}
