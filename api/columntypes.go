package api

import (
	"database/sql"
	"fmt"
	"net"
	"strings"
)

type ColumnType int

const (
	ColumnTypeShort        ColumnType = iota + 4 // cmdInt16Type
	ColumnTypeUShort       ColumnType = 104      // cmdUInt16Type
	ColumnTypeInteger      ColumnType = 8        // cmdInt32Type
	ColumnTypeUInteger     ColumnType = 108      // cmdUInt32Type
	ColumnTypeLong         ColumnType = 12       // cmdInt64Type
	ColumnTypeULong        ColumnType = 112      // cmdUInt64Type
	ColumnTypeFloat        ColumnType = 16       // cmdFlt32Type
	ColumnTypeDouble       ColumnType = 20       // cmdFlt64Type
	ColumnTypeVarchar      ColumnType = 5        // cmdVarcharType
	ColumnTypeText         ColumnType = 49       // cmdTextType
	ColumnTypeClob         ColumnType = 53       // cmdClobType
	ColumnTypeBlob         ColumnType = 57       // cmdBlobType
	ColumnTypeBinary       ColumnType = 97       // cmdBinaryType
	ColumnTypeDatetime     ColumnType = 6        // cmdDateType
	ColumnTypeIPv4         ColumnType = 32       // cmdIPv4Type
	ColumnTypeIPv6         ColumnType = 36       // cmdIPv6Type
	ColumnTypeIPNet        ColumnType = 101      // cmdIPNetType
	ColumnTypeJSON         ColumnType = 61       // cmdJSONType
	ColumnTypeNull         ColumnType = 24       // cmdNullType
	ColumnTypeBool         ColumnType = 40       // cmdBoolType
	ColumnTypeDecimal      ColumnType = 132      // cmdDecimalType
	ColumnTypeInt16Array   ColumnType = 137
	ColumnTypeUInt16Array  ColumnType = 141
	ColumnTypeInt32Array   ColumnType = 145
	ColumnTypeUInt32Array  ColumnType = 149
	ColumnTypeInt64Array   ColumnType = 153
	ColumnTypeUInt64Array  ColumnType = 157
	ColumnTypeFloatArray   ColumnType = 161
	ColumnTypeDoubleArray  ColumnType = 165
	ColumnTypeDecimalArray ColumnType = 169
	ColumnTypeChar         ColumnType = 45 // cmdCharType
	ColumnTypeUnknown      ColumnType = 0
)

const (
	COLUMN_TYPE_SHORT    = "short"
	COLUMN_TYPE_USHORT   = "ushort"
	COLUMN_TYPE_INTEGER  = "integer"
	COLUMN_TYPE_UINTEGER = "uinteger"
	COLUMN_TYPE_LONG     = "long"
	COLUMN_TYPE_ULONG    = "ulong"
	COLUMN_TYPE_FLOAT    = "float"
	COLUMN_TYPE_DOUBLE   = "double"
	COLUMN_TYPE_DATETIME = "datetime"
	COLUMN_TYPE_VARCHAR  = "varchar"
	COLUMN_TYPE_IPV4     = "ipv4"
	COLUMN_TYPE_IPV6     = "ipv6"
	COLUMN_TYPE_TEXT     = "text"
	COLUMN_TYPE_CLOB     = "clob"
	COLUMN_TYPE_BLOB     = "blob"
	COLUMN_TYPE_BINARY   = "binary"
	COLUMN_TYPE_JSON     = "json"
	COLUMN_TYPE_DECIMAL  = "decimal"
)

