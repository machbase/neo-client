package client

import (
	"context"
	"errors"
)

type Appender struct {
	ctx  context.Context
	db   *ClientDatabase
	conn *ClientConn
	raw  *ClientAppender

	columns     []string
	columnTypes []string
}

func (ap *Appender) Connect(ctx context.Context, dsn string, table string, columns ...string) error {
	if ctx == nil {
		ap.ctx = context.Background()
	} else {
		ap.ctx = ctx
	}
	cfg, err := ParseDSN(dsn)
	if err != nil {
		return err
	}
	if db, err := NewDatabase(&cfg); err != nil {
		return err
	} else {
		ap.db = db
	}

	if conn, err := ap.db.ConnectConfig(ap.ctx, &cfg); err != nil {
		return err
	} else {
		ap.conn = conn
	}

	if meta, ok := ap.ctx.Value(MetaKey).(*Meta); ok && meta != nil {
		meta.cbIOMetrics = ap.conn.IOMetrics
	}
	if raw, err := ap.conn.Appender(ap.ctx, table); err != nil {
		return err
	} else {
		if len(columns) > 0 {
			raw = raw.WithInputColumns(columns...)
		}
		ap.raw = raw
	}
	return nil
}

func (ap *Appender) Close() (successCount int64, failCount int64, err error) {
	if ap.raw != nil {
		successCount, failCount, err = ap.raw.Close()
	}
	if ap.conn != nil {
		if e := ap.conn.Close(); e != nil && err == nil {
			err = e
		}
	}
	if ap.db != nil {
		if e := ap.db.Close(); e != nil && err == nil {
			err = e
		}
	}
	return
}

func (ap *Appender) Flush() error {
	return ap.raw.Flush()
}

func (ap *Appender) TableType() (TableType, error) {
	if ap.raw == nil {
		return -1, errors.New("appender is not connected")
	}
	return ap.raw.TableType(), nil
}

func (ap *Appender) TableName() string {
	if ap.raw == nil {
		return ""
	}
	return ap.raw.TableName()
}

// ApiColumns() returns the columns of the appender in api.Columns format
// This is just for the compatibility with the api.Appender interface,
// and it is not recommended to use this method directly.
// Use Columns() and ColumnTypes() method instead.
// This will be removed in the future.
func (ap *Appender) ApiColumns() (Columns, error) {
	if ap.raw == nil {
		return nil, errors.New("appender is not connected")
	}
	return ap.raw.Columns()
}

func (ap *Appender) Columns() ([]string, error) {
	if ap.columns != nil {
		return ap.columns, nil
	}
	if ap.raw == nil {
		return nil, errors.New("appender is not connected")
	}
	columns, err := ap.raw.Columns()
	if err != nil {
		return nil, err
	}
	ap.columns = make([]string, len(columns))
	ap.columnTypes = make([]string, len(columns))
	for i, col := range columns {
		ap.columns[i] = col.Name
		ap.columnTypes[i] = col.Type.String()
	}
	return ap.columns, nil
}

func (ap *Appender) ColumnTypes() ([]string, error) {
	if ap.columnTypes != nil {
		return ap.columnTypes, nil
	}
	if ap.raw == nil {
		return nil, errors.New("appender is not connected")
	}
	_, err := ap.Columns()
	if err != nil {
		return nil, err
	}
	return ap.columnTypes, nil
}

func (ap *Appender) Append(values ...any) error {
	if ap.raw == nil {
		return errors.New("appender is not connected")
	}
	if err := ap.raw.Append(values...); err != nil {
		return err
	}
	return nil
}
