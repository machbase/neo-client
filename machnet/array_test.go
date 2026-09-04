package machnet

import (
	"encoding/binary"
	"math"
	"strconv"
	"testing"

	"github.com/machbase/neo-client/v2/api"
)

func TestSparseArrayEncodeDecode(t *testing.T) {
	value, err := api.NewSparseArray(api.SqlTypeInt32, 1024)
	if err != nil {
		t.Fatal(err)
	}
	_ = value.Set(0, int32(10))
	_ = value.Set(1023, int32(20))
	col := ColumnMeta{spinerType: cmdInt32ArrayType, precision: 1024}
	payload, err := encodeArrayPayload(value, col, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(payload) >= computeArrayColumnLength(cmdInt32ArrayType, 1024, 0)/20 {
		t.Fatalf("sparse payload too large: %d", len(payload))
	}
	if binary.BigEndian.Uint16(payload[:2]) != 0 || payload[2] != sparseArrayFormatVersion {
		t.Fatalf("invalid sparse envelope: %x", payload[:4])
	}

	dense, err := encodeArrayPayload(value, col, false)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeArrayPayload(col, dense)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.String(); got[:3] != "[10" || got[len(got)-3:] != "20]" {
		t.Fatalf("decoded boundary mismatch: %s", got)
	}
}

func TestAppendTargetEncoding(t *testing.T) {
	payload, err := encodeAppendTargets([]string{"ID", "VALUES_ARRAY[0]", "VALUES_ARRAY[5]"})
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(payload[:2]) != 1 || binary.BigEndian.Uint16(payload[2:4]) != 3 {
		t.Fatalf("invalid target header: %x", payload[:4])
	}
	offset := 4
	wantPositions := []uint16{0, 1, 6}
	for index, want := range wantPositions {
		nameLength := int(binary.BigEndian.Uint16(payload[offset : offset+2]))
		offset += 2 + nameLength
		if got := binary.BigEndian.Uint16(payload[offset : offset+2]); got != want {
			t.Fatalf("wire position[%d]=%d, want %d", index, got, want)
		}
		offset += 2
	}
	if _, err := encodeAppendTargets([]string{"A[-1]"}); err == nil {
		t.Fatal("negative position accepted")
	}
	if _, err := encodeAppendTargets([]string{"A[0]", "A[0]"}); err == nil {
		t.Fatal("duplicate accepted")
	}
	if _, err := encodeAppendTargets([]string{"A", "A[0]"}); err == nil {
		t.Fatal("whole/element conflict accepted")
	}
}

func TestArrayMetadataAndBind(t *testing.T) {
	wirePrecision := uint64(8 | (12 << cmiArrayElementPrecisionShift))
	cmType := (uint64(cmdDecimalArrayType) << 56) | (wirePrecision << 28) | (uint64(4) << 23)
	nameWriter := newMarshalWriter(cmiAppendOpenProtocol, 1, 0)
	nameWriter.addString(cmiPColNameID, "A")
	nameWriter.addUInt64(cmiPColTypeID, cmType)
	packets := nameWriter.finalize()
	if len(packets) != 1 {
		t.Fatalf("packets=%d", len(packets))
	}
	units, err := collectUnits(packets[0][packetHeaderSize:])
	if err != nil {
		t.Fatal(err)
	}
	columns := buildColumns(units, true)
	if len(columns) != 1 || columns[0].precision != 8 || columns[0].elementPrecision != 12 || columns[0].scale != 4 {
		t.Fatalf("invalid ARRAY metadata: %+v", columns)
	}

	value, err := api.NewSparseArray(api.SqlTypeInt32, 8)
	if err != nil {
		t.Fatal(err)
	}
	_ = value.Set(0, int32(10))
	typ, payload, err := encodeBoundParam(BoundParam{
		sqlType:     api.SqlTypeInt32Array,
		value:       value,
		cardinality: 8,
	})
	if err != nil {
		t.Fatal(err)
	}
	if typ != cmdInt32ArrayType || len(payload) == 0 || binary.BigEndian.Uint16(payload[:2]) != 0 {
		t.Fatalf("invalid sparse ARRAY bind type=%d payload=%x", typ, payload)
	}
}

func TestArrayDecodeRejectsMalformedCanonicalPayload(t *testing.T) {
	value, err := api.NewArray(api.SqlTypeInt32,
		[]any{int32(10), nil, int32(30)})
	if err != nil {
		t.Fatal(err)
	}
	col := ColumnMeta{spinerType: cmdInt32ArrayType, precision: 3}
	payload, err := encodeArrayPayload(value, col, false)
	if err != nil {
		t.Fatal(err)
	}

	invalidPadding := append([]byte(nil), payload...)
	invalidPadding[2] |= 0x80
	if _, err := decodeArrayPayload(col, invalidPadding); err == nil {
		t.Fatal("non-zero NULL bitmap padding accepted")
	}

	nullSentinel := append([]byte(nil), payload...)
	binary.BigEndian.PutUint32(nullSentinel[3:7], uint32(1)<<31)
	if _, err := decodeArrayPayload(col, nullSentinel); err == nil {
		t.Fatal("non-NULL element scalar NULL sentinel accepted")
	}

	wrongCardinality := append([]byte(nil), payload...)
	binary.BigEndian.PutUint16(wrongCardinality[:2], 2)
	if _, err := decodeArrayPayload(col, wrongCardinality); err == nil {
		t.Fatal("mismatched cardinality accepted")
	}
	if _, err := decodeArrayPayload(col, payload[:len(payload)-1]); err == nil {
		t.Fatal("truncated canonical payload accepted")
	}
	oversized := append(append([]byte(nil), payload...), 0)
	if _, err := decodeArrayPayload(col, oversized); err == nil {
		t.Fatal("oversized canonical payload accepted")
	}
}

func TestDenseArrayRoundTripAllElementTypes(t *testing.T) {
	decimalValue, err := api.ParseDecimal("12345678.1250", 12, 4)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name             string
		elementType      api.SqlType
		spinerType       int
		elementPrecision int
		scale            int
		values           []any
	}{
		{"int16", api.SqlTypeInt16, cmdInt16ArrayType, 0, 0, []any{int16(-32767), nil, int16(32767)}},
		{"uint16", api.SqlTypeUInt16, cmdUInt16ArrayType, 0, 0, []any{uint16(0), nil, uint16(65534)}},
		{"int32", api.SqlTypeInt32, cmdInt32ArrayType, 0, 0, []any{int32(-2147483647), nil, int32(2147483647)}},
		{"uint32", api.SqlTypeUInt32, cmdUInt32ArrayType, 0, 0, []any{uint32(0), nil, uint32(0xfffffffe)}},
		{"int64", api.SqlTypeInt64, cmdInt64ArrayType, 0, 0, []any{int64(-9223372036854775807), nil, int64(9223372036854775807)}},
		{"uint64", api.SqlTypeUInt64, cmdUInt64ArrayType, 0, 0, []any{uint64(0), nil, uint64(0xfffffffffffffffe)}},
		{"float", api.SqlTypeFloat, cmdFlt32ArrayType, 0, 0, []any{float32(-1.25), nil, float32(3.5)}},
		{"double", api.SqlTypeDouble, cmdFlt64ArrayType, 0, 0, []any{float64(-1.25), nil, float64(3.5)}},
		{"decimal", api.SqlTypeDecimal, cmdDecimalArrayType, 12, 4, []any{decimalValue, nil, decimalValue}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := api.NewArray(test.elementType, test.values)
			if err != nil {
				t.Fatal(err)
			}
			col := ColumnMeta{
				spinerType:       test.spinerType,
				precision:        len(test.values),
				elementPrecision: test.elementPrecision,
				scale:            test.scale,
			}
			payload, err := encodeArrayPayload(value, col, false)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := decodeArrayPayload(col, payload)
			if err != nil {
				t.Fatal(err)
			}
			if got, want := decoded.String(), value.String(); got != want {
				t.Fatalf("round trip got %s, want %s", got, want)
			}
		})
	}
}

