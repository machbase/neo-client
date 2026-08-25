package machgo

import (
	"context"
	"crypto"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machnet"
)

const (
	defaultQueryStmtPoolCap         = 128
	defaultQueryStmtPoolPerQueryCap = 8
	defaultFetchRows                = 1000
)

type Config struct {
	Host string
	Port int

	AlternativeHost string
	AlternativePort int

	MaxOpenConn        int     // deprecated
	MaxOpenConnFactor  float64 // deprecated
	MaxOpenQuery       int     // deprecated
	MaxOpenQueryFactor float64 // deprecated

	// StatementCache controls the statement cache mode for the connection.
	// Statement cache can improve performance by reusing prepared statements for identical queries.
	// It can be set for each connection using the ConnectOptionStatementCache option in the Connect method.
	StatementCache api.StatementCacheMode

	// FetchRows is used to the default fetch rows if the option is not specified in Connect options.
	// If the value is not specified or less than or equal to 0, the defaultFetchRows is used.
	FetchRows int64

	// unused
	ConType int
}

type Database struct {
	handle *machnet.EnvHandle
	host   string
	port   int

	alternativeHost string
	alternativePort int

	maxConnsMutex sync.RWMutex
	maxConnsChan  chan struct{}

	statementCache api.StatementCacheMode
	fetchRows      int64
}

var _ api.Database = (*Database)(nil)

