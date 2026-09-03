package machnet

import (
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

type BoundParam struct {
	sqlType          api.SqlType
	value            any
	isNull           bool
	cardinality      int
	elementPrecision int
	scale            int
}

func encodeParams(params []BoundParam, v403 bool) ([]byte, error) {
	if len(params) == 0 {
		return nil, nil
	}
	offset := 0
	buf := make([]byte, 0, 128)
	h := make([]byte, 2)
	if len(params) > 0xffff {
		return nil, fmt.Errorf("parameter count exceeds protocol limit")
	}
	if !v403 && len(params) > 0xff {
		return nil, fmt.Errorf("protocol versions before 4.0.3 support at most 255 parameters")
	}
	if v403 {
		binary.BigEndian.PutUint16(h, 2)
		buf = append(buf, h...)
		offset += 2
	}
	binary.BigEndian.PutUint16(h, uint16(len(params)))
	buf = append(buf, h...)
	offset += 2

	for idx, p := range params {
		typ, data, err := encodeBoundParam(p)
		if err != nil {
			return nil, fmt.Errorf("bind %d: %w", idx, err)
		}
		entryLen := 11
		if v403 {
			entryLen = 12
		}
		entry := make([]byte, entryLen)
		pos := 0
		if v403 {
			binary.BigEndian.PutUint16(entry[:2], uint16(idx+1))
			pos = 2
		} else {
			entry[0] = byte(idx + 1)
			pos = 1
		}
		entry[pos] = sqlParamInput
		entry[pos+1] = byte(typ)
		binary.BigEndian.PutUint32(entry[pos+2:pos+6], uint32(len(data)))
		binary.BigEndian.PutUint32(entry[pos+6:pos+10], uint32(len(data)))
		buf = append(buf, entry...)
		offset += len(entry)
		buf = append(buf, data...)
		offset += len(data)
		if offset&1 == 1 {
			buf = append(buf, 0)
			offset++
		}
	}
	return buf, nil
}

func encodeBoundParam(p BoundParam) (int, []byte, error) {
	cmdType := sqlTypeToCmdType(p.sqlType)
	if p.isNull || p.value == nil {
		switch p.sqlType {
		case api.SqlTypeInt16:
			b := make([]byte, 2)
			binary.BigEndian.PutUint16(b, 0x8000)
			return cmdType, b, nil
		case api.SqlTypeInt32:
			b := make([]byte, 4)
			binary.BigEndian.PutUint32(b, 0x80000000)
			return cmdType, b, nil
		case api.SqlTypeInt64:
			b := make([]byte, 8)
			binary.BigEndian.PutUint64(b, 0x8000000000000000)
			return cmdType, b, nil
		case api.SqlTypeDatetime:
			b := make([]byte, 8)
			binary.BigEndian.PutUint64(b, datetimeNull)
			return cmdType, b, nil
		case api.SqlTypeFloat:
			b := make([]byte, 4)
			binary.BigEndian.PutUint32(b, math.Float32bits(floatNull))
			return cmdType, b, nil
		case api.SqlTypeDouble:
			b := make([]byte, 8)
			binary.BigEndian.PutUint64(b, math.Float64bits(doubleNull))
			return cmdType, b, nil
		case api.SqlTypeIPv4:
			return cmdType, make([]byte, 5), nil
		case api.SqlTypeIPv6:
			return cmdType, make([]byte, 17), nil
		case api.SqlTypeDecimal:
			size, err := decimalSize(api.DecimalMaxPrecision)
			if err != nil {
				return 0, nil, err
			}
			return cmdType, make([]byte, size), nil
		default:
			return cmdType, nil, nil
		}
	}
	if p.sqlType.IsArray() {
		value, ok := p.value.(*api.Array)
		if !ok {
			if plain, plainOK := p.value.(api.Array); plainOK {
				value = plain.Clone()
			} else {
				return 0, nil, fmt.Errorf("unsupported ARRAY type %T", p.value)
			}
		}
		cardinality := p.cardinality
		if cardinality == 0 {
			cardinality = value.Cardinality()
		}
		elementPrecision := p.elementPrecision
		scale := p.scale
		if p.sqlType.ElementType() == api.SqlTypeDecimal && elementPrecision == 0 {
			elementPrecision = value.Precision()
			scale = value.Scale()
		}
		col := ColumnMeta{spinerType: cmdType, precision: cardinality, elementPrecision: elementPrecision, scale: scale}
		data, err := encodeArrayPayload(value, col, true)
		return cmdType, data, err
	}

	switch p.sqlType {
	case api.SqlTypeInt16:
		v, err := toInt64(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(int16(v)))
		return cmdType, b, nil
	case api.SqlTypeInt32:
		v, err := toInt64(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(int32(v)))
		return cmdType, b, nil
	case api.SqlTypeInt64:
		v, err := toInt64(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(v))
		return cmdType, b, nil
	case api.SqlTypeUInt16:
		v, err := toUint64(p.value)
		if err != nil {
			return 0, nil, err
		}
		if v > math.MaxUint16 {
			return 0, nil, fmt.Errorf("uint16 value %d overflows", v)
		}
		b := make([]byte, 2)
		binary.BigEndian.PutUint16(b, uint16(v))
		return cmdType, b, nil
	case api.SqlTypeUInt32:
		v, err := toUint64(p.value)
		if err != nil {
			return 0, nil, err
		}
		if v > math.MaxUint32 {
			return 0, nil, fmt.Errorf("uint32 value %d overflows", v)
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, uint32(v))
		return cmdType, b, nil
	case api.SqlTypeUInt64:
		v, err := toUint64(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, v)
		return cmdType, b, nil
	case api.SqlTypeDatetime:
		v, err := toDateTimeInt64(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, uint64(v))
		return cmdType, b, nil
	case api.SqlTypeFloat:
		v, err := toFloat64(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 4)
		binary.BigEndian.PutUint32(b, math.Float32bits(float32(v)))
		return cmdType, b, nil
	case api.SqlTypeDouble:
		v, err := toFloat64(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 8)
		binary.BigEndian.PutUint64(b, math.Float64bits(v))
		return cmdType, b, nil
	case api.SqlTypeIPv4:
		ip, err := toIP(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 5)
		if ip4 := ip.To4(); ip4 != nil {
			b[0] = 4
			copy(b[1:], ip4)
		}
		return cmdType, b, nil
	case api.SqlTypeIPv6:
		ip, err := toIP(p.value)
		if err != nil {
			return 0, nil, err
		}
		b := make([]byte, 17)
		if ip16 := ip.To16(); ip16 != nil {
			b[0] = 6
			copy(b[1:], ip16)
		}
		return cmdType, b, nil
	case api.SqlTypeBinary:
		switch v := p.value.(type) {
		case []byte:
			return cmdType, append([]byte(nil), v...), nil
		case string:
			value, err := api.DataTypeBinary.Apply(v, "", nil)
			if err != nil {
				return 0, nil, err
			}
			return cmdType, value.([]byte), nil
		default:
			return 0, nil, fmt.Errorf("unsupported binary type %T", p.value)
		}
	case api.SqlTypeDecimal:
		data, err := encodeDecimal(p.value, api.DecimalMaxPrecision, api.DecimalMaxScale)
		if err != nil {
			// Query DECIMAL values use a DECIMAL(65,30) wire carrier. Values
			// with more than 35 integer digits cannot fit that carrier even
			// though they are valid DECIMAL(65,0) values. Preserve the full
			// public Decimal domain by letting the server perform its exact
			// VARCHAR-to-DECIMAL conversion for that case.
			switch value := p.value.(type) {
			case api.Decimal:
				return cmdVarcharType, []byte(value.String()), nil
			case *api.Decimal:
				if value != nil {
					return cmdVarcharType, []byte(value.String()), nil
				}
			}
		}
		return cmdType, data, err
	default:
		switch v := p.value.(type) {
		case string:
			return cmdType, []byte(v), nil
		case []byte:
			return cmdType, append([]byte(nil), v...), nil
		default:
			return cmdType, []byte(fmt.Sprint(v)), nil
		}
	}
}

func toInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int32:
		return int64(x), nil
	case int64:
		return x, nil
	case uint:
		if uint64(x) > math.MaxInt64 {
			return 0, fmt.Errorf("unsigned integer %d overflows int64", x)
		}
		return int64(x), nil
	case uint16:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint64:
		if x > math.MaxInt64 {
			return 0, fmt.Errorf("unsigned integer %d overflows int64", x)
		}
		return int64(x), nil
	case float32:
		return int64(x), nil
	case float64:
		return int64(x), nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return 0, err
		}
		return n, nil
	default:
		return 0, fmt.Errorf("unsupported integer type %T", v)
	}
}

