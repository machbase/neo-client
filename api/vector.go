package api

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
)

const VectorMaxDimension = 65536

// Vector is a FLOAT32 embedding. A nil Vector represents SQL NULL.
type Vector []float32

func NewVector(values ...float32) (Vector, error) {
	ret := append(Vector(nil), values...)
	if err := ret.Validate(); err != nil {
		return nil, err
	}
	return ret, nil
}

func ParseVector(text string) (Vector, error) {
	var values []json.RawMessage
	if err := json.Unmarshal([]byte(text), &values); err != nil {
		return nil, fmt.Errorf("invalid VECTOR JSON: %w", err)
	}
	if len(values) < 1 || len(values) > VectorMaxDimension {
		return nil, fmt.Errorf("VECTOR dimension out of range: %d", len(values))
	}
	ret := make(Vector, len(values))
	for i, raw := range values {
		if string(raw) == "null" {
			return nil, fmt.Errorf("VECTOR element %d is NULL", i)
		}
		var value float64
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, fmt.Errorf("VECTOR element %d is not numeric: %w", i, err)
		}
		if math.IsNaN(value) || math.IsInf(value, 0) || math.IsInf(float64(float32(value)), 0) {
			return nil, fmt.Errorf("VECTOR element %d is not finite FLOAT32", i)
		}
		ret[i] = float32(value)
	}
	return ret, nil
}

func (v Vector) Validate() error {
	if len(v) < 1 || len(v) > VectorMaxDimension {
		return fmt.Errorf("VECTOR dimension out of range: %d", len(v))
	}
	for i, value := range v {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("VECTOR element %d is not finite", i)
		}
	}
	return nil
}

func (v Vector) Value() (driver.Value, error) {
	if v == nil {
		return nil, nil
	}
	if err := v.Validate(); err != nil {
		return nil, err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(data), nil
}

func (v *Vector) Scan(src any) error {
	if v == nil {
		return fmt.Errorf("cannot scan VECTOR into nil receiver")
	}
	if src == nil {
		*v = nil
		return nil
	}
	var result Vector
	switch value := src.(type) {
	case Vector:
		result = append(Vector(nil), value...)
	case *Vector:
		if value == nil {
			*v = nil
			return nil
		}
		result = append(Vector(nil), (*value)...)
	case []float32:
		result = append(Vector(nil), value...)
	case string:
		var err error
		result, err = ParseVector(value)
		if err != nil {
			return err
		}
	case []byte:
		var err error
		result, err = ParseVector(string(value))
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("cannot scan %T as VECTOR", src)
	}
	if err := result.Validate(); err != nil {
		return err
	}
	*v = result
	return nil
}
