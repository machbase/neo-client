package machnet

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"time"
	"unicode/utf8"

	"github.com/machbase/neo-client/api"
)

type ColumnMeta struct {
	name        string
	cmType      uint64
	precision   int
	scale       int
	spinerType  int
	length      int
	isVariable  bool
	sqlType     api.SqlType
	nullable    bool
	nullability api.Nullability
	primaryKey  bool
}

func buildColumns(units map[uint32][]MarshalUnit, v403 bool) []ColumnMeta {
	names := units[cmiPColNameID]
	types := units[cmiPColTypeID]
	count := len(names)
	if len(types) < count {
		count = len(types)
	}
	ret := make([]ColumnMeta, 0, count)
	for i := 0; i < count; i++ {
		cmType := uint64(0)
		if len(types[i].data) >= 8 {
			cmType = binary.LittleEndian.Uint64(types[i].data)
		}
		spiner := extractSpinerType(cmType)
		precision := extractPrecision(cmType)
		nullability := extractNullability(cmType, v403)
		meta := ColumnMeta{
			name:        string(names[i].data),
			cmType:      cmType,
			precision:   precision,
			scale:       extractScale(cmType, v403),
			spinerType:  spiner,
			length:      computeColumnLength(spiner, precision),
			isVariable:  isVariableSpinerType(spiner),
			sqlType:     spinerTypeToSqlType(spiner),
			nullable:    nullability == api.NullabilityNullable,
			nullability: nullability,
			primaryKey:  extractPrimaryKey(cmType, v403),
		}
		ret = append(ret, meta)
	}
	return ret
}

func buildParamDesc(units map[uint32][]MarshalUnit, count int, v403 bool) []ParamDesc {
	typUnits := units[cmiPParamTypeID]
	if count <= 0 {
		count = len(typUnits)
	}
	ret := make([]ParamDesc, count)
	for i := 0; i < count; i++ {
		d := ParamDesc{Type: api.SqlTypeString, Nullability: api.NullabilityUnknown, Ordinal: i + 1}
		if i < len(typUnits) && len(typUnits[i].data) >= 8 {
			cmType := binary.LittleEndian.Uint64(typUnits[i].data)
			d.Type = spinerTypeToSqlType(extractSpinerType(cmType))
			d.Precision = extractPrecision(cmType)
			d.Scale = extractScale(cmType, v403)
			d.Nullability = extractNullability(cmType, v403)
			d.Nullable = d.Nullability == api.NullabilityNullable
		}
		ret[i] = d
	}
	return ret
}

func applyParamMetadataV2(desc []ParamDesc, units map[uint32][]MarshalUnit) error {
	blocks := units[cmiPParamMetaV2ID]
	if len(blocks) != 1 {
		return fmt.Errorf("prepare response missing parameter name metadata")
	}
	data := blocks[0].data
	if len(data) < 4 || binary.BigEndian.Uint16(data[:2]) != 1 || int(binary.BigEndian.Uint16(data[2:4])) != len(desc) {
		return fmt.Errorf("parameter name metadata header mismatch")
	}
	offset := 4
	seen := make([]bool, len(desc))
	for range desc {
		if offset+8 > len(data) {
			return fmt.Errorf("truncated parameter name metadata entry")
		}
		ordinal := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		flags := binary.BigEndian.Uint16(data[offset+2 : offset+4])
		nameLen := int(binary.BigEndian.Uint32(data[offset+4 : offset+8]))
		offset += 8
		if ordinal < 1 || ordinal > len(desc) || seen[ordinal-1] || flags&^uint16(1) != 0 || nameLen < 0 || offset+nameLen > len(data) {
			return fmt.Errorf("invalid parameter name metadata entry")
		}
		named := flags&1 != 0
		if (!named && nameLen != 0) || (named && nameLen == 0) {
			return fmt.Errorf("parameter name metadata flag mismatch")
		}
		if named {
			if !utf8.Valid(data[offset : offset+nameLen]) {
				return fmt.Errorf("invalid UTF-8 in parameter name metadata")
			}
			desc[ordinal-1].Name = string(data[offset : offset+nameLen])
		}
		offset += nameLen
		if offset&1 != 0 {
			if offset >= len(data) || data[offset] != 0 {
				return fmt.Errorf("invalid parameter name metadata padding")
			}
			offset++
		}
		seen[ordinal-1] = true
	}
	if offset != len(data) {
		return fmt.Errorf("parameter name metadata occurrence mismatch")
	}
	return nil
}

