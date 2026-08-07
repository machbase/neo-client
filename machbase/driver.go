package machbase

import (
	"context"
	"crypto"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machgo"
)

const (
	DefaultDriverName = "machbase"
	defaultPort       = 5656
)

func init() {
	sql.Register(DefaultDriverName, &Driver{})
}

type Config struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	ProxyUser       string
	AuthMode        string
	AuthKeyFile     string
	AuthKeyPEM      string
	AuthSigScheme   string
	AlternativeHost string
	AlternativePort int
	FetchRows       int64
	StatementCache  api.StatementCacheMode
	IOMetrics       bool

	statementCacheSet bool
}

func (cfg Config) normalize() Config {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	return cfg
}

func (cfg Config) validate() error {
	if cfg.Host == "" {
		return errors.New("machbase dsn requires host or server")
	}
	if cfg.User == "" {
		return errors.New("machbase dsn requires user")
	}
	authMode := strings.ToUpper(strings.TrimSpace(cfg.AuthMode))
	hasAuthKey := strings.TrimSpace(cfg.AuthKeyFile) != "" || strings.TrimSpace(cfg.AuthKeyPEM) != ""
	if cfg.Password == "" && authMode == "PASSWORD" {
		return errors.New("machbase dsn requires password")
	}
	if cfg.Password == "" && authMode == "" && !hasAuthKey {
		return errors.New("machbase dsn requires password")
	}
	if authMode == "CHALLENGE" && strings.TrimSpace(cfg.AuthKeyFile) == "" && strings.TrimSpace(cfg.AuthKeyPEM) == "" {
		return errors.New("machbase dsn requires auth_key_file or auth_key_pem for auth_mode=CHALLENGE")
	}
	if cfg.Port <= 0 {
		return fmt.Errorf("machbase dsn has invalid port %d", cfg.Port)
	}
	return nil
}

func (cfg Config) machgoConfig() *machgo.Config {
	return &machgo.Config{
		Host:            cfg.Host,
		Port:            cfg.Port,
		AlternativeHost: cfg.AlternativeHost,
		AlternativePort: cfg.AlternativePort,
		StatementCache:  cfg.StatementCache,
		FetchRows:       cfg.FetchRows,
	}
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
	StatementCache  api.StatementCacheMode
	IOMetrics       bool
}

var _ driver.Driver = (*Driver)(nil)
var _ driver.DriverContext = (*Driver)(nil)

func (drv *Driver) baseConfig() Config {
	statementCache := drv.StatementCache
	if statementCache != api.StatementCacheOn && statementCache != api.StatementCacheOff {
		statementCache = api.StatementCacheAuto
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
	db, err := machgo.NewDatabase(effective.machgoConfig())
	if err != nil {
		return nil, err
	}
	return &Connector{driver: drv, cfg: effective, db: db}, nil
}

type Connector struct {
	driver *Driver
	cfg    Config
	db     *machgo.Database
}

var _ driver.Connector = (*Connector)(nil)

func (cn *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	if cn == nil || cn.db == nil {
		return nil, driver.ErrBadConn
	}
	opts := []api.ConnectOption{
		api.WithIOMetrics(cn.cfg.IOMetrics),
	}
	if cn.cfg.statementCacheSet {
		opts = append(opts, api.WithStatementCache(cn.cfg.StatementCache))
	}
	if cn.cfg.FetchRows > 0 {
		opts = append(opts, api.WithFetchRows(cn.cfg.FetchRows))
	}
	if strings.TrimSpace(cn.cfg.AuthKeyFile) != "" || strings.TrimSpace(cn.cfg.AuthKeyPEM) != "" || strings.EqualFold(strings.TrimSpace(cn.cfg.AuthMode), "CHALLENGE") {
		var key crypto.PrivateKey
		var err error
		if strings.TrimSpace(cn.cfg.AuthKeyPEM) != "" {
			key, err = machgo.LoadPrivateKeyFromPEM([]byte(cn.cfg.AuthKeyPEM))
		} else {
			key, err = machgo.LoadPrivateKeyFromFile(cn.cfg.AuthKeyFile)
		}
		if err != nil {
			return nil, err
		}
		opts = append(opts, api.WithAuthKey(cn.cfg.User, key))
	} else {
		opts = append(opts, api.WithPassword(cn.cfg.User, cn.cfg.Password))
	}
	if cn.cfg.ProxyUser != "" && cn.cfg.User != cn.cfg.ProxyUser {
		opts = append(opts, api.WithProxyUser(cn.cfg.ProxyUser))
	}
	if strings.TrimSpace(cn.cfg.Database) != "" {
		opts = append(opts, api.WithDatabase(cn.cfg.Database))
	}
	conn, err := cn.db.Connect(ctx, opts...)
	if err != nil {
		return nil, normalizeError(err)
	}
	concrete, ok := conn.(*machgo.Conn)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected connection type %T", conn)
	}
	return &Conn{connector: cn, conn: concrete, database: cn.cfg.Database}, nil
}

