package api

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

type catalogTestConn struct{}

type legacyCatalogTestConn struct{ catalogTestConn }

func (legacyCatalogTestConn) SupportsDatabaseMetadata() bool { return false }

func (catalogTestConn) Close() error { return nil }

func (catalogTestConn) Exec(context.Context, string, ...any) Result { return catalogTestResult{} }

func (catalogTestConn) Prepare(context.Context, string) (Stmt, error) {
	return nil, fmt.Errorf("unexpected Prepare")
}

func (catalogTestConn) Appender(context.Context, string, ...AppenderOption) (Appender, error) {
	return nil, fmt.Errorf("unexpected Appender")
}

func (catalogTestConn) Explain(context.Context, string, bool) (string, error) {
	return "", fmt.Errorf("unexpected Explain")
}

func (catalogTestConn) QueryRow(_ context.Context, sqlText string, params ...any) Row {
	switch {
	case strings.Contains(sqlText, "from V$DATABASES"):
		name := "DB_A"
		if len(params) == 1 {
			name = strings.ToUpper(params[0].(string))
		}
		return catalogTestRow{values: []any{name, catalogTestDatabaseID(name)}}
	case strings.Contains(sqlText, "from V$STORAGE_MOUNT_DATABASES"):
		if len(params) != 1 || params[0] != "ARCHIVE_DB" {
			return catalogTestRow{err: fmt.Errorf("unexpected mounted database params: %v", params)}
		}
		return catalogTestRow{values: []any{int64(303)}}
	case strings.Contains(sqlText, "select count(*) from M$SYS_TABLES"):
		if len(params) != 3 || params[0] != "SYS" || params[2] != "SHARED_TABLE" {
			return catalogTestRow{err: fmt.Errorf("unexpected table existence params: %v", params)}
		}
		dbID := params[1].(int64)
		count := 0
		if dbID == -1 || dbID == 101 || dbID == 202 || dbID == 303 {
			count = 1
		}
		return catalogTestRow{values: []any{count}}
	case strings.Contains(sqlText, "j.COLCOUNT as TABLE_COLCOUNT"):
		if len(params) != 3 || params[0] != "SYS" || params[2] != "SHARED_TABLE" {
			return catalogTestRow{err: fmt.Errorf("unexpected table description params: %v", params)}
		}
		return catalogTestRow{values: []any{int64(77), TableTypeLog, TableFlagNone, 1}}
	default:
		return catalogTestRow{err: fmt.Errorf("unexpected QueryRow: %s", sqlText)}
	}
}

func (catalogTestConn) Query(_ context.Context, sqlText string, params ...any) (Rows, error) {
	switch {
	case strings.Contains(sqlText, "from M$SYS_COLUMNS"):
		dbID, err := catalogTestDBParam(params, 2, 1)
		if err != nil {
			return nil, err
		}
		return &catalogTestRows{rows: [][]any{{catalogTestPrefix(dbID) + "_VALUE", ColumnTypeInteger, 11, uint64(0), ColumnFlag(0)}}}, nil
	case strings.Contains(sqlText, "M$SYS_INDEXES"):
		dbID, err := catalogTestDBParam(params, 3, 1)
		if err != nil || params[2] != dbID {
			return nil, fmt.Errorf("database ID was not applied to both index tables: %v", params)
		}
		return &catalogTestRows{rows: [][]any{{catalogTestPrefix(dbID) + "_IDX", int(IndexTypeRedBlack), int64(88), 0, 0, 0, "EQUAL"}}}, nil
	case strings.Contains(sqlText, "M$SYS_INDEX_COLUMNS"):
		dbID, err := catalogTestDBParam(params, 2, 1)
		if err != nil {
			return nil, err
		}
		return &catalogTestRows{rows: [][]any{{catalogTestPrefix(dbID) + "_VALUE"}}}, nil
	default:
		return nil, fmt.Errorf("unexpected Query: %s", sqlText)
	}
}

func catalogTestDBParam(params []any, count, index int) (int64, error) {
	if len(params) != count {
		return 0, fmt.Errorf("unexpected params: %v", params)
	}
	dbID, ok := params[index].(int64)
	if !ok || (dbID != -1 && dbID != 101 && dbID != 202 && dbID != 303) {
		return 0, fmt.Errorf("unexpected database ID: %v", params[index])
	}
	return dbID, nil
}

func catalogTestDatabaseID(name string) int64 {
	if name == "DB_B" {
		return 202
	}
	return 101
}

func catalogTestPrefix(dbID int64) string {
	switch dbID {
	case -1:
		return "LEGACY"
	case 202:
		return "B"
	case 303:
		return "ARCHIVE"
	default:
		return "A"
	}
}

type catalogTestResult struct{ err error }

func (r catalogTestResult) Err() error        { return r.err }
func (catalogTestResult) RowsAffected() int64 { return 0 }
func (catalogTestResult) Message() string     { return "" }

type catalogTestRow struct {
	values []any
	err    error
}

func (r catalogTestRow) Err() error              { return r.err }
func (catalogTestRow) RowsAffected() int64       { return 0 }
func (catalogTestRow) Message() string           { return "" }
func (catalogTestRow) Columns() (Columns, error) { return nil, nil }
func (r catalogTestRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	return catalogTestScan(r.values, dest)
}

