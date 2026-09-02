package api

import (
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net"
	"strings"
	"time"
)

type DataType string

const (
	DataTypeUnknown  DataType = ""
	DataTypeInt16    DataType = "int16"
	DataTypeInt32    DataType = "int32"
	DataTypeInt64    DataType = "int64"
	DataTypeDatetime DataType = "datetime"
	DataTypeFloat32  DataType = "float"
	DataTypeFloat64  DataType = "double"
	DataTypeIPv4     DataType = "ipv4"
	DataTypeIPv6     DataType = "ipv6"
	DataTypeString   DataType = "string"
	DataTypeBinary   DataType = "binary"
	DataTypeUInt16   DataType = "uint16"
	DataTypeUInt32   DataType = "uint32"
	DataTypeUInt64   DataType = "uint64"
	DataTypeJSON     DataType = "json"
	DataTypeDecimal  DataType = "decimal"
	DataTypeArray    DataType = "array"
	// exceptional case
	DataTypeBoolean DataType = "bool"
	DataTypeByte    DataType = "int8"
	DataTypeAny     DataType = "any"
	DataTypeList    DataType = "list"
	DataTypeDict    DataType = "dict"
)

func DataTypeOf(v any) DataType {
	switch v.(type) {
	default:
		return DataTypeAny
	case *bool, bool:
		return DataTypeBoolean
	case *string, string:
		return DataTypeString
	case *time.Time, time.Time:
		return DataTypeDatetime
	case int16, *int16:
		return DataTypeInt16
	case uint16, *uint16:
		return DataTypeUInt16
	case int32, *int32:
		return DataTypeInt32
	case uint32, *uint32:
		return DataTypeUInt32
	case int64, *int64:
		return DataTypeInt64
	case uint64, *uint64:
		return DataTypeUInt64
	case *float32, float32:
		return DataTypeFloat32
	case *float64, float64:
		return DataTypeFloat64
	case *Decimal, Decimal:
		return DataTypeDecimal
	case *Array, Array:
		return DataTypeArray
	}
}

func (typ DataType) Apply(value any, timeformat string, tz *time.Location) (any, error) {
	if timeformat == "" {
		timeformat = "ns"
	}
	if tz == nil {
		tz = time.UTC
	}
	switch typ {
	case DataTypeString, COLUMN_TYPE_VARCHAR, COLUMN_TYPE_TEXT, COLUMN_TYPE_JSON:
		switch v := value.(type) {
		case string:
			return v, nil
		default:
			return nil, fmt.Errorf("%T is not convertible to %s", v, typ)
		}
	case DataTypeDatetime:
		switch v := value.(type) {
		case string:
			return ParseTime(v, timeformat, tz)
		case time.Time:
			return v, nil
		case *time.Time:
			return *v, nil
		default:
			ts, err := ToInt64(v)
			if err != nil {
				return nil, fmt.Errorf("%T is not datetime convertible, %s", v, err)
			}
			switch timeformat {
			case "s":
				return time.Unix(ts, 0), nil
			case "ms":
				return time.Unix(0, ts*int64(time.Millisecond)), nil
			case "us":
				return time.Unix(0, ts*int64(time.Microsecond)), nil
			default: // "ns"
				return time.Unix(0, ts), nil
			}
		}
	case DataTypeInt16, COLUMN_TYPE_SHORT:
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
		}
		return ToInt16(value)
	case COLUMN_TYPE_USHORT, "unsigned short":
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
		}
		return ToUint16(value)
	case DataTypeInt32, COLUMN_TYPE_INTEGER, "int":
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
		}
		return ToInt32(value)
	case COLUMN_TYPE_UINTEGER, "unsigned integer":
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
		}
		return ToUint32(value)
	case DataTypeInt64, COLUMN_TYPE_LONG:
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
		}
		return ToInt64(value)
	case COLUMN_TYPE_ULONG, "unsigned long":
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
		}
		return ToUint64(value)
	case DataTypeFloat32: //, DB_COLUMN_TYPE_FLOAT:
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
		}
		return ToFloat32(value)
	case DataTypeFloat64: //, DB_COLUMN_TYPE_DOUBLE:
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
		}
		return ToFloat64(value)
	case DataTypeIPv4, DataTypeIPv6:
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
			return ParseIP(v)
		default:
			return nil, fmt.Errorf("%T is not %s convertible", v, typ)
		}
	case DataTypeBoolean:
		switch v := value.(type) {
		case string:
			if v == "" {
				return nil, nil
			}
			return ParseBoolean(v)
		default:
			return nil, fmt.Errorf("%T is not %s convertible", v, typ)
		}
	case DataTypeByte:
		return ToInt8(value)
	case DataTypeBinary:
		return parseBinary(value)
	case DataTypeDecimal:
		switch v := value.(type) {
		case Decimal:
			return v, nil
		case *Decimal:
			return *v, nil
		case string:
			return ParseDecimal(v, DecimalMaxPrecision, DecimalMaxScale)
		default:
			return nil, fmt.Errorf("%T is not convertible to decimal", value)
		}
	// case DB_COLUMN_TYPE_CLOB:
	// 	return util.ParseString(v)
	// case DB_COLUMN_TYPE_BLOB:
	// 	return util.ParseBinary(v)
	// case DB_COLUMN_TYPE_BINARY:
	// 	return util.ParseBinary(v)
	default:
		return nil, fmt.Errorf("unsupported column type; %s", typ)
	}
}

