package machnet

import (
	"encoding/binary"
	"math"
	"reflect"
	"testing"

	"github.com/machbase/neo-client/v2/api"
)

func TestVectorMetadataAndWire(t *testing.T) {
	cmType := (uint64(cmdVectorType) << 56) | (uint64(2) << 28)
	w := newMarshalWriter(cmiAppendOpenProtocol, 1, 0)
	w.addString(cmiPColNameID, "V")
	w.addUInt64(cmiPColTypeID, cmType)
	packets := w.finalize()
	units, err := collectUnits(packets[0][packetHeaderSize:])
	if err != nil {
		t.Fatal(err)
	}
	columns := buildColumns(units, true)
	if len(columns) != 1 || columns[0].sqlType != api.SqlTypeVector || columns[0].precision != 2 ||
		columns[0].length != 8 || !columns[0].isVariable {
		t.Fatalf("invalid VECTOR metadata: %+v", columns)
	}

	vector := api.Vector{1.5, -2}
	typ, payload, err := encodeBoundParam(BoundParam{sqlType: api.SqlTypeVector, value: vector, cardinality: 2})
	if err != nil || typ != cmdVectorType || !reflect.DeepEqual(payload, []byte{0, 0, 192, 63, 0, 0, 0, 192}) {
		t.Fatalf("VECTOR bind type=%d payload=%x err=%v", typ, payload, err)
	}
	decoded, err := decodeVectorPayload(columns[0], payload)
	if err != nil || !reflect.DeepEqual(decoded, vector) {
		t.Fatalf("VECTOR decode = %v, %v", decoded, err)
	}
	payload[0] = 9
	if decoded[0] != 1.5 {
		t.Fatal("decoded VECTOR aliases wire buffer")
	}
	if typ, data, err := encodeBoundParam(BoundParam{sqlType: api.SqlTypeVector, isNull: true}); err != nil || typ != cmdVectorType || len(data) != 0 {
		t.Fatalf("VECTOR NULL bind type=%d payload=%x err=%v", typ, data, err)
	}
}

func TestVectorRejectsMalformedWire(t *testing.T) {
	col := ColumnMeta{spinerType: cmdVectorType, precision: 2}
	for _, payload := range [][]byte{{}, {0, 0, 0}, make([]byte, 12)} {
		if _, err := decodeVectorPayload(col, payload); err == nil {
			t.Fatalf("malformed VECTOR payload %x accepted", payload)
		}
	}
	bad := make([]byte, 8)
	binary.LittleEndian.PutUint32(bad[:4], math.Float32bits(float32(math.NaN())))
	if _, err := decodeVectorPayload(col, bad); err == nil {
		t.Fatal("NaN VECTOR payload accepted")
	}
	if _, err := encodeVectorPayload(api.Vector{1}, 2); err == nil {
		t.Fatal("wrong VECTOR dimension accepted")
	}
	if _, err := encodeVectorPayload([]float64{1e100, 2}, 2); err == nil {
		t.Fatal("FLOAT32 overflow accepted")
	}
	dynamic := ColumnMeta{spinerType: cmdVectorType, precision: api.VectorMaxDimension}
	if got, err := decodeVectorPayload(dynamic, make([]byte, 8)); err != nil || len(got) != 2 {
		t.Fatalf("dynamic VECTOR payload = %v, %v", got, err)
	}
}

func TestVectorAppendEndianAndNull(t *testing.T) {
	col := ColumnMeta{name: "V", spinerType: cmdVectorType, precision: 2, isVariable: true}
	for _, endian := range []uint32{0, 1} {
		field, err := encodeAppendColumnValue(col, api.Vector{1.5, -2}, endian)
		if err != nil || len(field) != 12 || !reflect.DeepEqual(field[4:], []byte{0, 0, 192, 63, 0, 0, 0, 192}) {
			t.Fatalf("append endian=%d field=%x err=%v", endian, field, err)
		}
		length := binary.LittleEndian.Uint32(field[:4])
		if endian != 0 {
			length = binary.BigEndian.Uint32(field[:4])
		}
		if length != 8 {
			t.Fatalf("append endian=%d length=%d", endian, length)
		}
	}
	row, err := encodeAppendRow([]ColumnMeta{col}, AppendBindings{byColumn: []int{0}, arrivalArg: -1}, []any{api.Vector(nil)}, 0)
	if err != nil || len(row) != 6 || row[5]&0x80 == 0 {
		t.Fatalf("VECTOR NULL append row=%x err=%v", row, err)
	}
}
