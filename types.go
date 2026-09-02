package client

import (
	"database/sql"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

func ParseDecimal(text string, precision, scale int) (api.Decimal, error) {
	return api.ParseDecimal(text, precision, scale)
}

type Column struct {
	Id               uint64          `json:"id,omitempty"`       // if the column came from database table
	Name             string          `json:"name"`               //
	Type             api.ColumnType  `json:"type"`               //
	Length           int             `json:"length,omitempty"`   //
	DataType         api.DataType    `json:"data_type"`          //
	Flag             api.ColumnFlag  `json:"flag,omitempty"`     // database column flag
	Nullable         bool            `json:"nullable,omitempty"` // is column nullable
	Nullability      api.Nullability `json:"nullability,omitempty"`
	PrimaryKey       bool            `json:"primary_key,omitempty"`
	ElementType      api.SqlType     `json:"element_type,omitempty"`
	ElementPrecision int             `json:"element_precision,omitempty"`
	Scale            int             `json:"scale,omitempty"`
}

func (col Column) String() string {
	if col.Type != api.ColumnTypeUnknown {
		return fmt.Sprintf("%s(%s)", col.Name, col.Type.String())
	} else if col.DataType != api.DataTypeUnknown {
		return fmt.Sprintf("%s(%s)", col.Name, col.DataType)
	} else {
		return fmt.Sprintf("%s(unknown)", col.Name)
	}
}

func (col *Column) IsBaseTime() bool {
	return col.Flag&api.ColumnFlagBasetime == api.ColumnFlagBasetime
}

func (col *Column) IsBaseDistance() bool {
	if col.Flag&api.ColumnFlagBaseDistance == api.ColumnFlagBaseDistance {
		return true
	}
	return col.Flag&api.ColumnFlagBasetime == api.ColumnFlagBasetime && col.Type != api.ColumnTypeDatetime
}

func (col *Column) IsTagName() bool {
	return col.Flag&api.ColumnFlagTagName == api.ColumnFlagTagName
}

func (col *Column) IsSummarized() bool {
	return col.Flag&api.ColumnFlagSummarized == api.ColumnFlagSummarized
}

func (col *Column) IsMetaColumn() bool {
	return col.Flag&api.ColumnFlagMetaColumn == api.ColumnFlagMetaColumn
}

func (col *Column) makeBuffer() (any, error) {
	if col.Type != api.ColumnTypeUnknown {
		return col.Type.MakeBuffer()
	} else if col.DataType != api.DataTypeUnknown {
		return col.DataType.MakeBuffer(col.Nullable)
	} else {
		return nil, fmt.Errorf("Column type is not defined")
	}
}

// Width returns the size of the database column size.
// ,database column only
func (col *Column) Width() int {
	switch col.Type {
	case api.ColumnTypeShort:
		return 6
	case api.ColumnTypeUShort:
		return 5
	case api.ColumnTypeInteger:
		return 11
	case api.ColumnTypeUInteger:
		return 10
	case api.ColumnTypeLong:
		return 20
	case api.ColumnTypeULong:
		return 20
	case api.ColumnTypeFloat:
		return 17
	case api.ColumnTypeDouble:
		return 17
	case api.ColumnTypeIPv4:
		return 15
	case api.ColumnTypeIPv6:
		return 45
	case api.ColumnTypeDatetime:
		return 31
	}
	return col.Length
}

func NewColumnWithType(colType *sql.ColumnType) *Column {
	var dataType api.DataType = api.DataTypeAny
	switch colType.DatabaseTypeName() {
	case "VARCHAR", "TEXT", "NCHAR", "NVARCHAR":
		dataType = api.DataTypeString
	case "JSON":
		dataType = api.DataTypeJSON
	case "IPV4":
		dataType = api.DataTypeIPv4
	case "IPV6":
		dataType = api.DataTypeIPv6
	default:
		switch colType.ScanType().String() {
		case "bool", "sql.NullBool":
			dataType = api.DataTypeBoolean
		case "int8", "sql.NullByte":
			dataType = api.DataTypeInt16
		case "int16", "sql.NullInt16":
			dataType = api.DataTypeInt16
		case "uint16":
			dataType = api.DataTypeUInt16
		case "int32", "sql.NullInt32":
			dataType = api.DataTypeInt32
		case "uint32":
			dataType = api.DataTypeUInt32
		case "int64", "sql.NullInt64":
			dataType = api.DataTypeInt64
		case "uint64":
			dataType = api.DataTypeUInt64
		case "float32":
			dataType = api.DataTypeFloat32
		case "float64", "sql.NullFloat64":
			dataType = api.DataTypeFloat64
		case "string", "sql.NullString":
			dataType = api.DataTypeString
		case "time.Time", "sql.NullTime":
			dataType = api.DataTypeDatetime
		case "[]byte", "[]uint8", "sql.RawBytes":
			dataType = api.DataTypeBinary
		case "*interface {}":
			// FIXME: SQLite binds `count(*)` field as `*interface {}`
			dataType = api.DataTypeString
		default:
			dataType = api.DataTypeAny
		}
	}
	return &Column{Name: colType.Name(), DataType: dataType}
}

type Columns []*Column

func (cols Columns) String() string {
	if len(cols) == 0 {
		return "[]"
	}
	list := make([]string, len(cols))
	for i, col := range cols {
		list[i] = col.String()
	}
	return fmt.Sprintf("[%s]", strings.Join(list, ", "))
}

func (cols Columns) Names() []string {
	names := make([]string, len(cols))
	for i := range cols {
		names[i] = cols[i].Name
	}
	return names
}

func (cols Columns) NamesWithTimeLocation(tz *time.Location) []string {
	names := make([]string, len(cols))
	for i := range cols {
		if cols[i].DataType == api.DataTypeDatetime {
			names[i] = fmt.Sprintf("%s(%s)", cols[i].Name, tz.String())
		} else {
			names[i] = cols[i].Name
		}
	}
	return names
}

func (cols Columns) DataTypes() []api.DataType {
	types := make([]api.DataType, len(cols))
	for i := range cols {
		if cols[i].DataType == "" {
			types[i] = cols[i].Type.DataType()
		} else {
			types[i] = cols[i].DataType
		}
	}
	return types
}

func (cols Columns) MakeBuffer() ([]any, error) {
	rec := make([]any, len(cols))
	for i, c := range cols {
		if v, err := c.makeBuffer(); err != nil {
			return nil, err
		} else {
			rec[i] = v
		}
	}
	return rec, nil
}

type ColumnDesc struct {
	Name             string
	Type             api.SqlType
	Size             int
	Scale            int
	Nullable         bool
	Nullability      api.Nullability
	PrimaryKey       bool
	ElementType      api.SqlType
	ElementPrecision int
}

func MakeColumnRownum() *Column {
	return &Column{Name: "ROWNUM", Type: api.ColumnTypeInteger, DataType: api.DataTypeInt64}
}

func MakeColumnInt64(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeLong, DataType: api.DataTypeInt64}
}