func (cn *Connector) Driver() driver.Driver {
	if cn == nil {
		return nil
	}
	return cn.driver
}

type ConnectOptionsProvider func(context.Context) ([]api.ConnectOption, error)

type DatabaseConnector struct {
	driver          *Driver
	db              api.Database
	optionsProvider ConnectOptionsProvider
}

var _ driver.Connector = (*DatabaseConnector)(nil)

func NewDatabaseConnector(db api.Database, optionsProvider ConnectOptionsProvider) (*DatabaseConnector, error) {
	if db == nil {
		return nil, errors.New("database is nil")
	}
	return &DatabaseConnector{
		driver:          &Driver{},
		db:              db,
		optionsProvider: optionsProvider,
	}, nil
}

func OpenDBWithConnector(db api.Database, optionsProvider ConnectOptionsProvider) (*sql.DB, error) {
	cn, err := NewDatabaseConnector(db, optionsProvider)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(cn), nil
}

func (cn *DatabaseConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if cn == nil || cn.db == nil {
		return nil, driver.ErrBadConn
	}
	var opts []api.ConnectOption
	if cn.optionsProvider != nil {
		provided, err := cn.optionsProvider(ctx)
		if err != nil {
			return nil, err
		}
		opts = provided
	}
	conn, err := cn.db.Connect(ctx, opts...)
	if err != nil {
		return nil, normalizeError(err)
	}
	return &Conn{connector: nil, conn: conn, database: connectOptionDatabase(opts)}, nil
}

func (cn *DatabaseConnector) Driver() driver.Driver {
	if cn == nil {
		return nil
	}
	return cn.driver
}

