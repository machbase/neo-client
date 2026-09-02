package client

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestResetSessionRestoresConfiguredDatabase(t *testing.T) {
	native := &resetTestConn{}
	conn := &Conn{testResetConn: native, database: `Database "A`, dbDirty: true}

	if err := conn.ResetSession(context.Background()); err != nil {
		t.Fatalf("ResetSession() error = %v", err)
	}
	if len(native.queries) != 1 || native.queries[0] != `USE "Database ""A"` {
		t.Fatalf("queries=%q", native.queries)
	}
	if conn.dbDirty {
		t.Fatal("database remained dirty")
	}

	if err := conn.ResetSession(context.Background()); err != nil {
		t.Fatalf("second ResetSession() error = %v", err)
	}
	if len(native.queries) != 1 {
		t.Fatalf("clean reset executed another USE: %q", native.queries)
	}
}

func TestResetSessionDatabaseFailureDiscardsConnection(t *testing.T) {
	native := &resetTestConn{err: errors.New("database unavailable")}
	conn := &Conn{testResetConn: native, database: "DATABASE_A", dbDirty: true}

	if err := conn.ResetSession(context.Background()); !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("ResetSession() error = %v, want ErrBadConn", err)
	}
	if !native.closed || conn.testResetConn != nil || conn.handle != nil {
		t.Fatal("failed reset did not close the connection")
	}
}

func TestSetTransactionStateMarksDatabaseDirty(t *testing.T) {
	conn := &Conn{database: "DATABASE_A"}
	conn.setTransactionState("/* select another catalog */ USE DATABASE_B")
	if !conn.dbDirty {
		t.Fatal("successful USE was not marked dirty")
	}

	withoutDefault := &Conn{}
	withoutDefault.setTransactionState("USE DATABASE_B")
	if withoutDefault.dbDirty {
		t.Fatal("connection without configured database should not reset USE")
	}
}

func TestIsValidRestoresDatabaseBeforePooling(t *testing.T) {
	native := &resetTestConn{}
	conn := &Conn{testResetConn: native, database: "DATABASE_A", dbDirty: true}

	if !conn.IsValid() {
		t.Fatal("IsValid() = false, want true")
	}
	if len(native.queries) != 1 || native.queries[0] != `USE "DATABASE_A"` {
		t.Fatalf("queries=%q", native.queries)
	}
	if conn.dbDirty {
		t.Fatal("database remained dirty")
	}

	if !conn.IsValid() {
		t.Fatal("second IsValid() = false, want true")
	}
	if len(native.queries) != 1 {
		t.Fatalf("clean connection executed another USE: %q", native.queries)
	}
}

func TestIsValidDiscardsConnectionWhenRestoreFails(t *testing.T) {
	native := &resetTestConn{err: errors.New("database unavailable")}
	conn := &Conn{testResetConn: native, database: "DATABASE_A", dbDirty: true}

	if conn.IsValid() {
		t.Fatal("IsValid() = true, want false")
	}
	if !native.closed || conn.testResetConn != nil || conn.handle != nil {
		t.Fatal("failed restore did not close the connection")
	}
}

func TestCheckNamedValue(t *testing.T) {
	ip := net.ParseIP("127.0.0.1")
	tm := time.Unix(0, 1)
	values := []*driver.NamedValue{
		{Ordinal: 1, Value: int32(1)},
		{Ordinal: 2, Value: uint16(2)},
		{Ordinal: 3, Value: ip},
		{Ordinal: 4, Value: tm},
	}
	for _, nv := range values {
		if err := checkNamedValue(nv); err != nil {
			t.Fatalf("checkNamedValue(%T) error = %v", nv.Value, err)
		}
	}
	if _, ok := values[1].Value.(int64); !ok {
		t.Fatalf("expected uint16 to normalize to int64, got %T", values[1].Value)
	}
	named := &driver.NamedValue{Ordinal: 1, Name: "foo", Value: 1}
	if err := checkNamedValue(named); err != nil {
		t.Fatalf("named parameter error = %v", err)
	}
	converted, err := namedValuesToAny([]driver.NamedValue{*named})
	if err != nil {
		t.Fatalf("namedValuesToAny() error = %v", err)
	}
	if param, ok := converted[0].(driver.NamedValue); !ok || param.Name != "foo" || param.Value != 1 {
		t.Fatalf("unexpected named conversion: %#v", converted[0])
	}
	if err := checkNamedValue(&driver.NamedValue{Ordinal: 1, Value: true}); err == nil {
		t.Fatalf("expected unsupported bool error")
	}
}