func TestSparseArrayEncodingAllElementTypes(t *testing.T) {
	decimalValue, err := api.ParseDecimal("12345678.1250", 12, 4)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name             string
		elementType      api.SqlType
		spinerType       int
		elementPrecision int
		scale            int
		first            any
		last             any
	}{
		{"int16", api.SqlTypeInt16, cmdInt16ArrayType, 0, 0, int16(-32767), int16(32767)},
		{"uint16", api.SqlTypeUInt16, cmdUInt16ArrayType, 0, 0, uint16(0), uint16(65534)},
		{"int32", api.SqlTypeInt32, cmdInt32ArrayType, 0, 0, int32(-2147483647), int32(2147483647)},
		{"uint32", api.SqlTypeUInt32, cmdUInt32ArrayType, 0, 0, uint32(0), uint32(0xfffffffe)},
		{"int64", api.SqlTypeInt64, cmdInt64ArrayType, 0, 0, int64(-9223372036854775807), int64(9223372036854775807)},
		{"uint64", api.SqlTypeUInt64, cmdUInt64ArrayType, 0, 0, uint64(0), uint64(0xfffffffffffffffe)},
		{"float", api.SqlTypeFloat, cmdFlt32ArrayType, 0, 0, float32(-1.25), float32(3.5)},
		{"double", api.SqlTypeDouble, cmdFlt64ArrayType, 0, 0, float64(-1.25), float64(3.5)},
		{"decimal", api.SqlTypeDecimal, cmdDecimalArrayType, 12, 4, decimalValue, decimalValue},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value, err := api.NewSparseArrayWithMeta(test.elementType, 32, test.elementPrecision, test.scale)
			if err != nil {
				t.Fatal(err)
			}
			if err := value.Set(0, test.first); err != nil {
				t.Fatal(err)
			}
			if err := value.Set(31, test.last); err != nil {
				t.Fatal(err)
			}
			col := ColumnMeta{
				spinerType:       test.spinerType,
				precision:        32,
				elementPrecision: test.elementPrecision,
				scale:            test.scale,
			}
			payload, err := encodeArrayPayload(value, col, true)
			if err != nil {
				t.Fatal(err)
			}
			if binary.BigEndian.Uint16(payload[:2]) != 0 ||
				payload[2] != sparseArrayFormatVersion || payload[3] != sparseArrayFlag ||
				binary.BigEndian.Uint16(payload[4:6]) != 32 ||
				binary.BigEndian.Uint16(payload[6:8]) != 2 {
				t.Fatalf("invalid sparse header: %x", payload[:8])
			}
			elementSize := computeColumnLength(arrayBaseSpinerType(test.spinerType), test.elementPrecision)
			if binary.BigEndian.Uint16(payload[8:10]) != 1 ||
				binary.BigEndian.Uint16(payload[10+elementSize:12+elementSize]) != 32 {
				t.Fatalf("invalid sparse positions: %x", payload)
			}
		})
	}
}