type Conn struct {
	connector *Connector
	conn      api.Conn
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

func (c *Conn) Close() error {
	if c == nil || c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return normalizeError(err)
}

func (c *Conn) Appender(ctx context.Context, tableName string, opts ...api.AppenderOption) (api.Appender, error) {
	return c.conn.Appender(ctx, tableName, opts...)
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

func (c *Conn) ResetSession(ctx context.Context) error {
	if c == nil || c.conn == nil {
		return driver.ErrBadConn
	}
	c.txMu.Lock()
	inTx := c.inTx
	c.txMu.Unlock()
	if inTx {
		result := c.conn.Exec(ctx, "ROLLBACK")
		if err := normalizeError(result.Err()); err != nil {
			_ = c.Close()
			return driver.ErrBadConn
		}
		c.setTransactionState("ROLLBACK")
	}
	c.txMu.Lock()
	database := c.database
	dbDirty := c.dbDirty
	c.txMu.Unlock()
	if dbDirty && strings.TrimSpace(database) != "" {
		result := c.conn.Exec(ctx, "USE "+quoteIdentifier(database))
		if err := normalizeError(result.Err()); err != nil {
			_ = c.Close()
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
	return newRows(rows)
}

type Stmt struct {
	conn  *Conn
	stmt  api.Stmt
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
	return newRows(rows)
}

type Result struct {
	result api.Result
}

var _ driver.Result = (*Result)(nil)

func (r *Result) LastInsertId() (int64, error) {
	return 0, api.ErrNotImplemented("LastInsertId")
}

func (r *Result) RowsAffected() (int64, error) {
	if r == nil || r.result == nil {
		return 0, nil
	}
	return r.result.RowsAffected(), nil
}

type Rows struct {
	rows    api.Rows
	columns api.Columns
	desc    []api.ColumnDesc
	buffer  []any
}

var _ driver.Rows = (*Rows)(nil)
var _ driver.RowsColumnTypeDatabaseTypeName = (*Rows)(nil)
var _ driver.RowsColumnTypeLength = (*Rows)(nil)
var _ driver.RowsColumnTypeNullable = (*Rows)(nil)
var _ driver.RowsColumnTypePrecisionScale = (*Rows)(nil)
var _ driver.RowsColumnTypeScanType = (*Rows)(nil)

func newRows(rows api.Rows) (*Rows, error) {
	cols, err := rows.Columns()
	if err != nil {
		_ = rows.Close()
		return nil, normalizeError(err)
	}
	ret := &Rows{rows: rows, columns: cols}
	if provider, ok := rows.(interface{ ColumnDescriptions() []api.ColumnDesc }); ok {
		ret.desc = provider.ColumnDescriptions()
	}
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
	var row []any
	if provider, ok := r.rows.(interface{ Row() []any }); ok {
		row = provider.Row()
	} else {
		if r.buffer == nil {
			buf, err := r.columns.MakeBuffer()
			if err != nil {
				return normalizeError(err)
			}
			r.buffer = buf
		}
		if err := r.rows.Scan(r.buffer...); err != nil {
			return normalizeError(err)
		}
		row = r.buffer
	}
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

func (r *Rows) column(index int) (*api.Column, bool) {
	if r == nil || index < 0 || index >= len(r.columns) {
		return nil, false
	}
	return r.columns[index], true
}

// ParseDSN parses a Machbase DSN string and returns connection config.
//
// Supported syntax:
//
//  1. Server value only
//     - host
//     - host:port
//     - tcp://user:password@host:port/database?as=proxy&fetch_rows=100
//
//  2. Key-value pairs separated by semicolon
//     - key=value;key=value;...
//     - Example: user=sys;password=manager;host=127.0.0.1;port=5656
//
// For key-value syntax, value may be quoted with single or double quotes.
// A semicolon inside quotes is treated as a literal character, not a separator.
// Quotes can be escaped inside quoted values with backslash.
// Examples:
//   - user="sys as demo";password="12;34";host=127.0.0.1;
//   - user='sys as demo';password='12;34';host=127.0.0.1;
//   - password="a\"b";password2='a\'b';
//   - auth_mode=challenge;user=sys;auth_key_pem="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----";
func ParseDSN(dsn string) (Config, error) {
	var cfg Config
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return cfg, nil
	}
	scheme := strings.Index(dsn, "://")
	separator := strings.IndexByte(dsn, '=')
	if scheme >= 0 && (separator < 0 || separator > scheme) {
		if err := applyServerValue(&cfg, dsn); err != nil {
			return Config{}, err
		}
		return cfg.normalize(), nil
	}
	if strings.Contains(dsn, "=") {
		return parseKeyValueDSN(dsn)
	}
	if err := applyServerValue(&cfg, dsn); err != nil {
		return Config{}, err
	}
	return cfg.normalize(), nil
}

func parseKeyValueDSN(dsn string) (Config, error) {
	var cfg Config
	parts, err := splitDSNSegments(dsn)
	if err != nil {
		return Config{}, err
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return Config{}, fmt.Errorf("invalid dsn segment %q", part)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		value, err = unquoteDSNValue(value)
		if err != nil {
			return Config{}, fmt.Errorf("invalid value for %q: %w", key, err)
		}
		switch key {
		case "server":
			if err := applyServerValue(&cfg, value); err != nil {
				return Config{}, err
			}
		case "host":
			cfg.Host = value
		case "port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid port %q", value)
			}
			cfg.Port = port
		case "user", "uid":
			username, proxyed := api.ParseUserName(value)
			cfg.User = username.Login
			if proxyed && username.Proxy != "" {
				cfg.ProxyUser = username.Proxy
			}
		case "password", "pwd":
			cfg.Password = value
		case "database", "db":
			cfg.Database = value
		case "auth_mode":
			cfg.AuthMode = value
		case "auth_key_file":
			cfg.AuthKeyFile = value
		case "auth_key_pem":
			cfg.AuthKeyPEM = value
		case "auth_sig_scheme":
			cfg.AuthSigScheme = value
		case "fetch_rows", "fetchrows":
			rows, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return Config{}, fmt.Errorf("invalid fetch_rows %q", value)
			}
			cfg.FetchRows = rows
		case "statement_cache", "statementcache":
			mode, err := parseStatementCacheMode(value)
			if err != nil {
				return Config{}, err
			}
			cfg.StatementCache = mode
			cfg.statementCacheSet = true
		case "io_metrics", "iometrics":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid io_metrics %q", value)
			}
			cfg.IOMetrics = enabled
		case "alternative_servers":
			if err := applyAlternativeServers(&cfg, value); err != nil {
				return Config{}, err
			}
		case "alternative_host":
			cfg.AlternativeHost = value
		case "alternative_port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid alternative_port %q", value)
			}
			cfg.AlternativePort = port
		default:
			return Config{}, fmt.Errorf("unsupported dsn key %q", key)
		}
	}
	return cfg.normalize(), nil
}