func (typ ColumnType) String() string {
	switch typ {
	case ColumnTypeShort:
		return COLUMN_TYPE_SHORT
	case ColumnTypeUShort:
		return COLUMN_TYPE_USHORT
	case ColumnTypeInteger:
		return COLUMN_TYPE_INTEGER
	case ColumnTypeUInteger:
		return COLUMN_TYPE_UINTEGER
	case ColumnTypeLong:
		return COLUMN_TYPE_LONG
	case ColumnTypeULong:
		return COLUMN_TYPE_ULONG
	case ColumnTypeFloat:
		return COLUMN_TYPE_FLOAT
	case ColumnTypeDouble:
		return COLUMN_TYPE_DOUBLE
	case ColumnTypeVarchar:
		return COLUMN_TYPE_VARCHAR
	case ColumnTypeText:
		return COLUMN_TYPE_TEXT
	case ColumnTypeClob:
		return COLUMN_TYPE_CLOB
	case ColumnTypeBlob:
		return COLUMN_TYPE_BLOB
	case ColumnTypeBinary:
		return COLUMN_TYPE_BINARY
	case ColumnTypeDatetime:
		return COLUMN_TYPE_DATETIME
	case ColumnTypeIPv4:
		return COLUMN_TYPE_IPV4
	case ColumnTypeIPv6:
		return COLUMN_TYPE_IPV6
	case ColumnTypeJSON:
		return COLUMN_TYPE_JSON
	case ColumnTypeDecimal:
		return COLUMN_TYPE_DECIMAL
	case ColumnTypeInt16Array:
		return "int16_array"
	case ColumnTypeUInt16Array:
		return "uint16_array"
	case ColumnTypeInt32Array:
		return "int32_array"
	case ColumnTypeUInt32Array:
		return "uint32_array"
	case ColumnTypeInt64Array:
		return "int64_array"
	case ColumnTypeUInt64Array:
		return "uint64_array"
	case ColumnTypeFloatArray:
		return "float_array"
	case ColumnTypeDoubleArray:
		return "double_array"
	case ColumnTypeDecimalArray:
		return "decimal_array"
	default:
		return fmt.Sprintf("UndefinedColumnType-%d", typ)
	}
}

func ParseColumnType(typeName string) ColumnType {
	switch strings.ToLower(typeName) {
	case COLUMN_TYPE_SHORT:
		return ColumnTypeShort
	case COLUMN_TYPE_USHORT:
		return ColumnTypeUShort
	case COLUMN_TYPE_INTEGER:
		return ColumnTypeInteger
	case COLUMN_TYPE_UINTEGER:
		return ColumnTypeUInteger
	case COLUMN_TYPE_LONG:
		return ColumnTypeLong
	case COLUMN_TYPE_ULONG:
		return ColumnTypeULong
	case COLUMN_TYPE_FLOAT:
		return ColumnTypeFloat
	case COLUMN_TYPE_DOUBLE:
		return ColumnTypeDouble
	case COLUMN_TYPE_VARCHAR:
		return ColumnTypeVarchar
	case COLUMN_TYPE_TEXT:
		return ColumnTypeText
	case COLUMN_TYPE_CLOB:
		return ColumnTypeClob
	case COLUMN_TYPE_BLOB:
		return ColumnTypeBlob
	case COLUMN_TYPE_BINARY:
		return ColumnTypeBinary
	case COLUMN_TYPE_DATETIME:
		return ColumnTypeDatetime
	case COLUMN_TYPE_IPV4:
		return ColumnTypeIPv4
	case COLUMN_TYPE_IPV6:
		return ColumnTypeIPv6
	case COLUMN_TYPE_JSON:
		return ColumnTypeJSON
	case COLUMN_TYPE_DECIMAL:
		return ColumnTypeDecimal
	case "int16_array":
		return ColumnTypeInt16Array
	case "uint16_array":
		return ColumnTypeUInt16Array
	case "int32_array":
		return ColumnTypeInt32Array
	case "uint32_array":
		return ColumnTypeUInt32Array
	case "int64_array":
		return ColumnTypeInt64Array
	case "uint64_array":
		return ColumnTypeUInt64Array
	case "float_array":
		return ColumnTypeFloatArray
	case "double_array":
		return ColumnTypeDoubleArray
	case "decimal_array":
		return ColumnTypeDecimalArray
	default:
		return ColumnTypeUnknown
	}
}

