package api

import (
	"database/sql/driver"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
)

const (
	ArrayMinCardinality = 1
	ArrayMaxCardinality = 1024
)

// Array is a fixed-cardinality numeric ARRAY. Public positions are 1-based.
// A nil *Array represents a whole-ARRAY NULL; nil elements represent element NULLs.
type Array struct {
	elementType SqlType
	cardinality int
	precision   int
	scale       int
	entries     map[int]any
}

func NewSparseArray(elementType SqlType, cardinality int) (*Array, error) {
	return NewSparseArrayWithMeta(elementType, cardinality, 0, 0)
}

func NewSparseArrayWithMeta(elementType SqlType, cardinality, precision, scale int) (*Array, error) {
	elementType = elementType.ElementType()
	if !isNumericArrayElementType(elementType) {
		return nil, fmt.Errorf("unsupported ARRAY element type %s", elementType)
	}
	if cardinality < ArrayMinCardinality || cardinality > ArrayMaxCardinality {
		return nil, fmt.Errorf("ARRAY cardinality out of range: %d", cardinality)
	}
	if elementType == SqlTypeDecimal {
		if precision == 0 {
			precision, scale = DecimalMaxPrecision, DecimalMaxScale
		}
		if err := validateDecimalPrecisionScale(precision, scale); err != nil {
			return nil, err
		}
	} else if precision != 0 || scale != 0 {
		return nil, fmt.Errorf("precision/scale are valid only for DECIMAL ARRAY")
	}
	return &Array{
		elementType: elementType,
		cardinality: cardinality,
		precision:   precision,
		scale:       scale,
		entries:     make(map[int]any),
	}, nil
}