func TestSignedArrayRejectsUnsignedOverflow(t *testing.T) {
	tests := []struct {
		name       string
		spinerType int
		max        uint64
	}{
		{"int16", cmdInt16Type, math.MaxInt16},
		{"int32", cmdInt32Type, math.MaxInt32},
		{"int64", cmdInt64Type, math.MaxInt64},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := encodeArrayElement(test.spinerType, test.max, 0, 0); err != nil {
				t.Fatalf("valid unsigned boundary rejected: %v", err)
			}
			if _, err := encodeArrayElement(test.spinerType, uint64(math.MaxUint64), 0, 0); err == nil {
				t.Fatal("wrapping uint64 value accepted")
			}
		})
	}
	if strconv.IntSize == 64 {
		overflow := uint(uint64(math.MaxInt64) + 1)
		if _, err := toInt64(overflow); err == nil {
			t.Fatal("wrapping uint value accepted")
		}
	}
}

func TestDecimalArrayBindFallbackPreservesValueScale(t *testing.T) {
	decimalValue, err := api.ParseDecimal("8.1250", 12, 4)
	if err != nil {
		t.Fatal(err)
	}
	value, err := api.NewSparseArrayWithMeta(api.SqlTypeDecimal, 1, 12, 4)
	if err != nil {
		t.Fatal(err)
	}
	if err := value.Set(0, decimalValue); err != nil {
		t.Fatal(err)
	}
	typ, payload, err := encodeBoundParam(BoundParam{
		sqlType: api.SqlTypeDecimalArray,
		value:   value,
	})
	if err != nil {
		t.Fatal(err)
	}
	if typ != cmdDecimalArrayType {
		t.Fatalf("bind type = %d, want %d", typ, cmdDecimalArrayType)
	}
	decoded, err := decodeArrayPayload(ColumnMeta{
		spinerType:       cmdDecimalArrayType,
		precision:        1,
		elementPrecision: 12,
		scale:            4,
	}, payload)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.String(); got != "[8.1250]" {
		t.Fatalf("fallback DECIMAL ARRAY = %s, want [8.1250]", got)
	}
}