func parseBinary(v any) ([]byte, error) {
	switch v := v.(type) {
	case string:
		if strings.HasPrefix(v, "0x") || strings.HasPrefix(v, "0X") {
			return hex.DecodeString(v[2:])
		} else {
			// v is a base64 encoded string
			base64Data, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return nil, fmt.Errorf("failed to decode base64 string: %w", err)
			}
			return base64Data, nil
		}
	case []byte:
		return v, nil
	default:
		return nil, fmt.Errorf("%T is not convertible to binary", v)
	}
}

func (typ DataType) ColumnType() ColumnType {
	switch typ {
	case DataTypeInt16:
		return ColumnTypeShort
	case DataTypeInt32:
		return ColumnTypeInteger
	case DataTypeInt64:
		return ColumnTypeLong
	case DataTypeDatetime:
		return ColumnTypeDatetime
	case DataTypeFloat32:
		return ColumnTypeFloat
	case DataTypeFloat64:
		return ColumnTypeDouble
	case DataTypeIPv4:
		return ColumnTypeIPv4
	case DataTypeIPv6:
		return ColumnTypeIPv6
	case DataTypeString:
		return ColumnTypeVarchar
	case DataTypeBinary:
		return ColumnTypeBlob
	case DataTypeBoolean:
		return ColumnTypeInteger
	case DataTypeDecimal:
		return ColumnTypeDecimal
	case DataTypeByte:
		return ColumnTypeInteger
	default:
		switch strings.ToLower(string(typ)) {
		case COLUMN_TYPE_SHORT:
			return ColumnTypeShort
		case COLUMN_TYPE_USHORT, "unsigned short":
			return ColumnTypeUShort
		case COLUMN_TYPE_INTEGER, "int":
			return ColumnTypeInteger
		case COLUMN_TYPE_UINTEGER, "unsigned integer":
			return ColumnTypeUInteger
		case COLUMN_TYPE_LONG, "int64":
			return ColumnTypeLong
		case COLUMN_TYPE_ULONG, "unsigned long":
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
		default:
			return ColumnTypeVarchar
		}
	}
}

func (typ DataType) MakeBuffer(nullable bool) (any, error) {
	switch typ {
	case DataTypeInt16:
		if nullable {
			return new(sql.NullInt16), nil
		}
		return new(int16), nil
	case DataTypeUInt16:
		if nullable {
			return new(sql.Null[uint16]), nil
		}
		return new(uint16), nil
	case DataTypeInt32:
		if nullable {
			return new(sql.NullInt32), nil
		}
		return new(int32), nil
	case DataTypeUInt32:
		if nullable {
			return new(sql.Null[uint32]), nil
		}
		return new(uint32), nil
	case DataTypeInt64:
		if nullable {
			return new(sql.NullInt64), nil
		}
		return new(int64), nil
	case DataTypeUInt64:
		if nullable {
			return new(sql.Null[uint64]), nil
		}
		return new(uint64), nil
	case DataTypeDatetime:
		if nullable {
			return new(sql.NullTime), nil
		}
		return new(time.Time), nil
	case DataTypeFloat32:
		if nullable {
			return new(sql.Null[float32]), nil
		}
		return new(float32), nil
	case DataTypeFloat64:
		if nullable {
			return new(sql.NullFloat64), nil
		}
		return new(float64), nil
	case DataTypeIPv4:
		if nullable {
			return new(sql.Null[net.IP]), nil
		}
		return new(net.IP), nil
	case DataTypeIPv6:
		if nullable {
			return new(sql.Null[net.IP]), nil
		}
		return new(net.IP), nil
	case DataTypeString:
		if nullable {
			return new(sql.NullString), nil
		}
		return new(string), nil
	case DataTypeJSON:
		if nullable {
			return new(sql.Null[JSONString]), nil
		}
		return new(JSONString), nil
	case DataTypeBinary:
		return new([]byte), nil
	case DataTypeDecimal:
		if nullable {
			return new(sql.Null[Decimal]), nil
		}
		return new(Decimal), nil
	case DataTypeBoolean:
		if nullable {
			return new(sql.NullBool), nil
		}
		return new(bool), nil
	case DataTypeByte:
		return new(byte), nil
	case DataTypeAny:
		return new(string), nil
	default:
		return nil, fmt.Errorf("makeBuffer unsupported type %q", typ)
	}
}
