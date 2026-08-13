package client

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

const (
	DefaultDriverName = "machbase"
	defaultPort       = 5656
)

func init() {
	sql.Register(DefaultDriverName, &Driver{})
}

type Driver struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	AuthMode        string
	AuthKeyFile     string
	AuthKeyPEM      string
	AuthSigScheme   string
	AlternativeHost string
	AlternativePort int
	FetchRows       int64
	StatementCache  StatementCacheMode
	IOMetrics       bool
}

var _ driver.Driver = (*Driver)(nil)
var _ driver.DriverContext = (*Driver)(nil)

func (drv *Driver) baseConfig() Config {
	statementCache := drv.StatementCache
	if statementCache != StatementCacheOn && statementCache != StatementCacheOff {
		statementCache = StatementCacheAuto
	}
	return Config{
		Host:            drv.Host,
		Port:            drv.Port,
		User:            drv.User,
		Password:        drv.Password,
		Database:        drv.Database,
		AuthMode:        drv.AuthMode,
		AuthKeyFile:     drv.AuthKeyFile,
		AuthKeyPEM:      drv.AuthKeyPEM,
		AuthSigScheme:   drv.AuthSigScheme,
		AlternativeHost: drv.AlternativeHost,
		AlternativePort: drv.AlternativePort,
		FetchRows:       drv.FetchRows,
		StatementCache:  statementCache,
		IOMetrics:       drv.IOMetrics,
	}.normalize()
}

func (drv *Driver) Open(dsn string) (driver.Conn, error) {
	connector, err := drv.OpenConnector(dsn)
	if err != nil {
		return nil, err
	}
	return connector.Connect(context.Background())
}

func (drv *Driver) OpenConnector(dsn string) (driver.Connector, error) {
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	effective := mergeConfig(drv.baseConfig(), cfg).normalize()
	if err := effective.validate(); err != nil {
		return nil, err
	}
	db, err := NewDatabase(&effective)
	if err != nil {
		return nil, err
	}
	return &Connector{driver: drv, cfg: effective, db: db}, nil
}

type Connector struct {
	driver *Driver
	cfg    Config
	db     *ClientDatabase
}

var _ driver.Connector = (*Connector)(nil)

func (cn *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	if cn == nil || cn.db == nil {
		return nil, driver.ErrBadConn
	}
	conn, err := cn.db.ConnectConfig(ctx, &cn.cfg)
	if err != nil {
		return nil, normalizeError(err)
	}
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok && meta != nil {
		meta.cbIOMetrics = conn.IOMetrics
	}
	return &Conn{conn: conn, database: cn.cfg.Database}, nil
}

func (cn *Connector) Driver() driver.Driver {
	if cn == nil {
		return nil
	}
	return cn.driver
}

type resetSessionConn interface {
	Close() error
	Exec(context.Context, string, ...any) *ClientResult
}

type Conn struct {
	conn      *ClientConn
	resetConn resetSessionConn
	txMu      sync.Mutex
	inTx      bool
	database  string
	dbDirty   bool
}

var _ driver.Conn = (*Conn)(nil)
var _ driver.ConnBeginTx = (*Conn)(nil)
var _ driver.ConnPrepareContext = (*Conn)(nil)
var _ driver.QueryerContext = (*Conn)(nil)
var _ driver.ExecerContext = (*Conn)(nil)
var _ driver.NamedValueChecker = (*Conn)(nil)
var _ driver.Pinger = (*Conn)(nil)
var _ driver.SessionResetter = (*Conn)(nil)
var _ driver.Validator = (*Conn)(nil)

func (c *Conn) Explain(ctx context.Context, query string, full bool) (string, error) {
	if c == nil || c.conn == nil {
		return "", driver.ErrBadConn
	}
	return c.conn.Explain(ctx, query, full)
}

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *Conn) closeUnderlying() error {
	if c == nil {
		return nil
	}
	if c.conn != nil {
		err := c.conn.Close()
		c.conn = nil
		return normalizeError(err)
	}
	if c.resetConn != nil {
		err := c.resetConn.Close()
		c.resetConn = nil
		return normalizeError(err)
	}
	return nil
}

