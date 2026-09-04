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

// Array is a fixed-cardinality numeric ARRAY. Public positions are 0-based.
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

func NewArray(elementType SqlType, values ...any) (*Array, error) {
	ret, err := NewSparseArray(elementType, len(values))
	if err != nil {
		return nil, err
	}
	for i, value := range values {
		if err := ret.Set(i, value); err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// ParseArray parses the sparse-aware ARRAY literal convenience syntax into an
// *Array. This is distinct from the canonical DB string format produced by
// Array.String/Scan: it additionally supports "idx=>value" entries to set an
// arbitrary position. Matching machbase's own literal syntax, a single
// literal must be either all-dense or all-sparse; mixing plain values and
// "idx=>value" entries in the same literal is rejected.
//
//	[1,2,3,4]                    -> dense: positions 0,1,2,3
//	[1=>1.0, 2=>2.1, 11=>3.14]   -> sparse: only positions 1, 2 and 11 are set
//
// Dense values are assigned to sequential positions starting at 0. "null"
// (case-insensitive) leaves the referenced position as NULL.
//
// cardinality <= 0 means "infer from the highest referenced position + 1".
func ParseArray(text string, elementType SqlType, cardinality, precision, scale int) (*Array, error) {
	tokens, err := splitArrayLiteralTokens(text)
	if err != nil {
		return nil, err
	}
	type pendingElement struct {
		position int
		value    string
		isNull   bool
	}
	pending := make([]pendingElement, 0, len(tokens))
	cursor := 0
	maxPosition := -1
	hasDense := false
	hasSparse := false
	for _, token := range tokens {
		position := cursor
		value := token
		if idx := strings.Index(token, "=>"); idx >= 0 {
			idxText := strings.TrimSpace(token[:idx])
			value = strings.TrimSpace(token[idx+2:])
			pos, err := strconv.Atoi(idxText)
			if err != nil || pos < 0 {
				return nil, fmt.Errorf("invalid ARRAY literal position %q", idxText)
			}
			position = pos
			hasSparse = true
		} else {
			hasDense = true
		}
		cursor = position + 1
		if position > maxPosition {
			maxPosition = position
		}
		pending = append(pending, pendingElement{
			position: position,
			value:    value,
			isNull:   strings.EqualFold(value, "null"),
		})
	}
	if hasDense && hasSparse {
		return nil, fmt.Errorf("ARRAY literal cannot mix dense and idx=>value (sparse) elements")
	}
	if cardinality <= 0 {
		cardinality = maxPosition + 1
	}
	ret, err := NewSparseArrayWithMeta(elementType, cardinality, precision, scale)
	if err != nil {
		return nil, err
	}
	for _, p := range pending {
		if p.position >= cardinality {
			return nil, fmt.Errorf("ARRAY literal position %d out of range for cardinality %d", p.position, cardinality)
		}
		if p.isNull {
			continue
		}
		if err := ret.Set(p.position, p.value); err != nil {
			return nil, fmt.Errorf("ARRAY literal position %d: %w", p.position, err)
		}
	}
	return ret, nil
}

// splitArrayLiteralTokens splits the "[...]" body of an ARRAY literal into
// its top-level comma-separated tokens, without interpreting "=>" or "null".
func splitArrayLiteralTokens(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if len(text) < 2 || text[0] != '[' || text[len(text)-1] != ']' {
		return nil, fmt.Errorf("invalid ARRAY literal %q", text)
	}
	body := strings.TrimSpace(text[1 : len(text)-1])
	if body == "" {
		return nil, fmt.Errorf("ARRAY literal must have at least one element")
	}
	parts := strings.Split(body, ",")
	if len(parts) > ArrayMaxCardinality {
		return nil, fmt.Errorf("ARRAY literal has too many elements: %d", len(parts))
	}
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
		if parts[i] == "" {
			return nil, fmt.Errorf("empty ARRAY literal element at position %d", i)
		}
	}
	return parts, nil
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
	if position < 0 || position >= a.cardinality {
		return fmt.Errorf("ARRAY position out of range: %d", position)
	}
	if value == nil || isNilArrayElement(value) {
		delete(a.entries, position)
		return nil
	}
	normalized, err := normalizeArrayElement(a.elementType, value, a.precision, a.scale)
	if err != nil {
		return err
	}
	a.entries[position] = normalized
	return nil
}

func isNilArrayElement(value any) bool {
	ref := reflect.ValueOf(value)
	return ref.IsValid() &&
		(ref.Kind() == reflect.Pointer || ref.Kind() == reflect.Interface) &&
		ref.IsNil()
}

// normalizeArrayElement converts value to the canonical Go type of elementType,
// accepting any compatible numeric (or numeric-string) primitive.
func normalizeArrayElement(elementType SqlType, value any, precision, scale int) (any, error) {
	switch elementType {
	case SqlTypeInt16:
		v, ok := arrayElementToInt64(value)
		if !ok || v < math.MinInt16 || v > math.MaxInt16 {
			return nil, fmt.Errorf("invalid INT16 ARRAY element %v", value)
		}
		return int16(v), nil
	case SqlTypeUInt16:
		v, ok := arrayElementToUint64(value)
		if !ok || v > math.MaxUint16 {
			return nil, fmt.Errorf("invalid UINT16 ARRAY element %v", value)
		}
		return uint16(v), nil
	case SqlTypeInt32:
		v, ok := arrayElementToInt64(value)
		if !ok || v < math.MinInt32 || v > math.MaxInt32 {
			return nil, fmt.Errorf("invalid INT32 ARRAY element %v", value)
		}
		return int32(v), nil
	case SqlTypeUInt32:
		v, ok := arrayElementToUint64(value)
		if !ok || v > math.MaxUint32 {
			return nil, fmt.Errorf("invalid UINT32 ARRAY element %v", value)
		}
		return uint32(v), nil
	case SqlTypeInt64:
		v, ok := arrayElementToInt64(value)
		if !ok {
			return nil, fmt.Errorf("invalid INT64 ARRAY element %v", value)
		}
		return v, nil
	case SqlTypeUInt64:
		v, ok := arrayElementToUint64(value)
		if !ok {
			return nil, fmt.Errorf("invalid UINT64 ARRAY element %v", value)
		}
		return v, nil
	case SqlTypeFloat:
		v, ok := arrayElementToFloat64(value)
		if !ok {
			return nil, fmt.Errorf("invalid FLOAT ARRAY element %v", value)
		}
		return float32(v), nil
	case SqlTypeDouble:
		v, ok := arrayElementToFloat64(value)
		if !ok {
			return nil, fmt.Errorf("invalid DOUBLE ARRAY element %v", value)
		}
		return v, nil
	case SqlTypeDecimal:
		switch v := value.(type) {
		case Decimal:
			return v, nil
		case *Decimal:
			return ParseDecimal(v.String(), precision, scale)
		case string:
			return ParseDecimal(v, precision, scale)
		case []byte:
			return ParseDecimal(string(v), precision, scale)
		default:
			return ParseDecimal(fmt.Sprint(v), precision, scale)
		}
	default:
		return nil, fmt.Errorf("unsupported ARRAY element type %s", elementType)
	}
}

// arrayElementToInt64 accepts any signed/unsigned integer, float or numeric
// string kind and converts it to int64.
func arrayElementToInt64(value any) (int64, bool) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int(), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		u := rv.Uint()
		if u > math.MaxInt64 {
			return 0, false
		}
		return int64(u), true
	case reflect.Float32, reflect.Float64:
		return int64(rv.Float()), true
	case reflect.String:
		n, err := strconv.ParseInt(rv.String(), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// arrayElementToUint64 accepts any signed/unsigned integer, float or numeric
// string kind and converts it to uint64.
func arrayElementToUint64(value any) (uint64, bool) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return rv.Uint(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := rv.Int()
		if n < 0 {
			return 0, false
		}
		return uint64(n), true
	case reflect.Float32, reflect.Float64:
		f := rv.Float()
		if f < 0 {
			return 0, false
		}
		return uint64(f), true
	case reflect.String:
		n, err := strconv.ParseUint(rv.String(), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// arrayElementToFloat64 accepts any signed/unsigned integer, float or numeric
// string kind and converts it to float64.
func arrayElementToFloat64(value any) (float64, bool) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Float32, reflect.Float64:
		return rv.Float(), true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(rv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return float64(rv.Uint()), true
	case reflect.String:
		f, err := strconv.ParseFloat(rv.String(), 64)
		if err != nil {
			return 0, false
		}
		return f, true
	default:
		return 0, false
	}
}

func (a *Array) Get(position int) (any, error) {
	if a == nil {
		return nil, fmt.Errorf("cannot get nil ARRAY")
	}
	if position < 0 || position >= a.cardinality {
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
		ret[position] = value
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
	for i := 0; i < a.cardinality; i++ {
		value, ok := a.entries[i]
		if !ok || value == nil {
			parts[i] = "null"
			continue
		}
		switch v := value.(type) {
		case Decimal:
			parts[i] = v.String()
		case *Decimal:
			if v == nil {
				parts[i] = "null"
			} else {
				parts[i] = v.String()
			}
		default:
			parts[i] = fmt.Sprint(value)
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
		ret.entries[i] = value
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
			return nil, fmt.Errorf("empty ARRAY element at position %d", i)
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