func (typ ColumnType) ToSqlType() SqlType {
	switch typ {
	case ColumnTypeShort:
		return SqlTypeInt16
	case ColumnTypeUShort:
		return SqlTypeUInt16
	case ColumnTypeInteger:
		return SqlTypeInt32
	case ColumnTypeUInteger:
		return SqlTypeUInt32
	case ColumnTypeLong:
		return SqlTypeInt64
	case ColumnTypeULong:
		return SqlTypeUInt64
	case ColumnTypeDatetime:
		return SqlTypeDatetime
	case ColumnTypeFloat:
		return SqlTypeFloat
	case ColumnTypeDouble:
		return SqlTypeDouble
	case ColumnTypeIPv4:
		return SqlTypeIPv4
	case ColumnTypeIPv6:
		return SqlTypeIPv6
	case ColumnTypeVarchar:
		return SqlTypeString
	case ColumnTypeBinary:
		return SqlTypeBinary
	case ColumnTypeDecimal:
		return SqlTypeDecimal
	case ColumnTypeInt16Array:
		return SqlTypeInt16Array
	case ColumnTypeUInt16Array:
		return SqlTypeUInt16Array
	case ColumnTypeInt32Array:
		return SqlTypeInt32Array
	case ColumnTypeUInt32Array:
		return SqlTypeUInt32Array
	case ColumnTypeInt64Array:
		return SqlTypeInt64Array
	case ColumnTypeUInt64Array:
		return SqlTypeUInt64Array
	case ColumnTypeFloatArray:
		return SqlTypeFloatArray
	case ColumnTypeDoubleArray:
		return SqlTypeDoubleArray
	case ColumnTypeDecimalArray:
		return SqlTypeDecimalArray
	default:
		return SqlTypeString
	}
}

func (typ ColumnType) MakeBuffer() (any, error) {
	switch typ {
	case ColumnTypeShort:
		return new(sql.NullInt16), nil
		//return new(int16), nil
	case ColumnTypeUShort:
		return new(sql.Null[uint16]), nil
		//return new(uint16), nil
	case ColumnTypeInteger:
		return new(sql.NullInt32), nil
		//return new(int32), nil
	case ColumnTypeUInteger:
		return new(sql.Null[uint32]), nil
		//return new(uint32), nil
	case ColumnTypeLong:
		return new(sql.NullInt64), nil
		//return new(int64), nil
	case ColumnTypeULong:
		return new(sql.Null[uint64]), nil
		//return new(uint64), nil
	case ColumnTypeFloat:
		return new(sql.Null[float32]), nil
		//return new(float32), nil
	case ColumnTypeDouble:
		return new(sql.NullFloat64), nil
		//return new(float64), nil
	case ColumnTypeVarchar:
		return new(sql.NullString), nil
		//return new(string), nil
	case ColumnTypeText:
		return new(sql.NullString), nil
		//return new(string), nil
	case ColumnTypeIPv4:
		return new(sql.Null[net.IP]), nil
		//return new(net.IP), nil
	case ColumnTypeIPv6:
		return new(sql.Null[net.IP]), nil
		//return new(net.IP), nil
	case ColumnTypeJSON:
		return new(sql.Null[JSONString]), nil
		//return new(JSONString), nil
	case ColumnTypeDatetime:
		return new(sql.NullTime), nil
		//return new(time.Time), nil
	case ColumnTypeBinary:
		return new(sql.Null[[]byte]), nil
	case ColumnTypeDecimal:
		return new(sql.Null[Decimal]), nil
	case ColumnTypeInt16Array, ColumnTypeUInt16Array, ColumnTypeInt32Array,
		ColumnTypeUInt32Array, ColumnTypeInt64Array, ColumnTypeUInt64Array,
		ColumnTypeFloatArray, ColumnTypeDoubleArray, ColumnTypeDecimalArray:
		return new(Array), nil
		//return new([]byte), nil
	default:
		return nil, fmt.Errorf("unsupported column type: %d", typ)
	}
}

