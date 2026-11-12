package machnet

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/machbase/neo-client/v2/api"
)

// VECTOR elements are always little-endian FLOAT32, independently of the
// server endian used for APPEND's outer length field.
func encodeVectorPayload(value any, dimension int) ([]byte, error) {
	vector, err := vectorFromValue(value)
	if err != nil {
		return nil, err
	}
	if dimension > 0 && dimension != api.VectorMaxDimension && len(vector) != dimension {
		return nil, fmt.Errorf("VECTOR dimension %d does not match target %d", len(vector), dimension)
	}
	ret := make([]byte, len(vector)*4)
	for i, item := range vector {
		binary.LittleEndian.PutUint32(ret[i*4:i*4+4], math.Float32bits(item))
	}
	return ret, nil
}

func decodeVectorPayload(col ColumnMeta, payload []byte) (api.Vector, error) {
	if col.precision < 1 || col.precision > api.VectorMaxDimension ||
		len(payload) == 0 || len(payload)%4 != 0 || len(payload) > col.precision*4 {
		return nil, fmt.Errorf("invalid VECTOR payload length")
	}
	ret := make(api.Vector, len(payload)/4)
	for i := range ret {
		ret[i] = math.Float32frombits(binary.LittleEndian.Uint32(payload[i*4 : i*4+4]))
	}
	if err := ret.Validate(); err != nil {
		return nil, err
	}
	return ret, nil
}

func vectorFromValue(value any) (api.Vector, error) {
	switch v := value.(type) {
	case api.Vector:
		if err := v.Validate(); err != nil {
			return nil, err
		}
		return v, nil
	case *api.Vector:
		if v == nil {
			return nil, fmt.Errorf("nil VECTOR value")
		}
		return vectorFromValue(*v)
	case []float32:
		return vectorFromValue(api.Vector(v))
	case []float64:
		if len(v) < 1 || len(v) > api.VectorMaxDimension {
			return nil, fmt.Errorf("VECTOR dimension out of range: %d", len(v))
		}
		ret := make(api.Vector, len(v))
		for i, item := range v {
			if math.IsNaN(item) || math.IsInf(item, 0) || math.IsInf(float64(float32(item)), 0) {
				return nil, fmt.Errorf("VECTOR element %d is not finite FLOAT32", i)
			}
			ret[i] = float32(item)
		}
		return ret, nil
	case string:
		return api.ParseVector(v)
	case []byte:
		return api.ParseVector(string(v))
	default:
		return nil, fmt.Errorf("unsupported VECTOR value %T", value)
	}
}
