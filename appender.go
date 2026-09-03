package client

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

// Appender is not safe for concurrent use:
// callers must serialize Append/AppendLogTime/Flush/Close on a single goroutine.
type Appender struct {
	ctx  context.Context
	cn   *Connector
	conn *Conn

	stmt          *Stmt
	tableName     string
	tableType     TableType
	errCheckCount int
	columns       Columns
	columnNames   []string
	columnTypes   []api.SqlType
	inputColumns  []AppenderInputColumn
	inputAtOpen   bool
	inputFormats  []string
	opened        bool
	closed        bool
	closeErr      error
	successCount  int64
	failCount     int64

	stringColumns     []string
	stringColumnTypes []string
}

func (ap *Appender) Connect(ctx context.Context, dsn string, table string, columns ...string) error {
	if ap.opened && !ap.closed {
		return errors.New("appender is already opened")
	}
	ap.resetForConnect()
	if ctx == nil {
		ap.ctx = context.Background()
	} else {
		ap.ctx = ctx
	}
	drv := &Driver{}
	cn, err := drv.OpenConnector(dsn)
	if err != nil {
		return err
	} else {
		ap.cn = cn.(*Connector)
	}

	if conn, err := ap.cn.Connect(ap.ctx); err != nil {
		closeErr := closeAppenderResources(nil, ap.cn)
		ap.cn = nil
		return errors.Join(err, closeErr)
	} else {
		ap.conn = conn.(*Conn)
	}

	if meta, ok := ap.ctx.Value(MetaKey).(*Meta); ok && meta != nil {
		meta.cbIOMetrics = ap.conn.IOMetrics
	}
	requested := columns
	if len(requested) == 0 && len(ap.inputColumns) > 0 {
		requested = make([]string, len(ap.inputColumns))
		for i := range ap.inputColumns {
			requested[i] = ap.inputColumns[i].Name
		}
	}
	if _, err := ap.appender(ap.ctx, ap.conn, table, requested); err != nil {
		closeErr := closeAppenderResources(ap.conn, ap.cn)
		ap.conn = nil
		ap.cn = nil
		return errors.Join(err, closeErr)
	}
	return nil
}

func (ap *Appender) resetForConnect() {
	ap.ctx = nil
	ap.cn = nil
	ap.conn = nil
	ap.stmt = nil
	ap.tableName = ""
	ap.tableType = TableType(-1)
	ap.errCheckCount = 0
	ap.columns = nil
	ap.columnNames = nil
	ap.columnTypes = nil
	ap.inputAtOpen = false
	ap.opened = false
	ap.closed = false
	ap.closeErr = nil
	ap.successCount = 0
	ap.failCount = 0
	ap.stringColumns = nil
	ap.stringColumnTypes = nil
	for i := range ap.inputColumns {
		ap.inputColumns[i].Idx = -1
	}
}

func closeAppenderResources(resources ...io.Closer) error {
	errs := make([]error, 0, len(resources))
	for _, resource := range resources {
		if resource != nil {
			errs = append(errs, resource.Close())
		}
	}
	return errors.Join(errs...)
}

