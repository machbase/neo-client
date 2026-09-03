package client

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

type appenderTestCloser struct {
	closed bool
	err    error
}

func (c *appenderTestCloser) Close() error {
	c.closed = true
	return c.err
}

func TestCloseAppenderResourcesAttemptsEveryClose(t *testing.T) {
	stmtErr := errors.New("statement close")
	connErr := errors.New("connection close")
	connectorErr := errors.New("connector close")
	stmt := &appenderTestCloser{err: stmtErr}
	conn := &appenderTestCloser{err: connErr}
	connector := &appenderTestCloser{err: connectorErr}

	err := closeAppenderResources(stmt, conn, connector)
	if !stmt.closed || !conn.closed || !connector.closed {
		t.Fatalf("resources not closed: stmt=%v conn=%v connector=%v",
			stmt.closed, conn.closed, connector.closed)
	}
	if !errors.Is(err, stmtErr) || !errors.Is(err, connErr) || !errors.Is(err, connectorErr) {
		t.Fatalf("cleanup errors not preserved: %v", err)
	}
}

func TestAppenderResetForConnectPreservesProjection(t *testing.T) {
	closeErr := errors.New("prior close")
	ap := &Appender{
		ctx:               context.Background(),
		tableName:         "OLD_TABLE",
		tableType:         TableTypeLog,
		columns:           Columns{&Column{Name: "OLD"}},
		columnNames:       []string{"OLD"},
		columnTypes:       []api.SqlType{api.SqlTypeInt64},
		inputColumns:      []AppenderInputColumn{{Name: "ID", Idx: 3}, {Name: "A[2]", Idx: 4}},
		inputAtOpen:       true,
		opened:            true,
		closed:            true,
		closeErr:          closeErr,
		successCount:      7,
		failCount:         2,
		stringColumns:     []string{"OLD"},
		stringColumnTypes: []string{"LONG"},
	}

	ap.resetForConnect()
	if ap.opened || ap.closed || ap.closeErr != nil || ap.successCount != 0 || ap.failCount != 0 {
		t.Fatalf("lifecycle state not reset: %+v", ap)
	}
	if ap.tableName != "" || ap.tableType != TableType(-1) || ap.columns != nil ||
		ap.columnNames != nil || ap.columnTypes != nil || ap.stringColumns != nil ||
		ap.stringColumnTypes != nil || ap.inputAtOpen {
		t.Fatalf("metadata state not reset: %+v", ap)
	}
	if got, want := ap.appendColumnNames(), []string{"ID", "A[2]"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("projection not preserved: got=%v want=%v", got, want)
	}
	for _, column := range ap.inputColumns {
		if column.Idx != -1 {
			t.Fatalf("stale projection index preserved: %+v", column)
		}
	}
}

func TestAppenderRepeatedCloseReturnsStableResult(t *testing.T) {
	closeErr := errors.New("close failed")
	ap := &Appender{opened: true, closed: true, successCount: 3, failCount: 1, closeErr: closeErr}
	success, failure, err := ap.Close()
	if success != 3 || failure != 1 || !errors.Is(err, closeErr) {
		t.Fatalf("repeated Close = (%d,%d,%v)", success, failure, err)
	}
	if err := ap.Flush(); err == nil || err.Error() != "closed appender" {
		t.Fatalf("Flush after Close error = %v", err)
	}
}

func TestAppenderColumnNamesFollowLateInputColumns(t *testing.T) {
	ap := &Appender{columnNames: []string{"NAME", "TIME", "VALUE", "EXTRA"}}
	ap.WithInputColumns("VALUE", "NAME", "TIME")

	got := ap.appendColumnNames()
	want := []string{"VALUE", "NAME", "TIME"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("append column names = %v, want %v", got, want)
	}
}

func TestAppenderLateLogInputColumnsPreserveArrivalContract(t *testing.T) {
	ap := &Appender{
		tableType:   TableTypeLog,
		columnNames: []string{"_ARRIVAL_TIME", "ID", "VALUE"},
	}
	ap.WithInputColumns("ID", "VALUE")
	if !ap.requiresExplicitArrival(ap.appendColumnNames()) {
		t.Fatal("late log input columns without arrival were accepted")
	}
	if err := ap.Append(int64(1), int32(2)); err == nil || err.Error() != "log input columns configured after Connect must include _ARRIVAL_TIME" {
		t.Fatalf("late log Append error = %v", err)
	}
	if err := ap.AppendLogTime(time.Unix(1, 0), int64(1), int32(2)); err == nil || err.Error() != "log input columns configured after Connect must include _ARRIVAL_TIME" {
		t.Fatalf("late log AppendLogTime error = %v", err)
	}

	ap.WithInputColumns("_ARRIVAL_TIME", "ID", "VALUE")
	if ap.requiresExplicitArrival(ap.appendColumnNames()) {
		t.Fatal("late log input columns with arrival were rejected")
	}

	ap.WithInputColumns("ID", "VALUE")
	ap.inputAtOpen = true
	if ap.requiresExplicitArrival(ap.appendColumnNames()) {
		t.Fatal("projection opened with implicit arrival was rejected")
	}
}

func TestAppenderProjectionColumnsOmitImplicitArrival(t *testing.T) {
	names := []string{`"_arrival_time"`, "ID", "A[2]"}
	got := appenderProjectionColumns(TableTypeLog, names)
	want := []string{"ID", "A[2]"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("projection columns = %v, want %v", got, want)
	}

	got = appenderProjectionColumns(TableTypeTag, names)
	if !reflect.DeepEqual(got, names) {
		t.Fatalf("non-log projection columns = %v, want %v", got, names)
	}
}

func TestAppenderArrivalShapeStable(t *testing.T) {
	names := []string{"ID", "A[2]"}
	explicitTime := time.Unix(123, 456)

	defaultNames, defaultValues := withAppenderArrival(names, []any{int64(1), int32(2)}, time.Time{})
	explicitNames, explicitValues := withAppenderArrival(names, []any{int64(1), int32(2)}, explicitTime)
	if !reflect.DeepEqual(defaultNames, explicitNames) {
		t.Fatalf("arrival shapes differ: default=%v explicit=%v", defaultNames, explicitNames)
	}
	if got, want := defaultNames, []string{"_ARRIVAL_TIME", "ID", "A[2]"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("arrival names = %v, want %v", got, want)
	}
	if got, ok := explicitValues[0].(time.Time); !ok || !got.Equal(explicitTime) {
		t.Fatalf("explicit arrival value = %#v", explicitValues[0])
	}
	if got, ok := defaultValues[0].(time.Time); !ok || !got.IsZero() {
		t.Fatalf("default arrival value = %#v", defaultValues[0])
	}

	existing := []string{"ID", "ARRIVAL_TIME", "A[2]"}
	gotNames, gotValues := withAppenderArrival(existing, []any{int64(1), int32(2)}, explicitTime)
	if !reflect.DeepEqual(gotNames, existing) {
		t.Fatalf("explicit arrival names changed: %v", gotNames)
	}
	if len(gotValues) != 3 || gotValues[0] != int64(1) || gotValues[2] != int32(2) {
		t.Fatalf("explicit arrival shifted values incorrectly: %#v", gotValues)
	}
	if got, ok := gotValues[1].(time.Time); !ok || !got.Equal(explicitTime) {
		t.Fatalf("explicit arrival was not inserted at declared position: %#v", gotValues)
	}
}
