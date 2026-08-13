package client

import (
	"database/sql"
	"testing"
)

func TestResultLastInsertIDPreservesUint64Bits(t *testing.T) {
	for _, value := range []uint64{0, 1, 0x8000000000000000, 0xfffffffffffffffe} {
		result := &ClientResult{rowID: value, hasRowID: true}
		got, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId(%#x) error = %v", value, err)
		}
		if uint64(got) != value {
			t.Fatalf("LastInsertId(%#x) bits = %#x", value, uint64(got))
		}
	}

	if _, err := (&ClientResult{}).LastInsertId(); err == nil {
		t.Fatal("LastInsertId() without generated ROWID unexpectedly succeeded")
	}
}

func TestRowScanNullDestination(t *testing.T) {
	row := &ClientRow{values: []any{nil}}
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
	rows := &ClientRows{row: []any{nil}}
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