func (c *Conn) Close() error {
	return c.closeUnderlying()
}

func (c *Conn) Appender(ctx context.Context, tableName string) (*ClientAppender, error) {
	return c.conn.Appender(ctx, tableName)
}

func (c *Conn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *Conn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	if opts.Isolation != driver.IsolationLevel(sql.LevelDefault) {
		return nil, fmt.Errorf("machbase transaction isolation level %d is not supported", opts.Isolation)
	}
	if opts.ReadOnly {
		return nil, errors.New("machbase read-only transactions are not supported")
	}
	if c == nil || c.conn == nil {
		return nil, driver.ErrBadConn
	}
	if _, err := c.ExecContext(ctx, "BEGIN", nil); err != nil {
		return nil, err
	}
	return &Tx{conn: c}, nil
}

type Tx struct {
	mu   sync.Mutex
	conn *Conn
	done bool
}

var _ driver.Tx = (*Tx)(nil)

func (tx *Tx) finish(sqlText string) error {
	if tx == nil {
		return sql.ErrTxDone
	}
	tx.mu.Lock()
	defer tx.mu.Unlock()
	if tx.done || tx.conn == nil {
		return sql.ErrTxDone
	}
	_, err := tx.conn.ExecContext(context.Background(), sqlText, nil)
	if err == nil {
		tx.done = true
	}
	return err
}

func (tx *Tx) Commit() error { return tx.finish("COMMIT") }

func (tx *Tx) Rollback() error { return tx.finish("ROLLBACK") }

func (c *Conn) Ping(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return driver.ErrBadConn
	}
	rows, err := c.QueryContext(ctx, "SELECT 1", nil)
	if err != nil {
		return err
	}
	defer rows.Close()
	return nil
}

func (c *Conn) resetExec(ctx context.Context, query string) *ClientResult {
	if c == nil {
		return nil
	}
	if c.conn != nil {
		return c.conn.Exec(ctx, query)
	}
	if c.resetConn != nil {
		return c.resetConn.Exec(ctx, query)
	}
	return nil
}

func (c *Conn) ResetSession(ctx context.Context) error {
	if c == nil || (c.conn == nil && c.resetConn == nil) {
		return driver.ErrBadConn
	}
	c.txMu.Lock()
	inTx := c.inTx
	c.txMu.Unlock()
	if inTx {
		result := c.resetExec(ctx, "ROLLBACK")
		if result == nil || normalizeError(result.Err()) != nil {
			_ = c.closeUnderlying()
			return driver.ErrBadConn
		}
		c.setTransactionState("ROLLBACK")
	}
	c.txMu.Lock()
	database := c.database
	dbDirty := c.dbDirty
	c.txMu.Unlock()
	if dbDirty && strings.TrimSpace(database) != "" {
		result := c.resetExec(ctx, "USE "+quoteIdentifier(database))
		if result == nil || normalizeError(result.Err()) != nil {
			_ = c.closeUnderlying()
			return driver.ErrBadConn
		}
		c.txMu.Lock()
		c.dbDirty = false
		c.txMu.Unlock()
	}
	return nil
}

func (c *Conn) IsValid() bool {
	return c != nil && c.conn != nil
}

func (c *Conn) CheckNamedValue(nv *driver.NamedValue) error {
	return checkNamedValue(nv)
}

func (c *Conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if c == nil || c.conn == nil {
		return nil, driver.ErrBadConn
	}
	stmt, err := c.conn.Prepare(ctx, query)
	if err != nil {
		return nil, normalizeError(err)
	}
	return &Stmt{conn: c, stmt: stmt, query: query}, nil
}

