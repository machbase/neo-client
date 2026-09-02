package client

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

type appenderTestCloser struct {
	closed bool
	err    error
}

func (c *appenderTestCloser) Close() error {
	c.closed = true
	return c.err
}

func TestCloseAppenderConnectResources(t *testing.T) {
	connErr := errors.New("connection close")
	connectorErr := errors.New("connector close")
	conn := &appenderTestCloser{err: connErr}
	connector := &appenderTestCloser{err: connectorErr}

	err := closeAppenderConnectResources(conn, connector)
	if !conn.closed || !connector.closed {
		t.Fatalf("resources not closed: conn=%v connector=%v", conn.closed, connector.closed)
	}
	if !errors.Is(err, connErr) || !errors.Is(err, connectorErr) {
		t.Fatalf("cleanup errors not preserved: %v", err)
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