func MakeColumnInt32(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeLong, DataType: api.DataTypeInt32}
}

func MakeColumnDouble(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeDouble, DataType: api.DataTypeFloat64}
}

func MakeColumnDatetime(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeDatetime, DataType: api.DataTypeDatetime}
}

func MakeColumnString(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeVarchar, DataType: api.DataTypeString}
}

func MakeColumnBoolean(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeBool, DataType: api.DataTypeBoolean}
}

func MakeColumnAny(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeUnknown, DataType: api.DataTypeAny}
}

func MakeColumnList(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeUnknown, DataType: api.DataTypeList}
}

func MakeColumnDict(name string) *Column {
	return &Column{Name: name, Type: api.ColumnTypeUnknown, DataType: api.DataTypeDict}
}

func MakeColumnOf(name string, value any) *Column {
	switch v := value.(type) {
	case string, *string:
		return &Column{Name: name, Type: api.ColumnTypeVarchar, DataType: api.DataTypeString}
	case bool, *bool:
		return &Column{Name: name, Type: api.ColumnTypeUnknown, DataType: api.DataTypeBoolean}
	case int, int32, *int, *int32:
		return &Column{Name: name, Type: api.ColumnTypeInteger, DataType: api.DataTypeInt32}
	case int8, *int8:
		return &Column{Name: name, Type: api.ColumnTypeShort, DataType: api.DataTypeByte}
	case int16, *int16:
		return &Column{Name: name, Type: api.ColumnTypeShort, DataType: api.DataTypeInt16}
	case int64, *int64:
		return &Column{Name: name, Type: api.ColumnTypeLong, DataType: api.DataTypeInt64}
	case time.Time, *time.Time:
		return &Column{Name: name, Type: api.ColumnTypeDatetime, DataType: api.DataTypeDatetime}
	case float32, *float32:
		return &Column{Name: name, Type: api.ColumnTypeFloat, DataType: api.DataTypeFloat32}
	case float64, *float64:
		return &Column{Name: name, Type: api.ColumnTypeDouble, DataType: api.DataTypeFloat64}
	case net.IP:
		if len(v) == net.IPv6len {
			return &Column{Name: name, Type: api.ColumnTypeIPv6, DataType: api.DataTypeIPv6}
		} else {
			return &Column{Name: name, Type: api.ColumnTypeIPv4, DataType: api.DataTypeIPv4}
		}
	case []byte:
		return &Column{Name: name, Type: api.ColumnTypeBinary, DataType: api.DataTypeBinary}
	case api.Decimal, *api.Decimal:
		return &Column{Name: name, Type: api.ColumnTypeDecimal, DataType: api.DataTypeDecimal}
	default:
		return &Column{Name: name, Type: api.ColumnTypeUnknown, DataType: api.DataTypeAny}
	}
}

