package client

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/machbase/neo-client/v2/api"
	"github.com/machbase/neo-client/v2/machnet"
)

const (
	DefaultDriverName = "machbase"
)

func init() {
	sql.Register(DefaultDriverName, &Driver{})
}

type Driver struct {
}

var _ driver.Driver = (*Driver)(nil)
var _ driver.DriverContext = (*Driver)(nil)

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
	if err := cfg.normalize().validate(); err != nil {
		return nil, err
	}

	var handle *machnet.EnvHandle
	if h, err := machnet.Initialize(); err != nil {
		return nil, err
	} else {
		handle = h
	}
	return &Connector{
		driver: drv,
		cfg:    cfg,
		handle: handle,
	}, nil
}

type Connector struct {
	driver *Driver
	cfg    *Config
	handle *machnet.EnvHandle
}

var _ driver.Connector = (*Connector)(nil)
var _ io.Closer = (*Connector)(nil)

func (cn *Connector) Connect(ctx context.Context) (driver.Conn, error) {
	if cn == nil || cn.handle == nil {
		return nil, driver.ErrBadConn
	}
	cfg := machnet.ConnConfig{
		Host:               cn.cfg.Host,
		Port:               cn.cfg.Port,
		User:               strings.ToUpper(cn.cfg.loginName),
		Password:           cn.cfg.Password,
		AuthMode:           cn.cfg.AuthMode,
		AuthSigScheme:      cn.cfg.AuthSigScheme,
		AlternativeServers: cn.cfg.AlternativeServers,
		FetchRows:          cn.cfg.FetchRows,
		TrackIOBytes:       cn.cfg.IOMetrics,
		PrivateKey:         cn.cfg.key,
	}
	var handle *machnet.ConnHandle
	if c, err := cn.handle.Connect(cfg); err != nil {
		return nil, normalizeError(cn.ErrorOf(err))
	} else {
		handle = c
	}
	ret := &Conn{
		cn:                     cn,
		database:               cn.cfg.Database,
		handle:                 handle,
		user:                   strings.ToUpper(cn.cfg.loginName),
		usedAt:                 time.Now(),
		timeLocation:           cn.cfg.TimeLocation,
		queryStmtReuseMode:     cn.cfg.StatementCache,
		queryStmtPool:          map[string][]*Stmt{},
		queryStmtPoolCap:       defaultQueryStmtPoolCap,
		queryStmtPoolPerKeyCap: defaultQueryStmtPoolPerQueryCap,
	}
	if strings.TrimSpace(cn.cfg.Database) != "" && !strings.EqualFold(cn.cfg.Database, defaultDatabase) {
		if err := ret.exec(ctx, "USE "+QuoteIdentifier(cn.cfg.Database)).Err(); err != nil {
			return nil, errors.Join(err, ret.Close())
		}
	}
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok && meta != nil {
		meta.cbIOMetrics = ret.IOMetrics
	}
	return ret, nil
}

func (cn *Connector) connectionString() string {
	entries := []string{
		fmt.Sprintf("SERVER=%s", cn.cfg.Host),
		fmt.Sprintf("PORT_NO=%d", cn.cfg.Port),
		fmt.Sprintf("UID=%s", strings.ToUpper(cn.cfg.loginName)),
		fmt.Sprintf("PWD=%s", cn.cfg.Password),
		"CONNTYPE=1",
		fmt.Sprintf("FETCH_ROWS=%d", cn.cfg.FetchRows),
	}
	if cn.cfg.IOMetrics {
		entries = append(entries, "IO_METRICS=1")
	}
	if strings.TrimSpace(cn.cfg.AuthMode) != "" {
		entries = append(entries, fmt.Sprintf("AUTH_MODE=%s", cn.cfg.AuthMode))
	}
	if len(cn.cfg.AlternativeServers) > 0 {
		entries = append(entries,
			fmt.Sprintf("ALTERNATIVE_SERVERS=%s", strings.Join(cn.cfg.AlternativeServers, ",")))
	}
	return strings.Join(entries, ";")
}

func (cn *Connector) Driver() driver.Driver {
	if cn == nil {
		return nil
	}
	return cn.driver
}

// If a Connector implements [io.Closer], the [database/sql.DB.Close]
// method will call the Close method and return error (if any).
func (cn *Connector) Close() error {
	if cn == nil || cn.handle == nil {
		return nil
	}
	if err := cn.handle.Finalize(); err == nil {
		cn.handle = nil
		return nil
	} else {
		return err
	}
}

func (cn *Connector) ErrorOf(cause error) error {
	code, msg := cn.handle.Error()
	return formatMachcliError(code, msg, cause)
}

// testResetSessionConn is a seam for unit-testing ResetSession without a real machnet.ConnHandle.
type testResetSessionConn interface {
	Close() error
	Exec(context.Context, string, ...any) *Result
}