func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c == nil || c.conn == nil {
		return nil, driver.ErrBadConn
	}
	vals, err := namedValuesToAny(args)
	if err != nil {
		return nil, err
	}
	result := c.conn.Exec(ctx, query, vals...)
	if err := normalizeError(result.Err()); err != nil {
		return nil, err
	}
	c.setTransactionState(query)
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok {
		meta.cbMessage = result.Message
	}
	return &Result{result: result}, nil
}

func (c *Conn) setTransactionState(query string) {
	verb := leadingSQLKeyword(query)
	if verb != "BEGIN" && verb != "COMMIT" && verb != "ROLLBACK" && verb != "USE" {
		return
	}
	c.txMu.Lock()
	if verb == "USE" {
		c.dbDirty = strings.TrimSpace(c.database) != ""
	} else {
		c.inTx = verb == "BEGIN"
	}
	c.txMu.Unlock()
}

func leadingSQLKeyword(query string) string {
	remaining := strings.TrimSpace(query)
	for remaining != "" {
		switch {
		case strings.HasPrefix(remaining, "--"):
			newline := strings.IndexByte(remaining, '\n')
			if newline < 0 {
				return ""
			}
			remaining = strings.TrimSpace(remaining[newline+1:])
		case strings.HasPrefix(remaining, "/*"):
			end := strings.Index(remaining[2:], "*/")
			if end < 0 {
				return ""
			}
			remaining = strings.TrimSpace(remaining[end+4:])
		default:
			end := 0
			for end < len(remaining) {
				ch := remaining[end]
				if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
					(ch >= '0' && ch <= '9') || ch == '_' || ch == '$') {
					break
				}
				end++
			}
			return strings.ToUpper(remaining[:end])
		}
	}
	return ""
}

func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c == nil || c.conn == nil {
		return nil, driver.ErrBadConn
	}
	vals, err := namedValuesToAny(args)
	if err != nil {
		return nil, err
	}
	rows, err := c.conn.Query(ctx, query, vals...)
	if err != nil {
		return nil, normalizeError(err)
	}
	c.setTransactionState(query)
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok {
		meta.cbMessage = rows.Message
		meta.cbFetchable = rows.IsFetchable
	}
	return newRows(rows)
}

type Stmt struct {
	conn  *Conn
	stmt  *ClientPreparedStmt
	query string
}

var _ driver.Stmt = (*Stmt)(nil)
var _ driver.StmtExecContext = (*Stmt)(nil)
var _ driver.StmtQueryContext = (*Stmt)(nil)
var _ driver.NamedValueChecker = (*Stmt)(nil)

func (s *Stmt) Close() error {
	if s == nil || s.stmt == nil {
		return nil
	}
	err := s.stmt.Close()
	s.stmt = nil
	return normalizeError(err)
}

func (s *Stmt) NumInput() int {
	return -1
}

func (s *Stmt) CheckNamedValue(nv *driver.NamedValue) error {
	return checkNamedValue(nv)
}

func (s *Stmt) Exec(args []driver.Value) (driver.Result, error) {
	return s.exec(context.Background(), valuesToAny(args))
}

func (s *Stmt) Query(args []driver.Value) (driver.Rows, error) {
	return s.queryRows(context.Background(), valuesToAny(args))
}

func (s *Stmt) ExecContext(ctx context.Context, args []driver.NamedValue) (driver.Result, error) {
	vals, err := namedValuesToAny(args)
	if err != nil {
		return nil, err
	}
	return s.exec(ctx, vals)
}

func (s *Stmt) QueryContext(ctx context.Context, args []driver.NamedValue) (driver.Rows, error) {
	vals, err := namedValuesToAny(args)
	if err != nil {
		return nil, err
	}
	return s.queryRows(ctx, vals)
}

