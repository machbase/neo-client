package api

import (
	"database/sql"
	"net"
	"testing"
	"time"
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
