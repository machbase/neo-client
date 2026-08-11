package machbase

import (
	"context"
	"crypto"
	"errors"
	"strings"

	"github.com/machbase/neo-client/api"
	"github.com/machbase/neo-client/machgo"
)

type Appender struct {
	ctx  context.Context
	db   *machgo.Database
	conn *machgo.Conn
	raw  *machgo.Appender

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
	if db, err := machgo.NewDatabase(cfg.machgoConfig()); err != nil {
		return err
	} else {
		ap.db = db
	}

	opts := []api.ConnectOption{}
	if strings.TrimSpace(cfg.AuthKeyFile) != "" || strings.TrimSpace(cfg.AuthKeyPEM) != "" || strings.EqualFold(strings.TrimSpace(cfg.AuthMode), "CHALLENGE") {
		var key crypto.PrivateKey
		var err error
		if strings.TrimSpace(cfg.AuthKeyPEM) != "" {
			key, err = machgo.LoadPrivateKeyFromPEM([]byte(cfg.AuthKeyPEM))
		} else {
			key, err = machgo.LoadPrivateKeyFromFile(cfg.AuthKeyFile)
		}
		if err != nil {
			return err
		}
		opts = append(opts, api.WithAuthKey(cfg.User, key))
	} else {
		opts = append(opts, api.WithPassword(cfg.User, cfg.Password))
	}
	if cfg.ProxyUser != "" && cfg.User != cfg.ProxyUser {
		opts = append(opts, api.WithProxyUser(cfg.ProxyUser))
	}
	if strings.TrimSpace(cfg.Database) != "" {
		opts = append(opts, api.WithDatabase(cfg.Database))
	}

	if conn, err := ap.db.Connect(ap.ctx, opts...); err != nil {
		return err
	} else {
		ap.conn = conn.(*machgo.Conn)
	}

	if raw, err := ap.conn.Appender(ap.ctx, table); err != nil {
		return err
	} else {
		if len(columns) > 0 {
			raw = raw.WithInputColumns(columns...)
			ap.raw = raw.(*machgo.Appender)
		} else {
			ap.raw = raw.(*machgo.Appender)
		}
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

func (ap *Appender) TableType() (api.TableType, error) {
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
func (ap *Appender) ApiColumns() (api.Columns, error) {
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
