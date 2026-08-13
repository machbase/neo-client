package client

import (
	"context"
	"crypto"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/machbase/neo-client/v2/api"
	"github.com/machbase/neo-client/v2/machnet"
)

const (
	defaultQueryStmtPoolCap         = 128
	defaultQueryStmtPoolPerQueryCap = 8
	defaultFetchRows                = 1000
)

type ClientDatabase struct {
	handle *machnet.EnvHandle
	host   string
	port   int

	alternativeHost string
	alternativePort int

	statementCache StatementCacheMode
	fetchRows      int64
}

func NewDatabase(conf *Config) (*ClientDatabase, error) {
	var handle *machnet.EnvHandle
	if h, err := machnet.Initialize(); err != nil {
		return nil, err
	} else {
		handle = h
	}
	ret := &ClientDatabase{
		host:            conf.Host,
		port:            conf.Port,
		alternativeHost: conf.AlternativeHost,
		alternativePort: conf.AlternativePort,
		handle:          handle,
		statementCache:  conf.StatementCache,
		fetchRows:       conf.FetchRows,
	}
	if ret.fetchRows <= 0 {
		ret.fetchRows = defaultFetchRows
	}
	return ret, nil
}

func (db *ClientDatabase) Close() error {
	if err := db.handle.Finalize(); err == nil {
		return nil
	} else {
		return db.ErrorOf(err)
	}
}

func (db *ClientDatabase) ErrorOf(cause error) error {
	code, msg := db.handle.Error()
	if code == 0 && msg == "" && cause == nil {
		// no error
		return nil
	} else if code == 0 && msg != "" {
		// code == 0 means client-side error
		if cause == nil {
			return fmt.Errorf("MACHCLI %s", msg)
		} else {
			return fmt.Errorf("MACHCLI %s, %s", msg, cause.Error())
		}
	} else {
		// code > 0 means server-side error
		if cause == nil {
			return fmt.Errorf("MACHCLI-ERR-%d, %s", code, msg)
		} else {
			return fmt.Errorf("MACHCLI-ERR-%d, %s", code, msg)
		}
	}
}

func (db *ClientDatabase) Ping(ctx context.Context) (time.Duration, error) {
	tick := time.Now()
	if conn, err := db.Connect(ctx, "user=sys;password=manager"); err != nil {
		if !strings.Contains(err.Error(), "Invalid username/password") {
			return time.Since(tick), err
		}
	} else {
		if err := conn.Close(); err != nil {
			return time.Since(tick), err
		}
	}
	return time.Since(tick), nil
}

func (db *ClientDatabase) UserAuth(ctx context.Context, user, password string) (bool, string, error) {
	cfg := Config{User: user, Password: password}
	conn, err := db.ConnectConfig(ctx, &cfg)
	if err != nil {
		return false, "invalid username or password", nil
	}
	err = conn.Close()
	return true, "", err
}

func (db *ClientDatabase) connectionString(user string, password string, fetchRows int64, ioMetrics bool, authMode string) string {
	entries := []string{
		fmt.Sprintf("SERVER=%s", db.host),
		fmt.Sprintf("PORT_NO=%d", db.port),
		fmt.Sprintf("UID=%s", strings.ToUpper(user)),
		fmt.Sprintf("PWD=%s", strings.ToUpper(password)),
		"CONNTYPE=1",
		fmt.Sprintf("FETCH_ROWS=%d", fetchRows),
	}
	if ioMetrics {
		entries = append(entries, "IO_METRICS=1")
	}
	if strings.TrimSpace(authMode) != "" {
		entries = append(entries, fmt.Sprintf("AUTH_MODE=%s", authMode))
	}
	if db.alternativeHost != "" && db.alternativePort != 0 {
		entries = append(entries,
			fmt.Sprintf("ALTERNATIVE_SERVERS=%s:%d",
				db.alternativeHost, db.alternativePort))
	}
	return strings.Join(entries, ";")
}

// connectParams holds the resolved connection settings shared by every
// Connect entry point, regardless of how the caller supplied them.
type connectParams struct {
	user             string
	password         string
	stmtReuse        StatementCacheMode
	fetchRows        int64
	enabledIOMetrics bool
	authMode         string
	authKey          crypto.PrivateKey
	proxyUser        string
	database         string
	timeLocation     *time.Location
}

// Connect parses dsn and connects to the database, without going through api.ConnectOption.
func (db *ClientDatabase) Connect(ctx context.Context, dsn string) (*ClientConn, error) {
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	return db.ConnectConfig(ctx, &cfg)
}

