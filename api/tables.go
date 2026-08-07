package api

import (
	"context"
	"fmt"
	"strings"
)

// TableDescription is represents data that comes as a result of 'desc <table>'
type TableDescription struct {
	Database string              `json:"database"`
	User     string              `json:"user"`
	Name     string              `json:"name"`
	Id       int64               `json:"id"`
	Type     TableType           `json:"type"`
	Flag     TableFlag           `json:"flag,omitempty"`
	Columns  Columns             `json:"columns"`
	Indexes  []*IndexDescription `json:"indexes"`

	Summarized       bool   `json:"summarized"`
	SummarizedColumn string `json:"summarizedColum,omitempty"`
	TagNameColumn    string `json:"tagNameColumn,omitempty"`
}

type IndexDescription struct {
	Id             int64     `json:"id"`
	Name           string    `json:"name"`
	Type           IndexType `json:"type"`
	Cols           []string  `json:"columns"`
	KeyCompress    bool      `json:"keyCompress"`
	MaxLevel       int       `json:"maxLevel"`
	PartValueCount int       `json:"partValueCount"`
	BitMapEncode   string    `json:"bitMapEncode"`
}

// String returns string representation of table type.
func (td *TableDescription) String() string {
	desc := "undef"
	switch td.Type {
	case TableTypeLog:
		desc = "Log Table"
	case TableTypeFixed:
		desc = "Fixed Table"
	case TableTypeVolatile:
		desc = "Volatile Table"
	case TableTypeLookup:
		desc = "Lookup Table"
	case TableTypeKeyValue:
		desc = "KeyValue Table"
	case TableTypeTag:
		desc = "Tag Table"
	}
	switch td.Flag {
	case TableFlagData:
		desc += " (data)"
	case TableFlagRollup:
		desc += " (rollup)"
	case TableFlagMeta:
		desc += " (meta)"
	case TableFlagStat:
		desc += " (stat)"
	}
	return desc
}

func ExistsTable(ctx context.Context, conn Conn, fullTableName string) (bool, error) {
	dbName, userName, tableName := TableName(fullTableName).SplitOr("", "SYS")
	_, dbID, err := databaseInfo(ctx, conn, dbName)
	if err != nil {
		return false, err
	}
	sql := "select count(*) from M$SYS_TABLES T, M$SYS_USERS U where U.NAME = ? and U.USER_ID = T.USER_ID AND T.DATABASE_ID = ? AND T.NAME = ?"
	r := conn.QueryRow(ctx, sql, strings.ToUpper(userName), dbID, strings.ToUpper(tableName))
	if err := r.Err(); err != nil {
		fmt.Println("error", err.Error())
		return false, err
	}
	var count = 0
	if err := r.Scan(&count); err != nil {
		return false, err
	}
	return (count == 1), nil
}

func DatabaseID(ctx context.Context, conn Conn, dbName string) (int64, error) {
	_, dbID, err := databaseInfo(ctx, conn, dbName)
	return dbID, err
}

func databaseInfo(ctx context.Context, conn Conn, dbName string) (string, int64, error) {
	if support, ok := conn.(interface{ SupportsDatabaseMetadata() bool }); ok && !support.SupportsDatabaseMetadata() {
		return legacyDatabaseInfo(ctx, conn, dbName)
	}

	var row Row
	var resolvedName string
	var dbID int64

	if dbName == "" {
		row = conn.QueryRow(ctx, "select NAME, DATABASE_ID from V$DATABASES where NAME = CURRENT_DATABASE()")
	} else {
		row = conn.QueryRow(ctx, "select NAME, DATABASE_ID from V$DATABASES where NAME = ?", dbName)
	}
	if row.Err() != nil {
		return "", 0, row.Err()
	}
	if err := row.Scan(&resolvedName, &dbID); err != nil {
		return "", 0, err
	}
	return resolvedName, dbID, nil
}

func legacyDatabaseInfo(ctx context.Context, conn Conn, dbName string) (string, int64, error) {
	resolvedName := strings.ToUpper(dbName)
	if resolvedName == "" || resolvedName == "MACHBASEDB" {
		return "MACHBASEDB", -1, nil
	}
	row := conn.QueryRow(ctx, "select BACKUP_TBSID from V$STORAGE_MOUNT_DATABASES where MOUNTDB = ?", resolvedName)
	if row.Err() != nil {
		return "", 0, row.Err()
	}
	var dbID int64
	if err := row.Scan(&dbID); err != nil {
		return "", 0, err
	}
	return resolvedName, dbID, nil
}

// Describe retrieves the result of 'desc table'.
//
// If includeHiddenColumns is true, the result includes hidden columns those name start with '_'
// such as "_RID" and "_ARRIVAL_TIME".
func DescribeTable(ctx context.Context, conn Conn, name string, includeHiddenColumns bool) (*TableDescription, error) {
	_, _, tableName := TableName(name).Split()
	if strings.HasPrefix(tableName, "V$") {
		return describe_mv(ctx, conn, TableName(name), includeHiddenColumns)
	} else if strings.HasPrefix(tableName, "M$") {
		return describe_mv(ctx, conn, TableName(name), includeHiddenColumns)
	} else {
		return describe(ctx, conn, TableName(name), includeHiddenColumns)
	}
}