type catalogTestRows struct {
	rows  [][]any
	index int
	err   error
}

func (r *catalogTestRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *catalogTestRows) Scan(dest ...any) error {
	if r.index == 0 || r.index > len(r.rows) {
		return fmt.Errorf("Scan called without Next")
	}
	return catalogTestScan(r.rows[r.index-1], dest)
}

func (*catalogTestRows) Close() error              { return nil }
func (r *catalogTestRows) Err() error              { return r.err }
func (*catalogTestRows) IsFetchable() bool         { return true }
func (*catalogTestRows) RowsAffected() int64       { return 0 }
func (*catalogTestRows) Message() string           { return "" }
func (*catalogTestRows) Columns() (Columns, error) { return nil, nil }

func catalogTestScan(values, dest []any) error {
	if len(values) != len(dest) {
		return fmt.Errorf("scan count mismatch: %d != %d", len(values), len(dest))
	}
	for i := range values {
		target := reflect.ValueOf(dest[i])
		if target.Kind() != reflect.Ptr || target.IsNil() {
			return fmt.Errorf("destination %d is not a pointer", i)
		}
		source := reflect.ValueOf(values[i])
		if source.Type().AssignableTo(target.Elem().Type()) {
			target.Elem().Set(source)
		} else if source.Type().ConvertibleTo(target.Elem().Type()) {
			target.Elem().Set(source.Convert(target.Elem().Type()))
		} else {
			return fmt.Errorf("cannot scan %T into %T", values[i], dest[i])
		}
	}
	return nil
}

func TestCatalogTablesAreIsolatedByDatabase(t *testing.T) {
	ctx := context.Background()
	conn := catalogTestConn{}

	for _, name := range []string{"SHARED_TABLE", "DB_B.SYS.SHARED_TABLE"} {
		exists, err := ExistsTable(ctx, conn, name)
		if err != nil {
			t.Fatalf("ExistsTable(%q): %v", name, err)
		}
		if !exists {
			t.Fatalf("ExistsTable(%q) = false", name)
		}
	}

	tests := []struct {
		name       string
		database   string
		columnName string
		indexName  string
	}{
		{name: "SHARED_TABLE", database: "DB_A", columnName: "A_VALUE", indexName: "A_IDX"},
		{name: "DB_B.SYS.SHARED_TABLE", database: "DB_B", columnName: "B_VALUE", indexName: "B_IDX"},
	}
	for _, tt := range tests {
		desc, err := DescribeTable(ctx, conn, tt.name, false)
		if err != nil {
			t.Fatalf("DescribeTable(%q): %v", tt.name, err)
		}
		if desc.Database != tt.database || len(desc.Columns) != 1 || desc.Columns[0].Name != tt.columnName {
			t.Fatalf("DescribeTable(%q) returned wrong table: %#v", tt.name, desc)
		}
		if len(desc.Indexes) != 1 || desc.Indexes[0].Name != tt.indexName || !reflect.DeepEqual(desc.Indexes[0].Cols, []string{tt.columnName}) {
			t.Fatalf("DescribeTable(%q) returned wrong index: %#v", tt.name, desc.Indexes)
		}
	}
}

func TestDatabaseIDFallsBackForLegacyServer(t *testing.T) {
	ctx := context.Background()
	conn := legacyCatalogTestConn{}

	currentID, err := DatabaseID(ctx, conn, "")
	if err != nil || currentID != -1 {
		t.Fatalf("current legacy database ID = %d, %v; want -1", currentID, err)
	}
	mountedID, err := DatabaseID(ctx, conn, "archive_db")
	if err != nil || mountedID != 303 {
		t.Fatalf("mounted legacy database ID = %d, %v; want 303", mountedID, err)
	}
}

func TestCatalogTablesFallBackForLegacyServer(t *testing.T) {
	ctx := context.Background()
	conn := legacyCatalogTestConn{}
	tests := []struct {
		name       string
		database   string
		columnName string
		indexName  string
	}{
		{name: "SHARED_TABLE", database: "MACHBASEDB", columnName: "LEGACY_VALUE", indexName: "LEGACY_IDX"},
		{name: "ARCHIVE_DB.SYS.SHARED_TABLE", database: "ARCHIVE_DB", columnName: "ARCHIVE_VALUE", indexName: "ARCHIVE_IDX"},
	}
	for _, tt := range tests {
		desc, err := DescribeTable(ctx, conn, tt.name, false)
		if err != nil {
			t.Fatalf("DescribeTable(%q): %v", tt.name, err)
		}
		if desc.Database != tt.database || len(desc.Columns) != 1 || desc.Columns[0].Name != tt.columnName {
			t.Fatalf("DescribeTable(%q) returned wrong legacy table: %#v", tt.name, desc)
		}
		if len(desc.Indexes) != 1 || desc.Indexes[0].Name != tt.indexName {
			t.Fatalf("DescribeTable(%q) returned wrong legacy index: %#v", tt.name, desc.Indexes)
		}
	}
}
