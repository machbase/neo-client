package machbase

import (
	"context"
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
	"time"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machgo"
)

const (
	DefaultDriverName = "machbase"
	defaultPort       = 5656
)

var errTransactionsUnsupported = errors.New("machbase does not support explicit transactions")

func init() {
	sql.Register(DefaultDriverName, &Driver{})
}

type Config struct {
	Host            string
	Port            int
	User            string
	Password        string
	AuthMode        string
	AuthKeyFile     string
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
	if cfg.Password == "" && (authMode == "" || authMode == "PASSWORD") {
		return errors.New("machbase dsn requires password")
	}
	if authMode == "CHALLENGE" && strings.TrimSpace(cfg.AuthKeyFile) == "" {
		return errors.New("machbase dsn requires auth_key_file for auth_mode=CHALLENGE")
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
		MaxOpenConn:     -1,
		StatementCache:  cfg.StatementCache,
		FetchRows:       cfg.FetchRows,
		AuthMode:        cfg.AuthMode,
		AuthKeyFile:     cfg.AuthKeyFile,
		AuthSigScheme:   cfg.AuthSigScheme,
	}
}

type Driver struct {
	Host            string
	Port            int
	User            string
	Password        string
	AuthMode        string
	AuthKeyFile     string
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
		AuthMode:        drv.AuthMode,
		AuthKeyFile:     drv.AuthKeyFile,
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
		api.WithStatementCache(cn.cfg.StatementCache),
		api.WithFetchRows(cn.cfg.FetchRows),
		api.WithIOMetrics(cn.cfg.IOMetrics),
	}
	if strings.TrimSpace(cn.cfg.AuthKeyFile) != "" || strings.EqualFold(strings.TrimSpace(cn.cfg.AuthMode), "CHALLENGE") {
		opts = append(opts, api.WithAuthKeyFile(cn.cfg.User, cn.cfg.AuthKeyFile))
	} else {
		opts = append(opts, api.WithPassword(cn.cfg.User, cn.cfg.Password))
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
	return &Conn{connector: cn, conn: concrete}, nil
}

func (cn *Connector) Driver() driver.Driver {
	if cn == nil {
		return nil
	}
	return cn.driver
}

type Conn struct {
	connector *Connector
	conn      *machgo.Conn
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

func (c *Conn) Begin() (driver.Tx, error) {
	return nil, errTransactionsUnsupported
}

func (c *Conn) BeginTx(context.Context, driver.TxOptions) (driver.Tx, error) {
	return nil, errTransactionsUnsupported
}

func (c *Conn) Ping(context.Context) error {
	if c == nil || c.conn == nil {
		return driver.ErrBadConn
	}
	return nil
}

func (c *Conn) ResetSession(context.Context) error {
	if c == nil || c.conn == nil {
		return driver.ErrBadConn
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
	return &Result{result: result}, nil
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
	desc    []machgo.ColumnDesc
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
	if provider, ok := rows.(interface{ ColumnDescriptions() []machgo.ColumnDesc }); ok {
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
	provider, ok := r.rows.(interface{ Row() []any })
	if !ok {
		return errors.New("rows does not expose current row")
	}
	row := provider.Row()
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
	switch col.Type {
	case api.ColumnTypeVarchar, api.ColumnTypeText, api.ColumnTypeClob, api.ColumnTypeBlob, api.ColumnTypeBinary, api.ColumnTypeJSON:
		if col.Length <= 0 {
			return 0, false
		}
		return int64(col.Length), true
	default:
		return 0, false
	}
}

func (r *Rows) ColumnTypeNullable(index int) (nullable, ok bool) {
	col, exists := r.column(index)
	if !exists {
		return false, false
	}
	return col.Nullable, true
}

func (r *Rows) ColumnTypePrecisionScale(index int) (precision, scale int64, ok bool) {
	if index < 0 || index >= len(r.desc) {
		return 0, 0, false
	}
	desc := r.desc[index]
	switch desc.Type {
	case machgo.MACHCLI_SQL_TYPE_FLOAT, machgo.MACHCLI_SQL_TYPE_DOUBLE:
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
	case api.ColumnTypeShort, api.ColumnTypeUShort, api.ColumnTypeInteger, api.ColumnTypeUInteger, api.ColumnTypeLong, api.ColumnTypeULong:
		return reflect.TypeOf(int64(0))
	case api.ColumnTypeFloat, api.ColumnTypeDouble:
		return reflect.TypeOf(float64(0))
	case api.ColumnTypeDatetime:
		return reflect.TypeOf(time.Time{})
	case api.ColumnTypeBinary, api.ColumnTypeBlob, api.ColumnTypeClob:
		return reflect.TypeOf([]byte(nil))
	case api.ColumnTypeIPv4, api.ColumnTypeIPv6, api.ColumnTypeVarchar, api.ColumnTypeText, api.ColumnTypeJSON:
		return reflect.TypeOf("")
	default:
		switch col.DataType {
		case api.DataTypeInt16, api.DataTypeInt32, api.DataTypeInt64:
			return reflect.TypeOf(int64(0))
		case api.DataTypeFloat32, api.DataTypeFloat64:
			return reflect.TypeOf(float64(0))
		case api.DataTypeDatetime:
			return reflect.TypeOf(time.Time{})
		case api.DataTypeBinary:
			return reflect.TypeOf([]byte(nil))
		case api.DataTypeIPv4, api.DataTypeIPv6, api.DataTypeString:
			return reflect.TypeOf("")
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

func ParseDSN(dsn string) (Config, error) {
	var cfg Config
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return cfg, nil
	}
	if strings.Contains(dsn, "=") {
		return parseKeyValueDSN(dsn)
	}
	if strings.Contains(dsn, "://") {
		if err := applyServerValue(&cfg, dsn); err != nil {
			return Config{}, err
		}
		return cfg.normalize(), nil
	}
	if err := applyServerValue(&cfg, dsn); err != nil {
		return Config{}, err
	}
	return cfg.normalize(), nil
}

func parseKeyValueDSN(dsn string) (Config, error) {
	var cfg Config
	parts := strings.Split(dsn, ";")
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
			cfg.User = value
		case "password", "pwd":
			cfg.Password = value
		case "auth_mode":
			cfg.AuthMode = value
		case "auth_key_file":
			cfg.AuthKeyFile = value
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
	if override.Password != "" {
		base.Password = override.Password
	}
	if override.AuthMode != "" {
		base.AuthMode = override.AuthMode
	}
	if override.AuthKeyFile != "" {
		base.AuthKeyFile = override.AuthKeyFile
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

func namedValuesToAny(args []driver.NamedValue) ([]any, error) {
	vals := make([]any, len(args))
	for i := range args {
		arg := args[i]
		if err := checkNamedValue(&arg); err != nil {
			return nil, err
		}
		vals[i] = arg.Value
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
	if nv.Name != "" {
		return fmt.Errorf("machbase does not support named parameters: %s", nv.Name)
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
	case int16, *int16, int32, *int32, int64, *int64, float32, *float32, float64, *float64, string, *string, []byte, time.Time, *time.Time, net.IP:
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
	case time.Time:
		return v, nil
	case net.IP:
		return v.String(), nil
	default:
		return nil, fmt.Errorf("machbase cannot convert row value %T to driver.Value", value)
	}
}