func describe(ctx context.Context, conn Conn, name TableName, includeHiddenColumns bool) (*TableDescription, error) {
	d := &TableDescription{}
	var colCount int

	dbName, userName, tableName := name.SplitOr("", "SYS")
	resolvedDBName, dbId, err := databaseInfo(ctx, conn, dbName)
	if err != nil {
		return nil, err
	}

	describeSqlText := SqlTidy(
		`SELECT
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
		and j.NAME = ?`)

	r := conn.QueryRow(ctx, describeSqlText, userName, dbId, tableName)
	if r.Err() != nil {
		return nil, r.Err()
	}
	if err := r.Scan(&d.Id, &d.Type, &d.Flag, &colCount); err != nil {
		return nil, err
	}
	d.Database = resolvedDBName
	d.User = userName
	d.Name = tableName

	rows, err := conn.Query(ctx, "select name, type, length, id, flag from M$SYS_COLUMNS where table_id = ? AND database_id = ? order by id", d.Id, dbId)
	if err != nil {
		return nil, err
	}
	defer func() {
		if rows != nil {
			rows.Close()
		}
	}()

	for rows.Next() {
		col := &Column{}
		err = rows.Scan(&col.Name, &col.Type, &col.Length, &col.Id, &col.Flag)
		if err != nil {
			return nil, err
		}
		if !includeHiddenColumns && strings.HasPrefix(col.Name, "_") {
			continue
		}
		col.DataType = col.Type.DataType()
		d.Columns = append(d.Columns, col)

		if col.Flag&ColumnFlagSummarized > 0 {
			d.Summarized = true
			d.SummarizedColumn = col.Name
		}
		if col.Flag&ColumnFlagTagName > 0 {
			d.TagNameColumn = col.Name
		}
		if col.Flag&ColumnFlagBasetime > 0 && col.Type != ColumnTypeDatetime {
			col.Flag = ColumnFlagBaseDistance
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rows = nil

	if indexes, err := describe_idx(ctx, conn, d.Id, dbId); err != nil {
		return nil, err
	} else {
		d.Indexes = indexes
	}
	return d, nil
}

func describe_mv(ctx context.Context, conn Conn, name TableName, includeHiddenColumns bool) (*TableDescription, error) {
	d := &TableDescription{}
	var tableType int
	var colCount int

	d.Database, d.User, d.Name = name.Split()
	tablesTable := "M$SYS_TABLES"
	columnsTable := "M$SYS_COLUMNS"
	if strings.HasPrefix(d.Name, "V$") {
		tablesTable = "V$TABLES"
		columnsTable = "V$COLUMNS"
	} else if strings.HasPrefix(d.Name, "M$") {
		tablesTable = "M$TABLES"
		columnsTable = "M$COLUMNS"
	}
	r := conn.QueryRow(ctx, fmt.Sprintf("select name, type, flag, id, colcount from %s where name = ?", tablesTable), d.Name)
	if err := r.Scan(&d.Name, &tableType, &d.Flag, &d.Id, &colCount); err != nil {
		return nil, err
	}
	d.Type = TableType(tableType)

	rows, err := conn.Query(ctx, fmt.Sprintf(`select name, type, length, id from %s where table_id = ? order by id`, columnsTable), d.Id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		col := &Column{}
		err = rows.Scan(&col.Name, &col.Type, &col.Length, &col.Id)
		if err != nil {
			return nil, err
		}
		if !includeHiddenColumns && strings.HasPrefix(col.Name, "_") {
			continue
		}
		col.DataType = col.Type.DataType()
		d.Columns = append(d.Columns, col)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return d, nil
}

func describe_idx(ctx context.Context, conn Conn, tableId int64, dbId int64) ([]*IndexDescription, error) {
	rows, err := conn.Query(ctx,
		`select
			b.name,
			b.type,
			b.id,
			b.key_compress,
			b.max_level,
			b.part_value_count,
			case b.bitmap_encode
				when 0 then 'EQUAL'
				else 'RANGE' end
			as bitmap_encode
		from
			M$SYS_TABLES  a,
			M$SYS_INDEXES b
		where
			a.id = ?
		AND a.database_id = ?
		AND a.id = b.table_id
		AND a.database_id = b.database_id
		AND b.database_id = ?
		`, tableId, dbId, dbId)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	indexes := []*IndexDescription{}
	for rows.Next() {
		d := &IndexDescription{}
		var indexType int
		var keyCompress int
		if err = rows.Scan(&d.Name, &indexType, &d.Id, &keyCompress, &d.MaxLevel, &d.PartValueCount, &d.BitMapEncode); err != nil {
			return nil, err
		}
		d.Type = IndexType(indexType)
		d.KeyCompress = (keyCompress != 0)
		idxCols, err := conn.Query(ctx, `select name from M$SYS_INDEX_COLUMNS where index_id = ? AND database_id = ? order by col_id`, d.Id, dbId)
		if err != nil {
			return nil, err
		}
		for idxCols.Next() {
			var col string
			if err = idxCols.Scan(&col); err != nil {
				idxCols.Close()
				return nil, err
			}
			d.Cols = append(d.Cols, col)
		}
		if err := idxCols.Err(); err != nil {
			idxCols.Close()
			return nil, err
		}
		if err := idxCols.Close(); err != nil {
			return nil, err
		}
		indexes = append(indexes, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return indexes, nil
}
