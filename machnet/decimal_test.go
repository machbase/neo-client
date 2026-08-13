package machnet

import (
	"encoding/binary"
	"math/big"
	"testing"

	"github.com/machbase/neo-client/v2/api"
)

func TestDecimalCodec(t *testing.T) {
	for _, tc := range []struct {
		text      string
		precision int
		scale     int
	}{
		{"0.00", 10, 2},
		{"12345678.90", 10, 2},
		{"-99999999.99", 10, 2},
		{"99999999999999999999999999999999999", 65, 30},
	} {
		encoded, err := encodeDecimal(tc.text, tc.precision, tc.scale)
		if err != nil {
			t.Fatalf("encodeDecimal(%q) error = %v", tc.text, err)
		}
		decoded, err := decodeDecimal(encoded, tc.precision, tc.scale)
		if err != nil {
			t.Fatalf("decodeDecimal(%q) error = %v", tc.text, err)
		}
		want, _ := api.ParseDecimal(tc.text, tc.precision, tc.scale)
		if got := decoded.(api.Decimal).String(); got != want.String() {
			t.Fatalf("decimal roundtrip = %q, want %q", got, want.String())
		}
	}
	null, err := encodeDecimal(nil, 65, 30)
	if err != nil || len(null) != 28 {
		t.Fatalf("decimal NULL = %x, %v", null, err)
	}
	decoded, err := decodeDecimal(null, 65, 30)
	if err != nil || decoded != nil {
		t.Fatalf("decoded NULL = %#v, %v", decoded, err)
	}
}

func TestDecimalCodecAllSupportedPrecisions(t *testing.T) {
	for precision := 1; precision <= api.DecimalMaxPrecision; precision++ {
		max := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(precision)), nil)
		max.Sub(max, big.NewInt(1))
		for _, scale := range []int{0, min(precision, api.DecimalMaxScale)} {
			for _, unscaled := range []*big.Int{
				big.NewInt(0),
				new(big.Int).Set(max),
				new(big.Int).Neg(new(big.Int).Set(max)),
			} {
				value, err := api.NewDecimal(unscaled, precision, scale)
				if err != nil {
					t.Fatalf("NewDecimal(p=%d,s=%d,value=%s): %v", precision, scale, unscaled, err)
				}
				encoded, err := encodeDecimal(value, precision, scale)
				if err != nil {
					t.Fatalf("encodeDecimal(p=%d,s=%d,value=%s): %v", precision, scale, unscaled, err)
				}
				if got, want := len(encoded), decimalSizes[precision]; got != want {
					t.Fatalf("encoded width(p=%d)=%d, want %d", precision, got, want)
				}
				decodedValue, err := decodeDecimal(encoded, precision, scale)
				if err != nil {
					t.Fatalf("decodeDecimal(p=%d,s=%d,value=%s): %v", precision, scale, unscaled, err)
				}
				decoded := decodedValue.(api.Decimal)
				if decoded.Unscaled().Cmp(unscaled) != 0 || decoded.Precision() != precision || decoded.Scale() != scale {
					t.Fatalf("decimal roundtrip(p=%d,s=%d)=(%s,%d,%d), want (%s,%d,%d)",
						precision, scale, decoded.Unscaled(), decoded.Precision(), decoded.Scale(),
						unscaled, precision, scale)
				}
			}
		}
	}
}

func TestTypeMetadataLayouts(t *testing.T) {
	legacy := uint64(cmdDecimalType)<<56 | uint64(30)<<28 | 12
	if got := extractScale(legacy, false); got != 12 {
		t.Fatalf("legacy scale = %d", got)
	}
	if got := extractNullability(legacy, false); got != api.NullabilityUnknown {
		t.Fatalf("legacy nullability = %d", got)
	}
	v403 := uint64(cmdDecimalType)<<56 | uint64(30)<<28 | uint64(12)<<23 | 0x3
	if got := extractScale(v403, true); got != 12 {
		t.Fatalf("v403 scale = %d", got)
	}
	if got := extractNullability(v403, true); got != api.NullabilityNullable || !extractPrimaryKey(v403, true) {
		t.Fatalf("v403 flags nullability=%d primary=%v", got, extractPrimaryKey(v403, true))
	}
}

