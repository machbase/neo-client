package api

import (
	"fmt"
)

type SqlType int

const (
	SqlTypeString       SqlType = 1
	SqlTypeDatetime     SqlType = 2
	SqlTypeFloat        SqlType = 3
	SqlTypeDouble       SqlType = 4
	SqlTypeIPv4         SqlType = 5
	SqlTypeIPv6         SqlType = 6
	SqlTypeBinary       SqlType = 7
	SqlTypeInt16        SqlType = 8
	SqlTypeInt32        SqlType = 9
	SqlTypeInt64        SqlType = 10
	SqlTypeUInt16       SqlType = 11
	SqlTypeUInt32       SqlType = 12
	SqlTypeUInt64       SqlType = 13
	SqlTypeJSON         SqlType = 14
	SqlTypeDecimal      SqlType = 15
	SqlTypeInt16Array   SqlType = 16
	SqlTypeUInt16Array  SqlType = 17
	SqlTypeInt32Array   SqlType = 18
	SqlTypeUInt32Array  SqlType = 19
	SqlTypeInt64Array   SqlType = 20
	SqlTypeUInt64Array  SqlType = 21
	SqlTypeFloatArray   SqlType = 22
	SqlTypeDoubleArray  SqlType = 23
	SqlTypeDecimalArray SqlType = 24
)

func (st SqlType) String() string {
	switch st {
	case SqlTypeInt16:
		return "INT16"
	case SqlTypeUInt16:
		return "UINT16"
	case SqlTypeInt32:
		return "INT32"
	case SqlTypeUInt32:
		return "UINT32"
	case SqlTypeInt64:
		return "INT64"
	case SqlTypeUInt64:
		return "UINT64"
	case SqlTypeDatetime:
		return "DATETIME"
	case SqlTypeFloat:
		return "FLOAT"
	case SqlTypeDouble:
		return "DOUBLE"
	case SqlTypeIPv4:
		return "IPV4"
	case SqlTypeIPv6:
		return "IPV6"
	case SqlTypeString:
		return "STRING"
	case SqlTypeBinary:
		return "BINARY"
	case SqlTypeJSON:
		return "JSON"
	case SqlTypeDecimal:
		return "DECIMAL"
	case SqlTypeInt16Array:
		return "INT16_ARRAY"
	case SqlTypeUInt16Array:
		return "UINT16_ARRAY"
	case SqlTypeInt32Array:
		return "INT32_ARRAY"
	case SqlTypeUInt32Array:
		return "UINT32_ARRAY"
	case SqlTypeInt64Array:
		return "INT64_ARRAY"
	case SqlTypeUInt64Array:
		return "UINT64_ARRAY"
	case SqlTypeFloatArray:
		return "FLOAT_ARRAY"
	case SqlTypeDoubleArray:
		return "DOUBLE_ARRAY"
	case SqlTypeDecimalArray:
		return "DECIMAL_ARRAY"
	default:
		return fmt.Sprintf("UNKNOWN(%d)", st)
	}
}

func (st SqlType) IsArray() bool {
	return st >= SqlTypeInt16Array && st <= SqlTypeDecimalArray
}

func (st SqlType) ArrayType() SqlType {
	switch st {
	case SqlTypeInt16:
		return SqlTypeInt16Array
	case SqlTypeUInt16:
		return SqlTypeUInt16Array
	case SqlTypeInt32:
		return SqlTypeInt32Array
	case SqlTypeUInt32:
		return SqlTypeUInt32Array
	case SqlTypeInt64:
		return SqlTypeInt64Array
	case SqlTypeUInt64:
		return SqlTypeUInt64Array
	case SqlTypeFloat:
		return SqlTypeFloatArray
	case SqlTypeDouble:
		return SqlTypeDoubleArray
	case SqlTypeDecimal:
		return SqlTypeDecimalArray
	default:
		return st
	}
}

