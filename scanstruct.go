package client

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
)

// RowsScanner is the subset of *sql.Rows used by the scan helpers.
type RowsScanner interface {
	Columns() ([]string, error)
	Next() bool
	Scan(dest ...any) error
	Err() error
}

// Queryer is implemented by *sql.DB, *sql.Conn and *sql.Tx.
type Queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type planMode int

const (
	planStruct planMode = iota
	planScalar
	planMap
)

type scanPlan struct {
	cfg    *scanConfig
	cols   []string
	mode   planMode
	fields []*fieldInfo // struct mode only; nil entry discards the column
}

var mapStringAnyType = reflect.TypeOf(map[string]any{})

func newScanPlan(cfg *scanConfig, cols []string, t reflect.Type) (*scanPlan, error) {
	plan := &scanPlan{cfg: cfg, cols: cols}
	switch {
	case t == mapStringAnyType:
		plan.mode = planMap
		return plan, nil
	case isNestedStruct(t):
		plan.mode = planStruct
		return plan, plan.bindStruct(t)
	default:
		plan.mode = planScalar
		if len(cols) != 1 {
			return nil, fmt.Errorf("machbase: cannot scan %d columns into %s", len(cols), t)
		}
		return plan, nil
	}
}

func (p *scanPlan) bindStruct(t reflect.Type) error {
	sm, err := p.cfg.structMapOf(t)
	if err != nil {
		return err
	}
	p.fields = make([]*fieldInfo, len(p.cols))
	seen := make(map[string]bool, len(p.cols))
	matched := make(map[string]bool, len(p.cols))
	for i, col := range p.cols {
		key := strings.ToLower(col)
		if seen[key] {
			return fmt.Errorf("%w: %q appears more than once in the result set", ErrScanDuplicateColumn, col)
		}
		seen[key] = true

		field, ok := sm.lookup(col)
		if !ok {
			if !p.cfg.laxColumns {
				return fmt.Errorf("%w: %q in %s; add a %q tag or use WithLaxColumns",
					ErrScanNoMatchedField, col, t, p.cfg.tagKey)
			}
			continue
		}
		p.fields[i] = field
		matched[strings.ToLower(field.name)] = true
	}
	if !p.cfg.laxFields {
		for i := range sm.fields {
			if !matched[strings.ToLower(sm.fields[i].name)] {
				return fmt.Errorf("%w: %q of %s; adjust the query or use WithLaxFields",
					ErrScanNoMatchedColumn, sm.fields[i].name, t)
			}
		}
	}
	return nil
}

// rowReader scans consecutive rows using a single plan and a reusable target buffer.
type rowReader struct {
	rows RowsScanner
	plan *scanPlan
	buf  []any
	sink any
}

func newRowReader(rows RowsScanner, cfg *scanConfig, t reflect.Type) (*rowReader, error) {
	cols, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	plan, err := newScanPlan(cfg, cols, t)
	if err != nil {
		return nil, err
	}
	return &rowReader{rows: rows, plan: plan, buf: make([]any, len(cols))}, nil
}

// scanValue scans the current row into v, which must be an addressable value.
func (r *rowReader) scanValue(v reflect.Value) error {
	switch r.plan.mode {
	case planMap:
		values := make([]any, len(r.plan.cols))
		for i := range values {
			r.buf[i] = &values[i]
		}
		if err := r.rows.Scan(r.buf...); err != nil {
			return err
		}
		m := make(map[string]any, len(values))
		for i, col := range r.plan.cols {
			m[col] = values[i]
		}
		v.Set(reflect.ValueOf(m))
		return nil
	case planScalar:
		r.buf[0] = v.Addr().Interface()
		return r.rows.Scan(r.buf...)
	default:
		for i, field := range r.plan.fields {
			if field == nil {
				r.buf[i] = &r.sink
				continue
			}
			r.buf[i] = v.FieldByIndex(field.index).Addr().Interface()
		}
		return r.rows.Scan(r.buf...)
	}
}