func splitDSNSegments(dsn string) ([]string, error) {
	parts := make([]string, 0)
	var current strings.Builder
	var quote rune
	escaped := false

	for _, ch := range dsn {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			current.WriteRune(ch)
			if quote != 0 {
				escaped = true
			}
		case '\'', '"':
			switch quote {
			case 0:
				quote = ch
			case ch:
				quote = 0
			}
			current.WriteRune(ch)
		case ';':
			if quote != 0 {
				current.WriteRune(ch)
				continue
			}
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}

	if quote != 0 {
		return nil, errors.New("unterminated quoted value")
	}
	if escaped {
		return nil, errors.New("unterminated escape in quoted value")
	}
	parts = append(parts, current.String())
	return parts, nil
}

func unquoteDSNValue(value string) (string, error) {
	if len(value) < 2 {
		return value, nil
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		quote := rune(value[0])
		content := value[1 : len(value)-1]
		var out strings.Builder
		escaped := false
		for _, ch := range content {
			if escaped {
				if ch == quote || ch == '\\' {
					out.WriteRune(ch)
				} else {
					out.WriteRune('\\')
					out.WriteRune(ch)
				}
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			out.WriteRune(ch)
		}
		if escaped {
			return "", errors.New("unterminated escape sequence")
		}
		return out.String(), nil
	}
	if value[0] == '"' || value[0] == '\'' || value[len(value)-1] == '"' || value[len(value)-1] == '\'' {
		return "", errors.New("mismatched quotes")
	}
	return value, nil
}

func parseStatementCacheMode(value string) (api.StatementCacheMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return api.StatementCacheAuto, nil
	case "on", "true", "1":
		return api.StatementCacheOn, nil
	case "off", "false", "0":
		return api.StatementCacheOff, nil
	default:
		return api.StatementCacheAuto, fmt.Errorf("invalid statement_cache %q", value)
	}
}

