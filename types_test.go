package client

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

// fakeColumnTypeMeta/fakeColumnTypeDriver let tests build a *sql.ColumnType
// with an arbitrary DatabaseTypeName()/ScanType(), without a real connection.
type fakeColumnTypeMeta struct {
	dbType   string
	scanType reflect.Type
}

var (
	fakeColumnTypeDriverOnce sync.Once
	fakeColumnTypeDriverMu   sync.Mutex
	fakeColumnTypeMetaByDSN  = map[string][]fakeColumnTypeMeta{}
)

type fakeColumnTypeDriver struct{}

func (d *fakeColumnTypeDriver) Open(name string) (driver.Conn, error) {
	return &fakeColumnTypeConn{dsn: name}, nil
}

type fakeColumnTypeConn struct{ dsn string }

func (c *fakeColumnTypeConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("not implemented Prepare")
}
func (c *fakeColumnTypeConn) Close() error { return nil }
func (c *fakeColumnTypeConn) Begin() (driver.Tx, error) {
	return nil, errors.New("not implemented Begin")
}
func (c *fakeColumnTypeConn) QueryContext(_ context.Context, _ string, _ []driver.NamedValue) (driver.Rows, error) {
	fakeColumnTypeDriverMu.Lock()
	metas := fakeColumnTypeMetaByDSN[c.dsn]
	fakeColumnTypeDriverMu.Unlock()
	return &fakeColumnTypeRows{metas: metas}, nil
}

type fakeColumnTypeRows struct{ metas []fakeColumnTypeMeta }

func (r *fakeColumnTypeRows) Columns() []string {
	ret := make([]string, len(r.metas))
	for i := range r.metas {
		ret[i] = "col"
	}
	return ret
}
func (r *fakeColumnTypeRows) Close() error                { return nil }
func (r *fakeColumnTypeRows) Next(_ []driver.Value) error { return io.EOF }
func (r *fakeColumnTypeRows) ColumnTypeDatabaseTypeName(index int) string {
	return r.metas[index].dbType
}
func (r *fakeColumnTypeRows) ColumnTypeScanType(index int) reflect.Type {
	return r.metas[index].scanType
}

func makeColumnTypeForTest(t *testing.T, dbType string, scanType reflect.Type) *sql.ColumnType {
	t.Helper()
	fakeColumnTypeDriverOnce.Do(func() {
		sql.Register("neo_client_fake_column_type_driver", &fakeColumnTypeDriver{})
	})
	dsn := t.Name() + "/" + time.Now().Format(time.RFC3339Nano)
	fakeColumnTypeDriverMu.Lock()
	fakeColumnTypeMetaByDSN[dsn] = []fakeColumnTypeMeta{{dbType: dbType, scanType: scanType}}
	fakeColumnTypeDriverMu.Unlock()
	t.Cleanup(func() {
		fakeColumnTypeDriverMu.Lock()
		delete(fakeColumnTypeMetaByDSN, dsn)
		fakeColumnTypeDriverMu.Unlock()
	})

	db, err := sql.Open("neo_client_fake_column_type_driver", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	rows, err := db.QueryContext(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { rows.Close() })

	colTypes, err := rows.ColumnTypes()
	if err != nil {
		t.Fatal(err)
	}
	if len(colTypes) != 1 {
		t.Fatalf("expected 1 column type, got %d", len(colTypes))
	}
	return colTypes[0]
}

func TestNewColumnWithTypeRecognizesDecimalAndArrayTypes(t *testing.T) {
	tests := []struct {
		dbType string
		want   api.DataType
	}{
		{"DECIMAL", api.DataTypeDecimal},
		{"INT16_ARRAY", api.DataTypeInt16Array},
		{"UINT16_ARRAY", api.DataTypeUInt16Array},
		{"INT32_ARRAY", api.DataTypeInt32Array},
		{"UINT32_ARRAY", api.DataTypeUInt32Array},
		{"INT64_ARRAY", api.DataTypeInt64Array},
		{"UINT64_ARRAY", api.DataTypeUInt64Array},
		{"FLOAT_ARRAY", api.DataTypeFloatArray},
		{"DOUBLE_ARRAY", api.DataTypeDoubleArray},
		{"DECIMAL_ARRAY", api.DataTypeDecimalArray},
	}
	for _, tc := range tests {
		t.Run(tc.dbType, func(t *testing.T) {
			colType := makeColumnTypeForTest(t, tc.dbType, reflect.TypeOf(""))
			col := NewColumnWithType(colType)
			if col.DataType != tc.want {
				t.Fatalf("DataType = %v, want %v", col.DataType, tc.want)
			}
		})
	}
}