func derefDest(dest any) (reflect.Value, error) {
	rv := reflect.ValueOf(dest)
	if !rv.IsValid() || rv.Kind() != reflect.Pointer || rv.IsNil() {
		return reflect.Value{}, fmt.Errorf("%w, got %T", ErrScanDestNotPointer, dest)
	}
	return rv.Elem(), nil
}

// ScanStruct scans the current row of rows into the value pointed to by dest.
// It does not call rows.Next(). The caller keeps ownership of rows.
func ScanStruct(rows RowsScanner, dest any, opts ...ScanOption) error {
	elem, err := derefDest(dest)
	if err != nil {
		return err
	}
	reader, err := newRowReader(rows, newScanConfig(opts), elem.Type())
	if err != nil {
		return err
	}
	return reader.scanValue(elem)
}

// ScanRow scans exactly one row into dest and returns sql.ErrNoRows when the result
// set is empty. The caller keeps ownership of rows.
func ScanRow(rows RowsScanner, dest any, opts ...ScanOption) error {
	elem, err := derefDest(dest)
	if err != nil {
		return err
	}
	reader, err := newRowReader(rows, newScanConfig(opts), elem.Type())
	if err != nil {
		return err
	}
	if !rows.Next() {
		if err := rows.Err(); err != nil {
			return err
		}
		return sql.ErrNoRows
	}
	if err := reader.scanValue(elem); err != nil {
		return err
	}
	return rows.Err()
}

