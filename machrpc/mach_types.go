package machrpc

import "fmt"

// 0: Log Table, 1: Fixed Table, 3: Volatile Table,
// 4: Lookup Table, 5: KeyValue Table, 6: Tag Table
type TableType int

const (
	LogTableType      TableType = iota + 0
	FixedTableType              = 1
	VolatileTableType           = 3
	LookupTableType             = 4
	KeyValueTableType           = 5
	TagTableType                = 6
)

func (t TableType) String() string {
	switch t {
	case LogTableType:
		return "LogTable"
	case FixedTableType:
		return "FixedTable"
	case VolatileTableType:
		return "VolatileTable"
	case LookupTableType:
		return "LookupTable"
	case KeyValueTableType:
		return "KeyValueTable"
	case TagTableType:
		return "TagTable"
	default:
		return "Undefined"
	}
}

type ColumnType int

const (
	Int16ColumnType    ColumnType = iota + 4
	Uint16ColumnType              = 104
	Int32ColumnType               = 8
	Uint32ColumnType              = 108
	Int64ColumnType               = 12
	Uint64ColumnType              = 112
	Float32ColumnType             = 16
	Float64ColumnType             = 20
	VarcharColumnType             = 5
	TextColumnType                = 49
	ClobColumnType                = 53
	BlobColumnType                = 57
	BinaryColumnType              = 97
	DatetimeColumnType            = 6
	IpV4ColumnType                = 32
	IpV6ColumnType                = 36
	JsonColumnType                = 61
)

// ColumnTypeString converts ColumnType into string.
func ColumnTypeString(typ ColumnType) string {
	switch typ {
	case Int16ColumnType:
		return "int16"
	case Uint16ColumnType:
		return "uint16"
	case Int32ColumnType:
		return "int32"
	case Uint32ColumnType:
		return "uint32"
	case Int64ColumnType:
		return "int64"
	case Uint64ColumnType:
		return "uint64"
	case Float32ColumnType:
		return "float"
	case Float64ColumnType:
		return "double"
	case VarcharColumnType:
		return "varchar"
	case TextColumnType:
		return "text"
	case ClobColumnType:
		return "clob"
	case BlobColumnType:
		return "blob"
	case BinaryColumnType:
		return "binary"
	case DatetimeColumnType:
		return "datetime"
	case IpV4ColumnType:
		return "ipv4"
	case IpV6ColumnType:
		return "ipv6"
	case JsonColumnType:
		return "json"
	default:
		return fmt.Sprintf("undef-%d", typ)
	}
}