func applyServerValue(cfg *Config, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("server value is empty")
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return fmt.Errorf("invalid server %q", value)
		}
		if u.Host == "" {
			return fmt.Errorf("invalid server %q", value)
		}
		cfg.Host = u.Hostname()
		if port := u.Port(); port != "" {
			parsedPort, err := strconv.Atoi(port)
			if err != nil {
				return fmt.Errorf("invalid server port %q", port)
			}
			cfg.Port = parsedPort
		}
		if u.User != nil {
			if user := u.User.Username(); user != "" {
				cfg.User = user
			}
			if pass, ok := u.User.Password(); ok {
				cfg.Password = pass
			}
		}
		if u.Path != "" && u.Path != "/" {
			cfg.Database = strings.TrimPrefix(u.Path, "/")
		}
		for key, values := range u.Query() {
			switch strings.ToLower(key) {
			case "as":
				if len(values) > 0 {
					cfg.ProxyUser = values[0]
				}
			case "database", "db":
				if len(values) > 0 {
					cfg.Database = values[0]
				}
			case "auth_mode":
				cfg.AuthMode = values[0]
			case "auth_key_file":
				cfg.AuthKeyFile = values[0]
			case "auth_key_pem":
				cfg.AuthKeyPEM = values[0]
			case "auth_sig_scheme":
				cfg.AuthSigScheme = values[0]
			case "fetch_rows", "fetchrows":
				rows, err := strconv.ParseInt(values[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid fetch_rows %q", values[0])
				}
				cfg.FetchRows = rows
			case "statement_cache", "statementcache":
				mode, err := parseStatementCacheMode(values[0])
				if err != nil {
					return err
				}
				cfg.StatementCache = mode
				cfg.statementCacheSet = true
			case "io_metrics", "iometrics":
				enabled, err := strconv.ParseBool(values[0])
				if err != nil {
					return fmt.Errorf("invalid io_metrics %q", values[0])
				}
				cfg.IOMetrics = enabled
			case "alternative_servers":
				if err := applyAlternativeServers(cfg, values[0]); err != nil {
					return err
				}
			case "alternative_host":
				cfg.AlternativeHost = values[0]
			case "alternative_port":
				port, err := strconv.Atoi(values[0])
				if err != nil {
					return fmt.Errorf("invalid alternative_port %q", values[0])
				}
				cfg.AlternativePort = port
			}
		}
		return nil
	}
	host, port, err := net.SplitHostPort(value)
	if err == nil {
		cfg.Host = host
		parsedPort, convErr := strconv.Atoi(port)
		if convErr != nil {
			return fmt.Errorf("invalid server port %q", port)
		}
		cfg.Port = parsedPort
		return nil
	}
	if strings.Contains(err.Error(), "missing port in address") {
		cfg.Host = value
		return nil
	}
	return fmt.Errorf("invalid server %q", value)
}

func applyAlternativeServers(cfg *Config, value string) error {
	entries := strings.Split(value, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		host, port, err := net.SplitHostPort(entry)
		if err != nil {
			return fmt.Errorf("invalid alternative server %q", entry)
		}
		parsedPort, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("invalid alternative server port %q", port)
		}
		cfg.AlternativeHost = host
		cfg.AlternativePort = parsedPort
		return nil
	}
	return nil
}

func mergeConfig(base Config, override Config) Config {
	if override.Host != "" {
		base.Host = override.Host
	}
	if override.Port != 0 {
		base.Port = override.Port
	}
	if override.User != "" {
		base.User = override.User
	}
	if override.ProxyUser != "" {
		base.ProxyUser = override.ProxyUser
	}
	if override.Password != "" {
		base.Password = override.Password
	}
	if override.Database != "" {
		base.Database = override.Database
	}
	if override.AuthMode != "" {
		base.AuthMode = override.AuthMode
	}
	if override.AuthKeyFile != "" {
		base.AuthKeyFile = override.AuthKeyFile
	}
	if override.AuthKeyPEM != "" {
		base.AuthKeyPEM = override.AuthKeyPEM
	}
	if override.AuthSigScheme != "" {
		base.AuthSigScheme = override.AuthSigScheme
	}
	if override.AlternativeHost != "" {
		base.AlternativeHost = override.AlternativeHost
	}
	if override.AlternativePort != 0 {
		base.AlternativePort = override.AlternativePort
	}
	if override.FetchRows != 0 {
		base.FetchRows = override.FetchRows
	}
	if override.statementCacheSet {
		base.StatementCache = override.StatementCache
	}
	if override.IOMetrics {
		base.IOMetrics = true
	}
	return base
}

func connectOptionDatabase(opts []api.ConnectOption) string {
	for _, opt := range opts {
		if database, ok := opt.(*api.ConnectOptionDatabase); ok {
			return database.Database
		}
	}
	return ""
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
			vals[i] = api.Named(arg.Name, arg.Value)
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