// ScanRows scans all remaining rows into dest, which must be a pointer to a slice of
// structs, pointer-to-structs, maps or scalars. The caller keeps ownership of rows.
//
// ScanRows materializes the entire result set in memory and is limited by WithMaxRows.
// Use ScanEach or a Cursor for unbounded result sets.
func ScanRows(rows RowsScanner, dest any, opts ...ScanOption) error {
	slice, err := derefDest(dest)
	if err != nil {
		return err
	}
	if slice.Kind() != reflect.Slice {
		return fmt.Errorf("machbase: scan destination must be a pointer to a slice, got %T", dest)
	}
	cfg := newScanConfig(opts)
	elemType := slice.Type().Elem()
	baseType := elemType
	isPtr := elemType.Kind() == reflect.Pointer
	if isPtr {
		baseType = elemType.Elem()
	}
	reader, err := newRowReader(rows, cfg, baseType)
	if err != nil {
		return err
	}

	out := reflect.MakeSlice(slice.Type(), 0, cfg.initialCapacity())
	var count int64
	for rows.Next() {
		count++
		if cfg.maxRows > 0 && count > cfg.maxRows {
			return tooManyRowsError(cfg.maxRows)
		}
		item := reflect.New(baseType)
		if err := reader.scanValue(item.Elem()); err != nil {
			return err
		}
		if isPtr {
			out = reflect.Append(out, item)
		} else {
			out = reflect.Append(out, item.Elem())
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	slice.Set(out)
	return nil
}

func tooManyRowsError(limit int64) error {
	return fmt.Errorf("%w (%d); raise it with WithMaxRows(n), disable it with WithMaxRows(0), "+
		"or stream the result with ScanEach or NewCursor", ErrScanTooManyRows, limit)
}

// typedReader adapts rowReader to a concrete element type.
type typedReader[T any] struct {
	reader *rowReader
	base   reflect.Type
	isPtr  bool
}

func newTypedReader[T any](rows RowsScanner, cfg *scanConfig) (*typedReader[T], error) {
	elemType := reflect.TypeOf((*T)(nil)).Elem()
	base := elemType
	isPtr := elemType.Kind() == reflect.Pointer
	if isPtr {
		base = elemType.Elem()
	}
	reader, err := newRowReader(rows, cfg, base)
	if err != nil {
		return nil, err
	}
	return &typedReader[T]{reader: reader, base: base, isPtr: isPtr}, nil
}

// next advances to the next row and scans it. It reports whether a row was read.
func (tr *typedReader[T]) next() (T, bool, error) {
	var zero T
	if !tr.reader.rows.Next() {
		return zero, false, tr.reader.rows.Err()
	}
	item := reflect.New(tr.base)
	if err := tr.reader.scanValue(item.Elem()); err != nil {
		return zero, false, err
	}
	if tr.isPtr {
		return item.Interface().(T), true, nil
	}
	return item.Elem().Interface().(T), true, nil
}

// ScanOne scans the next row into a new T and returns sql.ErrNoRows when the result
// set is empty. The caller keeps ownership of rows.
func ScanOne[T any](rows RowsScanner, opts ...ScanOption) (T, error) {
	var zero T
	reader, err := newTypedReader[T](rows, newScanConfig(opts))
	if err != nil {
		return zero, err
	}
	item, ok, err := reader.next()
	if err != nil {
		return zero, err
	}
	if !ok {
		return zero, sql.ErrNoRows
	}
	return item, nil
}

// ScanAll scans all remaining rows into a new slice. The caller keeps ownership of rows.
//
// ScanAll materializes the entire result set in memory and is limited by WithMaxRows.
// Use ScanEach or a Cursor for unbounded result sets.
func ScanAll[T any](rows RowsScanner, opts ...ScanOption) ([]T, error) {
	cfg := newScanConfig(opts)
	reader, err := newTypedReader[T](rows, cfg)
	if err != nil {
		return nil, err
	}
	out := make([]T, 0, cfg.initialCapacity())
	for {
		item, ok, err := reader.next()
		if err != nil {
			return nil, err
		}
		if !ok {
			return out, nil
		}
		out = append(out, item)
		if cfg.maxRows > 0 && int64(len(out)) > cfg.maxRows {
			return nil, tooManyRowsError(cfg.maxRows)
		}
	}
}

// ScanEach scans rows one at a time and invokes fn for each row, so only one T is
// alive at a time. A non-nil error from fn stops the iteration and is returned as-is.
// The caller keeps ownership of rows.
func ScanEach[T any](rows RowsScanner, fn func(T) error, opts ...ScanOption) error {
	reader, err := newTypedReader[T](rows, newScanConfig(opts))
	if err != nil {
		return err
	}
	for {
		item, ok, err := reader.next()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if err := fn(item); err != nil {
			return err
		}
	}
}

// Cursor is an explicit iterator for callers that need to control the loop.
// The caller keeps ownership of rows and must close them.
type Cursor[T any] struct {
	reader *typedReader[T]
	value  T
	err    error
}

// NewCursor creates a Cursor over rows. The caller keeps ownership of rows.
func NewCursor[T any](rows RowsScanner, opts ...ScanOption) (*Cursor[T], error) {
	reader, err := newTypedReader[T](rows, newScanConfig(opts))
	if err != nil {
		return nil, err
	}
	return &Cursor[T]{reader: reader}, nil
}

// Next advances to the next row. It returns false at the end of the result set or on
// error, which is then reported by Err.
func (c *Cursor[T]) Next() bool {
	if c.err != nil {
		return false
	}
	item, ok, err := c.reader.next()
	if err != nil {
		c.err = err
		return false
	}
	if !ok {
		return false
	}
	c.value = item
	return true
}

// Value returns the row read by the most recent call to Next.
func (c *Cursor[T]) Value() T { return c.value }

// Err returns the error, if any, that stopped the iteration.
func (c *Cursor[T]) Err() error { return c.err }

// Get runs query and scans the first row into a new T.
// It returns sql.ErrNoRows when the result set is empty.
func Get[T any](ctx context.Context, q Queryer, query string, args ...any) (T, error) {
	var zero T
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return zero, err
	}
	defer rows.Close()
	return ScanOne[T](rows)
}

// Select runs query and scans all rows into a new slice.
// It is limited by the default WithMaxRows bound.
func Select[T any](ctx context.Context, q Queryer, query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return ScanAll[T](rows)
}
