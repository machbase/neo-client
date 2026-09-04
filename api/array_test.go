package api

import (
	"strings"
	"testing"
)

func TestSparseArraySemantics(t *testing.T) {
	value, err := NewSparseArray(SqlTypeInt32, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Set(0, int32(10)); err != nil {
		t.Fatal(err)
	}
	if err := value.Set(5, int32(60)); err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "[10,null,null,null,null,60,null,null]" {
		t.Fatalf("String()=%s", got)
	}
	if got, err := value.Get(5); err != nil || got != int32(60) {
		t.Fatalf("Get(5)=(%v,%v)", got, err)
	}
	if got, err := value.Get(1); err != nil || got != nil {
		t.Fatalf("Get(1)=(%v,%v)", got, err)
	}
	value.Clear()
	if got := value.String(); got != "[null,null,null,null,null,null,null,null]" {
		t.Fatalf("Clear()=%s", got)
	}
}

func TestSparseArrayTypedNilElementIsNull(t *testing.T) {
	value, err := NewSparseArray(SqlTypeDecimal, 3)
	if err != nil {
		t.Fatal(err)
	}
	var decimal *Decimal
	if err := value.Set(1, decimal); err != nil {
		t.Fatal(err)
	}
	if len(value.Entries()) != 0 || value.String() != "[null,null,null]" {
		t.Fatalf("typed nil was not normalized to element NULL: %s", value)
	}
}

func TestArrayValidationAndScan(t *testing.T) {
	if _, err := NewSparseArray(SqlTypeString, 3); err == nil {
		t.Fatal("string ARRAY accepted")
	}
	if _, err := NewSparseArray(SqlTypeInt32, 0); err == nil {
		t.Fatal("zero cardinality accepted")
	}
	value, err := NewSparseArray(SqlTypeInt32, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Set(-1, 1); err == nil {
		t.Fatal("negative position accepted")
	}
	if err := value.Set(3, 1); err == nil {
		t.Fatal("position N accepted")
	}
	var scanned Array
	if err := scanned.Scan("[1,null,3]"); err != nil {
		t.Fatal(err)
	}
	if got := scanned.String(); got != "[1,null,3]" {
		t.Fatalf("scanned=%s", got)
	}
}

func TestArrayScanPreservesExtendedNumericDomain(t *testing.T) {
	var unsigned Array
	if err := unsigned.Scan("[18446744073709551614,null,1]"); err != nil {
		t.Fatal(err)
	}
	if unsigned.ElementType() != SqlTypeUInt64 ||
		unsigned.String() != "[18446744073709551614,null,1]" {
		t.Fatalf("UINT64 scan mismatch: type=%s value=%s",
			unsigned.ElementType(), unsigned.String())
	}

	var decimal Array
	if err := decimal.Scan("[1.2500,null,-3.7500]"); err != nil {
		t.Fatal(err)
	}
	if decimal.ElementType() != SqlTypeDecimal || decimal.Scale() != 4 ||
		decimal.String() != "[1.2500,null,-3.7500]" {
		t.Fatalf("DECIMAL scan mismatch: type=%s scale=%d value=%s",
			decimal.ElementType(), decimal.Scale(), decimal.String())
	}

	floating, err := NewSparseArray(SqlTypeDouble, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := floating.Scan("[NaN,+Inf,-Inf]"); err != nil {
		t.Fatal(err)
	}
	if floating.String() != "[NaN,+Inf,-Inf]" {
		t.Fatalf("special floating scan mismatch: %s", floating.String())
	}

	var mixed Array
	if err := mixed.Scan("[-1,18446744073709551614]"); err == nil {
		t.Fatal("mixed negative and UINT64-only values accepted")
	}
}

func TestParseArrayDense(t *testing.T) {
	value, err := ParseArray("[1,2,3,4]", SqlTypeInt32, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if value.Cardinality() != 4 {
		t.Fatalf("cardinality=%d, want 4", value.Cardinality())
	}
	if got := value.String(); got != "[1,2,3,4]" {
		t.Fatalf("String()=%s", got)
	}
}

func TestParseArraySparse(t *testing.T) {
	value, err := ParseArray("[1=>1.0, 2=>2.1, 11=>3.14]", SqlTypeDouble, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if value.Cardinality() != 12 {
		t.Fatalf("cardinality=%d, want 12", value.Cardinality())
	}
	if got, err := value.Get(1); err != nil || got != 1.0 {
		t.Fatalf("Get(1)=(%v,%v)", got, err)
	}
	if got, err := value.Get(2); err != nil || got != 2.1 {
		t.Fatalf("Get(2)=(%v,%v)", got, err)
	}
	if got, err := value.Get(11); err != nil || got != 3.14 {
		t.Fatalf("Get(11)=(%v,%v)", got, err)
	}
	if got, err := value.Get(0); err != nil || got != nil {
		t.Fatalf("Get(0)=(%v,%v), want NULL", got, err)
	}
}

func TestParseArrayRejectsMixedDenseAndSparse(t *testing.T) {
	cases := []string{
		"[1,2,3,10=>4]", // dense followed by sparse
		"[10=>4,5]",     // sparse followed by dense
	}
	for _, text := range cases {
		if _, err := ParseArray(text, SqlTypeInt32, 0, 0, 0); err == nil {
			t.Fatalf("expected mixed dense/sparse ARRAY literal to be rejected for %q", text)
		}
	}
}

func TestParseArrayNullToken(t *testing.T) {
	value, err := ParseArray("[1,null,3]", SqlTypeInt32, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "[1,null,3]" {
		t.Fatalf("String()=%s", got)
	}
}

func TestParseArrayExplicitCardinalityFromParamDesc(t *testing.T) {
	value, err := ParseArray("[1,2]", SqlTypeInt32, 8, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if value.Cardinality() != 8 {
		t.Fatalf("cardinality=%d, want 8", value.Cardinality())
	}
	if got := value.String(); got != "[1,2,null,null,null,null,null,null]" {
		t.Fatalf("String()=%s", got)
	}
}

func TestParseArrayDecimalUsesPrecisionScale(t *testing.T) {
	value, err := ParseArray("[1.2500,null,-3.7500]", SqlTypeDecimal, 0, 8, 4)
	if err != nil {
		t.Fatal(err)
	}
	if got := value.String(); got != "[1.2500,null,-3.7500]" {
		t.Fatalf("String()=%s", got)
	}
}

func TestParseArrayRejectsMalformedInput(t *testing.T) {
	cases := []string{
		"",
		"1,2,3",    // missing brackets
		"[]",       // no elements
		"[1,,3]",   // empty element
		"[abc=>1]", // non-numeric position
		"[-1=>1]",  // negative position
	}
	for _, text := range cases {
		if _, err := ParseArray(text, SqlTypeInt32, 0, 0, 0); err == nil {
			t.Fatalf("expected error for %q", text)
		}
	}
}

func TestParseArrayRejectsCardinalityOverflow(t *testing.T) {
	if _, err := ParseArray("[0=>1,2000=>2]", SqlTypeInt32, 0, 0, 0); err == nil {
		t.Fatal("expected cardinality overflow error")
	}
	if _, err := ParseArray("[1,2,3]", SqlTypeInt32, 2, 0, 0); err == nil {
		t.Fatal("expected out-of-range position error against explicit cardinality")
	}
}

func TestParseArrayRejectsUnknownElementType(t *testing.T) {
	if _, err := ParseArray("[1,2]", SqlTypeString, 0, 0, 0); err == nil {
		t.Fatal("expected unsupported element type error")
	}
}

func BenchmarkParseArrayMaxCardinality(b *testing.B) {
	text := "[" + strings.Repeat("1,", ArrayMaxCardinality-1) + "1]"
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := ParseArray(text, SqlTypeInt32, 0, 0, 0); err != nil {
			b.Fatal(err)
		}
	}
}