func TestDescribeDecimalReturnsDeclaredPrecision(t *testing.T) {
	stmt := &StmtHandle{columns: []ColumnMeta{{
		spinerType: cmdDecimalType,
		precision:  30,
		scale:      12,
		length:     13,
		sqlType:    api.SqlTypeDecimal,
	}}}
	var size int
	var scale int
	if err := stmt.DescribeColEx(0, nil, nil, &size, &scale, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if size != 30 || scale != 12 {
		t.Fatalf("decimal metadata size=%d scale=%d", size, scale)
	}
}

func TestParameterMetadataV2(t *testing.T) {
	data := make([]byte, 4)
	binary.BigEndian.PutUint16(data[:2], 1)
	binary.BigEndian.PutUint16(data[2:4], 2)
	appendEntry := func(ordinal uint16, name string) {
		entry := make([]byte, 8)
		binary.BigEndian.PutUint16(entry[:2], ordinal)
		binary.BigEndian.PutUint16(entry[2:4], 1)
		binary.BigEndian.PutUint32(entry[4:8], uint32(len(name)))
		data = append(data, entry...)
		data = append(data, name...)
		if len(data)&1 != 0 {
			data = append(data, 0)
		}
	}
	appendEntry(1, "id")
	appendEntry(2, "id")
	desc := []ParamDesc{{Ordinal: 1}, {Ordinal: 2}}
	units := map[uint32][]MarshalUnit{cmiPParamMetaV2ID: {{data: data}}}
	if err := applyParamMetadataV2(desc, units); err != nil {
		t.Fatal(err)
	}
	if desc[0].Name != "id" || desc[1].Name != "id" {
		t.Fatalf("unexpected parameter metadata: %#v", desc)
	}
}

func TestEncodeParamsLegacyAndV2(t *testing.T) {
	params := []BoundParam{{sqlType: api.SqlTypeInt32, value: int32(7)}}
	legacy, err := encodeParams(params, false)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(legacy[:2]) != 1 || legacy[2] != 1 {
		t.Fatalf("invalid legacy parameter header: %x", legacy[:4])
	}
	v2, err := encodeParams(params, true)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(v2[:2]) != 2 || binary.BigEndian.Uint16(v2[2:4]) != 1 || binary.BigEndian.Uint16(v2[4:6]) != 1 {
		t.Fatalf("invalid v2 parameter header: %x", v2[:8])
	}
	many := make([]BoundParam, 256)
	for idx := range many {
		many[idx] = BoundParam{sqlType: api.SqlTypeInt32, value: int32(idx)}
	}
	if _, err := encodeParams(many, false); err == nil {
		t.Fatalf("legacy encoding should reject 256 parameters")
	}
	encoded, err := encodeParams(many, true)
	if err != nil {
		t.Fatalf("v2 encoding rejected 256 parameters: %v", err)
	}
	if len(encoded) == 0 {
		t.Fatalf("v2 encoding returned empty payload")
	}
}

func TestDecimalBindUsesMaxCarrier(t *testing.T) {
	decimal, err := api.ParseDecimal("123.45", 10, 2)
	if err != nil {
		t.Fatal(err)
	}
	typ, data, err := encodeBoundParam(BoundParam{sqlType: api.SqlTypeDecimal, value: decimal})
	if err != nil {
		t.Fatal(err)
	}
	if typ != cmdDecimalType || len(data) != 28 {
		t.Fatalf("decimal bind type=%d length=%d", typ, len(data))
	}
	decoded, err := decodeDecimal(data, api.DecimalMaxPrecision, api.DecimalMaxScale)
	if err != nil {
		t.Fatal(err)
	}
	if got := decoded.(api.Decimal).String(); got != "123.450000000000000000000000000000" {
		t.Fatalf("decimal bind value=%s", got)
	}
	_, nullData, err := encodeBoundParam(BoundParam{sqlType: api.SqlTypeDecimal, isNull: true})
	if err != nil || len(nullData) != 28 {
		t.Fatalf("decimal NULL bind length=%d err=%v", len(nullData), err)
	}
}

func TestDecimalBindUsesStringCarrierForWideInteger(t *testing.T) {
	const text = "99999999999999999999999999999999999999999999999999999999999999999"
	decimal, err := api.ParseDecimal(text, api.DecimalMaxPrecision, 0)
	if err != nil {
		t.Fatal(err)
	}
	typ, data, err := encodeBoundParam(BoundParam{sqlType: api.SqlTypeDecimal, value: decimal})
	if err != nil {
		t.Fatal(err)
	}
	if typ != cmdVarcharType || string(data) != text {
		t.Fatalf("wide decimal bind type=%d value=%q", typ, data)
	}
}