func (s *Stmt) exec(ctx context.Context, vals []any) (driver.Result, error) {
	if s == nil || s.stmt == nil {
		return nil, driver.ErrBadConn
	}
	result := s.stmt.Exec(ctx, vals...)
	if err := normalizeError(result.Err()); err != nil {
		return nil, err
	}
	if s.conn != nil {
		s.conn.setTransactionState(s.query)
	}
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok {
		meta.cbMessage = result.Message
	}
	return &Result{result: result}, nil
}

func (s *Stmt) queryRows(ctx context.Context, vals []any) (driver.Rows, error) {
	if s == nil || s.stmt == nil {
		return nil, driver.ErrBadConn
	}
	rows, err := s.stmt.Query(ctx, vals...)
	if err != nil {
		return nil, normalizeError(err)
	}
	if s.conn != nil {
		s.conn.setTransactionState(s.query)
	}
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok {
		meta.cbMessage = rows.Message
	}
	return newRows(rows)
}

type Result struct {
	result *ClientResult
}

var _ driver.Result = (*Result)(nil)

func (r *Result) LastInsertId() (int64, error) {
	if r != nil && r.result != nil {
		return r.result.LastInsertId()
	}
	return 0, errors.New("not implemented LastInsertId")
}

func (r *Result) RowsAffected() (int64, error) {
	if r == nil || r.result == nil {
		return 0, nil
	}
	return r.result.RowsAffected(), nil
}

type Rows struct {
	rows    *ClientRows
	columns Columns
	desc    []ColumnDesc
	buffer  []any
}

var _ driver.Rows = (*Rows)(nil)
var _ driver.RowsColumnTypeDatabaseTypeName = (*Rows)(nil)
var _ driver.RowsColumnTypeLength = (*Rows)(nil)
var _ driver.RowsColumnTypeNullable = (*Rows)(nil)
var _ driver.RowsColumnTypePrecisionScale = (*Rows)(nil)
var _ driver.RowsColumnTypeScanType = (*Rows)(nil)

func newRows(rows *ClientRows) (*Rows, error) {
	cols, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return nil, normalizeError(err)
	}
	ret := &Rows{rows: rows, columns: cols}
	ret.desc = rows.ColumnDescriptions()
	return ret, nil
}

func (r *Rows) Columns() []string {
	if r == nil || len(r.columns) == 0 {
		return nil
	}
	return r.columns.Names()
}

func (r *Rows) Close() error {
	if r == nil || r.rows == nil {
		return nil
	}
	err := r.rows.Close()
	r.rows = nil
	return normalizeError(err)
}

func (r *Rows) Next(dest []driver.Value) error {
	if r == nil || r.rows == nil {
		return io.EOF
	}
	if !r.rows.Next() {
		if err := normalizeError(r.rows.Err()); err != nil {
			return err
		}
		return io.EOF
	}
	var row = r.rows.Row()
	for i := range dest {
		if i >= len(row) {
			dest[i] = nil
			continue
		}
		value, err := toDriverValue(row[i])
		if err != nil {
			return err
		}
		dest[i] = value
	}
	return nil
}

func (r *Rows) ColumnTypeDatabaseTypeName(index int) string {
	if col, ok := r.column(index); ok {
		if col.Type != api.ColumnTypeUnknown {
			return strings.ToUpper(col.Type.String())
		}
		return strings.ToUpper(string(col.DataType))
	}
	return ""
}

func (r *Rows) ColumnTypeLength(index int) (length int64, ok bool) {
	col, exists := r.column(index)
	if !exists {
		return 0, false
	}
	if col.Length > 0 {
		return int64(col.Length), true
	}
	return 0, false
}

// The nullable value should be true if it is known the column may be null,
// or false if the column is known to be not nullable.
// If the driver doses not support this function, ok should be false.
func (r *Rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	col, exists := r.column(index)
	if !exists {
		return true, false
	}
	if col.Nullability == api.NullabilityUnknown {
		return true, true
	}
	return col.Nullability == api.NullabilityNullable, true
}