func TestArrayEncodeRejectsInvalidMetadataWithoutPanic(t *testing.T) {
	value, err := api.NewArray(api.SqlTypeDecimal,
		[]any{"1.0", nil, "3.0"})
	if err != nil {
		t.Fatal(err)
	}
	col := ColumnMeta{
		spinerType: cmdDecimalArrayType,
		precision:  3,
		scale:      0,
	}
	if _, err := encodeArrayPayload(value, col, true); err == nil {
		t.Fatal("invalid DECIMAL ARRAY element precision accepted")
	}
}

func TestProjectedNumericElementSentinelParity(t *testing.T) {
	tests := []struct {
		name       string
		spinerType int
		value      any
	}{
		{"int16", cmdInt16Type, int16(-32768)},
		{"uint16", cmdUInt16Type, uint16(0xffff)},
		{"int32", cmdInt32Type, int32(-2147483648)},
		{"uint32", cmdUInt32Type, uint32(0xffffffff)},
		{"int64", cmdInt64Type, int64(-9223372036854775808)},
		{"uint64", cmdUInt64Type, uint64(0xffffffffffffffff)},
		{"float", cmdFlt32Type, floatNull},
		{"double", cmdFlt64Type, doubleNull},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			col := ColumnMeta{name: "A[0]", spinerType: test.spinerType}
			if _, err := encodeAppendColumnValue(col, test.value, 0); err == nil {
				t.Fatal("reserved scalar NULL sentinel accepted")
			}
		})
	}
	col := ColumnMeta{name: "A[0]", spinerType: cmdUInt64Type}
	if _, err := encodeAppendColumnValue(
		col, uint64(0xfffffffffffffffe), 0); err != nil {
		t.Fatalf("valid UINT64 ARRAY element rejected: %v", err)
	}
}

func TestScalarAppendSentinelCompatibility(t *testing.T) {
	tests := []struct {
		name       string
		spinerType int
		value      any
	}{
		{"int16", cmdInt16Type, int16(-32768)},
		{"uint16", cmdUInt16Type, uint16(0xffff)},
		{"int32", cmdInt32Type, int32(-2147483648)},
		{"uint32", cmdUInt32Type, uint32(0xffffffff)},
		{"int64", cmdInt64Type, int64(-9223372036854775808)},
		{"float", cmdFlt32Type, floatNull},
		{"double", cmdFlt64Type, doubleNull},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			col := ColumnMeta{name: "SCALAR_VALUE", spinerType: test.spinerType}
			if _, err := encodeAppendColumnValue(col, test.value, 0); err != nil {
				t.Fatalf("legacy scalar sentinel behavior changed: %v", err)
			}
		})
	}
}