// ConnectConfig connects using Config fields directly.
func (db *ClientDatabase) ConnectConfig(ctx context.Context, cfg *Config) (*ClientConn, error) {
	p := connectParams{
		user:             cfg.User,
		password:         cfg.Password,
		authMode:         "PASSWORD",
		stmtReuse:        db.statementCache,
		fetchRows:        db.fetchRows,
		enabledIOMetrics: cfg.IOMetrics,
		proxyUser:        cfg.ProxyUser,
		database:         cfg.Database,
		timeLocation:     time.UTC,
	}
	if cfg.statementCacheSet {
		p.stmtReuse = cfg.StatementCache
	}
	if cfg.FetchRows > 0 {
		p.fetchRows = cfg.FetchRows
	}
	if strings.TrimSpace(cfg.AuthKeyFile) != "" || strings.TrimSpace(cfg.AuthKeyPEM) != "" || strings.EqualFold(strings.TrimSpace(cfg.AuthMode), "CHALLENGE") {
		var key crypto.PrivateKey
		var err error
		if strings.TrimSpace(cfg.AuthKeyPEM) != "" {
			key, err = api.LoadPrivateKeyFromPEM([]byte(cfg.AuthKeyPEM))
		} else {
			key, err = api.LoadPrivateKeyFromFile(cfg.AuthKeyFile)
		}
		if err != nil {
			return nil, err
		}
		p.authMode = "CHALLENGE"
		p.authKey = key
	}
	return db.connect(ctx, p)
}

func (db *ClientDatabase) connect(ctx context.Context, p connectParams) (*ClientConn, error) {
	if strings.EqualFold(p.user, "sys") && p.proxyUser != "" && !strings.EqualFold(p.proxyUser, "sys") {
		// "SYS AS PROXY_USER" format is required for proxy user authentication,
		// and the proxy user cannot be "SYS" ('sys as sys' is 'sys').
		p.user = fmt.Sprintf("SYS AS %s", strings.ToUpper(p.proxyUser))
	}

	var handle *machnet.ConnHandle
	if c, err := db.handle.Connect(db.connectionString(p.user, p.password, p.fetchRows, p.enabledIOMetrics, p.authMode), p.authKey); err != nil {
		return nil, db.ErrorOf(err)
	} else {
		handle = c
	}

	ret := &ClientConn{
		db:                     db,
		handle:                 handle,
		user:                   strings.ToUpper(p.user),
		usedAt:                 time.Now(),
		timeLocation:           p.timeLocation,
		queryStmtReuseMode:     p.stmtReuse,
		queryStmtPool:          map[string][]*ClientStmt{},
		queryStmtPoolCap:       defaultQueryStmtPoolCap,
		queryStmtPoolPerKeyCap: defaultQueryStmtPoolPerQueryCap,
	}
	if strings.TrimSpace(p.database) != "" && !strings.EqualFold(p.database, "MACHBASEDB") {
		if err := ret.Exec(ctx, "USE "+quoteClientIdentifier(p.database)).Err(); err != nil {
			return nil, errors.Join(err, ret.Close())
		}
	}
	return ret, nil
}

type ClientConn struct {
	handle *machnet.ConnHandle
	db     *ClientDatabase

	user      string
	usedAt    time.Time
	usedCount int64
	closeOnce sync.Once
	sessionMu sync.Mutex

	timeLocation           *time.Location
	queryStmtReuseMode     StatementCacheMode
	queryStmtPoolMu        sync.Mutex
	queryStmtFastKey       string
	queryStmtFast          *ClientStmt
	queryStmtPool          map[string][]*ClientStmt
	queryStmtPoolCount     int
	queryStmtPoolCap       int
	queryStmtPoolPerKeyCap int
	catalogGeneration      atomic.Uint64
}

func (c *ClientConn) Close() (ret error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.close()
}

