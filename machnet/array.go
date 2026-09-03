package machnet

import (
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"github.com/machbase/neo-client/v2/api"
)

const sparseArrayFormatVersion = 1
const sparseArrayFlag = 1

func decodeArrayPayload(col ColumnMeta, payload []byte) (*api.Array, error) {
	cardinality := col.precision
	elementSize := computeColumnLength(arrayBaseSpinerType(col.spinerType), col.elementPrecision)
	bitmapSize := (cardinality + 7) / 8
	expected := 2 + bitmapSize + elementSize*cardinality
	if cardinality < 1 || cardinality > api.ArrayMaxCardinality || elementSize <= 0 || len(payload) != expected {
		return nil, fmt.Errorf("invalid ARRAY payload size")
	}
	if int(binary.BigEndian.Uint16(payload[:2])) != cardinality {
		return nil, fmt.Errorf("ARRAY cardinality mismatch")
	}
	if cardinality%8 != 0 {
		unusedMask := byte(0xff << uint(cardinality%8))
		if payload[2+bitmapSize-1]&unusedMask != 0 {
			return nil, fmt.Errorf("invalid ARRAY NULL bitmap padding")
		}
	}
	ret, err := api.NewSparseArrayWithMeta(
		spinerTypeToSqlType(arrayBaseSpinerType(col.spinerType)),
		cardinality,
		col.elementPrecision,
		col.scale,
	)
	if err != nil {
		return nil, err
	}
	for idx := 0; idx < cardinality; idx++ {
		if payload[2+idx/8]&(1<<uint(idx%8)) != 0 {
			continue
		}
		begin := 2 + bitmapSize + idx*elementSize
		value, err := decodeArrayElement(
			arrayBaseSpinerType(col.spinerType),
			payload[begin:begin+elementSize],
			col.elementPrecision,
			col.scale,
		)
		if err != nil {
			return nil, err
		}
		if value == nil {
			return nil, fmt.Errorf("non-NULL ARRAY element uses NULL sentinel")
		}
		if err := ret.Set(idx+1, value); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func decodeArrayElement(spinerType int, field []byte, precision, scale int) (any, error) {
	switch spinerType {
	case cmdInt16Type:
		value := int16(binary.BigEndian.Uint16(field))
		if value == shortNull {
			return nil, fmt.Errorf("INT16 ARRAY element uses NULL sentinel")
		}
		return value, nil
	case cmdUInt16Type:
		value := binary.BigEndian.Uint16(field)
		if value == ushortNull {
			return nil, fmt.Errorf("UINT16 ARRAY element uses NULL sentinel")
		}
		return value, nil
	case cmdInt32Type:
		value := int32(binary.BigEndian.Uint32(field))
		if value == intNull {
			return nil, fmt.Errorf("INT32 ARRAY element uses NULL sentinel")
		}
		return value, nil
	case cmdUInt32Type:
		value := binary.BigEndian.Uint32(field)
		if value == uintNull {
			return nil, fmt.Errorf("UINT32 ARRAY element uses NULL sentinel")
		}
		return value, nil
	case cmdInt64Type:
		value := int64(binary.BigEndian.Uint64(field))
		if value == longNull {
			return nil, fmt.Errorf("INT64 ARRAY element uses NULL sentinel")
		}
		return value, nil
	case cmdUInt64Type:
		value := binary.BigEndian.Uint64(field)
		if value == ulongNull {
			return nil, fmt.Errorf("UINT64 ARRAY element uses NULL sentinel")
		}
		return value, nil
	case cmdFlt32Type:
		bits := binary.BigEndian.Uint32(field)
		if bits == math.Float32bits(floatNull) {
			return nil, fmt.Errorf("FLOAT ARRAY element uses NULL sentinel")
		}
		return math.Float32frombits(bits), nil
	case cmdFlt64Type:
		bits := binary.BigEndian.Uint64(field)
		if bits == math.Float64bits(doubleNull) {
			return nil, fmt.Errorf("DOUBLE ARRAY element uses NULL sentinel")
		}
		return math.Float64frombits(bits), nil
	case cmdDecimalType:
		return decodeDecimal(field, precision, scale)
	default:
		return nil, fmt.Errorf("unsupported ARRAY element type %d", spinerType)
	}
}

func encodeArrayPayload(value *api.Array, col ColumnMeta, allowSparse bool) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	if value.Cardinality() != col.precision {
		return nil, fmt.Errorf("ARRAY cardinality mismatch: expected %d, got %d", col.precision, value.Cardinality())
	}
	expectedElement := spinerTypeToSqlType(arrayBaseSpinerType(col.spinerType))
	if value.ElementType() != expectedElement {
		return nil, fmt.Errorf("ARRAY element type mismatch: expected %s, got %s", expectedElement, value.ElementType())
	}
	entries := value.Entries()
	positions := make([]int, 0, len(entries))
	for position := range entries {
		positions = append(positions, position)
	}
	sort.Ints(positions)
	elementType := arrayBaseSpinerType(col.spinerType)
	elementSize := computeColumnLength(elementType, col.elementPrecision)
	denseSize := computeArrayColumnLength(col.spinerType, col.precision, col.elementPrecision)
	if elementSize <= 0 || denseSize <= 0 {
		return nil, fmt.Errorf("invalid ARRAY column metadata")
	}
	sparseSize := 8 + len(positions)*(2+elementSize)
	if allowSparse && sparseSize < denseSize {
		ret := make([]byte, sparseSize)
		ret[2] = sparseArrayFormatVersion
		ret[3] = sparseArrayFlag
		binary.BigEndian.PutUint16(ret[4:6], uint16(col.precision))
		binary.BigEndian.PutUint16(ret[6:8], uint16(len(positions)))
		offset := 8
		for _, position := range positions {
			binary.BigEndian.PutUint16(ret[offset:offset+2], uint16(position))
			offset += 2
			encoded, err := encodeArrayElement(elementType, entries[position], col.elementPrecision, col.scale)
			if err != nil {
				return nil, err
			}
			copy(ret[offset:offset+elementSize], encoded)
			offset += elementSize
		}
		return ret, nil
	}
	ret := make([]byte, denseSize)
	binary.BigEndian.PutUint16(ret[:2], uint16(col.precision))
	bitmapSize := (col.precision + 7) / 8
	for idx := 0; idx < col.precision; idx++ {
		ret[2+idx/8] |= 1 << uint(idx%8)
	}
	for _, position := range positions {
		encoded, err := encodeArrayElement(elementType, entries[position], col.elementPrecision, col.scale)
		if err != nil {
			return nil, err
		}
		ret[2+(position-1)/8] &^= 1 << uint((position-1)%8)
		copy(ret[2+bitmapSize+(position-1)*elementSize:], encoded)
	}
	return ret, nil
}

func encodeArrayElement(spinerType int, value any, precision, scale int) ([]byte, error) {
	switch spinerType {
	case cmdInt16Type:
		v, err := toInt64(value)
		if err != nil || v < -32767 || v > 32767 {
			return nil, fmt.Errorf("invalid INT16 ARRAY element")
		}
		ret := make([]byte, 2)
		binary.BigEndian.PutUint16(ret, uint16(int16(v)))
		return ret, nil
	case cmdUInt16Type:
		v, err := toUint64(value)
		if err != nil || v > 65534 {
			return nil, fmt.Errorf("invalid UINT16 ARRAY element")
		}
		ret := make([]byte, 2)
		binary.BigEndian.PutUint16(ret, uint16(v))
		return ret, nil
	case cmdInt32Type:
		v, err := toInt64(value)
		if err != nil || v < -2147483647 || v > 2147483647 {
			return nil, fmt.Errorf("invalid INT32 ARRAY element")
		}
		ret := make([]byte, 4)
		binary.BigEndian.PutUint32(ret, uint32(int32(v)))
		return ret, nil
	case cmdUInt32Type:
		v, err := toUint64(value)
		if err != nil || v > 0xfffffffe {
			return nil, fmt.Errorf("invalid UINT32 ARRAY element")
		}
		ret := make([]byte, 4)
		binary.BigEndian.PutUint32(ret, uint32(v))
		return ret, nil
	case cmdInt64Type:
		v, err := toInt64(value)
		if err != nil || v == math.MinInt64 {
			return nil, fmt.Errorf("invalid INT64 ARRAY element")
		}
		ret := make([]byte, 8)
		binary.BigEndian.PutUint64(ret, uint64(v))
		return ret, nil
	case cmdUInt64Type:
		v, err := toUint64(value)
		if err != nil || v == math.MaxUint64 {
			return nil, fmt.Errorf("invalid UINT64 ARRAY element")
		}
		ret := make([]byte, 8)
		binary.BigEndian.PutUint64(ret, v)
		return ret, nil
	case cmdFlt32Type:
		v, err := toFloat64(value)
		if err != nil {
			return nil, err
		}
		ret := make([]byte, 4)
		bits := math.Float32bits(float32(v))
		if bits == math.Float32bits(floatNull) {
			return nil, fmt.Errorf("FLOAT ARRAY element uses NULL sentinel")
		}
		binary.BigEndian.PutUint32(ret, bits)
		return ret, nil
	case cmdFlt64Type:
		v, err := toFloat64(value)
		if err != nil {
			return nil, err
		}
		ret := make([]byte, 8)
		bits := math.Float64bits(v)
		if bits == math.Float64bits(doubleNull) {
			return nil, fmt.Errorf("DOUBLE ARRAY element uses NULL sentinel")
		}
		binary.BigEndian.PutUint64(ret, bits)
		return ret, nil
	case cmdDecimalType:
		ret, err := encodeDecimal(value, precision, scale)
		if err != nil {
			return nil, err
		}
		allZero := true
		for _, octet := range ret {
			if octet != 0 {
				allZero = false
				break
			}
		}
		if allZero {
			return nil, fmt.Errorf("DECIMAL ARRAY element uses NULL sentinel")
		}
		return ret, nil
	default:
		return nil, fmt.Errorf("unsupported ARRAY element type %d", spinerType)
	}
}