func (r *Rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	if index < 0 || index >= len(r.desc) {
		return 0, 0, false
	}
	desc := r.desc[index]
	switch desc.Type {
	case api.SqlTypeFloat, api.SqlTypeDouble, api.SqlTypeDecimal:
		if desc.Size <= 0 {
			return 0, int64(desc.Scale), false
		}
		return int64(desc.Size), int64(desc.Scale), true
	default:
		return 0, 0, false
	}
}

func (r *Rows) ColumnTypeScanType(index int) reflect.Type {
	col, ok := r.column(index)
	if !ok {
		return reflect.TypeOf(new(any)).Elem()
	}
	switch col.Type {
	case api.ColumnTypeShort:
		return reflect.TypeOf(int16(0))
	case api.ColumnTypeUShort:
		return reflect.TypeOf(uint16(0))
	case api.ColumnTypeInteger:
		return reflect.TypeOf(int32(0))
	case api.ColumnTypeUInteger:
		return reflect.TypeOf(uint32(0))
	case api.ColumnTypeLong:
		return reflect.TypeOf(int64(0))
	case api.ColumnTypeULong:
		return reflect.TypeOf(uint64(0))
	case api.ColumnTypeFloat:
		return reflect.TypeOf(float32(0))
	case api.ColumnTypeDouble:
		return reflect.TypeOf(float64(0))
	case api.ColumnTypeDatetime:
		return reflect.TypeOf(time.Time{})
	case api.ColumnTypeDecimal:
		return reflect.TypeOf("")
	case api.ColumnTypeBinary, api.ColumnTypeBlob, api.ColumnTypeClob:
		return reflect.TypeOf([]byte(nil))
	case api.ColumnTypeIPv4, api.ColumnTypeIPv6:
		return reflect.TypeOf(net.IP(nil))
	case api.ColumnTypeVarchar, api.ColumnTypeText:
		return reflect.TypeOf("")
	case api.ColumnTypeJSON:
		return reflect.TypeOf(api.JSONString(""))
	default:
		switch col.DataType {
		case api.DataTypeInt16:
			return reflect.TypeOf(int16(0))
		case api.DataTypeUInt16:
			return reflect.TypeOf(uint16(0))
		case api.DataTypeInt32:
			return reflect.TypeOf(int32(0))
		case api.DataTypeUInt32:
			return reflect.TypeOf(uint32(0))
		case api.DataTypeInt64:
			return reflect.TypeOf(int64(0))
		case api.DataTypeUInt64:
			return reflect.TypeOf(uint64(0))
		case api.DataTypeFloat32:
			return reflect.TypeOf(float32(0))
		case api.DataTypeFloat64:
			return reflect.TypeOf(float64(0))
		case api.DataTypeDatetime:
			return reflect.TypeOf(time.Time{})
		case api.DataTypeBinary:
			return reflect.TypeOf([]byte(nil))
		case api.DataTypeIPv4, api.DataTypeIPv6:
			return reflect.TypeOf(net.IP(nil))
		case api.DataTypeString:
			return reflect.TypeOf("")
		case api.DataTypeJSON:
			return reflect.TypeOf(api.JSONString(""))
		default:
			return reflect.TypeOf(new(any)).Elem()
		}
	}
}