func NewDatabase(conf *Config) (*Database, error) {
	var handle *machnet.EnvHandle
	if h, err := machnet.Initialize(); err != nil {
		return nil, err
	} else {
		handle = h
	}
	ret := &Database{
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

	ret.SetMaxOpenConns(-1)
	return ret, nil
}

func (db *Database) Close() error {
	if err := db.handle.Finalize(); err == nil {
		return nil
	} else {
		return db.ErrorOf(err)
	}
}

func (db *Database) ErrorOf(cause error) error {
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

// MaxOpenConns returns the maximum number of open connections
// and the current remains capacity.
func (db *Database) MaxOpenConns() (int, int) {
	db.maxConnsMutex.RLock()
	defer db.maxConnsMutex.RUnlock()
	if db.maxConnsChan == nil {
		// unlimited
		return -1, -1
	}
	limit := cap(db.maxConnsChan)
	remains := len(db.maxConnsChan)
	return limit, remains
}

func (db *Database) SetMaxOpenConns(desiredMaxOpenConns int) {
	if desiredMaxOpenConns < 0 {
		desiredMaxOpenConns = -1
	}
	if desiredMaxOpenConns == 0 {
		desiredMaxOpenConns = int(float64(runtime.NumCPU()) * 1.5)
	}

	currentCap := cap(db.maxConnsChan)
	if currentCap == desiredMaxOpenConns {
		return
	}

	var newChan chan struct{}
	db.maxConnsMutex.Lock()
	defer func() {
		db.maxConnsChan = newChan
		db.maxConnsMutex.Unlock()
	}()

	if desiredMaxOpenConns > 0 {
		newChan = make(chan struct{}, desiredMaxOpenConns)
		for i := 0; i < desiredMaxOpenConns; i++ {
			newChan <- struct{}{}
		}
	}
}

func (db *Database) Ping(ctx context.Context) (time.Duration, error) {
	tick := time.Now()
	if conn, err := db.Connect(ctx, api.WithPassword("sys", "manager")); err != nil {
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

func (db *Database) UserAuth(ctx context.Context, user, password string) (bool, string, error) {
	conn, err := db.Connect(ctx, api.WithPassword(user, password))
	if err != nil {
		return false, "invalid username or password", nil
	}
	err = conn.Close()
	return true, "", err
}

func (db *Database) connectionString(user string, password string, fetchRows int64, ioMetrics bool, authMode string) string {
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

func (db *Database) Connect(ctx context.Context, opts ...api.ConnectOption) (api.Conn, error) {
	var user, password string
	var stmtReuse = db.statementCache
	var fetchRows = db.fetchRows
	var enabledIOMetrics bool = false
	var authMode string
	var authKey crypto.PrivateKey = nil
	var proxyUser string
	var database string
	var timeLocation *time.Location = time.UTC

	for _, opt := range opts {
		switch o := opt.(type) {
		case *api.ConnectOptionPassword:
			user = o.User
			password = o.Password
			authMode = "PASSWORD"
		case *api.ConnectOptionStatementCache:
			stmtReuse = o.Mode
		case *api.ConnectOptionFetchRows:
			fetchRows = o.Rows
		case *api.ConnectOptionIOMetrics:
			enabledIOMetrics = o.Enabled
		case *api.ConnectOptionAuthKey:
			user = o.User
			authMode = o.AuthMode
			authKey = o.Key
		case *api.ConnectOptionProxyUser:
			proxyUser = o.ProxyUser
		case *api.ConnectOptionDatabase:
			database = o.Database
		case *api.ConnectOptionTimeLocation:
			timeLocation = o.Location
		default:
			return nil, fmt.Errorf("unknown option type-%T", o)
		}
	}

	if strings.EqualFold(user, "sys") && proxyUser != "" && !strings.EqualFold(proxyUser, "sys") {
		// "SYS AS PROXY_USER" format is required for proxy user authentication,
		// and the proxy user cannot be "SYS" ('sys as sys' is 'sys').
		user = fmt.Sprintf("SYS AS %s", strings.ToUpper(proxyUser))
	}

	returnChan := db.maxConnsChan
	tokenAcquired := false

	if returnChan != nil {
		select {
		case <-returnChan:
			tokenAcquired = true
		case <-ctx.Done():
			return nil, api.NewError("connect canceled")
		}
	}
	defer func() {
		if tokenAcquired && returnChan != nil {
			returnChan <- struct{}{}
		}
	}()
	var handle *machnet.ConnHandle
	if c, err := db.handle.Connect(db.connectionString(user, password, fetchRows, enabledIOMetrics, authMode), authKey); err != nil {
		return nil, db.ErrorOf(err)
	} else {
		handle = c
	}

	ret := &Conn{
		db:                     db,
		handle:                 handle,
		user:                   strings.ToUpper(user),
		usedAt:                 time.Now(),
		returnChan:             returnChan,
		timeLocation:           timeLocation,
		queryStmtReuseMode:     stmtReuse,
		queryStmtPool:          map[string][]*Stmt{},
		queryStmtPoolCap:       defaultQueryStmtPoolCap,
		queryStmtPoolPerKeyCap: defaultQueryStmtPoolPerQueryCap,
	}
	tokenAcquired = false
	if strings.TrimSpace(database) != "" && !strings.EqualFold(database, "MACHBASEDB") {
		if err := ret.Exec(ctx, "USE "+quoteIdentifier(database)).Err(); err != nil {
			return nil, errors.Join(err, ret.Close())
		}
	}
	return ret, nil
}

type Conn struct {
	handle *machnet.ConnHandle
	db     *Database

	user       string
	usedAt     time.Time
	usedCount  int64
	closeOnce  sync.Once
	returnChan chan struct{}
	sessionMu  sync.Mutex

	timeLocation           *time.Location
	queryStmtReuseMode     api.StatementCacheMode
	queryStmtPoolMu        sync.Mutex
	queryStmtFastKey       string
	queryStmtFast          *Stmt
	queryStmtPool          map[string][]*Stmt
	queryStmtPoolCount     int
	queryStmtPoolCap       int
	queryStmtPoolPerKeyCap int
	catalogGeneration      atomic.Uint64
}

var _ api.Conn = (*Conn)(nil)

func (c *Conn) Close() (ret error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.close()
}

func (c *Conn) close() (ret error) {
	c.closeOnce.Do(func() {
		defer func() {
			c.usedAt = time.Now()
			c.usedCount++
			if c.returnChan != nil {
				c.returnChan <- struct{}{}
			}
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

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func (c *Conn) shouldReuseStmtForQuery(query string) bool {
	switch c.queryStmtReuseMode {
	case api.StatementCacheOn:
		return true
	case api.StatementCacheOff:
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

func (c *Conn) acquireQueryStmt(query string) (*Stmt, error) {
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
		// The server still holds an open cursor for this statement id.
		// Freeing it is the only way to drop a partially consumed result set.
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
	if c.queryStmtReuseMode == api.StatementCacheOff {
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

func (c *Conn) Explain(ctx context.Context, query string, full bool) (string, error) {
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

func (c *Conn) Exec(ctx context.Context, query string, args ...any) api.Result {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.exec(ctx, query, args...)
}

func (c *Conn) exec(ctx context.Context, query string, args ...any) api.Result {
	ret := &Result{}
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

func (c *Conn) Prepare(ctx context.Context, query string) (api.Stmt, error) {
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
	return &PreparedStmt{stmt: stmt}, nil
}

func (c *Conn) QueryRow(ctx context.Context, query string, args ...any) api.Row {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.queryRow(ctx, query, args...)
}

func (c *Conn) queryRow(ctx context.Context, query string, args ...any) api.Row {
	ret := &Row{timeLocation: c.timeLocation}
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
	ret.columns = make(api.Columns, len(stmt.columnDesc))
	for i, desc := range stmt.columnDesc {
		ret.columns[i] = &api.Column{
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

func (c *Conn) Query(ctx context.Context, query string, args ...any) (api.Rows, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.query(ctx, query, args...)
}

func (c *Conn) query(ctx context.Context, query string, args ...any) (api.Rows, error) {
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
	ret := &Rows{
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

type PreparedStmt struct {
	stmt *Stmt
}

func (pStmt *PreparedStmt) Close() error {
	if pStmt.stmt == nil {
		return nil
	}
	err := pStmt.stmt.Close()
	pStmt.stmt = nil
	return err
}

func (pStmt *PreparedStmt) Exec(ctx context.Context, params ...any) api.Result {
	ret := &Result{}
	if err := pStmt.stmt.discardOpenCursor(); err != nil {
		ret.err = err
		return ret
	}
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

func (pStmt *PreparedStmt) Query(ctx context.Context, params ...any) (api.Rows, error) {
	if err := pStmt.stmt.discardOpenCursor(); err != nil {
		return nil, err
	}
	if err := pStmt.stmt.reprepareIfSupported(); err != nil {
		return nil, err
	}
	if err := pStmt.stmt.bindParams(params...); err != nil {
		return nil, err
	}
	if err := pStmt.stmt.execute(); err != nil {
		return nil, err
	}
	ret := &Rows{
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

func (pStmt *PreparedStmt) QueryRow(ctx context.Context, params ...any) api.Row {
	ret := &Row{timeLocation: pStmt.stmt.conn.timeLocation}
	if err := pStmt.stmt.discardOpenCursor(); err != nil {
		ret.err = err
		return ret
	}
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
	ret.columns = make(api.Columns, len(pStmt.stmt.columnDesc))
	for i, desc := range pStmt.stmt.columnDesc {
		ret.columns[i] = &api.Column{
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

func (stmt *Stmt) prepare(query string) error {
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

// renewHandle frees the underlying statement and allocates a freshly prepared
// one. It is used when a result set is abandoned before the server delivered
// its last chunk, since any further Prepare/Execute on that statement id would
// fail with MACHCLI-ERR-3008 (fetch in progress).
func (stmt *Stmt) renewHandle() error {
	if stmt == nil || stmt.conn == nil || stmt.conn.handle == nil {
		return nil
	}
	sqlText := stmt.sqlText
	if err := stmt.Close(); err != nil {
		return err
	}
	handle, err := stmt.conn.handle.AllocStmt()
	if err != nil {
		return stmt.conn.ErrorOf(err)
	}
	stmt.handle = handle
	stmt.reachEOF = false
	if sqlText == "" {
		return nil
	}
	if err := stmt.prepare(sqlText); err != nil {
		return err
	}
	return nil
}

// discardOpenCursor renews the statement when its previous result set was
// abandoned before the server sent the last chunk.
func (stmt *Stmt) discardOpenCursor() error {
	if stmt == nil || stmt.handle == nil || stmt.handle.FetchCompleted() {
		return nil
	}
	return stmt.renewHandle()
}

func (stmt *Stmt) reprepareIfSupported() error {
	if stmt == nil || stmt.handle == nil || !stmt.handle.SupportsReprepare() {
		return nil
	}
	return stmt.prepare(stmt.sqlText)
}

func (stmt *Stmt) bindParams(args ...any) error {
	numParam, err := stmt.handle.NumParam()
	if err != nil {
		return stmt.ErrorOf(err)
	}
	args, err = stmt.mapNamedParams(args, numParam)
	if err != nil {
		return err
	}
	if len(args) != numParam {
		return api.ErrParamCount(numParam, len(args))
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
				return api.ErrDatabaseBindUnknownType(idx, fmt.Sprintf("%T, expect: %d", val, pd.Type))
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

func (stmt *Stmt) mapNamedParams(args []any, numParam int) ([]any, error) {
	hasNamed := false
	for _, arg := range args {
		switch arg.(type) {
		case api.NamedParam, *api.NamedParam:
			hasNamed = true
		}
	}
	if !hasNamed {
		return args, nil
	}
	provided := make(map[string]any, len(args))
	for _, arg := range args {
		var named api.NamedParam
		switch value := arg.(type) {
		case api.NamedParam:
			named = value
		case *api.NamedParam:
			if value == nil {
				return nil, api.NewError("named parameter is nil")
			}
			named = *value
		default:
			return nil, api.NewError("named and positional parameters cannot be mixed")
		}
		if named.Name == "" {
			return nil, api.NewError("named parameter name is empty")
		}
		if _, exists := provided[named.Name]; exists {
			return nil, api.NewErrorf("duplicate named parameter %q", named.Name)
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
			return nil, api.NewError("named parameters require Machbase protocol 4.0.3 metadata and cannot be mixed with anonymous markers")
		}
		value, exists := provided[desc.Name]
		if !exists {
			return nil, api.NewErrorf("missing named parameter %q", desc.Name)
		}
		ret[idx] = value
		required[desc.Name] = struct{}{}
	}
	for name := range provided {
		if _, exists := required[name]; !exists {
			return nil, api.NewErrorf("unexpected named parameter %q", name)
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
		return api.FormatIntWithCommas(rowCount) + " rows " + verb
	}
}

type Result struct {
	err      error
	rowCount int64
	stmtType machnet.StmtType
	rowID    uint64
	hasRowID bool
}

var _ api.Result = (*Result)(nil)

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
	return 0, api.ErrNotImplemented("LastInsertId")
}

func (rs *Result) RowsAffected() int64 {
	return rs.rowCount
}

func (c *Conn) NewStmt() (*Stmt, error) {
	var handle *machnet.StmtHandle
	if h, err := c.handle.AllocStmt(); err != nil {
		return nil, c.ErrorOf(err)
	} else {
		handle = h
	}
	ret := &Stmt{conn: c, handle: handle}
	return ret, nil
}

type Stmt struct {
	handle            *machnet.StmtHandle
	conn              *Conn
	sqlText           string
	columnDesc        []api.ColumnDesc
	reachEOF          bool
	sqlHead           string
	rowCount          int64
	execCount         int64
	catalogGeneration uint64
}

func (stmt *Stmt) Close() error {
	if err := stmt.handle.Free(); err == nil {
		return nil
	} else {
		return stmt.ErrorOf(err)
	}
}

func (stmt *Stmt) Error() error {
	return stmt.ErrorOf(nil)
}

func (stmt *Stmt) ErrorOf(cause error) error {
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

func (stmt *Stmt) execute() error {
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
	stmt.columnDesc = make([]api.ColumnDesc, num)
	for i := 0; i < num; i++ {
		d := api.ColumnDesc{}
		if err := stmt.handle.DescribeColEx(i, &d.Name, (*api.SqlType)(&d.Type), &d.Size, &d.Scale, &d.Nullable, &d.Nullability, &d.PrimaryKey); err != nil {
			return stmt.ErrorOf(err)
		}
		stmt.columnDesc[i] = d
	}
	return nil
}

// fetch fetches the next row from the result set.
// It returns true if it reaches end of the result, otherwise false.
func (stmt *Stmt) fetch() ([]any, error) {
	if stmt.reachEOF {
		return nil, api.ErrDatabaseFetch(fmt.Errorf("reached end of the result set"))
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

type Row struct {
	err          error
	values       []any
	columns      api.Columns
	rowCount     int64
	stmtType     machnet.StmtType
	timeLocation *time.Location
}

var _ api.Row = (*Row)(nil)

func (r *Row) Success() bool {
	return r.err == nil
}

func (r *Row) Err() error {
	return r.err
}

func (r *Row) Columns() (api.Columns, error) {
	return r.columns, nil
}

func (r *Row) Scan(dest ...any) error {
	if r.err == sql.ErrNoRows {
		return r.err
	}
	if len(dest) > len(r.values) {
		return api.ErrParamCount(len(r.values), len(dest))
	}
	for i, d := range dest {
		if r.values[i] == nil {
			if !api.ScanNull(d) {
				return api.ErrDatabaseScanNull(fmt.Sprintf("VALUE into %T", d))
			}
			continue
		}
		if err := api.Scan(r.values[i], d, r.timeLocation); err != nil {
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
}

var _ api.Rows = (*Rows)(nil)

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
			return stmt.renewHandle()
		}
		return stmt.handle.ExecuteClean()
	}
	if r.queryStmtPooled && stmt.conn != nil {
		return stmt.conn.releaseQueryStmt(r.queryStmtKey, stmt, true)
	}
	return stmt.Close()
}

func (r *Rows) IsFetchable() bool {
	if r.stmt == nil || r.stmt.handle == nil {
		return false
	}
	typ, _ := r.stmt.handle.GetStmtType()
	return typ.IsSelect()
}

func (r *Rows) Columns() (api.Columns, error) {
	if r.stmt == nil {
		return nil, nil
	}
	ret := make(api.Columns, len(r.stmt.columnDesc))
	for i, desc := range r.stmt.columnDesc {
		ret[i] = &api.Column{
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

func (r *Rows) Message() string {
	return formatResultMessage(r.err, r.stmtType, r.rowsCount)
}

func (r *Rows) RowsAffected() int64 {
	return r.rowsCount
}

func (r *Rows) Next() bool {
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

func (r *Rows) Row() []any {
	return r.row
}

func (r *Rows) ColumnDescriptions() []api.ColumnDesc {
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
		return api.ErrParamCount(len(r.row), len(dest))
	}
	for i, d := range dest {
		if d == nil {
			continue
		}
		if r.row[i] == nil {
			if !api.ScanNull(dest[i]) {
				return api.ErrDatabaseScanNull(fmt.Sprintf("into %T", dest[i]))
			}
			continue
		}
		if err := api.Scan(r.row[i], d, r.timeLocation); err != nil {
			return err
		}
	}
	return nil
}

func (c *Conn) Appender(ctx context.Context, tableName string, opts ...api.AppenderOption) (api.Appender, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	db, user, table := api.TableName(tableName).SplitOr("", c.user)
	tableId := int64(-1)
	var tableType api.TableType = api.TableType(-1)
	var tableFlag api.TableFlag
	var tableColCount int

	dbId := int64(-1)
	if c.handle.SupportsDatabaseMetadata() {
		var dbRow api.Row
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
	if tableType != api.TableTypeLog && tableType != api.TableTypeTag && tableType != api.TableTypeTransaction {
		return nil, fmt.Errorf("%s '%s' doesn't support append", tableType, tableName)
	}
	rows, err := c.query(ctx, "select name, type, length, id, flag from M$SYS_COLUMNS where table_id = ? and database_id = ? order by id", tableId, dbId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ret := &Appender{tableName: strings.ToUpper(tableName), tableType: tableType}
	for _, opt := range opts {
		switch o := opt.(type) {
		case *api.AppenderOptionBuffer:
			ret.errCheckCount = o.Threshold
		default:
			return nil, fmt.Errorf("unknown option type-%T", o)
		}
	}
	for rows.Next() {
		col := &api.Column{}
		err = rows.Scan(&col.Name, &col.Type, &col.Length, &col.Id, &col.Flag)
		if err != nil {
			return nil, err
		}
		if strings.HasPrefix(col.Name, "_") {
			if tableType != api.TableTypeLog || col.Name != "_ARRIVAL_TIME" {
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
			if err := c.exec(ctx, "USE "+quoteIdentifier(db)).Err(); err != nil {
				stmt.Close()
				return nil, err
			}
			restoreDB = currentDB
		}
		openName = user + "." + table
	}

	openErr := stmt.handle.AppendOpen(openName, ret.errCheckCount)
	if restoreDB != "" {
		if restoreErr := c.exec(ctx, "USE "+quoteIdentifier(restoreDB)).Err(); restoreErr != nil {
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

type Appender struct {
	stmt          *Stmt
	tableName     string
	tableType     api.TableType
	errCheckCount int
	columns       api.Columns
	columnNames   []string
	columnTypes   []api.SqlType
	inputColumns  []AppenderInputColumn
	inputFormats  []string
	closed        bool
	successCount  int64
	failCount     int64
}

var _ api.Appender = (*Appender)(nil)
var _ api.Flusher = (*Appender)(nil)

type AppenderInputColumn struct {
	Name string
	Idx  int
}

// Close returns the number of success and fail rows.
func (a *Appender) Close() (int64, int64, error) {
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

func (a *Appender) WithInputColumns(columns ...string) api.Appender {
	a.inputColumns = nil
	for _, col := range columns {
		a.inputColumns = append(a.inputColumns, AppenderInputColumn{Name: strings.ToUpper(col), Idx: -1})
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

func (a *Appender) WithInputFormats(formats ...string) api.Appender {
	a.inputFormats = formats
	return a
}

// WithBatchMaxRows sets the maximum batch size in rows for batch append. If the batch size exceeds the limit, it will be flushed immediately.
// The default value is 512 rows. The minimum value is 1 row.
func (a *Appender) WithBatchMaxRows(rows int) api.Appender {
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
func (a *Appender) WithBatchMaxBytes(bytes int) api.Appender {
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
func (a *Appender) WithBatchMaxDelay(duration time.Duration) api.Appender {
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

func (a *Appender) TableName() string {
	return a.tableName
}

func (a *Appender) TableType() api.TableType {
	return a.tableType
}

func (a *Appender) Columns() (api.Columns, error) {
	return a.columns, nil
}

func (a *Appender) Flush() error {
	if err := a.stmt.handle.AppendFlush(); err == nil {
		return nil
	} else {
		return a.stmt.ErrorOf(err)
	}
}

func (a *Appender) Append(values ...any) error {
	switch a.tableType {
	case api.TableTypeTag, api.TableTypeTransaction:
		return a.append(values...)
	case api.TableTypeLog:
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

func (a *Appender) AppendLogTime(ts time.Time, values ...any) error {
	if a.tableType != api.TableTypeLog {
		return fmt.Errorf("%s is not a log table, use Append() instead", a.tableName)
	}
	values = append([]any{ts}, values...)
	return a.append(values...)
}

func (a *Appender) append(values ...any) error {
	if len(a.columns) == 0 {
		return api.ErrDatabaseNoColumns(a.tableName)
	}
	if len(a.inputColumns) > 0 {
		if len(a.inputColumns) != len(values) {
			return api.ErrDatabaseLengthOfColumns(a.tableName, len(a.columns), len(values))
		}
		newValues := make([]any, len(a.columns))
		for i, inputCol := range a.inputColumns {
			newValues[inputCol.Idx] = values[i]
		}
		values = newValues
	} else {
		if len(a.columns) != len(values) {
			return api.ErrDatabaseLengthOfColumns(a.tableName, len(a.columns), len(values))
		}
	}
	if a.closed {
		return api.ErrDatabaseClosedAppender
	}
	if a.stmt == nil || a.stmt.conn == nil {
		return api.ErrDatabaseNoConnection
	}

	if err := a.stmt.handle.AppendData(a.columnTypes, a.columnNames, values, a.inputFormats); err != nil {
		return a.stmt.ErrorOf(err)
	}
	return nil
}
