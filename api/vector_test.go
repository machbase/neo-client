package api

import (
	"math"
	"reflect"
	"testing"
)

func TestVectorValueAndScan(t *testing.T) {
	vector, err := NewVector(1.5, -2, 0)
	if err != nil {
		t.Fatal(err)
	}
	value, err := vector.Value()
	if err != nil || value != "[1.5,-2,0]" {
		t.Fatalf("VECTOR driver value = %v, %v", value, err)
	}
	var decoded Vector
	if err := decoded.Scan(value); err != nil || !reflect.DeepEqual(decoded, vector) {
		t.Fatalf("VECTOR Scan = %v, %v", decoded, err)
	}
	if err := decoded.Scan(vector); err != nil {
		t.Fatal(err)
	}
	vector[0] = 9
	if decoded[0] != 1.5 {
		t.Fatal("VECTOR Scan retained caller's mutable storage")
	}
	if err := decoded.Scan(nil); err != nil || decoded != nil {
		t.Fatalf("VECTOR NULL Scan = %v, %v", decoded, err)
	}
	if value, err := decoded.Value(); err != nil || value != nil {
		t.Fatalf("VECTOR NULL Value = %v, %v", value, err)
	}
}

func TestVectorRejectsInvalidValues(t *testing.T) {
	if _, err := NewVector(); err == nil {
		t.Fatal("zero-dimensional VECTOR accepted")
	}
	if _, err := NewVector(float32(math.NaN())); err == nil {
		t.Fatal("NaN VECTOR accepted")
	}
	if _, err := NewVector(float32(math.Inf(1))); err == nil {
		t.Fatal("infinite VECTOR accepted")
	}
	for _, input := range []string{"[]", "[1,null]", "[1e100]", "[true]", "[1] garbage"} {
		if _, err := ParseVector(input); err == nil {
			t.Fatalf("invalid VECTOR %q accepted", input)
		}
	}
	if err := (Vector{1, float32(math.Inf(-1))}).Validate(); err == nil {
		t.Fatal("infinite VECTOR element accepted")
	}
}

func TestVectorTypeMetadata(t *testing.T) {
	if SqlTypeVector.String() != "VECTOR" || SqlTypeVector.ColumnType() != ColumnTypeVector ||
		SqlTypeVector.DataType() != DataTypeVector || ColumnTypeVector.ToSqlType() != SqlTypeVector ||
		ColumnTypeVector.DataType() != DataTypeVector || DataTypeVector.ColumnType() != ColumnTypeVector ||
		DataTypeOf(Vector{1, 2}) != DataTypeVector || ParseColumnType("VECTOR") != ColumnTypeVector {
		t.Fatal("VECTOR type metadata mapping is incomplete")
	}
}