func (c *ClientConn) close() (ret error) {
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

func (c *ClientConn) IOMetrics(reset bool) (readBytes uint64, writtenBytes uint64, enabled bool) {
	if c == nil || c.handle == nil {
		return 0, 0, false
	}
	if reset {
		return c.handle.ResetIOMetrics()
	}
	return c.handle.IOMetrics()
}

func (c *ClientConn) SupportsDatabaseMetadata() bool {
	return c != nil && c.handle != nil && c.handle.SupportsDatabaseMetadata()
}

func (c *ClientConn) Error() error {
	return c.ErrorOf(nil)
}

func (c *ClientConn) ErrorOf(cause error) error {
	code, msg := c.handle.Error()
	if code == 0 && msg == "" && cause == nil {
		// no error
		return nil
	} else if code == 0 && msg != "" {
		// code == 0 means client-side error
		if cause == nil {
			return fmt.Errorf("MACHCLI %s", msg)
		} else {
			return fmt.Errorf("MACHCLI %s, %s", msg, cause.Error())
		}
	} else {
		// code > 0 means server-side error
		if cause == nil {
			return fmt.Errorf("MACHCLI-ERR-%d, %s", code, msg)
		} else {
			return fmt.Errorf("MACHCLI-ERR-%d, %s", code, msg)
		}
	}
}

func queryHead(query string) string {
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToUpper(parts[0])
}

func quoteClientIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (c *ClientConn) shouldReuseStmtForQuery(query string) bool {
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

func (c *ClientConn) acquireQueryStmt(query string) (*ClientStmt, error) {
	generation := c.catalogGeneration.Load()
	if !c.shouldReuseStmtForQuery(query) {
		stmt, err := c.NewStmt()
		if err != nil {
			return nil, err
		}
		stmt.sqlHead = queryHead(query)
		stmt.catalogGeneration = generation
		if err := stmt.prepare(query); err != nil {
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
			return c.acquireQueryStmt(query)
		}
		stmt.sqlHead = queryHead(query)
		stmt.reachEOF = false
		if stmt.handle.SupportsReprepare() {
			if err := stmt.prepare(query); err != nil {
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
			return c.acquireQueryStmt(query)
		}
		stmt.sqlHead = queryHead(query)
		stmt.reachEOF = false
		if stmt.handle.SupportsReprepare() {
			if err := stmt.prepare(query); err != nil {
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
	stmt.sqlHead = queryHead(query)
	stmt.catalogGeneration = generation
	if err := stmt.prepare(query); err != nil {
		_ = stmt.Close()
		return nil, err
	}
	return stmt, nil
}

func (c *ClientConn) releaseQueryStmt(query string, stmt *ClientStmt, reusable bool) error {
	if stmt == nil {
		return nil
	}
	if !c.shouldReuseStmtForQuery(query) {
		return stmt.Close()
	}
	if !reusable {
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

func (c *ClientConn) closeQueryStmtPool() error {
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
	statements := make([]*ClientStmt, 0, capHint)
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

func (c *ClientConn) Explain(ctx context.Context, query string, full bool) (string, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

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

	if err := stmt.ExecDirect(query); err != nil {
		return "", c.ErrorOf(err)
	}

	ret := make([]string, 0, 20)
	for {
		if row, err := stmt.Fetch(); err != nil {
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

func (c *ClientConn) Exec(ctx context.Context, query string, args ...any) *ClientResult {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.exec(ctx, query, args...)
}

func (c *ClientConn) exec(ctx context.Context, query string, args ...any) *ClientResult {
	ret := &ClientResult{}
	if len(args) == 0 {
		stmt, err := c.NewStmt()
		if err != nil {
			ret.err = err
			return ret
		}
		defer stmt.Close()

		stmt.sqlHead = queryHead(query)
		if err := stmt.handle.ExecDirect(query); err != nil {
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
		if ret.stmtType == machnet.QPP_STMT_TYPE_ALTER_SESSION_SET ||
			ret.stmtType == machnet.QPP_STMT_TYPE_CONNECT_USER {
			c.catalogGeneration.Add(1)
			if err := c.closeQueryStmtPool(); err != nil {
				ret.err = err
			}
		}
		return ret
	}

	stmt, err := c.acquireQueryStmt(query)
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
	ret.err = stmt.execute()
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

func (c *ClientConn) Prepare(ctx context.Context, query string) (*ClientPreparedStmt, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	stmt, err := c.NewStmt()
	if err != nil {
		return nil, err
	}

	stmt.sqlHead = queryHead(query)
	if err := stmt.prepare(query); err != nil {
		stmt.Close()
		return nil, err
	}
	return &ClientPreparedStmt{stmt: stmt}, nil
}

func (c *ClientConn) QueryRow(ctx context.Context, query string, args ...any) *ClientRow {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.queryRow(ctx, query, args...)
}

func (c *ClientConn) queryRow(ctx context.Context, query string, args ...any) *ClientRow {
	ret := &ClientRow{timeLocation: c.timeLocation}
	stmt, err := c.acquireQueryStmt(query)
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
	if ret.err = stmt.execute(); ret.err != nil {
		return ret
	}
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
			Name:        desc.Name,
			Length:      desc.Size,
			Type:        desc.Type.ColumnType(),
			DataType:    desc.Type.DataType(),
			Nullable:    desc.Nullable,
			Nullability: desc.Nullability,
			PrimaryKey:  desc.PrimaryKey,
		}
	}
	if values, err := stmt.fetch(); err != nil {
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

func (c *ClientConn) Query(ctx context.Context, query string, args ...any) (*ClientRows, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.query(ctx, query, args...)
}

func (c *ClientConn) query(ctx context.Context, query string, args ...any) (*ClientRows, error) {
	stmt, err := c.acquireQueryStmt(query)
	if err != nil {
		return nil, err
	}
	if err := stmt.bindParams(args...); err != nil {
		if relErr := c.releaseQueryStmt(query, stmt, false); relErr != nil {
			return nil, relErr
		}
		return nil, err
	}
	if err := stmt.execute(); err != nil {
		if relErr := c.releaseQueryStmt(query, stmt, false); relErr != nil {
			return nil, relErr
		}
		return nil, err
	}
	ret := &ClientRows{
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

type ClientPreparedStmt struct {
	stmt *ClientStmt
}

func (pStmt *ClientPreparedStmt) Close() error {
	if pStmt.stmt == nil {
		return nil
	}
	err := pStmt.stmt.Close()
	pStmt.stmt = nil
	return err
}

func (pStmt *ClientPreparedStmt) Exec(ctx context.Context, params ...any) *ClientResult {
	ret := &ClientResult{}
	if err := pStmt.stmt.reprepareIfSupported(); err != nil {
		ret.err = err
		return ret
	}
	defer pStmt.stmt.handle.ExecuteClean()
	if err := pStmt.stmt.bindParams(params...); err != nil {
		ret.err = err
		return ret
	}
	if err := pStmt.stmt.execute(); err != nil {
		ret.err = err
		return ret
	}
	ret.rowCount = pStmt.stmt.rowCount
	ret.rowID, ret.hasRowID = pStmt.stmt.handle.GeneratedRowID()
	if typ, err := pStmt.stmt.handle.GetStmtType(); err != nil {
		ret.err = err
		return ret
	} else {
		ret.stmtType = typ
	}
	return ret
}

func (pStmt *ClientPreparedStmt) Query(ctx context.Context, params ...any) (*ClientRows, error) {
	if err := pStmt.stmt.reprepareIfSupported(); err != nil {
		return nil, err
	}
	if err := pStmt.stmt.bindParams(params...); err != nil {
		return nil, err
	}
	if err := pStmt.stmt.execute(); err != nil {
		return nil, err
	}
	ret := &ClientRows{
		stmt:         pStmt.stmt,
		isPrepared:   true,
		timeLocation: pStmt.stmt.conn.timeLocation,
	}
	if typ, err := pStmt.stmt.handle.GetStmtType(); err != nil {
		return nil, err
	} else {
		ret.stmtType = typ
	}
	if !ret.stmtType.IsSelect() {
		ret.rowsCount = 0
	} else {
		ret.rowsCount = pStmt.stmt.rowCount
	}
	return ret, nil
}

func (pStmt *ClientPreparedStmt) QueryRow(ctx context.Context, params ...any) *ClientRow {
	ret := &ClientRow{timeLocation: pStmt.stmt.conn.timeLocation}
	if err := pStmt.stmt.reprepareIfSupported(); err != nil {
		ret.err = err
		return ret
	}
	if err := pStmt.stmt.bindParams(params...); err != nil {
		ret.err = err
		return ret
	}
	if err := pStmt.stmt.execute(); err != nil {
		ret.err = err
		return ret
	}
	ret.rowCount = pStmt.stmt.rowCount
	if values, err := pStmt.stmt.fetch(); err != nil {
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
	ret.columns = make(Columns, len(pStmt.stmt.columnDesc))
	for i, desc := range pStmt.stmt.columnDesc {
		ret.columns[i] = &Column{
			Name:        desc.Name,
			Length:      desc.Size,
			Type:        desc.Type.ColumnType(),
			DataType:    desc.Type.DataType(),
			Nullable:    desc.Nullable,
			Nullability: desc.Nullability,
			PrimaryKey:  desc.PrimaryKey,
		}
	}
	return ret
}

func (stmt *ClientStmt) prepare(query string) error {
	if err := stmt.handle.Prepare(query); err != nil {
		return stmt.ErrorOf(err)
	}
	stmt.sqlText = query
	stmt.columnDesc = nil
	stmt.rowCount = 0
	stmt.execCount = 0
	stmt.reachEOF = false
	return nil
}

func (stmt *ClientStmt) reprepareIfSupported() error {
	if stmt == nil || stmt.handle == nil || !stmt.handle.SupportsReprepare() {
		return nil
	}
	return stmt.prepare(stmt.sqlText)
}

func (stmt *ClientStmt) bindParams(args ...any) error {
	numParam, err := stmt.handle.NumParam()
	if err != nil {
		return stmt.ErrorOf(err)
	}
	args, err = stmt.mapNamedParams(args, numParam)
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
			pd, err := stmt.handle.DescribeParam(idx)
			if err != nil {
				return stmt.ErrorOf(err)
			}
			if val == nil {
				sqlType = pd.Type
				value = nil
			} else {
				return fmt.Errorf("bind unknown type at column %d %T, expect: %d", idx, val, pd.Type)
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
			sqlType = api.SqlTypeString
			value = val
		case *string:
			sqlType = api.SqlTypeString
			value = *val
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
		}
		if err := stmt.handle.BindParam(idx, sqlType, value); err != nil {
			return stmt.ErrorOf(err)
		}
	}
	return nil
}

func (stmt *ClientStmt) mapNamedParams(args []any, numParam int) ([]any, error) {
	hasNamed := false
	for _, arg := range args {
		switch arg.(type) {
		case NamedParam, *NamedParam:
			hasNamed = true
		}
	}
	if !hasNamed {
		return args, nil
	}
	provided := make(map[string]any, len(args))
	for _, arg := range args {
		var named NamedParam
		switch value := arg.(type) {
		case NamedParam:
			named = value
		case *NamedParam:
			if value == nil {
				return nil, fmt.Errorf("named parameter is nil")
			}
			named = *value
		default:
			return nil, fmt.Errorf("named and positional parameters cannot be mixed")
		}
		if named.Name == "" {
			return nil, fmt.Errorf("named parameter name is empty")
		}
		if _, exists := provided[named.Name]; exists {
			return nil, fmt.Errorf("duplicate named parameter %q", named.Name)
		}
		provided[named.Name] = named.Value
	}
	ret := make([]any, numParam)
	required := make(map[string]struct{}, numParam)
	for idx := 0; idx < numParam; idx++ {
		desc, err := stmt.handle.DescribeParam(idx)
		if err != nil {
			return nil, stmt.ErrorOf(err)
		}
		if desc.Name == "" {
			return nil, fmt.Errorf("named parameters require Machbase protocol 4.0.3 metadata and cannot be mixed with anonymous markers")
		}
		value, exists := provided[desc.Name]
		if !exists {
			return nil, fmt.Errorf("missing named parameter %q", desc.Name)
		}
		ret[idx] = value
		required[desc.Name] = struct{}{}
	}
	for name := range provided {
		if _, exists := required[name]; !exists {
			return nil, fmt.Errorf("unexpected named parameter %q", name)
		}
	}
	return ret, nil
}

func formatResultMessage(err error, stmtType machnet.StmtType, rowCount int64) string {
	if err != nil {
		return err.Error()
	}
	switch stmtType {
	case machnet.QPP_STMT_TYPE_CREATE_TABLE:
		return "table created."
	case machnet.QPP_STMT_TYPE_DROP_TABLE:
		return "table dropped."
	case machnet.QPC_STMT_TYPE_CREATE_ROLLUP:
		return "rollup created."
	case machnet.QPC_STMT_TYPE_DROP_ROLLUP:
		return "rollup dropped."
	case machnet.QPC_STMT_TYPE_CREATE_RETENTION:
		return "retention created."
	case machnet.QPC_STMT_TYPE_DROP_RETENTION:
		return "retention dropped."
	case machnet.QPP_STMT_TYPE_CREATE_INDEX:
		return "index created."
	case machnet.QPP_STMT_TYPE_DROP_INDEX:
		return "index dropped."
	case machnet.QPP_STMT_TYPE_ALTER_INDEX:
		return "index altered."
	case machnet.QPP_STMT_TYPE_CREATE_USER:
		return "user created."
	case machnet.QPP_STMT_TYPE_DROP_USER:
		return "user dropped."
	case machnet.QPP_STMT_TYPE_ALTER_USER:
		return "user altered."
	case machnet.QPP_STMT_TYPE_GRANT_USER:
		return "user granted."
	case machnet.QPP_STMT_TYPE_REVOKE_USER:
		return "user revoked."
	case machnet.QPP_STMT_TYPE_CREATE_VIEW:
		return "view created."
	case machnet.QPP_STMT_TYPE_DROP_VIEW:
		return "view dropped."
	}

	verb := ""
	if stmtType >= 256 && stmtType <= 511 {
		if msg := stmtType.SuccessfulMessage(); msg != "" {
			return msg
		}
	} else if stmtType.IsSelect() {
		verb = "selected."
	} else if stmtType.IsInsert() {
		verb = "inserted."
	} else if stmtType.IsDelete() {
		verb = "deleted."
	} else if stmtType.IsInsertSelect() {
		verb = "inserted from select."
	} else if stmtType.IsUpdate() {
		verb = "updated."
	} else if stmtType.IsExecRollup() {
		verb = "rollup executed."
	} else {
		return fmt.Sprintf("executed (%d).", stmtType)
	}

	switch rowCount {
	case 0:
		return "no rows " + verb
	case 1:
		return "a row " + verb
	default:
		return formatIntWithCommas(rowCount) + " rows " + verb
	}
}

func formatIntWithCommas(value int64) string {
	digits := strconv.FormatInt(value, 10)
	start := 0
	if digits[0] == '-' {
		start = 1
	}
	if len(digits)-start <= 3 {
		return digits
	}

	var builder strings.Builder
	builder.Grow(len(digits) + (len(digits)-start-1)/3)
	if start == 1 {
		builder.WriteByte('-')
	}

	head := (len(digits) - start) % 3
	if head == 0 {
		head = 3
	}
	builder.WriteString(digits[start : start+head])
	for index := start + head; index < len(digits); index += 3 {
		builder.WriteByte(',')
		builder.WriteString(digits[index : index+3])
	}
	return builder.String()
}

type ClientResult struct {
	err      error
	rowCount int64
	stmtType machnet.StmtType
	rowID    uint64
	hasRowID bool
}

func (rs *ClientResult) Message() string {
	return formatResultMessage(rs.err, rs.stmtType, rs.rowCount)
}

func (rs *ClientResult) Err() error {
	return rs.err
}

func (rs *ClientResult) LastInsertId() (int64, error) {
	if rs != nil && rs.hasRowID {
		return int64(rs.rowID), nil
	}
	return 0, errors.New("not implemented LastInsertId")
}

func (rs *ClientResult) RowsAffected() int64 {
	return rs.rowCount
}

func (c *ClientConn) NewStmt() (*ClientStmt, error) {
	var handle *machnet.StmtHandle
	if h, err := c.handle.AllocStmt(); err != nil {
		return nil, c.ErrorOf(err)
	} else {
		handle = h
	}
	ret := &ClientStmt{conn: c, handle: handle}
	return ret, nil
}

type ClientStmt struct {
	handle            *machnet.StmtHandle
	conn              *ClientConn
	sqlText           string
	columnDesc        []ColumnDesc
	reachEOF          bool
	sqlHead           string
	rowCount          int64
	execCount         int64
	catalogGeneration uint64
}

func (stmt *ClientStmt) Close() error {
	if err := stmt.handle.Free(); err == nil {
		return nil
	} else {
		return stmt.ErrorOf(err)
	}
}

func (stmt *ClientStmt) Error() error {
	return stmt.ErrorOf(nil)
}

func (stmt *ClientStmt) ErrorOf(cause error) error {
	code, msg := stmt.handle.Error()
	if code == 0 && msg == "" && cause == nil {
		// no error
		return nil
	} else if code == 0 && msg != "" {
		// code == 0 means client-side error
		if cause == nil {
			return fmt.Errorf("MACHCLI %s", msg)
		} else {
			return fmt.Errorf("MACHCLI %s, %s", msg, cause.Error())
		}
	} else {
		// code > 0 means server-side error
		if cause == nil {
			return fmt.Errorf("MACHCLI-ERR-%d, %s", code, msg)
		} else {
			return fmt.Errorf("MACHCLI-ERR-%d, %s", code, msg)
		}
	}

}

func (stmt *ClientStmt) execute() error {
	stmt.reachEOF = false
	if err := stmt.handle.Execute(); err != nil {
		return stmt.ErrorOf(err)
	}
	defer func() {
		stmt.execCount++
	}()
	if rowCount, err := stmt.handle.RowCount(); err != nil {
		return stmt.ErrorOf(err)
	} else {
		stmt.rowCount = rowCount
	}
	if stmt.execCount > 0 {
		return nil
	}
	if stmt.sqlHead != "SELECT" {
		return nil
	}
	num, err := stmt.handle.NumResultCol()
	if err != nil {
		return stmt.ErrorOf(err)
	}
	stmt.columnDesc = make([]ColumnDesc, num)
	for i := 0; i < num; i++ {
		d := ColumnDesc{}
		if err := stmt.handle.DescribeColEx(i, &d.Name, (*api.SqlType)(&d.Type), &d.Size, &d.Scale, &d.Nullable, &d.Nullability, &d.PrimaryKey); err != nil {
			return stmt.ErrorOf(err)
		}
		stmt.columnDesc[i] = d
	}
	return nil
}

// fetch fetches the next row from the result set.
// It returns true if it reaches end of the result, otherwise false.
func (stmt *ClientStmt) fetch() ([]any, error) {
	if stmt.reachEOF {
		return nil, errors.New("fetch reached end of the result set")
	}
	row, err := stmt.handle.Fetch()
	if err != nil {
		return nil, err
	}
	stmt.reachEOF = row == nil
	if stmt.reachEOF {
		return nil, io.EOF
	}
	if row == nil {
		return nil, io.EOF
	}
	return row, nil
}

type ClientRow struct {
	err          error
	values       []any
	columns      Columns
	rowCount     int64
	stmtType     machnet.StmtType
	timeLocation *time.Location
}

func (r *ClientRow) Success() bool {
	return r.err == nil
}

func (r *ClientRow) Err() error {
	return r.err
}

func (r *ClientRow) Columns() (Columns, error) {
	return r.columns, nil
}

func (r *ClientRow) Scan(dest ...any) error {
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

func (r *ClientRow) Values() []any {
	return r.values
}

func (r *ClientRow) RowsAffected() int64 {
	return r.rowCount
}

func (r *ClientRow) Message() string {
	return formatResultMessage(r.err, r.stmtType, r.rowCount)
}

type ClientRows struct {
	stmt            *ClientStmt
	err             error
	row             []any
	rowsCount       int64
	stmtType        machnet.StmtType
	isPrepared      bool
	queryStmtPooled bool
	queryStmtKey    string
	timeLocation    *time.Location
}

func (r *ClientRows) Err() error {
	return r.err
}

func (r *ClientRows) Close() error {
	if r.stmt == nil {
		return nil
	}
	stmt := r.stmt
	r.stmt = nil
	if r.isPrepared {
		stmt.reachEOF = false
		return stmt.handle.ExecuteClean()
	}
	if r.queryStmtPooled && stmt.conn != nil {
		return stmt.conn.releaseQueryStmt(r.queryStmtKey, stmt, true)
	}
	return stmt.Close()
}

func (r *ClientRows) IsFetchable() bool {
	if r.stmt == nil || r.stmt.handle == nil {
		return false
	}
	typ, _ := r.stmt.handle.GetStmtType()
	return typ.IsSelect()
}

func (r *ClientRows) Columns() (Columns, error) {
	if r.stmt == nil {
		return nil, nil
	}
	ret := make(Columns, len(r.stmt.columnDesc))
	for i, desc := range r.stmt.columnDesc {
		ret[i] = &Column{
			Name:        desc.Name,
			Length:      desc.Size,
			Type:        desc.Type.ColumnType(),
			DataType:    desc.Type.DataType(),
			Nullable:    desc.Nullable,
			Nullability: desc.Nullability,
			PrimaryKey:  desc.PrimaryKey,
		}
	}
	return ret, nil
}

func (r *ClientRows) Message() string {
	return formatResultMessage(r.err, r.stmtType, r.rowsCount)
}

func (r *ClientRows) RowsAffected() int64 {
	return r.rowsCount
}

func (r *ClientRows) Next() bool {
	if r.stmt == nil {
		return false
	}
	if r.stmt.reachEOF {
		return false
	}
	row, err := r.stmt.fetch()
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

func (r *ClientRows) Row() []any {
	return r.row
}

func (r *ClientRows) ColumnDescriptions() []ColumnDesc {
	if r.stmt == nil {
		return nil
	}
	return r.stmt.columnDesc
}

func (r *ClientRows) Scan(dest ...any) error {
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

func (c *ClientConn) Appender(ctx context.Context, tableName string) (*ClientAppender, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	db, user, table := parseTableName(tableName, "", c.user)
	tableId := int64(-1)
	var tableType TableType = TableType(-1)
	var tableFlag TableFlag
	var tableColCount int

	dbId := int64(-1)
	if c.handle.SupportsDatabaseMetadata() {
		var dbRow *ClientRow
		if db == "" {
			dbRow = c.queryRow(ctx, "select DATABASE_ID from V$DATABASES where NAME = CURRENT_DATABASE()")
		} else {
			dbRow = c.queryRow(ctx, "select DATABASE_ID from V$DATABASES where NAME = ?", db)
		}
		if err := dbRow.Scan(&dbId); err != nil {
			return nil, err
		}
	} else if db != "" && db != "MACHBASEDB" {
		dbRow := c.queryRow(ctx, "select BACKUP_TBSID from V$STORAGE_MOUNT_DATABASES where MOUNTDB = ?", db)
		if err := dbRow.Scan(&dbId); err != nil {
			return nil, err
		}
	}

	if user == "" {
		user = c.user
	}

	describeSqlText := `SELECT
			j.ID as TABLE_ID,
			j.TYPE as TABLE_TYPE,
			j.FLAG as TABLE_FLAG,
			j.COLCOUNT as TABLE_COLCOUNT
		from
			M$SYS_USERS u,
			M$SYS_TABLES j
		where
			u.NAME = ?
		and j.USER_ID = u.USER_ID
		and j.DATABASE_ID = ?
		and j.NAME = ?`

	r := c.queryRow(ctx, describeSqlText, user, dbId, table)
	if r.Err() != nil {
		if r.Err() == sql.ErrNoRows {
			return nil, fmt.Errorf("table '%s' does not exist", tableName)
		}
		return nil, r.Err()
	}
	if err := r.Scan(&tableId, &tableType, &tableFlag, &tableColCount); err != nil {
		return nil, err
	}
	if tableType != TableTypeLog && tableType != TableTypeTag && tableType != TableTypeTransaction {
		return nil, fmt.Errorf("%s '%s' doesn't support append", tableType, tableName)
	}
	rows, err := c.query(ctx, "select name, type, length, id, flag from M$SYS_COLUMNS where table_id = ? and database_id = ? order by id", tableId, dbId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := &ClientAppender{tableName: strings.ToUpper(tableName), tableType: tableType}
	ret.errCheckCount = 0
	for rows.Next() {
		col := &Column{}
		err = rows.Scan(&col.Name, &col.Type, &col.Length, &col.Id, &col.Flag)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(col.Name, "_") {
			if tableType != TableTypeLog || col.Name != "_ARRIVAL_TIME" {
				continue
			}
		}
		col.DataType = col.Type.DataType()
		ret.columns = append(ret.columns, col)
		ret.columnNames = append(ret.columnNames, col.Name)
		ret.columnTypes = append(ret.columnTypes, col.Type.ToSqlType())
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}

	stmt, err := c.NewStmt()
	if err != nil {
		return nil, err
	}
	ret.stmt = stmt

	openName := ret.tableName
	restoreDB := ""
	if db != "" {
		var currentDB string
		row := c.queryRow(ctx, "SELECT CURRENT_DATABASE()")
		if row.Err() != nil {
			stmt.Close()
			return nil, row.Err()
		}
		if err := row.Scan(&currentDB); err != nil {
			stmt.Close()
			return nil, err
		}
		if !strings.EqualFold(currentDB, db) {
			if err := c.exec(ctx, "USE "+quoteClientIdentifier(db)).Err(); err != nil {
				stmt.Close()
				return nil, err
			}
			restoreDB = currentDB
		}
		openName = user + "." + table
	}

	openErr := stmt.handle.AppendOpen(openName, ret.errCheckCount)
	if restoreDB != "" {
		if restoreErr := c.exec(ctx, "USE "+quoteClientIdentifier(restoreDB)).Err(); restoreErr != nil {
			var cleanupErr error
			var appendOpenErr error
			if openErr != nil {
				appendOpenErr = stmt.ErrorOf(openErr)
			}
			if openErr == nil {
				if _, _, err := stmt.handle.AppendClose(); err != nil {
					cleanupErr = stmt.ErrorOf(err)
				}
			}
			if err := stmt.Close(); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
			if err := c.close(); err != nil {
				cleanupErr = errors.Join(cleanupErr, err)
			}
			restoreErr = fmt.Errorf("restore current database %s: %w", restoreDB, restoreErr)
			return nil, errors.Join(appendOpenErr, restoreErr, cleanupErr)
		}
	}
	if openErr != nil {
		err := stmt.ErrorOf(openErr)
		stmt.Close()
		return nil, err
	}
	return ret, nil
}

type ClientAppender struct {
	stmt          *ClientStmt
	tableName     string
	tableType     TableType
	errCheckCount int
	columns       Columns
	columnNames   []string
	columnTypes   []api.SqlType
	inputColumns  []ClientAppenderInputColumn
	inputFormats  []string
	closed        bool
	successCount  int64
	failCount     int64
}

type ClientAppenderInputColumn struct {
	Name string
	Idx  int
}

// Close returns the number of success and fail rows.
func (a *ClientAppender) Close() (int64, int64, error) {
	if a.closed {
		return a.successCount, a.failCount, nil
	}
	a.closed = true
	var err error

	//// even if error occurred, we should close the statement
	a.successCount, a.failCount, err = a.stmt.handle.AppendClose()

	if errClose := a.stmt.Close(); errClose != nil {
		return a.successCount, a.failCount, a.stmt.ErrorOf(errClose)
	}
	return a.successCount, a.failCount, err
}

func (a *ClientAppender) WithInputColumns(columns ...string) *ClientAppender {
	a.inputColumns = nil
	for _, col := range columns {
		a.inputColumns = append(a.inputColumns, ClientAppenderInputColumn{Name: strings.ToUpper(col), Idx: -1})
	}
	if len(a.inputColumns) > 0 {
		for idx, col := range a.columns {
			for inIdx, inputCol := range a.inputColumns {
				if col.Name == inputCol.Name {
					a.inputColumns[inIdx].Idx = idx
				}
			}
		}
	}
	return a
}

func (a *ClientAppender) WithInputFormats(formats ...string) *ClientAppender {
	a.inputFormats = formats
	return a
}

// WithBatchMaxRows sets the maximum batch size in rows for batch append. If the batch size exceeds the limit, it will be flushed immediately.
// The default value is 512 rows. The minimum value is 1 row.
func (a *ClientAppender) WithBatchMaxRows(rows int) *ClientAppender {
	if a.stmt == nil || a.stmt.handle == nil {
		return a
	}
	if rows < 1 {
		rows = 1
	}
	a.stmt.handle.SetAppendBatchMaxRows(rows)
	return a
}

// WithBatchMaxBytes sets the maximum batch size in bytes for batch append. If the batch size exceeds the limit, it will be flushed immediately.
// The default value is 512KB. The minimum value is 4KB.
func (a *ClientAppender) WithBatchMaxBytes(bytes int) *ClientAppender {
	if a.stmt == nil || a.stmt.handle == nil {
		return a
	}
	if bytes < 4*1024 {
		bytes = 4 * 1024
	}
	a.stmt.handle.SetAppendBatchMaxBytes(bytes)
	return a
}

// WithBatchMaxDelay sets the maximum delay for batch append. If the batch is not full, it will be flushed when the delay is reached.
// The default value is 5 milliseconds.
// The minimum value is 1ms.
// 0 means no delay-based flush.
func (a *ClientAppender) WithBatchMaxDelay(duration time.Duration) *ClientAppender {
	if a.stmt == nil || a.stmt.handle == nil {
		return a
	}
	if duration <= 0 {
		duration = 0
	} else if duration < time.Millisecond {
		duration = time.Millisecond
	}
	a.stmt.handle.SetAppendBatchMaxDelay(duration)
	return a
}

func (a *ClientAppender) TableName() string {
	return a.tableName
}

func (a *ClientAppender) TableType() TableType {
	return a.tableType
}

func (a *ClientAppender) Columns() (Columns, error) {
	return a.columns, nil
}

func (a *ClientAppender) Flush() error {
	if err := a.stmt.handle.AppendFlush(); err == nil {
		return nil
	} else {
		return a.stmt.ErrorOf(err)
	}
}

func (a *ClientAppender) Append(values ...any) error {
	switch a.tableType {
	case TableTypeTag, TableTypeTransaction:
		return a.append(values...)
	case TableTypeLog:
		var valuesWithTime []any
		if len(values) == len(a.columns) {
			valuesWithTime = values
		} else {
			valuesWithTime = append([]any{time.Time{}}, values...)
		}
		return a.append(valuesWithTime...)
	default:
		return fmt.Errorf("%s can not be appended", a.tableName)
	}
}

func (a *ClientAppender) AppendLogTime(ts time.Time, values ...any) error {
	if a.tableType != TableTypeLog {
		return fmt.Errorf("%s is not a log table, use Append() instead", a.tableName)
	}
	values = append([]any{ts}, values...)
	return a.append(values...)
}

func (a *ClientAppender) append(values ...any) error {
	if len(a.columns) == 0 {
		return fmt.Errorf("table '%s' has no columns", a.tableName)
	}
	if len(a.inputColumns) > 0 {
		if len(a.inputColumns) != len(values) {
			return fmt.Errorf("value count %d, table '%s' requires %d columns to append", len(values), a.tableName, len(a.columns))
		}
		newValues := make([]any, len(a.columns))
		for i, inputCol := range a.inputColumns {
			newValues[inputCol.Idx] = values[i]
		}
		values = newValues
	} else {
		if len(a.columns) != len(values) {
			return fmt.Errorf("value count %d, table '%s' requires %d columns to append", len(values), a.tableName, len(a.columns))
		}
	}
	if a.closed {
		return errors.New("closed appender")
	}
	if a.stmt == nil || a.stmt.conn == nil {
		return errors.New("invalid connection")
	}

	if err := a.stmt.handle.AppendData(a.columnTypes, a.columnNames, values, a.inputFormats); err != nil {
		return a.stmt.ErrorOf(err)
	}
	return nil
}