func (r *Rows) column(index int) (*Column, bool) {
	if r == nil || index < 0 || index >= len(r.columns) {
		return nil, false
	}
	return r.columns[index], true
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func namedValuesToAny(args []driver.NamedValue) ([]any, error) {
	vals := make([]any, len(args))
	for i := range args {
		arg := args[i]
		if err := checkNamedValue(&arg); err != nil {
			return nil, err
		}
		if arg.Name != "" {
			vals[i] = Named(arg.Name, arg.Value)
		} else {
			vals[i] = arg.Value
		}
	}
	return vals, nil
}

func valuesToAny(args []driver.Value) []any {
	vals := make([]any, len(args))
	for i := range args {
		vals[i] = args[i]
	}
	return vals
}

func checkNamedValue(nv *driver.NamedValue) error {
	if nv == nil {
		return nil
	}
	value, err := normalizeNamedValue(nv.Value)
	if err != nil {
		return err
	}
	nv.Value = value
	return nil
}

func normalizeNamedValue(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case int:
		if v > math.MaxInt32 || v < math.MinInt32 {
			return int64(v), nil
		}
		return v, nil
	case int16, *int16, int32, *int32, int64, *int64, float32, *float32, float64, *float64, string, *string, []byte, time.Time, *time.Time, net.IP, api.Decimal, *api.Decimal:
		return v, nil
	case *int:
		if v == nil {
			return nil, nil
		}
		if *v > math.MaxInt32 || *v < math.MinInt32 {
			return int64(*v), nil
		}
		return *v, nil
	case bool:
		return nil, fmt.Errorf("machbase does not support bool parameter type")
	case driver.Valuer:
		resolved, err := v.Value()
		if err != nil {
			return nil, err
		}
		return normalizeNamedValue(resolved)
	case uint:
		if uint64(v) > math.MaxInt64 {
			return nil, fmt.Errorf("uint value %d overflows int64", v)
		}
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return nil, fmt.Errorf("uint64 value %d overflows int64", v)
		}
		return int64(v), nil
	case *uint:
		if v == nil {
			return nil, nil
		}
		return normalizeNamedValue(*v)
	case *uint8:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint16:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint32:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint64:
		if v == nil {
			return nil, nil
		}
		return normalizeNamedValue(*v)
	default:
		return nil, fmt.Errorf("machbase does not support parameter type %T", value)
	}
}

func normalizeError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection closed") ||
		strings.Contains(msg, "invalid connection") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "eof") {
		return driver.ErrBadConn
	}
	return err
}

func toDriverValue(value any) (driver.Value, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case *bool:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case *int16:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint16:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *int32:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint32:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *int64:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case *uint64:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *float32:
		if v == nil {
			return nil, nil
		}
		return float64(*v), nil
	case *float64:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case *string:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case *[]byte:
		if v == nil {
			return nil, nil
		}
		buf := make([]byte, len(*v))
		copy(buf, *v)
		return buf, nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case time.Time:
		return v, nil
	case *net.IP:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	case net.IP:
		return v, nil
	case api.Decimal:
		return v.String(), nil
	case *api.Decimal:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	case int:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		return v, nil
	case []byte:
		buf := make([]byte, len(v))
		copy(buf, v)
		return buf, nil
	default:
		return nil, fmt.Errorf("machbase cannot convert row value %T to driver.Value", value)
	}
}

const MetaKey = "machbase:meta"

type Meta struct {
	cbMessage   func() string
	cbFetchable func() bool
	cbIOMetrics func(reset bool) (readBytes uint64, writtenBytes uint64, enabled bool)
}

func (m *Meta) Message() string {
	if m == nil || m.cbMessage == nil {
		return ""
	}
	return m.cbMessage()
}

func (m *Meta) IsFetchable() bool {
	if m == nil || m.cbFetchable == nil {
		return false
	}
	return m.cbFetchable()
}

// Use dsn parameter "io_metrics=1" to enable I/O metrics collection. When enabled, the driver will track the number of bytes read and written for each connection. You can retrieve these metrics using the IOMetrics method on the Conn type.
func (m *Meta) IOMetrics(reset bool) (readBytes uint64, writtenBytes uint64, enabled bool) {
	if m.cbIOMetrics != nil {
		return m.cbIOMetrics(reset)
	}
	return 0, 0, false
}

// NamedParam represents one named bind argument.
type NamedParam struct {
	Name  string
	Value any
}

// Named creates a named bind argument for native APIs.
func Named(name string, value any) NamedParam {
	return NamedParam{Name: name, Value: value}
}
