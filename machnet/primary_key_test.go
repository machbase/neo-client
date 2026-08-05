package machnet

import (
	"encoding/binary"
	"testing"

	"github.com/machbase/neo-client/api"
)

func TestExtractPrimaryKey(t *testing.T) {
	tests := []struct {
		name   string
		cmType uint64
		v403   bool
		want   bool
	}{
		{name: "v403 primary", cmType: 0x1, v403: true, want: true},
		{name: "v403 non primary", cmType: 0x2, v403: true, want: false},
		{name: "legacy ignores primary bit", cmType: 0x1, v403: false, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := extractPrimaryKey(tt.cmType, tt.v403); got != tt.want {
				t.Fatalf("extractPrimaryKey(%#x, %v) = %v, want %v", tt.cmType, tt.v403, got, tt.want)
			}
		})
	}
}

func TestBuildColumnsPrimaryKeyLayout(t *testing.T) {
	typeWord := make([]byte, 8)
	binary.LittleEndian.PutUint64(typeWord, uint64(cmdInt32Type)<<56|uint64(11)<<28|0x1)
	units := map[uint32][]MarshalUnit{
		cmiPColNameID: {{data: []byte("ID")}},
		cmiPColTypeID: {{data: typeWord}},
	}

	columns := buildColumns(units, true)
	if len(columns) != 1 || !columns[0].primaryKey {
		t.Fatalf("v403 columns = %#v, want one primary key column", columns)
	}
	columns = buildColumns(units, false)
	if len(columns) != 1 || columns[0].primaryKey {
		t.Fatalf("legacy columns = %#v, want primary key false", columns)
	}

	units[cmiPColTypeID] = []MarshalUnit{{data: []byte{0x1}}}
	columns = buildColumns(units, true)
	if len(columns) != 1 || columns[0].primaryKey {
		t.Fatalf("short type word columns = %#v, want primary key false", columns)
	}
}

func TestDescribeColExPrimaryKey(t *testing.T) {
	stmt := &StmtHandle{columns: []ColumnMeta{{
		name:        "ID",
		sqlType:     api.SqlTypeInt32,
		primaryKey:  true,
		nullability: api.NullabilityNoNulls,
	}}}
	var primaryKey bool
	if err := stmt.DescribeColEx(0, nil, nil, nil, nil, nil, nil, &primaryKey); err != nil {
		t.Fatal(err)
	}
	if !primaryKey {
		t.Fatal("DescribeColEx primary key = false, want true")
	}
}
