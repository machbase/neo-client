package client

import (
	"testing"

	"github.com/machbase/neo-client/v2/api"
)

func TestArrayColumnBufferPreservesWholeNull(t *testing.T) {
	columns := Columns{&Column{Type: api.ColumnTypeInt32Array}}
	buffers, err := columns.MakeBuffer()
	if err != nil {
		t.Fatal(err)
	}
	destination, ok := buffers[0].(**api.Array)
	if !ok {
		t.Fatalf("ARRAY buffer type = %T, want **api.Array", buffers[0])
	}

	row := &Row{values: []any{nil}}
	if err := row.Scan(buffers...); err != nil {
		t.Fatal(err)
	}
	if *destination != nil {
		t.Fatalf("whole-NULL ARRAY buffer = %v, want nil", *destination)
	}

	value, err := api.NewSparseArray(api.SqlTypeInt32, 3)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Set(2, int32(20)); err != nil {
		t.Fatal(err)
	}
	row.values[0] = value
	if err := row.Scan(buffers...); err != nil {
		t.Fatal(err)
	}
	if *destination == nil || (*destination).String() != "[null,20,null]" {
		t.Fatalf("non-NULL ARRAY buffer = %v", *destination)
	}

	row.values[0] = nil
	if err := row.Scan(buffers...); err != nil {
		t.Fatal(err)
	}
	if *destination != nil {
		t.Fatalf("reused whole-NULL ARRAY buffer = %v, want nil", *destination)
	}
}

func TestArrayDataTypeBufferPreservesWholeNull(t *testing.T) {
	buffer, err := api.DataTypeArray.MakeBuffer(true)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := buffer.(**api.Array); !ok {
		t.Fatalf("ARRAY data type buffer = %T, want **api.Array", buffer)
	}
}