func decodeRowsFromUnits(units []MarshalUnit, columns []ColumnMeta) ([][]any, error) {
	rows := make([][]any, len(units))
	if len(units) == 0 {
		return rows, nil
	}
	if len(columns) == 0 {
		return rows, nil
	}
	flat := make([]any, len(units)*len(columns))
	for i, unit := range units {
		row := flat[i*len(columns) : (i+1)*len(columns)]
		if err := decodeRowInto(row, unit.data, columns); err != nil {
			return nil, err
		}
		rows[i] = row
	}
	return rows, nil
}

func decodeRowInto(ret []any, data []byte, columns []ColumnMeta) error {
	if len(ret) < len(columns) {
		return fmt.Errorf("invalid row buffer size: have=%d need=%d", len(ret), len(columns))
	}
	off := 0
	for i, col := range columns {
		if col.isVariable {
			if off+4 > len(data) {
				return fmt.Errorf("malformed row variable length")
			}
			l := int(binary.BigEndian.Uint32(data[off : off+4]))
			off += 4
			if l == 0 {
				ret[i] = nil
				continue
			}
			if off+l > len(data) {
				return fmt.Errorf("malformed row variable overrun")
			}
			field := data[off : off+l]
			off += l
			ret[i] = decodeVariableField(col, field)
			continue
		}
		length := col.length
		if length == 0 {
			ret[i] = nil
			continue
		}
		if off+length > len(data) {
			return fmt.Errorf("malformed row fixed overrun")
		}
		field := data[off : off+length]
		off += length
		if col.spinerType == cmdDecimalType {
			value, err := decodeDecimal(field, col.precision, col.scale)
			if err != nil {
				return err
			}
			ret[i] = value
			continue
		}
		ret[i] = decodeFixedField(col, field)
	}
	return nil
}

func decodeVariableField(col ColumnMeta, field []byte) any {
	switch col.spinerType {
	case cmdVarcharType, cmdTextType, cmdCharType, cmdJSONType, cmdClobType:
		return string(field)
	case cmdIPNetType:
		return string(field)
	case cmdBinaryType, cmdBlobType:
		b := make([]byte, len(field))
		copy(b, field)
		return b
	default:
		b := make([]byte, len(field))
		copy(b, field)
		return b
	}
}

func decodeFixedField(col ColumnMeta, field []byte) any {
	switch col.spinerType {
	case cmdBoolType, cmdInt16Type:
		if len(field) < 2 {
			return nil
		}
		v := int16(binary.BigEndian.Uint16(field))
		if v == shortNull {
			return nil
		}
		return v
	case cmdUInt16Type:
		if len(field) < 2 {
			return nil
		}
		v := binary.BigEndian.Uint16(field)
		if v == ushortNull {
			return nil
		}
		return int32(v)
	case cmdInt32Type:
		if len(field) < 4 {
			return nil
		}
		v := int32(binary.BigEndian.Uint32(field))
		if v == intNull {
			return nil
		}
		return v
	case cmdUInt32Type:
		if len(field) < 4 {
			return nil
		}
		v := binary.BigEndian.Uint32(field)
		if v == uintNull {
			return nil
		}
		return int64(v)
	case cmdInt64Type:
		if len(field) < 8 {
			return nil
		}
		v := int64(binary.BigEndian.Uint64(field))
		if v == longNull {
			return nil
		}
		return v
	case cmdUInt64Type:
		if len(field) < 8 {
			return nil
		}
		v := binary.BigEndian.Uint64(field)
		if v == ulongNull {
			return nil
		}
		return int64(v)
	case cmdFlt32Type:
		if len(field) < 4 {
			return nil
		}
		v := math.Float32frombits(binary.BigEndian.Uint32(field))
		if v == floatNull {
			return nil
		}
		return v
	case cmdFlt64Type:
		if len(field) < 8 {
			return nil
		}
		v := math.Float64frombits(binary.BigEndian.Uint64(field))
		if v == doubleNull {
			return nil
		}
		return v
	case cmdDateType:
		if len(field) < 8 {
			return nil
		}
		raw := binary.BigEndian.Uint64(field)
		if raw == datetimeNull {
			return nil
		}
		return time.Unix(0, int64(raw))
	case cmdIpv4Type:
		if len(field) < 5 || field[0] == 0 {
			return nil
		}
		return net.IP(append([]byte(nil), field[1:5]...))
	case cmdIpv6Type:
		if len(field) < 17 || field[0] == 0 {
			return nil
		}
		return net.IP(append([]byte(nil), field[1:17]...))
	case cmdNulType:
		return nil
	default:
		b := make([]byte, len(field))
		copy(b, field)
		if len(bytes.Trim(b, "\x00")) == 0 {
			return nil
		}
		return b
	}
}
