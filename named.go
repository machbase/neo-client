package client

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// NamedArgs builds a []any of sql.Named values from a map[string]any or a struct,
// using the same tag rules as ScanStruct. The result is passed straight to the
// standard database/sql query methods.
//
// It does not inspect or rewrite the SQL text: the server resolves the :name
// placeholders itself and reports the parameter names through its metadata.
func NamedArgs(arg any, opts ...ScanOption) ([]any, error) {
	if arg == nil {
		return nil, errors.New("machbase: named argument source must not be nil")
	}
	rv := reflect.ValueOf(arg)
	for rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil, errors.New("machbase: named argument source must not be nil")
		}
		rv = rv.Elem()
	}

	if rv.Type() == mapStringAnyType {
		values := rv.Interface().(map[string]any)
		names := make([]string, 0, len(values))
		for name := range values {
			names = append(names, name)
		}
		sort.Strings(names)
		args := make([]any, 0, len(names))
		for _, name := range names {
			args = append(args, sql.Named(name, values[name]))
		}
		return args, nil
	}

	if !isNestedStruct(rv.Type()) {
		return nil, fmt.Errorf("machbase: named argument source must be a struct or map[string]any, got %T", arg)
	}
	cfg := newScanConfig(opts)
	sm, err := cfg.structMapOf(rv.Type())
	if err != nil {
		return nil, err
	}
	args := make([]any, 0, len(sm.fields))
	for i := range sm.fields {
		field := &sm.fields[i]
		args = append(args, sql.Named(field.name, rv.FieldByIndex(field.index).Interface()))
	}
	return args, nil
}

// SupportsNamedParameters reports whether the server exposes parameter-name metadata,
// which named parameters require (Machbase protocol 4.0.3 or later).
func SupportsNamedParameters(ctx context.Context, db *sql.DB) (bool, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Close()

	var supported bool
	err = conn.Raw(func(driverConn any) error {
		c, ok := driverConn.(*Conn)
		if !ok {
			return fmt.Errorf("machbase: not a machbase connection, got %T", driverConn)
		}
		supported = c.SupportsDatabaseMetadata()
		return nil
	})
	if err != nil {
		return false, err
	}
	return supported, nil
}