type Conn struct {
	handle *machnet.ConnHandle
	cn     *Connector

	user      string
	usedAt    time.Time
	usedCount int64
	closeOnce sync.Once
	// sessionMu serializes Conn-level operations that talk to the server
	// (Exec/Query/Prepare/Close/Explain). Lock order: sessionMu is always
	// acquired before txMu (see setTransactionState, called while
	// QueryContext still holds sessionMu). Never acquire sessionMu while
	// already holding txMu or queryStmtPoolMu.
	sessionMu sync.Mutex
	// txMu guards inTx/database/dbDirty only; it is always acquired and
	// released on its own (see setTransactionState, ResetSession) and never
	// held while calling back into a function that needs sessionMu.
	txMu     sync.Mutex
	inTx     bool
	database string
	dbDirty  bool

	timeLocation           *time.Location
	queryStmtReuseMode     StatementCacheMode
	queryStmtPoolMu        sync.Mutex
	queryStmtFastKey       string
	queryStmtFast          *Stmt
	queryStmtPool          map[string][]*Stmt
	queryStmtPoolCount     int
	queryStmtPoolCap       int
	queryStmtPoolPerKeyCap int
	catalogGeneration      atomic.Uint64

	testResetConn testResetSessionConn // only set by tests; nil in production
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

func (c *Conn) SupportDatabaseMetadata() bool {
	if c == nil || c.handle == nil {
		return false
	}
	return c.handle.SupportsDatabaseMetadata()
}

func (c *Conn) Explain(ctx context.Context, query string, full bool) (string, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	if c == nil || c.handle == nil {
		return "", driver.ErrBadConn
	}
	if full {
		query = "explain full " + query
	} else {
		query = "explain " + query
	}

	var stmt *machnet.StmtHandle
	if s, err := c.handle.AllocStmt(); err != nil {
		return "", c.ErrorOf(err)
	} else {
		stmt = s
	}
	defer stmt.Free()

	if err := stmt.ExecDirect(ctx, query); err != nil {
		return "", c.ErrorOf(err)
	}

	ret := make([]string, 0, 20)
	for {
		if row, err := stmt.Fetch(ctx); err != nil {
			return "", err
		} else if row == nil {
			break
		} else {
			line := make([]string, 0, len(row))
			for _, col := range row {
				if col == nil {
					line = append(line, "NULL")
				} else {
					line = append(line, fmt.Sprintf("%v", col))
				}
			}
			ret = append(ret, strings.Join(line, " "))
		}
	}
	return strings.Join(ret, "\n"), nil
}

func (c *Conn) IOMetrics(reset bool) (readBytes uint64, writtenBytes uint64, enabled bool) {
	if c == nil || c.handle == nil {
		return 0, 0, false
	}
	if reset {
		return c.handle.ResetIOMetrics()
	}
	return c.handle.IOMetrics()
}

func (c *Conn) SupportsDatabaseMetadata() bool {
	return c != nil && c.handle != nil && c.handle.SupportsDatabaseMetadata()
}

func (c *Conn) Error() error {
	return c.ErrorOf(nil)
}

func (c *Conn) ErrorOf(cause error) error {
	code, msg := c.handle.Error()
	return formatMachcliError(code, msg, cause)
}

func (c *Conn) Prepare(query string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), query)
}

