package client

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/machbase/neo-client/v2/api"
)

func TestArrayIntegration(t *testing.T) {
	dsn := os.Getenv("MACHBASE_ARRAY_TEST_DSN")
	if dsn == "" {
		t.Skip("MACHBASE_ARRAY_TEST_DSN is not set")
	}
	ctx := context.Background()
	db, err := sql.Open(DefaultDriverName, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const table = "T_NEO_ARRAY_SPARSE"
	_, _ = db.ExecContext(ctx, "DROP TABLE "+table)
	if _, err := db.ExecContext(ctx, "CREATE LOG TABLE "+table+" (ID LONG, A INT32[8], D DECIMAL(12,4)[8])"); err != nil {
		t.Fatal(err)
	}
	defer db.ExecContext(ctx, "DROP TABLE "+table)

	dense, err := api.NewArray(api.SqlTypeInt32, []any{int32(1), nil, nil, nil, nil, int32(6), nil, nil})
	if err != nil {
		t.Fatal(err)
	}
	decimal, err := api.ParseDecimal("8.1250", 12, 4)
	if err != nil {
		t.Fatal(err)
	}
	denseDecimal, err := api.NewArray(api.SqlTypeDecimal, []any{decimal, nil, nil, nil, nil, nil, nil, decimal})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO "+table+"(ID,A,D) VALUES(?,?,?)", int64(1), dense, denseDecimal); err != nil {
		t.Fatal(err)
	}

	sparse, _ := api.NewSparseArray(api.SqlTypeInt32, 8)
	_ = sparse.Set(1, int32(10))
	_ = sparse.Set(6, int32(60))
	sparseDecimal, _ := api.NewSparseArray(api.SqlTypeDecimal, 8)
	_ = sparseDecimal.Set(8, decimal)
	app := &Appender{}
	if err := app.Connect(ctx, dsn, table, "ID", "A", "D"); err != nil {
		t.Fatal(err)
	}
	if err := app.Append(int64(2), sparse, sparseDecimal); err != nil {
		t.Fatal(err)
	}
	if success, failure, err := app.Close(); err != nil || success != 1 || failure != 0 {
		t.Fatalf("sparse append close=(%d,%d,%v)", success, failure, err)
	}

	projected := &Appender{}
	if err := projected.Connect(ctx, dsn, table, "ID", "A[1]", "A[6]", "D[8]"); err != nil {
		t.Fatal(err)
	}
	if err := projected.Append(int64(3), int32(20), int32(70), decimal); err != nil {
		t.Fatal(err)
	}
	if success, failure, err := projected.Close(); err != nil || success != 1 || failure != 0 {
		t.Fatalf("projected append close=(%d,%d,%v)", success, failure, err)
	}

	allNull, _ := api.NewSparseArray(api.SqlTypeInt32, 8)
	if _, err := db.ExecContext(ctx, "INSERT INTO "+table+"(ID,A) VALUES(?,?)", int64(4), allNull); err != nil {
		t.Fatal(err)
	}

	want := map[int64][2]string{
		1: {"[1,null,null,null,null,6,null,null]", "[8.1250,null,null,null,null,null,null,8.1250]"},
		2: {"[10,null,null,null,null,60,null,null]", "[null,null,null,null,null,null,null,8.1250]"},
		3: {"[20,null,null,null,null,70,null,null]", "[null,null,null,null,null,null,null,8.1250]"},
		4: {"[null,null,null,null,null,null,null,null]", ""},
	}
	rows, err := db.QueryContext(ctx, "SELECT ID,A,D FROM "+table+" ORDER BY ID")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	seen := 0
	for rows.Next() {
		var id int64
		var a string
		var d sql.NullString
		if err := rows.Scan(&id, &a, &d); err != nil {
			t.Fatal(err)
		}
		expected := want[id]
		if a != expected[0] || (d.Valid && d.String != expected[1]) || (!d.Valid && expected[1] != "") {
			t.Fatalf("row %d got A=%q D=(%q,%v), want=%v", id, a, d.String, d.Valid, expected)
		}
		seen++
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if seen != 4 {
		t.Fatalf("row count=%d", seen)
	}
}