func NewArray(elementType SqlType, values []any) (*Array, error) {
	ret, err := NewSparseArray(elementType, len(values))
	if err != nil {
		return nil, err
	}
	for i, value := range values {
		if err := ret.Set(i+1, value); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

func (a *Array) ElementType() SqlType {
	if a == nil {
		return 0
	}
	return a.elementType
}
func (a *Array) SqlType() SqlType {
	if a == nil {
		return 0
	}
	return a.elementType.ArrayType()
}
func (a *Array) Cardinality() int {
	if a == nil {
		return 0
	}
	return a.cardinality
}
func (a *Array) Precision() int {
	if a == nil {
		return 0
	}
	return a.precision
}
func (a *Array) Scale() int {
	if a == nil {
		return 0
	}
	return a.scale
}

func (a *Array) Clear() {
	if a == nil {
		return
	}
	clear(a.entries)
}

func (a *Array) Set(position int, value any) error {
	if a == nil {
		return fmt.Errorf("cannot set nil ARRAY")
	}
	if position < 1 || position > a.cardinality {
		return fmt.Errorf("ARRAY position out of range: %d", position)
	}
	if value == nil || isNilArrayElement(value) {
		delete(a.entries, position)
		return nil
	}
	a.entries[position] = value
	return nil
}

func isNilArrayElement(value any) bool {
	ref := reflect.ValueOf(value)
	return ref.IsValid() &&
		(ref.Kind() == reflect.Pointer || ref.Kind() == reflect.Interface) &&
		ref.IsNil()
}

func (a *Array) Get(position int) (any, error) {
	if a == nil {
		return nil, fmt.Errorf("cannot get nil ARRAY")
	}
	if position < 1 || position > a.cardinality {
		return nil, fmt.Errorf("ARRAY position out of range: %d", position)
	}
	return a.entries[position], nil
}

func (a *Array) Values() []any {
	if a == nil {
		return nil
	}
	ret := make([]any, a.cardinality)
	for position, value := range a.entries {
		ret[position-1] = value
	}
	return ret
}

func (a *Array) Entries() map[int]any {
	if a == nil {
		return nil
	}
	ret := make(map[int]any, len(a.entries))
	for position, value := range a.entries {
		ret[position] = value
	}
	return ret
}

func (a *Array) String() string {
	if a == nil {
		return "NULL"
	}
	parts := make([]string, a.cardinality)
	for i := 1; i <= a.cardinality; i++ {
		value, ok := a.entries[i]
		if !ok || value == nil {
			parts[i-1] = "null"
			continue
		}
		switch v := value.(type) {
		case Decimal:
			parts[i-1] = v.String()
		case *Decimal:
			if v == nil {
				parts[i-1] = "null"
			} else {
				parts[i-1] = v.String()
			}
		default:
			parts[i-1] = fmt.Sprint(value)
		}
	}
	return "[" + strings.Join(parts, ",") + "]"
}

func (a Array) Value() (driver.Value, error) { return a.String(), nil }

// Scan accepts another Array or a canonical JSON-compatible ARRAY string.
// A preconfigured receiver preserves its element type; a zero receiver infers
// INT64 or DOUBLE from the JSON number spelling.
func (a *Array) Scan(src any) error {
	if a == nil {
		return fmt.Errorf("cannot scan ARRAY into nil receiver")
	}
	if src == nil {
		*a = Array{}
		return nil
	}
	if value, ok := src.(Array); ok {
		*a = *value.Clone()
		return nil
	}
	if value, ok := src.(*Array); ok {
		if value == nil {
			*a = Array{}
		} else {
			*a = *value.Clone()
		}
		return nil
	}
	var text string
	switch value := src.(type) {
	case string:
		text = value
	case []byte:
		text = string(value)
	default:
		return fmt.Errorf("cannot scan %T as ARRAY", src)
	}
	tokens, err := parseCanonicalArrayTokens(text)
	if err != nil {
		return err
	}
	elementType := a.elementType
	if elementType == 0 {
		elementType, a.precision, a.scale, err = inferArrayElementType(tokens)
		if err != nil {
			return err
		}
	}
	ret, err := NewSparseArrayWithMeta(elementType, len(tokens), a.precision, a.scale)
	if err != nil {
		return err
	}
	for i, token := range tokens {
		if strings.EqualFold(token, "null") {
			continue
		}
		value, err := parseArrayToken(token, ret)
		if err != nil {
			return err
		}
		ret.entries[i+1] = value
	}
	*a = *ret
	return nil
}

func parseCanonicalArrayTokens(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return nil, fmt.Errorf("invalid ARRAY text %q", text)
	}
	body := strings.TrimSpace(text[1 : len(text)-1])
	if body == "" {
		return nil, fmt.Errorf("ARRAY cardinality must be at least 1")
	}
	parts := strings.Split(body, ",")
	if len(parts) > ArrayMaxCardinality {
		return nil, fmt.Errorf("ARRAY cardinality out of range: %d", len(parts))
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if parts[i] == "" {
			return nil, fmt.Errorf("empty ARRAY element at position %d", i+1)
		}
	}
	return parts, nil
}

func inferArrayElementType(tokens []string) (SqlType, int, int, error) {
	hasNegative := false
	hasUnsigned := false
	hasDecimal := false
	maxScale := 0
	maxIntegerDigits := 1
	for _, token := range tokens {
		if strings.EqualFold(token, "null") {
			continue
		}
		lower := strings.ToLower(token)
		if lower == "nan" || lower == "+inf" || lower == "-inf" ||
			lower == "inf" || lower == "+infinity" || lower == "-infinity" ||
			lower == "infinity" || strings.ContainsAny(token, "eE") {
			return SqlTypeDouble, 0, 0, nil
		}
		unsigned := strings.TrimPrefix(token, "+")
		negative := strings.HasPrefix(unsigned, "-")
		if negative {
			hasNegative = true
			unsigned = unsigned[1:]
		}
		if dot := strings.IndexByte(unsigned, '.'); dot >= 0 {
			hasDecimal = true
			integerDigits := len(strings.TrimLeft(unsigned[:dot], "0"))
			if integerDigits == 0 {
				integerDigits = 1
			}
			if integerDigits > maxIntegerDigits {
				maxIntegerDigits = integerDigits
			}
			if scale := len(unsigned) - dot - 1; scale > maxScale {
				maxScale = scale
			}
			continue
		}
		value, err := strconv.ParseUint(unsigned, 10, 64)
		if err != nil {
			return 0, 0, 0, fmt.Errorf("invalid ARRAY integer %q: %w", token, err)
		}
		if !negative && value > math.MaxInt64 {
			hasUnsigned = true
		}
		if digits := len(strings.TrimLeft(unsigned, "0")); digits > maxIntegerDigits {
			maxIntegerDigits = digits
		}
	}
	if hasDecimal {
		precision := maxIntegerDigits + maxScale
		if precision > DecimalMaxPrecision || maxScale > DecimalMaxScale {
			return 0, 0, 0, fmt.Errorf("ARRAY DECIMAL text exceeds supported precision/scale")
		}
		return SqlTypeDecimal, precision, maxScale, nil
	}
	if hasNegative && hasUnsigned {
		return 0, 0, 0, fmt.Errorf("ARRAY integer text mixes negative and UINT64-only values")
	}
	if hasUnsigned {
		return SqlTypeUInt64, 0, 0, nil
	}
	return SqlTypeInt64, 0, 0, nil
}

func parseArrayToken(token string, array *Array) (any, error) {
	switch array.elementType {
	case SqlTypeDecimal:
		return ParseDecimal(token, array.precision, array.scale)
	case SqlTypeFloat, SqlTypeDouble:
		switch strings.ToLower(token) {
		case "nan":
			return math.NaN(), nil
		case "inf", "+inf", "infinity", "+infinity":
			return math.Inf(1), nil
		case "-inf", "-infinity":
			return math.Inf(-1), nil
		default:
			return strconv.ParseFloat(token, 64)
		}
	case SqlTypeUInt16, SqlTypeUInt32, SqlTypeUInt64:
		return strconv.ParseUint(token, 10, 64)
	default:
		return strconv.ParseInt(token, 10, 64)
	}
}

func (a *Array) Clone() *Array {
	if a == nil {
		return nil
	}
	ret := &Array{elementType: a.elementType, cardinality: a.cardinality, precision: a.precision, scale: a.scale, entries: make(map[int]any, len(a.entries))}
	for position, value := range a.entries {
		ret.entries[position] = value
	}
	return ret
}

func isNumericArrayElementType(typ SqlType) bool {
	switch typ {
	case SqlTypeInt16, SqlTypeUInt16, SqlTypeInt32, SqlTypeUInt32,
		SqlTypeInt64, SqlTypeUInt64, SqlTypeFloat, SqlTypeDouble, SqlTypeDecimal:
		return true
	default:
		return false
	}
}
