package machgo

import (
	"database/sql"
	"testing"
)

func TestRowScanNullDestination(t *testing.T) {
	row := &Row{values: []any{nil}}
	var direct string
	if err := row.Scan(&direct); err == nil {
		t.Fatal("Row.Scan NULL into string should fail")
	}
	nullable := sql.NullString{String: "stale", Valid: true}
	if err := row.Scan(&nullable); err != nil {
		t.Fatalf("Row.Scan NULL into NullString: %v", err)
	}
	if nullable.Valid {
		t.Fatal("Row.Scan NULL left NullString valid")
	}
	if nullable.String != "" {
		t.Fatalf("Row.Scan NULL left stale value %q", nullable.String)
	}
}

func TestRowsScanNullDestination(t *testing.T) {
	rows := &Rows{row: []any{nil}}
	var direct string
	if err := rows.Scan(&direct); err == nil {
		t.Fatal("Rows.Scan NULL into string should fail")
	}
	nullable := sql.NullString{String: "stale", Valid: true}
	if err := rows.Scan(&nullable); err != nil {
		t.Fatalf("Rows.Scan NULL into NullString: %v", err)
	}
	if nullable.Valid {
		t.Fatal("Rows.Scan NULL left NullString valid")
	}
	if nullable.String != "" {
		t.Fatalf("Rows.Scan NULL left stale value %q", nullable.String)
	}
}
