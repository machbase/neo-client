package machgo

import (
	"testing"

	"github.com/machbase/neo-client/api"
)

func TestRowsColumnsPreservesPrimaryKey(t *testing.T) {
	rows := &Rows{stmt: &Stmt{columnDesc: []api.ColumnDesc{
		{Name: "ID", Type: api.SqlTypeInt32, PrimaryKey: true},
		{Name: "VALUE", Type: api.SqlTypeString, PrimaryKey: false},
	}}}
	columns, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 2 || !columns[0].PrimaryKey || columns[1].PrimaryKey {
		t.Fatalf("Rows.Columns primary keys = [%v, %v], want [true, false]", columns[0].PrimaryKey, columns[1].PrimaryKey)
	}
}

func TestRowColumnsPreservesPrimaryKey(t *testing.T) {
	row := &Row{columns: api.Columns{
		&api.Column{Name: "ID", PrimaryKey: true},
		&api.Column{Name: "VALUE", PrimaryKey: false},
	}}
	columns, err := row.Columns()
	if err != nil {
		t.Fatal(err)
	}
	if len(columns) != 2 || !columns[0].PrimaryKey || columns[1].PrimaryKey {
		t.Fatalf("Row.Columns primary keys = [%v, %v], want [true, false]", columns[0].PrimaryKey, columns[1].PrimaryKey)
	}
}