func (ap *Appender) appender(ctx context.Context, c *Conn, tableName string, inputColumns []string) (*Appender, error) {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()

	db, user, table := parseTableName(tableName, "", c.user)
	tableId := int64(-1)
	var tableType TableType = TableType(-1)
	var tableFlag TableFlag
	var tableColCount int

	dbId := int64(-1)
	if c.handle.SupportsDatabaseMetadata() {
		var dbRow *Row
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

	ap.tableName = strings.ToUpper(tableName)
	ap.tableType = tableType
	ap.errCheckCount = 0
	for rows.next() {
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
		ap.columns = append(ap.columns, col)
		ap.columnNames = append(ap.columnNames, col.Name)
		ap.columnTypes = append(ap.columnTypes, col.Type.ToSqlType())
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
	ap.stmt = stmt
	projectionColumns := appenderProjectionColumns(tableType, inputColumns)
	ap.inputAtOpen = len(inputColumns) > 0
	if len(inputColumns) > 0 {
		ap.WithInputColumns(inputColumns...)
	}

	var openErr error
	if len(projectionColumns) > 0 {
		openErr = stmt.handle.AppendOpenColumns(ap.tableName, projectionColumns, ap.errCheckCount)
	} else {
		openErr = stmt.handle.AppendOpen(ap.tableName, ap.errCheckCount)
	}
	if openErr != nil {
		err := stmt.ErrorOf(openErr)
		_ = stmt.Close()
		ap.stmt = nil
		return nil, err
	}
	ap.opened = true
	return ap, nil
}

// Close returns the number of success and fail rows.
func (ap *Appender) Close() (int64, int64, error) {
	if !ap.opened {
		return 0, 0, fmt.Errorf("appender is not opened")
	}
	if ap.closed {
		return ap.successCount, ap.failCount, ap.closeErr
	}
	ap.closed = true

	var appendErr error
	if ap.stmt != nil && ap.stmt.handle != nil {
		ap.successCount, ap.failCount, appendErr = ap.stmt.handle.AppendClose()
		if appendErr != nil {
			appendErr = ap.stmt.ErrorOf(appendErr)
		}
	}
	cleanupErr := closeAppenderResources(ap.stmt, ap.conn, ap.cn)
	ap.stmt = nil
	ap.conn = nil
	ap.cn = nil
	ap.closeErr = errors.Join(appendErr, cleanupErr)
	return ap.successCount, ap.failCount, ap.closeErr
}

func (ap *Appender) TableType() TableType {
	if !ap.opened {
		return TableType(-1)
	}
	return ap.tableType
}

func (ap *Appender) TableName() string {
	return ap.tableName
}

// ApiColumns() returns the columns of the appender in api.Columns format
// This is just for the compatibility with the api.Appender interface,
// and it is not recommended to use this method directly.
// Use Columns() and ColumnTypes() method instead.
// This will be removed in the future.
func (ap *Appender) Columns() Columns {
	return ap.columns
}

func (ap *Appender) ColumnNames() []string {
	if ap.stringColumns != nil {
		return ap.stringColumns
	}
	ap.stringColumns = make([]string, len(ap.columns))
	ap.stringColumnTypes = make([]string, len(ap.columns))
	for i, col := range ap.columns {
		ap.stringColumns[i] = col.Name
		ap.stringColumnTypes[i] = col.Type.String()
	}
	return ap.stringColumns
}

func (ap *Appender) ColumnTypes() []string {
	if ap.stringColumnTypes != nil {
		return ap.stringColumnTypes
	}
	ap.ColumnNames()
	return ap.stringColumnTypes
}

func (ap *Appender) Flush() error {
	if !ap.opened {
		return errors.New("appender is not opened")
	}
	if ap.closed || ap.stmt == nil || ap.stmt.handle == nil {
		return errors.New("closed appender")
	}
	if err := ap.stmt.handle.AppendFlush(); err == nil {
		return nil
	} else {
		return ap.stmt.ErrorOf(err)
	}
}

func (ap *Appender) AppendLogTime(ts time.Time, values ...any) error {
	if ap.tableType != TableTypeLog {
		return fmt.Errorf("%s is not a log table, use Append() instead", ap.tableName)
	}
	names := ap.appendColumnNames()
	if ap.requiresExplicitArrival(names) {
		return fmt.Errorf("log input columns configured after Connect must include _ARRIVAL_TIME")
	}
	names, values = withAppenderArrival(names, values, ts)
	return ap.appendNamed(names, values...)
}

func (ap *Appender) Append(values ...any) error {
	switch ap.tableType {
	case TableTypeTag, TableTypeTransaction:
		return ap.appendNamed(ap.appendColumnNames(), values...)
	case TableTypeLog:
		names := ap.appendColumnNames()
		if ap.requiresExplicitArrival(names) {
			return fmt.Errorf("log input columns configured after Connect must include _ARRIVAL_TIME")
		}
		if arrivalIdx := appenderArrivalIndex(names); arrivalIdx >= 0 {
			if len(values) == len(names)-1 {
				values = insertAppenderValue(values, arrivalIdx, time.Time{})
			}
		} else {
			names, values = withAppenderArrival(names, values, time.Time{})
		}
		return ap.appendNamed(names, values...)
	default:
		return fmt.Errorf("%s can not be appended", ap.tableName)
	}
}

func (ap *Appender) requiresExplicitArrival(names []string) bool {
	return len(ap.inputColumns) > 0 && !ap.inputAtOpen && appenderArrivalIndex(names) < 0
}

func (ap *Appender) appendColumnNames() []string {
	if len(ap.inputColumns) == 0 {
		return ap.columnNames
	}
	names := make([]string, len(ap.inputColumns))
	for i := range ap.inputColumns {
		names[i] = ap.inputColumns[i].Name
	}
	return names
}

func isAppenderArrivalColumn(name string) bool {
	normalized := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(name), `"`, ""))
	return normalized == "_ARRIVAL_TIME" || normalized == "ARRIVAL_TIME"
}