func toUint64(v any) (uint64, error) {
	switch x := v.(type) {
	case int:
		if x < 0 {
			return 0, fmt.Errorf("negative unsigned integer %d", x)
		}
		return uint64(x), nil
	case int16:
		if x < 0 {
			return 0, fmt.Errorf("negative unsigned integer %d", x)
		}
		return uint64(x), nil
	case int32:
		if x < 0 {
			return 0, fmt.Errorf("negative unsigned integer %d", x)
		}
		return uint64(x), nil
	case int64:
		if x < 0 {
			return 0, fmt.Errorf("negative unsigned integer %d", x)
		}
		return uint64(x), nil
	case uint:
		return uint64(x), nil
	case uint16:
		return uint64(x), nil
	case uint32:
		return uint64(x), nil
	case uint64:
		return x, nil
	case string:
		return strconv.ParseUint(strings.TrimSpace(x), 10, 64)
	default:
		return 0, fmt.Errorf("unsupported unsigned integer type %T", v)
	}
}

func toFloat64(v any) (float64, error) {
	switch x := v.(type) {
	case float32:
		return float64(x), nil
	case float64:
		return x, nil
	case int:
		return float64(x), nil
	case int16:
		return float64(x), nil
	case int32:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case uint16:
		return float64(x), nil
	case uint32:
		return float64(x), nil
	case uint64:
		return float64(x), nil
	case string:
		return strconv.ParseFloat(strings.TrimSpace(x), 64)
	default:
		return 0, fmt.Errorf("unsupported float type %T", v)
	}
}

func toDateTimeInt64(v any) (int64, error) {
	switch x := v.(type) {
	case time.Time:
		return x.UnixNano(), nil
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case uint64:
		return int64(x), nil
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
			return n, nil
		}
		t, err := time.Parse(time.RFC3339Nano, x)
		if err != nil {
			return 0, err
		}
		return t.UnixNano(), nil
	default:
		return 0, fmt.Errorf("unsupported datetime type %T", v)
	}
}

func toIP(v any) (net.IP, error) {
	switch x := v.(type) {
	case net.IP:
		return x, nil
	case string:
		ip := net.ParseIP(strings.TrimSpace(x))
		if ip == nil {
			return nil, fmt.Errorf("invalid ip %q", x)
		}
		return ip, nil
	case []byte:
		if len(x) == 4 || len(x) == 16 {
			ip := net.IP(x)
			if ip != nil {
				return ip, nil
			}
		}
		ip := net.ParseIP(strings.TrimSpace(string(x)))
		if ip == nil {
			return nil, fmt.Errorf("invalid ip bytes")
		}
		return ip, nil
	default:
		return nil, fmt.Errorf("unsupported ip type %T", v)
	}
}