func (st SqlType) ElementType() SqlType {
	switch st {
	case SqlTypeInt16Array:
		return SqlTypeInt16
	case SqlTypeUInt16Array:
		return SqlTypeUInt16
	case SqlTypeInt32Array:
		return SqlTypeInt32
	case SqlTypeUInt32Array:
		return SqlTypeUInt32
	case SqlTypeInt64Array:
		return SqlTypeInt64
	case SqlTypeUInt64Array:
		return SqlTypeUInt64
	case SqlTypeFloatArray:
		return SqlTypeFloat
	case SqlTypeDoubleArray:
		return SqlTypeDouble
	case SqlTypeDecimalArray:
		return SqlTypeDecimal
	default:
		return st
	}
}

func (st SqlType) ColumnType() ColumnType {
	switch st {
	default:
		return ColumnTypeUnknown
	case SqlTypeInt16:
		return ColumnTypeShort
	case SqlTypeUInt16:
		return ColumnTypeUShort
	case SqlTypeInt32:
		return ColumnTypeInteger
	case SqlTypeUInt32:
		return ColumnTypeUInteger
	case SqlTypeInt64:
		return ColumnTypeLong
	case SqlTypeUInt64:
		return ColumnTypeULong
	case SqlTypeDatetime:
		return ColumnTypeDatetime
	case SqlTypeFloat:
		return ColumnTypeFloat
	case SqlTypeDouble:
		return ColumnTypeDouble
	case SqlTypeIPv4:
		return ColumnTypeIPv4
	case SqlTypeIPv6:
		return ColumnTypeIPv6
	case SqlTypeString:
		return ColumnTypeVarchar
	case SqlTypeBinary:
		return ColumnTypeBinary
	case SqlTypeDecimal:
		return ColumnTypeDecimal
	case SqlTypeInt16Array:
		return ColumnTypeInt16Array
	case SqlTypeUInt16Array:
		return ColumnTypeUInt16Array
	case SqlTypeInt32Array:
		return ColumnTypeInt32Array
	case SqlTypeUInt32Array:
		return ColumnTypeUInt32Array
	case SqlTypeInt64Array:
		return ColumnTypeInt64Array
	case SqlTypeUInt64Array:
		return ColumnTypeUInt64Array
	case SqlTypeFloatArray:
		return ColumnTypeFloatArray
	case SqlTypeDoubleArray:
		return ColumnTypeDoubleArray
	case SqlTypeDecimalArray:
		return ColumnTypeDecimalArray
	}
}

func (st SqlType) DataType() DataType {
	switch st {
	default:
		return DataTypeAny
	case SqlTypeInt16:
		return DataTypeInt16
	case SqlTypeInt32:
		return DataTypeInt32
	case SqlTypeInt64:
		return DataTypeInt64
	case SqlTypeDatetime:
		return DataTypeDatetime
	case SqlTypeFloat:
		return DataTypeFloat32
	case SqlTypeDouble:
		return DataTypeFloat64
	case SqlTypeIPv4:
		return DataTypeIPv4
	case SqlTypeIPv6:
		return DataTypeIPv6
	case SqlTypeString:
		return DataTypeString
	case SqlTypeBinary:
		return DataTypeBinary
	case SqlTypeUInt16:
		return DataTypeUInt16
	case SqlTypeUInt32:
		return DataTypeUInt32
	case SqlTypeUInt64:
		return DataTypeUInt64
	case SqlTypeJSON:
		return DataTypeJSON
	case SqlTypeDecimal:
		return DataTypeDecimal
	case SqlTypeInt16Array, SqlTypeUInt16Array, SqlTypeInt32Array,
		SqlTypeUInt32Array, SqlTypeInt64Array, SqlTypeUInt64Array,
		SqlTypeFloatArray, SqlTypeDoubleArray, SqlTypeDecimalArray:
		return DataTypeArray
	}
}

// Nullability preserves whether NULL support is known from server metadata.
type Nullability uint8

const (
	NullabilityUnknown Nullability = iota
	NullabilityNoNulls
	NullabilityNullable
)
