package api

import "testing"

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