func (typ ColumnType) MarshalJSON() ([]byte, error) {
	return []byte(fmt.Sprintf(`"%s"`, typ.String())), nil
}

func (typ ColumnType) DataType() DataType {
	switch typ {
	case ColumnTypeShort:
		return DataTypeInt16
	case ColumnTypeUShort:
		return DataTypeUInt16
	case ColumnTypeInteger:
		return DataTypeInt32
	case ColumnTypeUInteger:
		return DataTypeUInt32
	case ColumnTypeLong:
		return DataTypeInt64
	case ColumnTypeULong:
		return DataTypeUInt64
	case ColumnTypeFloat:
		return DataTypeFloat32
	case ColumnTypeDouble:
		return DataTypeFloat64
	case ColumnTypeVarchar:
		return DataTypeString
	case ColumnTypeText:
		return DataTypeString
	case ColumnTypeClob:
		return DataTypeBinary
	case ColumnTypeBlob:
		return DataTypeBinary
	case ColumnTypeBinary:
		return DataTypeBinary
	case ColumnTypeDatetime:
		return DataTypeDatetime
	case ColumnTypeIPv4:
		return DataTypeIPv4
	case ColumnTypeIPv6:
		return DataTypeIPv6
	case ColumnTypeJSON:
		return DataTypeJSON
	case ColumnTypeDecimal:
		return DataTypeDecimal
	case ColumnTypeInt16Array, ColumnTypeUInt16Array, ColumnTypeInt32Array,
		ColumnTypeUInt32Array, ColumnTypeInt64Array, ColumnTypeUInt64Array,
		ColumnTypeFloatArray, ColumnTypeDoubleArray, ColumnTypeDecimalArray:
		return DataTypeArray
	default:
		return DataType(fmt.Sprintf("UndefinedColumnType-%d", typ))
	}
}

type ColumnFlag int

const (
	ColumnFlagAutoIncrement = 0x00100000
	ColumnFlagPrimaryKey    = 0x00400000
	ColumnFlagNotNull       = 0x00800000
	ColumnFlagTagName       = 0x08000000
	ColumnFlagBasetime      = 0x01000000
	ColumnFlagSummarized    = 0x02000000
	ColumnFlagMetaColumn    = 0x04000000
	ColumnFlagDefault       = 0x40000000
	// This is not a real column flag(not defined in the neo-engine),
	// just workaround for base distance
	ColumnFlagBaseDistance = 0x11000000
)

func (flag ColumnFlag) String() string {
	var rt []string
	if flag&ColumnFlagPrimaryKey == ColumnFlagPrimaryKey {
		rt = append(rt, "primary key")
	} else if flag&ColumnFlagNotNull == ColumnFlagNotNull {
		// because a primary key is implicitly not null,
		// we only append "not null" if it's not a primary key.
		rt = append(rt, "not null")
	}

	if flag&ColumnFlagAutoIncrement == ColumnFlagAutoIncrement {
		rt = append(rt, "auto increment")
	}
	if flag&ColumnFlagTagName == ColumnFlagTagName {
		rt = append(rt, "tag name")
	}
	if flag&ColumnFlagBasetime == ColumnFlagBasetime {
		rt = append(rt, "base time")
	}
	if flag&ColumnFlagBaseDistance == ColumnFlagBaseDistance {
		rt = append(rt, "base distance")
	}
	if flag&ColumnFlagSummarized == ColumnFlagSummarized {
		rt = append(rt, "summarized")
	}
	if flag&ColumnFlagMetaColumn == ColumnFlagMetaColumn {
		rt = append(rt, "meta")
	}
	if flag&ColumnFlagDefault == ColumnFlagDefault {
		rt = append(rt, "default")
	}
	return strings.Join(rt, ", ")
}