func appenderProjectionColumns(tableType TableType, names []string) []string {
	if tableType != TableTypeLog {
		return names
	}
	ret := make([]string, 0, len(names))
	for _, name := range names {
		if !isAppenderArrivalColumn(name) {
			ret = append(ret, name)
		}
	}
	return ret
}

func appenderArrivalIndex(names []string) int {
	for i, name := range names {
		if isAppenderArrivalColumn(name) {
			return i
		}
	}
	return -1
}

func insertAppenderValue(values []any, idx int, value any) []any {
	if idx < 0 || idx > len(values) {
		return values
	}
	ret := make([]any, len(values)+1)
	copy(ret, values[:idx])
	ret[idx] = value
	copy(ret[idx+1:], values[idx:])
	return ret
}

func withAppenderArrival(names []string, values []any, value any) ([]string, []any) {
	if idx := appenderArrivalIndex(names); idx >= 0 {
		return names, insertAppenderValue(values, idx, value)
	}
	retNames := make([]string, 1, len(names)+1)
	retNames[0] = "_ARRIVAL_TIME"
	retNames = append(retNames, names...)
	retValues := make([]any, 1, len(values)+1)
	retValues[0] = value
	retValues = append(retValues, values...)
	return retNames, retValues
}

func (ap *Appender) appendNamed(names []string, values ...any) error {
	if len(ap.columns) == 0 {
		return fmt.Errorf("table '%s' has no columns", ap.tableName)
	}
	if len(ap.inputColumns) > 0 {
		if len(names) != len(values) {
			return fmt.Errorf("value count %d, table '%s' requires %d projected columns to append", len(values), ap.tableName, len(names))
		}
	} else {
		if len(ap.columns) != len(values) {
			return fmt.Errorf("value count %d, table '%s' requires %d columns to append", len(values), ap.tableName, len(ap.columns))
		}
	}
	if ap.closed {
		return errors.New("closed appender")
	}
	if ap.stmt == nil || ap.stmt.conn == nil {
		return errors.New("invalid connection")
	}

	if err := ap.stmt.handle.AppendData(ap.columnTypes, names, values, ap.inputFormats); err != nil {
		return ap.stmt.ErrorOf(err)
	}
	return nil
}

type AppenderInputColumn struct {
	Name string
	Idx  int
}

func (ap *Appender) WithInputColumns(columns ...string) *Appender {
	ap.inputColumns = nil
	for _, col := range columns {
		ap.inputColumns = append(ap.inputColumns, AppenderInputColumn{Name: strings.ToUpper(col), Idx: -1})
	}
	if len(ap.inputColumns) > 0 {
		for idx, col := range ap.columns {
			for inIdx, inputCol := range ap.inputColumns {
				if col.Name == inputCol.Name {
					ap.inputColumns[inIdx].Idx = idx
				}
			}
		}
	}
	return ap
}

func (ap *Appender) WithInputFormats(formats ...string) *Appender {
	ap.inputFormats = formats
	return ap
}

// WithBatchMaxRows sets the maximum batch size in rows for batch append. If the batch size exceeds the limit, it will be flushed immediately.
// The default value is 512 rows. The minimum value is 1 row.
func (ap *Appender) WithBatchMaxRows(rows int) *Appender {
	if ap.stmt == nil || ap.stmt.handle == nil {
		return ap
	}
	if rows < 1 {
		rows = 1
	}
	ap.stmt.handle.SetAppendBatchMaxRows(rows)
	return ap
}

// WithBatchMaxBytes sets the maximum batch size in bytes for batch append. If the batch size exceeds the limit, it will be flushed immediately.
// The default value is 512KB. The minimum value is 4KB.
func (ap *Appender) WithBatchMaxBytes(bytes int) *Appender {
	if ap.stmt == nil || ap.stmt.handle == nil {
		return ap
	}
	if bytes < 4*1024 {
		bytes = 4 * 1024
	}
	ap.stmt.handle.SetAppendBatchMaxBytes(bytes)
	return ap
}

// WithBatchMaxDelay sets the maximum delay for batch append. If the batch is not full, it will be flushed when the delay is reached.
// The default value is 5 milliseconds.
// The minimum value is 1ms.
// 0 means no delay-based flush.
func (ap *Appender) WithBatchMaxDelay(duration time.Duration) *Appender {
	if ap.stmt == nil || ap.stmt.handle == nil {
		return ap
	}
	if duration <= 0 {
		duration = 0
	} else if duration < time.Millisecond {
		duration = time.Millisecond
	}
	ap.stmt.handle.SetAppendBatchMaxDelay(duration)
	return ap
}
