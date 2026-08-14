package client

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	inputFormats  []string
	opened        bool
	closed        bool
	successCount  int64
	failCount     int64

	stringColumns     []string
	stringColumnTypes []string
}

func (ap *Appender) Connect(ctx context.Context, dsn string, table string, columns ...string) error {
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
		return err
	} else {
		ap.conn = conn.(*Conn)
	}

	if meta, ok := ap.ctx.Value(MetaKey).(*Meta); ok && meta != nil {
		meta.cbIOMetrics = ap.conn.IOMetrics
	}
	if _, err := ap.appender(ap.ctx, ap.conn, table); err != nil {
		return err
	} else {
		if len(columns) > 0 {
			ap.WithInputColumns(columns...)
		}
	}
	return nil
}

func (ap *Appender) appender(ctx context.Context, c *Conn, tableName string) (*Appender, error) {
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

	openName := ap.tableName
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

	openErr := stmt.handle.AppendOpen(openName, ap.errCheckCount)
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
			if err := c.Close(); err != nil {
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
	ap.opened = true
	return ap, nil
}

// Close returns the number of success and fail rows.
func (ap *Appender) Close() (int64, int64, error) {
	if !ap.opened {
		return 0, 0, fmt.Errorf("appender is not opened")
	}
	if ap.closed {
		return ap.successCount, ap.failCount, nil
	}
	ap.closed = true

	var err error
	//// even if error occurred, we should close the statement
	ap.successCount, ap.failCount, err = ap.stmt.handle.AppendClose()

	if errClose := ap.stmt.Close(); errClose != nil {
		return ap.successCount, ap.failCount, ap.stmt.ErrorOf(errClose)
	}
	if ap.conn != nil {
		if e := ap.conn.Close(); e != nil && err == nil {
			err = e
		}
	}
	if ap.cn != nil {
		if e := ap.cn.Close(); e != nil && err == nil {
			err = e
		}
	}
	return ap.successCount, ap.failCount, err
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
	values = append([]any{ts}, values...)
	return ap.append(values...)
}

func (ap *Appender) Append(values ...any) error {
	switch ap.tableType {
	case TableTypeTag, TableTypeTransaction:
		return ap.append(values...)
	case TableTypeLog:
		var valuesWithTime []any
		if len(values) == len(ap.columns) {
			valuesWithTime = values
		} else {
			valuesWithTime = append([]any{time.Time{}}, values...)
		}
		return ap.append(valuesWithTime...)
	default:
		return fmt.Errorf("%s can not be appended", ap.tableName)
	}
}

func (ap *Appender) append(values ...any) error {
	if len(ap.columns) == 0 {
		return fmt.Errorf("table '%s' has no columns", ap.tableName)
	}
	if len(ap.inputColumns) > 0 {
		if len(ap.inputColumns) != len(values) {
			return fmt.Errorf("value count %d, table '%s' requires %d columns to append", len(values), ap.tableName, len(ap.columns))
		}
		newValues := make([]any, len(ap.columns))
		for i, inputCol := range ap.inputColumns {
			newValues[inputCol.Idx] = values[i]
		}
		values = newValues
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

	if err := ap.stmt.handle.AppendData(ap.columnTypes, ap.columnNames, values, ap.inputFormats); err != nil {
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