func TestBeginTxOptions(t *testing.T) {
	conn := &Conn{}
	_, err := conn.BeginTx(context.Background(), driver.TxOptions{Isolation: driver.IsolationLevel(1)})
	if err == nil || !strings.Contains(err.Error(), "isolation") {
		t.Fatalf("BeginTx(isolation) error = %v", err)
	}
	_, err = conn.BeginTx(context.Background(), driver.TxOptions{ReadOnly: true})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("BeginTx(read-only) error = %v", err)
	}
	_, err = conn.BeginTx(context.Background(), driver.TxOptions{})
	if !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("BeginTx(default) error = %v", err)
	}
}

func TestLeadingSQLKeyword(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  string
	}{
		{query: " BEGIN", want: "BEGIN"},
		{query: "begin;", want: "BEGIN"},
		{query: "BEGIN/* suffix */;", want: "BEGIN"},
		{query: "BEGIN-- suffix\n;", want: "BEGIN"},
		{query: "BEGINNING", want: "BEGINNING"},
		{query: "-- leading comment\n COMMIT", want: "COMMIT"},
		{query: "/* block */ -- line\n rollback", want: "ROLLBACK"},
		{query: "SELECT 'BEGIN'", want: "SELECT"},
		{query: "-- comment only", want: ""},
		{query: "/* unterminated", want: ""},
	} {
		if got := leadingSQLKeyword(tc.query); got != tc.want {
			t.Fatalf("leadingSQLKeyword(%q)=%q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestNormalizeErrorBadConn(t *testing.T) {
	if !errors.Is(normalizeError(errors.New("connection closed")), driver.ErrBadConn) {
		t.Fatalf("expected ErrBadConn for connection closed")
	}
	if errors.Is(normalizeError(errors.New("other error")), driver.ErrBadConn) {
		t.Fatalf("did not expect ErrBadConn for generic error")
	}
}

type resetTestResult struct {
	err error
}

func (r *resetTestResult) Err() error          { return r.err }
func (r *resetTestResult) RowsAffected() int64 { return 0 }
func (r *resetTestResult) Message() string     { return "" }

type resetTestConn struct {
	queries []string
	err     error
	closed  bool
}

var _ testResetSessionConn = (*resetTestConn)(nil)

func (c *resetTestConn) Close() error {
	c.closed = true
	return nil
}

func (c *resetTestConn) Exec(_ context.Context, query string, _ ...any) *Result {
	c.queries = append(c.queries, query)
	return &Result{err: c.err}
}

func (c *resetTestConn) Query(context.Context, string, ...any) (*Rows, error) {
	return nil, errors.New("not implemented")
}

func (c *resetTestConn) QueryRow(context.Context, string, ...any) *Row { return nil }

func (c *resetTestConn) Prepare(context.Context, string) (*Stmt, error) {
	return nil, errors.New("not implemented")
}

func (c *resetTestConn) Appender(context.Context, string) (*Appender, error) {
	return nil, errors.New("not implemented")
}

func (c *resetTestConn) Explain(context.Context, string, bool) (string, error) {
	return "", errors.New("not implemented")
}

func TestResultLastInsertIDPreservesUint64Bits(t *testing.T) {
	for _, value := range []uint64{0, 1, 0x8000000000000000, 0xfffffffffffffffe} {
		result := &Result{rowID: value, hasRowID: true}
		got, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId(%#x) error = %v", value, err)
		}
		if uint64(got) != value {
			t.Fatalf("LastInsertId(%#x) bits = %#x", value, uint64(got))
		}
	}

	if _, err := (&Result{}).LastInsertId(); err == nil {
		t.Fatal("LastInsertId() without generated ROWID unexpectedly succeeded")
	}
}

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