// 0: Log Table, 1: Fixed Table, 3: Volatile Table,
// 4: Lookup Table, 5: KeyValue Table, 6: Tag Table, 8: Transaction Table
type TableType int

const (
	TableTypeLog         TableType = iota + 0
	TableTypeFixed       TableType = 1
	TableTypeVolatile    TableType = 3
	TableTypeLookup      TableType = 4
	TableTypeKeyValue    TableType = 5
	TableTypeTag         TableType = 6
	TableTypeView        TableType = 7
	TableTypeTransaction TableType = 8
)

func (typ TableType) String() string {
	switch typ {
	case TableTypeLog:
		return "LogTable"
	case TableTypeFixed:
		return "FixedTable"
	case TableTypeVolatile:
		return "VolatileTable"
	case TableTypeLookup:
		return "LookupTable"
	case TableTypeKeyValue:
		return "KeyValueTable"
	case TableTypeTag:
		return "TagTable"
	case TableTypeView:
		return "View"
	case TableTypeTransaction:
		return "TransactionTable"
	default:
		return fmt.Sprintf("UndefinedTable-%d", typ)
	}
}

func (typ TableType) ShortString() string {
	switch typ {
	case TableTypeLog:
		return "Log"
	case TableTypeFixed:
		return "Fixed"
	case TableTypeVolatile:
		return "Volatile"
	case TableTypeLookup:
		return "Lookup"
	case TableTypeKeyValue:
		return "KeyValue"
	case TableTypeTag:
		return "Tag"
	case TableTypeView:
		return "View"
	case TableTypeTransaction:
		return "Transaction"
	default:
		return fmt.Sprintf("UndefinedTable-%d", typ)
	}
}

func (typ TableType) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, typ.String())), nil
}

type TableFlag int

const (
	TableFlagNone   TableFlag = 0
	TableFlagData   TableFlag = 1
	TableFlagRollup TableFlag = 2
	TableFlagMeta   TableFlag = 4
	TableFlagStat   TableFlag = 8
)

func (flag TableFlag) String() string {
	switch flag {
	case TableFlagNone:
		return ""
	case TableFlagData:
		return "Data"
	case TableFlagRollup:
		return "Rollup"
	case TableFlagMeta:
		return "Meta"
	case TableFlagStat:
		return "Stat"
	default:
		return fmt.Sprintf("UndefinedTableFlag-%d", flag)
	}
}

func (flag TableFlag) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, flag.String())), nil
}

type IndexType int

const (
	IndexTypeBitmap   IndexType = iota + 6
	IndexTypeRedBlack IndexType = 8
	IndexTypeKeyword  IndexType = 9
	IndexTypeTag      IndexType = 11
)

func (typ IndexType) String() string {
	switch typ {
	case IndexTypeBitmap:
		return "BITMAP (LSM)"
	case IndexTypeRedBlack:
		return "REDBLACK"
	case IndexTypeKeyword:
		return "KEYWORD (LSM)"
	case IndexTypeTag:
		return "TAG"
	default:
		return fmt.Sprintf("UndefinedIndex-%d", typ)
	}
}