func (c *Conn) closeUnderlying() error {
	if c == nil {
		return nil
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	if c.handle != nil {
		if err := c.close(); err != nil {
			return normalizeError(err)
		}
	}
	if c.testResetConn != nil {
		err := c.testResetConn.Close()
		c.testResetConn = nil
		return normalizeError(err)
	}
	return nil
}

func (c *Conn) close() (ret error) {
	c.closeOnce.Do(func() {
		defer func() {
			c.usedAt = time.Now()
			c.usedCount++
		}()
		c.catalogGeneration.Add(1)
		if err := c.closeQueryStmtPool(); err != nil && ret == nil {
			ret = err
		}
		if err := c.handle.Disconnect(); err != nil {
			if ret == nil {
				ret = c.ErrorOf(err)
			}
		}
	})
	return ret
}

func (c *Conn) Close() error {
	return c.closeUnderlying()
}

func (c *Conn) shouldReuseStmtForQuery(query string) bool {
	switch c.queryStmtReuseMode {
	case StatementCacheOn:
		return true
	case StatementCacheOff:
		return false
	default:
		switch queryHead(query) {
		case "SELECT", "WITH", "INSERT", "UPDATE", "DELETE", "MERGE", "UPSERT", "REPLACE":
			return true
		default:
			return false
		}
	}
}

func (c *Conn) acquireQueryStmt(ctx context.Context, query string) (*Stmt, error) {
	// catalogGeneration is loaded outside queryStmtPoolMu because it's an
	// atomic counter, not memory protected by that mutex; each cached stmt's
	// generation is re-checked against it right after the pool lock is
	// (re)acquired below, so a concurrent bump is never missed.
	generation := c.catalogGeneration.Load()
	if !c.shouldReuseStmtForQuery(query) {
		stmt, err := c.NewStmt()
		if err != nil {
			return nil, err
		}
		stmt.catalogGeneration = generation
		if err := stmt.prepare(ctx, query); err != nil {
			_ = stmt.Close()
			return nil, err
		}
		return stmt, nil
	}
	c.queryStmtPoolMu.Lock()
	if c.queryStmtFast != nil && c.queryStmtFastKey == query {
		stmt := c.queryStmtFast
		c.queryStmtFast = nil
		c.queryStmtFastKey = ""
		c.queryStmtPoolMu.Unlock()

		if stmt.catalogGeneration != generation {
			_ = stmt.Close()
			return c.acquireQueryStmt(ctx, query)
		}
		stmt.reachEOF = false
		if stmt.handle.SupportsReprepare() {
			if err := stmt.prepare(ctx, query); err != nil {
				_ = stmt.Close()
				return nil, err
			}
		}
		return stmt, nil
	}
	if pool := c.queryStmtPool[query]; len(pool) > 0 {
		idx := len(pool) - 1
		stmt := pool[idx]
		if idx == 0 {
			delete(c.queryStmtPool, query)
		} else {
			c.queryStmtPool[query] = pool[:idx]
		}
		if c.queryStmtPoolCount > 0 {
			c.queryStmtPoolCount--
		}
		c.queryStmtPoolMu.Unlock()

		if stmt.catalogGeneration != generation {
			_ = stmt.Close()
			return c.acquireQueryStmt(ctx, query)
		}
		stmt.reachEOF = false
		if stmt.handle.SupportsReprepare() {
			if err := stmt.prepare(ctx, query); err != nil {
				_ = stmt.Close()
				return nil, err
			}
		}
		return stmt, nil
	}
	c.queryStmtPoolMu.Unlock()

	stmt, err := c.NewStmt()
	if err != nil {
		return nil, err
	}
	stmt.catalogGeneration = generation
	if err := stmt.prepare(ctx, query); err != nil {
		_ = stmt.Close()
		return nil, err
	}
	return stmt, nil
}

func (c *Conn) releaseQueryStmt(query string, stmt *Stmt, reusable bool) error {
	if stmt == nil {
		return nil
	}
	if !c.shouldReuseStmtForQuery(query) {
		return stmt.Close()
	}
	if !reusable {
		return stmt.Close()
	}
	if !stmt.handle.FetchCompleted() {
		// the server still holds an open cursor for this statement id; freeing it
		// is the only way to drop a partially consumed result set.
		return stmt.Close()
	}
	stmt.reachEOF = false
	if err := stmt.handle.ExecuteClean(); err != nil {
		_ = stmt.Close()
		return stmt.ErrorOf(err)
	}

	keep := false
	c.queryStmtPoolMu.Lock()
	if stmt.catalogGeneration != c.catalogGeneration.Load() {
		keep = false
	} else if c.queryStmtFast == nil {
		c.queryStmtFast = stmt
		c.queryStmtFastKey = query
		keep = true
	} else if c.queryStmtPool != nil &&
		c.queryStmtPoolCount < c.queryStmtPoolCap &&
		len(c.queryStmtPool[query]) < c.queryStmtPoolPerKeyCap {
		c.queryStmtPool[query] = append(c.queryStmtPool[query], stmt)
		c.queryStmtPoolCount++
		keep = true
	}
	c.queryStmtPoolMu.Unlock()

	if keep {
		return nil
	}
	return stmt.Close()
}

func (c *Conn) closeQueryStmtPool() error {
	if c.queryStmtReuseMode == StatementCacheOff {
		return nil
	}
	c.queryStmtPoolMu.Lock()
	if len(c.queryStmtPool) == 0 && c.queryStmtFast == nil {
		c.queryStmtPoolMu.Unlock()
		return nil
	}
	capHint := c.queryStmtPoolCount
	if c.queryStmtFast != nil {
		capHint++
	}
	statements := make([]*Stmt, 0, capHint)
	if c.queryStmtFast != nil {
		statements = append(statements, c.queryStmtFast)
		c.queryStmtFast = nil
		c.queryStmtFastKey = ""
	}
	for key, pool := range c.queryStmtPool {
		statements = append(statements, pool...)
		delete(c.queryStmtPool, key)
	}
	c.queryStmtPoolCount = 0
	c.queryStmtPoolMu.Unlock()

	var firstErr error
	for _, stmt := range statements {
		if stmt == nil {
			continue
		}
		if err := stmt.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
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
	if c == nil || c.handle == nil {
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
	if c == nil || c.handle == nil {
		return driver.ErrBadConn
	}
	rows, err := c.QueryContext(ctx, "SELECT 1", nil)
	if err != nil {
		return err
	}
	defer rows.Close()
	return nil
}

func (c *Conn) resetExec(ctx context.Context, query string) *Result {
	if c == nil {
		return nil
	}
	if c.handle != nil {
		return c.exec(ctx, query)
	}
	if c.testResetConn != nil {
		return c.testResetConn.Exec(ctx, query)
	}
	return nil
}

// ResetSession relies on the database/sql contract that a Conn is never
// touched by another goroutine while it sits idle in the pool: it is called
// right before a pooled *Conn is handed back out, so no other goroutine can
// be running Exec/Query/Close concurrently. That is why inTx/database/dbDirty
// are read via separate short txMu critical sections below instead of one
// held for the whole function.
func (c *Conn) ResetSession(ctx context.Context) error {
	if c == nil || (c.handle == nil && c.testResetConn == nil) {
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
		result := c.resetExec(ctx, "USE "+QuoteIdentifier(database))
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
	return c != nil && c.handle != nil && c.handle.IsOpen()
}

// Implement driver.NamedValueChecker
func (c *Conn) CheckNamedValue(nv *driver.NamedValue) error {
	return checkNamedValue(nv)
}

func (c *Conn) PrepareContext(ctx context.Context, query string) (driver.Stmt, error) {
	if c == nil || c.handle == nil {
		return nil, driver.ErrBadConn
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	stmt, err := c.NewStmt()
	if err != nil {
		return nil, err
	}

	if err := stmt.prepare(ctx, query); err != nil {
		stmt.Close()
		return nil, normalizeError(err)
	}
	return stmt, nil
}

func (c *Conn) NewStmt() (*Stmt, error) {
	var handle *machnet.StmtHandle
	if h, err := c.handle.AllocStmt(); err != nil {
		return nil, normalizeError(c.ErrorOf(err))
	} else {
		handle = h
	}
	ret := &Stmt{conn: c, handle: handle}
	return ret, nil
}

func (c *Conn) ExecContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c == nil || c.handle == nil {
		return nil, driver.ErrBadConn
	}
	vals, err := namedValuesToAny(args)
	if err != nil {
		return nil, err
	}
	result := c.exec(ctx, query, vals...)
	if err := normalizeError(result.Err()); err != nil {
		return nil, err
	}
	c.setTransactionState(query)
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok {
		meta.cbMessage = result.Message
	}
	return result, nil
}

func (c *Conn) exec(ctx context.Context, query string, args ...any) *Result {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	ret := &Result{}
	if len(args) == 0 {
		stmt, err := c.NewStmt()
		if err != nil {
			ret.err = err
			return ret
		}
		defer stmt.Close()

		if err := stmt.handle.ExecDirect(ctx, query); err != nil {
			ret.err = c.ErrorOf(err)
			return ret
		}
		ret.rowCount, ret.err = stmt.handle.RowCount()
		ret.rowID, ret.hasRowID = stmt.handle.GeneratedRowID()
		if typ, err := stmt.handle.GetStmtType(); err != nil {
			ret.err = err
			return ret
		} else {
			ret.stmtType = typ
		}
		if stmtTypeInvalidatesCatalog(ret.stmtType) {
			c.catalogGeneration.Add(1)
			if err := c.closeQueryStmtPool(); err != nil {
				ret.err = err
			}
		}
		return ret
	}

	stmt, err := c.acquireQueryStmt(ctx, query)
	if err != nil {
		ret.err = err
		return ret
	}
	defer func() {
		keep := ret.err == nil
		if relErr := c.releaseQueryStmt(query, stmt, keep); relErr != nil && ret.err == nil {
			ret.err = relErr
		}
	}()

	if ret.err = stmt.bindParams(args...); ret.err != nil {
		return ret
	}
	ret.err = stmt.execute(ctx)
	ret.rowCount = stmt.rowCount
	ret.rowID, ret.hasRowID = stmt.handle.GeneratedRowID()
	if typ, err := stmt.handle.GetStmtType(); err != nil {
		ret.err = err
		return ret
	} else {
		ret.stmtType = typ
	}
	return ret
}

func stmtTypeInvalidatesCatalog(stmtType machnet.StmtType) bool {
	switch stmtType {
	case machnet.QPP_STMT_TYPE_ALTER_SESSION_SET,
		machnet.QPP_STMT_TYPE_CONNECT_USER,
		machnet.QPP_STMT_TYPE_CREATE_DATABASE,
		machnet.QPP_STMT_TYPE_DROP_DATABASE,
		machnet.QPP_STMT_TYPE_ALTER_DATABASE:
		return true
	default:
		return false
	}
}

// setTransactionState updates inTx/dbDirty after a statement has completed
// successfully. It is intentionally NOT called from exec()/query() (the
// shared primitives behind acquireQueryStmt), because those are also reused
// by paths that must not affect this tracking: Connector.Connect's initial
// "USE <database>" (already at the intended db, so not "dirty") and
// resetExec's internal ROLLBACK/USE during ResetSession (which already
// updates inTx/dbDirty itself once the reset succeeds). Instead every
// public success path calls it directly: ExecContext, QueryContext,
// Conn.queryRow, Stmt.exec and Stmt.queryRows.
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

func (c *Conn) QueryContext(ctx context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	if c == nil || c.handle == nil {
		return nil, driver.ErrBadConn
	}
	vals, err := namedValuesToAny(args)
	if err != nil {
		return nil, err
	}
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	rows, err := c.query(ctx, query, vals...)
	if err != nil {
		return nil, normalizeError(err)
	}
	c.setTransactionState(query)
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok {
		meta.cbMessage = rows.Message
		meta.cbFetchable = rows.IsFetchable
	}
	return rows, nil
}

func (c *Conn) query(ctx context.Context, query string, args ...any) (*Rows, error) {
	stmt, err := c.acquireQueryStmt(ctx, query)
	if err != nil {
		return nil, err
	}
	if err := stmt.bindParams(args...); err != nil {
		if relErr := c.releaseQueryStmt(query, stmt, false); relErr != nil {
			return nil, relErr
		}
		return nil, err
	}
	if err := stmt.execute(ctx); err != nil {
		if relErr := c.releaseQueryStmt(query, stmt, false); relErr != nil {
			return nil, relErr
		}
		return nil, err
	}
	ret := &Rows{
		ctx:             ctx,
		stmt:            stmt,
		queryStmtKey:    query,
		queryStmtPooled: true,
		timeLocation:    c.timeLocation,
	}
	if typ, err := stmt.handle.GetStmtType(); err != nil {
		if relErr := c.releaseQueryStmt(query, stmt, false); relErr != nil {
			return nil, relErr
		}
		return nil, err
	} else {
		ret.stmtType = typ
	}
	if ret.stmtType.IsSelect() {
		ret.rowsCount = 0
	} else {
		ret.rowsCount = stmt.rowCount
	}
	return ret, nil
}

func (c *Conn) QueryRow(ctx context.Context, query string, args ...any) *Row {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.queryRow(ctx, query, args...)
}

func (c *Conn) queryRow(ctx context.Context, query string, args ...any) *Row {
	ret := &Row{timeLocation: c.timeLocation}
	stmt, err := c.acquireQueryStmt(ctx, query)
	if err != nil {
		ret.err = err
		return ret
	}
	defer func() {
		keep := ret.err == nil || errors.Is(ret.err, sql.ErrNoRows)
		if relErr := c.releaseQueryStmt(query, stmt, keep); relErr != nil && ret.err == nil {
			ret.err = relErr
		}
	}()
	if ret.err = stmt.bindParams(args...); ret.err != nil {
		return ret
	}
	if ret.err = stmt.execute(ctx); ret.err != nil {
		return ret
	}
	c.setTransactionState(query)
	if typ, err := stmt.handle.GetStmtType(); err != nil {
		ret.err = err
		return ret
	} else {
		ret.stmtType = typ
	}
	ret.rowCount = stmt.rowCount
	ret.columns = make(Columns, len(stmt.columnDesc))
	for i, desc := range stmt.columnDesc {
		ret.columns[i] = &Column{
			Name:             desc.Name,
			Length:           desc.Size,
			Type:             desc.Type.ColumnType(),
			DataType:         desc.Type.DataType(),
			Nullable:         desc.Nullable,
			Nullability:      desc.Nullability,
			PrimaryKey:       desc.PrimaryKey,
			ElementType:      desc.ElementType,
			ElementPrecision: desc.ElementPrecision,
			Scale:            desc.Scale,
		}
	}
	if values, err := stmt.fetch(ctx); err != nil {
		if err == io.EOF {
			// it means no row fetched
			ret.err = sql.ErrNoRows
		} else {
			ret.err = err
		}
		return ret
	} else {
		ret.values = values
	}
	return ret
}

type Stmt struct {
	conn              *Conn
	handle            *machnet.StmtHandle
	sqlText           string
	columnDesc        []ColumnDesc
	reachEOF          bool
	rowCount          int64
	execCount         int64
	catalogGeneration uint64
}

var _ driver.Stmt = (*Stmt)(nil)
var _ driver.StmtExecContext = (*Stmt)(nil)
var _ driver.StmtQueryContext = (*Stmt)(nil)
var _ driver.NamedValueChecker = (*Stmt)(nil)

func (s *Stmt) Close() error {
	if s == nil || s.handle == nil {
		return nil
	}
	if err := s.handle.Free(); err == nil {
		s.handle = nil
		return nil
	} else {
		return normalizeError(s.ErrorOf(err))
	}
}

func (s *Stmt) Error() error {
	return s.ErrorOf(nil)
}

func (s *Stmt) ErrorOf(cause error) error {
	code, msg := s.handle.Error()
	return formatMachcliError(code, msg, cause)
}

func (s *Stmt) prepare(ctx context.Context, query string) error {
	if err := s.handle.Prepare(ctx, query); err != nil {
		return s.ErrorOf(err)
	}
	s.sqlText = query
	s.columnDesc = nil
	s.rowCount = 0
	s.execCount = 0
	s.reachEOF = false
	return nil
}

// renewHandle frees the underlying statement and allocates a freshly prepared
// one. It is used when a result set is abandoned before the server delivered
// its last chunk, since any further Prepare/Execute on that statement id would
// fail with MACHCLI-ERR-3008 (fetch in progress).
func (s *Stmt) renewHandle(ctx context.Context) error {
	if s == nil || s.conn == nil || s.conn.handle == nil {
		return nil
	}
	sqlText := s.sqlText
	if err := s.Close(); err != nil {
		return err
	}
	handle, err := s.conn.handle.AllocStmt()
	if err != nil {
		return normalizeError(s.conn.ErrorOf(err))
	}
	s.handle = handle
	s.reachEOF = false
	if sqlText == "" {
		return nil
	}
	if err := s.prepare(context.WithoutCancel(ctx), sqlText); err != nil {
		return normalizeError(err)
	}
	return nil
}

// discardOpenCursor renews the statement when its previous result set was
// abandoned before the server sent the last chunk.
func (s *Stmt) discardOpenCursor(ctx context.Context) error {
	if s == nil || s.handle == nil || s.handle.FetchCompleted() {
		return nil
	}
	return s.renewHandle(ctx)
}

func (s *Stmt) reprepareIfSupported(ctx context.Context) error {
	if s == nil || s.handle == nil || !s.handle.SupportsReprepare() {
		return nil
	}
	return s.prepare(ctx, s.sqlText)
}

// NumInput intentionally returns -1: parameter counts are validated by the
// server (via bindParams/DescribeParam at bind time), not by database/sql
// before dispatch, so binding errors surface after a round trip instead of
// up front.
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
	if s == nil || s.handle == nil {
		return nil, driver.ErrBadConn
	}
	result := &Result{}
	if err := s.discardOpenCursor(ctx); err != nil {
		result.err = err
		goto doResult
	}
	if err := s.reprepareIfSupported(ctx); err != nil {
		result.err = err
		goto doResult
	}
	defer s.handle.ExecuteClean()
	if err := s.bindParams(vals...); err != nil {
		result.err = err
		goto doResult
	}
	if err := s.execute(ctx); err != nil {
		result.err = err
		goto doResult
	}
	result.rowCount = s.rowCount
	result.rowID, result.hasRowID = s.handle.GeneratedRowID()
	if typ, err := s.handle.GetStmtType(); err != nil {
		result.err = err
		goto doResult
	} else {
		result.stmtType = typ
	}
doResult:
	if err := normalizeError(result.Err()); err != nil {
		return nil, err
	}
	if s.conn != nil {
		s.conn.setTransactionState(s.sqlText)
	}
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok {
		meta.cbMessage = result.Message
	}
	return result, nil
}

func (s *Stmt) execute(ctx context.Context) error {
	s.reachEOF = false
	if err := s.handle.Execute(ctx); err != nil {
		return s.ErrorOf(err)
	}
	defer func() {
		s.execCount++
	}()
	if rowCount, err := s.handle.RowCount(); err != nil {
		return s.ErrorOf(err)
	} else {
		s.rowCount = rowCount
	}
	if s.execCount > 0 {
		return nil
	}
	stmtType, _ := s.handle.GetStmtType()
	if !stmtType.IsSelect() {
		return nil
	}
	num, err := s.handle.NumResultCol()
	if err != nil {
		return s.ErrorOf(err)
	}
	s.columnDesc = make([]ColumnDesc, num)
	for i := 0; i < num; i++ {
		d := ColumnDesc{}
		if err := s.handle.DescribeColEx(i, &d.Name, (*api.SqlType)(&d.Type), &d.Size, &d.Scale, &d.Nullable, &d.Nullability, &d.PrimaryKey); err != nil {
			return s.ErrorOf(err)
		}
		if d.Type.IsArray() {
			if err := s.handle.DescribeArrayCol(i, &d.ElementType, &d.ElementPrecision); err != nil {
				return s.ErrorOf(err)
			}
		}
		s.columnDesc[i] = d
	}
	return nil
}

func (s *Stmt) QueryRow(ctx context.Context, params ...any) *Row {
	ret := &Row{timeLocation: s.conn.timeLocation}
	if err := s.discardOpenCursor(ctx); err != nil {
		ret.err = err
		return ret
	}
	if err := s.reprepareIfSupported(ctx); err != nil {
		ret.err = err
		return ret
	}
	if err := s.bindParams(params...); err != nil {
		ret.err = err
		return ret
	}
	if err := s.execute(ctx); err != nil {
		ret.err = err
		return ret
	}
	ret.rowCount = s.rowCount
	if values, err := s.fetch(ctx); err != nil {
		if err == io.EOF {
			// it means no row fetched
			ret.err = sql.ErrNoRows
		} else {
			ret.err = err
		}
		return ret
	} else {
		ret.values = values
	}
	ret.columns = make(Columns, len(s.columnDesc))
	for i, desc := range s.columnDesc {
		ret.columns[i] = &Column{
			Name:             desc.Name,
			Length:           desc.Size,
			Type:             desc.Type.ColumnType(),
			DataType:         desc.Type.DataType(),
			Nullable:         desc.Nullable,
			Nullability:      desc.Nullability,
			PrimaryKey:       desc.PrimaryKey,
			ElementType:      desc.ElementType,
			ElementPrecision: desc.ElementPrecision,
			Scale:            desc.Scale,
		}
	}
	return ret
}

func (s *Stmt) queryRows(ctx context.Context, vals []any) (driver.Rows, error) {
	if s == nil || s.handle == nil {
		return nil, driver.ErrBadConn
	}
	if err := s.discardOpenCursor(ctx); err != nil {
		return nil, normalizeError(err)
	}
	if err := s.reprepareIfSupported(ctx); err != nil {
		return nil, normalizeError(err)
	}
	if err := s.bindParams(vals...); err != nil {
		return nil, normalizeError(err)
	}
	if err := s.execute(ctx); err != nil {
		return nil, normalizeError(err)
	}
	rows := &Rows{
		ctx:          ctx,
		stmt:         s,
		isPrepared:   true,
		timeLocation: s.conn.timeLocation,
	}
	if typ, err := s.handle.GetStmtType(); err != nil {
		return nil, normalizeError(err)
	} else {
		rows.stmtType = typ
	}
	if !rows.stmtType.IsSelect() {
		rows.rowsCount = 0
	} else {
		rows.rowsCount = s.rowCount
	}
	if s.conn != nil {
		s.conn.setTransactionState(s.sqlText)
	}
	if meta, ok := ctx.Value(MetaKey).(*Meta); ok {
		meta.cbMessage = rows.Message
	}
	return rows, nil
}

// fetch fetches the next row from the result set.
// It returns true if it reaches end of the result, otherwise false.
func (s *Stmt) fetch(ctx context.Context) ([]any, error) {
	if s.reachEOF {
		return nil, errors.New("fetch reached end of the result set")
	}
	row, err := s.handle.Fetch(ctx)
	if err != nil {
		return nil, err
	}
	s.reachEOF = row == nil
	if s.reachEOF {
		return nil, io.EOF
	}
	if row == nil {
		return nil, io.EOF
	}
	return row, nil
}

func (s *Stmt) bindParams(args ...any) error {
	numParam, err := s.handle.NumParam()
	if err != nil {
		return s.ErrorOf(err)
	}
	args, err = s.mapNamedParams(args, numParam)
	if err != nil {
		return err
	}
	if len(args) != numParam {
		return fmt.Errorf("params required %d, but got %d", numParam, len(args))
	}

	for idx, arg := range args {
		var value any
		var sqlType api.SqlType
		switch val := arg.(type) {
		default:
			pd, err := s.handle.DescribeParam(idx)
			if err != nil {
				return s.ErrorOf(err)
			}
			if val == nil {
				sqlType = pd.Type
				value = nil
			} else {
				return fmt.Errorf("bind unknown type at column %d %T, expect: %s(%d)", idx, val, pd.Type.String(), pd.Type)
			}
		case int16:
			sqlType = api.SqlTypeInt16
			value = val
		case *int16:
			sqlType = api.SqlTypeInt16
			value = *val
		case uint16:
			sqlType = api.SqlTypeUInt16
			value = val
		case *uint16:
			sqlType = api.SqlTypeUInt16
			value = *val
		case int32:
			sqlType = api.SqlTypeInt32
			value = val
		case *int32:
			sqlType = api.SqlTypeInt32
			value = *val
		case uint32:
			sqlType = api.SqlTypeUInt32
			value = val
		case *uint32:
			sqlType = api.SqlTypeUInt32
			value = *val
		case int:
			sqlType = api.SqlTypeInt32
			value = val
		case *int:
			sqlType = api.SqlTypeInt32
			value = *val
		case int64:
			sqlType = api.SqlTypeInt64
			value = val
		case *int64:
			sqlType = api.SqlTypeInt64
			value = *val
		case uint64:
			sqlType = api.SqlTypeUInt64
			value = val
		case *uint64:
			sqlType = api.SqlTypeUInt64
			value = *val
		case time.Time:
			sqlType = api.SqlTypeDatetime
			value = val.UnixNano()
		case *time.Time:
			sqlType = api.SqlTypeDatetime
			value = (*val).UnixNano()
		case float32:
			sqlType = api.SqlTypeFloat
			value = val
		case *float32:
			sqlType = api.SqlTypeFloat
			value = *val
		case float64:
			sqlType = api.SqlTypeDouble
			value = val
		case *float64:
			sqlType = api.SqlTypeDouble
			value = *val
		case net.IP:
			if ipv4 := val.To4(); ipv4 != nil {
				sqlType = api.SqlTypeIPv4
				value = []byte(ipv4.String())
			} else {
				sqlType = api.SqlTypeIPv6
				value = []byte(val.To16().String())
			}
		case string:
			pd, err := s.handle.DescribeParam(idx)
			if err != nil {
				return s.ErrorOf(err)
			}
			// Preserve datetime strings for the engine, which accepts more formats than RFC3339.
			sqlType = api.SqlTypeString
			if pd.Type != api.SqlTypeDatetime {
				sqlType = pd.Type
			}
			value = val
		case *string:
			pd, err := s.handle.DescribeParam(idx)
			if err != nil {
				return s.ErrorOf(err)
			}
			// Preserve datetime strings for the engine, which accepts more formats than RFC3339.
			sqlType = api.SqlTypeString
			if pd.Type != api.SqlTypeDatetime {
				sqlType = pd.Type
			}
			if val != nil {
				value = *val
			}
		case api.JSONString:
			sqlType = api.SqlTypeJSON
			value = string(val)
		case []byte:
			sqlType = api.SqlTypeBinary
			value = val
		case api.Decimal:
			sqlType = api.SqlTypeDecimal
			value = val
		case *api.Decimal:
			sqlType = api.SqlTypeDecimal
			if val != nil {
				value = *val
			}
		case api.Array:
			sqlType = val.SqlType()
			value = val
		case *api.Array:
			if val == nil {
				pd, err := s.handle.DescribeParam(idx)
				if err != nil {
					return s.ErrorOf(err)
				}
				sqlType = pd.Type
				value = nil
			} else {
				sqlType = val.SqlType()
				value = val
			}
		}
		if err := s.handle.BindParam(idx, sqlType, value); err != nil {
			return s.ErrorOf(err)
		}
	}
	return nil
}

func (s *Stmt) mapNamedParams(args []any, numParam int) ([]any, error) {
	hasNamed := false
	for _, arg := range args {
		if _, ok := arg.(driver.NamedValue); ok {
			hasNamed = true
		}
	}
	if !hasNamed {
		return args, nil
	}
	provided := make(map[string]any, len(args))
	for _, arg := range args {
		named, ok := arg.(driver.NamedValue)
		if !ok {
			return nil, fmt.Errorf("named and positional parameters cannot be mixed")
		}
		if named.Name == "" {
			return nil, fmt.Errorf("named parameter name is empty")
		}
		// Parameter names are matched case-insensitively, like column names.
		key := strings.ToLower(named.Name)
		if _, exists := provided[key]; exists {
			return nil, fmt.Errorf("duplicate named parameter %q", named.Name)
		}
		provided[key] = named.Value
	}
	ret := make([]any, numParam)
	required := make(map[string]struct{}, numParam)
	for idx := 0; idx < numParam; idx++ {
		desc, err := s.handle.DescribeParam(idx)
		if err != nil {
			return nil, s.ErrorOf(err)
		}
		if desc.Name == "" {
			return nil, fmt.Errorf("%w: the server returned no parameter name metadata, "+
				"which also happens when named and anonymous markers are mixed", ErrNamedParamsUnsupported)
		}
		key := strings.ToLower(desc.Name)
		value, exists := provided[key]
		if !exists {
			return nil, fmt.Errorf("missing named parameter %q", desc.Name)
		}
		ret[idx] = value
		required[key] = struct{}{}
	}
	for name := range provided {
		if _, exists := required[name]; !exists {
			return nil, fmt.Errorf("unexpected named parameter %q", name)
		}
	}
	return ret, nil
}

type Result struct {
	err      error
	rowCount int64
	stmtType machnet.StmtType
	rowID    uint64
	hasRowID bool
}

var _ driver.Result = (*Result)(nil)

func (rs *Result) Message() string {
	return formatResultMessage(rs.err, rs.stmtType, rs.rowCount)
}

func (rs *Result) Err() error {
	return rs.err
}

func (rs *Result) LastInsertId() (int64, error) {
	if rs != nil && rs.hasRowID {
		return int64(rs.rowID), nil
	}
	return 0, errors.New("not implemented LastInsertId")
}

func (rs *Result) RowsAffected() (int64, error) {
	return rs.rowCount, nil
}

type Rows struct {
	stmt            *Stmt
	err             error
	row             []any
	rowsCount       int64
	stmtType        machnet.StmtType
	isPrepared      bool
	queryStmtPooled bool
	queryStmtKey    string
	timeLocation    *time.Location
	// ctx is the context supplied to the QueryContext/Query call that created
	// these Rows; driver.Rows.Next has no context of its own, so it is reused
	// to let cancellation interrupt a blocked Fetch during row iteration.
	ctx context.Context
}

var _ driver.Rows = (*Rows)(nil)
var _ driver.RowsColumnTypeDatabaseTypeName = (*Rows)(nil)
var _ driver.RowsColumnTypeLength = (*Rows)(nil)
var _ driver.RowsColumnTypeNullable = (*Rows)(nil)
var _ driver.RowsColumnTypePrecisionScale = (*Rows)(nil)
var _ driver.RowsColumnTypeScanType = (*Rows)(nil)

func (r *Rows) Columns() []string {
	if r == nil {
		return nil
	}
	cols, _ := r.columns()
	if cols == nil {
		return nil
	}
	return cols.Names()
}

func (r *Rows) columns() (Columns, error) {
	if r.stmt == nil {
		return nil, nil
	}
	ret := make(Columns, len(r.stmt.columnDesc))
	for i, desc := range r.stmt.columnDesc {
		ret[i] = &Column{
			Name:             desc.Name,
			Length:           desc.Size,
			Type:             desc.Type.ColumnType(),
			DataType:         desc.Type.DataType(),
			Nullable:         desc.Nullable,
			Nullability:      desc.Nullability,
			PrimaryKey:       desc.PrimaryKey,
			ElementType:      desc.ElementType,
			ElementPrecision: desc.ElementPrecision,
			Scale:            desc.Scale,
		}
	}
	return ret, nil
}

func (r *Rows) column(index int) (*Column, bool) {
	cols, err := r.columns()
	if err != nil {
		return nil, false
	}
	if index < 0 || index >= len(cols) {
		return nil, false
	}
	return cols[index], true
}

func (r *Rows) Next(dest []driver.Value) error {
	if r == nil {
		return io.EOF
	}
	if !r.next() {
		if err := normalizeError(r.Err()); err != nil {
			return err
		}
		return io.EOF
	}
	var row = r.Row()
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

func (r *Rows) next() bool {
	if r.stmt == nil {
		return false
	}
	if r.stmt.reachEOF {
		return false
	}
	row, err := r.stmt.fetch(r.ctx)
	if err != nil {
		if err != io.EOF {
			r.err = err
		}
		return false
	}
	r.row = row
	r.rowsCount++
	return true
}

func (r *Rows) Err() error {
	return r.err
}

func (r *Rows) Close() error {
	if r.stmt == nil {
		return nil
	}
	stmt := r.stmt
	r.stmt = nil
	if r.isPrepared {
		stmt.reachEOF = false
		if !stmt.handle.FetchCompleted() {
			return stmt.renewHandle(r.ctx)
		}
		return stmt.handle.ExecuteClean()
	}
	if r.queryStmtPooled && stmt.conn != nil {
		return stmt.conn.releaseQueryStmt(r.queryStmtKey, stmt, true)
	}
	if err := stmt.Close(); err != nil {
		return normalizeError(err)
	}
	return nil
}

func (r *Rows) IsFetchable() bool {
	if r.stmt == nil || r.stmt.handle == nil {
		return false
	}
	typ, _ := r.stmt.handle.GetStmtType()
	return typ.IsSelect()
}

func (r *Rows) Message() string {
	return formatResultMessage(r.err, r.stmtType, r.rowsCount)
}

func (r *Rows) RowsAffected() int64 {
	return r.rowsCount
}

func (r *Rows) Row() []any {
	return r.row
}

func (r *Rows) ColumnDescriptions() []ColumnDesc {
	if r.stmt == nil {
		return nil
	}
	return r.stmt.columnDesc
}

func (r *Rows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) > len(r.row) {
		return fmt.Errorf("params required %d, but got %d", len(r.row), len(dest))
	}
	for i, d := range dest {
		if d == nil {
			continue
		}
		if r.row[i] == nil {
			if !ScanNull(dest[i]) {
				return fmt.Errorf("scan NULL into %T", dest[i])
			}
			continue
		}
		if err := Scan(r.row[i], d, r.timeLocation); err != nil {
			return err
		}
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
	if index < 0 || index >= len(r.stmt.columnDesc) {
		return 0, 0, false
	}
	desc := r.stmt.columnDesc[index]
	switch desc.Type {
	case api.SqlTypeFloat, api.SqlTypeDouble, api.SqlTypeDecimal:
		if desc.Size <= 0 {
			return 0, int64(desc.Scale), false
		}
		return int64(desc.Size), int64(desc.Scale), true
	case api.SqlTypeInt16Array, api.SqlTypeUInt16Array, api.SqlTypeInt32Array,
		api.SqlTypeUInt32Array, api.SqlTypeInt64Array, api.SqlTypeUInt64Array,
		api.SqlTypeFloatArray, api.SqlTypeDoubleArray, api.SqlTypeDecimalArray:
		return int64(desc.ElementPrecision), int64(desc.Scale), true
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
	case api.ColumnTypeInt16Array, api.ColumnTypeUInt16Array, api.ColumnTypeInt32Array,
		api.ColumnTypeUInt32Array, api.ColumnTypeInt64Array, api.ColumnTypeUInt64Array,
		api.ColumnTypeFloatArray, api.ColumnTypeDoubleArray, api.ColumnTypeDecimalArray:
		return reflect.TypeOf(api.Array{})
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

type Row struct {
	err          error
	values       []any
	columns      Columns
	rowCount     int64
	stmtType     machnet.StmtType
	timeLocation *time.Location
}

func (r *Row) Success() bool {
	return r.err == nil
}

func (r *Row) Err() error {
	return r.err
}

func (r *Row) Columns() (Columns, error) {
	return r.columns, nil
}

func (r *Row) Scan(dest ...any) error {
	if r.err == sql.ErrNoRows {
		return r.err
	}
	if len(dest) > len(r.values) {
		return fmt.Errorf("params required %d, but got %d", len(r.values), len(dest))
	}
	for i, d := range dest {
		if r.values[i] == nil {
			if !ScanNull(d) {
				return fmt.Errorf("scan NULL VALUE into %T", d)
			}
			continue
		}
		if err := Scan(r.values[i], d, r.timeLocation); err != nil {
			return err
		}
	}
	return nil
}

func (r *Row) Values() []any {
	return r.values
}

func (r *Row) RowsAffected() int64 {
	return r.rowCount
}

func (r *Row) Message() string {
	return formatResultMessage(r.err, r.stmtType, r.rowCount)
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
