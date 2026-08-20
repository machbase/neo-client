package client

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Sentinel errors returned by the struct scanning helpers.
var (
	ErrScanDestNotPointer     = errors.New("machbase: scan destination must be a non-nil pointer")
	ErrScanNoMatchedField     = errors.New("machbase: column has no matching struct field")
	ErrScanNoMatchedColumn    = errors.New("machbase: struct field has no matching column")
	ErrScanDuplicateColumn    = errors.New("machbase: duplicated column name")
	ErrScanNoMappedField      = errors.New("machbase: struct has no db-tagged field")
	ErrScanTooManyRows        = errors.New("machbase: result set exceeds the configured row limit")
	ErrNamedParamsUnsupported = errors.New("machbase: named parameters require server protocol 4.0.3 or later")
	ErrScanInvalidTagOption   = errors.New("machbase: invalid db tag option")
	ErrScanNullNotSupported   = errors.New("machbase: NULL DATETIME column requires a pointer field")
)

const (
	defaultTagKey         = "db"
	defaultFallbackTagKey = "json"
	// defaultMaxRows is a safe upper bound for the helpers that materialize a whole
	// result set. It mirrors defaultFetchRows but does not follow the DSN fetch_rows
	// value, which is unreachable from *sql.Rows.
	defaultMaxRows = int64(defaultFetchRows)
	// defaultDateTimeFormat is used for string fields matched to a DATETIME column
	// when neither the field's db tag nor WithDateTime specify a timeformat.
	defaultDateTimeFormat = "2006-01-02 15:04:05.999"
)

// ScanOption customizes how struct fields are mapped to columns.
type ScanOption func(*scanConfig)

type scanConfig struct {
	tagKey         string
	fallbackTagKey string
	nameMapper     func(string) string
	laxColumns     bool
	laxFields      bool
	maxRows        int64
	capacity       int
	// dateTimeFormat and dateTimeZone are the fallback timeformat/tz applied to
	// string, int64, and time.Time fields matched to a DATETIME column when the
	// field's own db tag does not specify one. Set via WithDateTime.
	dateTimeFormat string
	dateTimeZone   *time.Location
	// optErr carries a validation error from an option (e.g. an invalid tz name
	// passed to WithDateTime) until the next call that returns an error.
	optErr error
}

func newScanConfig(opts []ScanOption) *scanConfig {
	cfg := &scanConfig{
		tagKey:         defaultTagKey,
		fallbackTagKey: defaultFallbackTagKey,
		maxRows:        defaultMaxRows,
		dateTimeFormat: defaultDateTimeFormat,
		dateTimeZone:   time.Local,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(cfg)
		}
	}
	return cfg
}

// WithTagKey replaces the primary struct tag key. The default is "db".
func WithTagKey(key string) ScanOption {
	return func(cfg *scanConfig) { cfg.tagKey = key }
}

// WithFallbackTagKey replaces the fallback struct tag key used when the primary key
// is absent. The default is "json"; pass "" to disable the fallback.
func WithFallbackTagKey(key string) ScanOption {
	return func(cfg *scanConfig) { cfg.fallbackTagKey = key }
}

// WithNameMapper enables implicit mapping for untagged fields.
// When not set, untagged fields are treated as db:"-".
func WithNameMapper(fn func(fieldName string) string) ScanOption {
	return func(cfg *scanConfig) { cfg.nameMapper = fn }
}

// WithLaxColumns allows columns that have no matching struct field.
func WithLaxColumns() ScanOption {
	return func(cfg *scanConfig) { cfg.laxColumns = true }
}

// WithLaxFields allows struct fields that have no matching column.
func WithLaxFields() ScanOption {
	return func(cfg *scanConfig) { cfg.laxFields = true }
}

// WithMaxRows limits how many rows ScanRows and ScanAll may materialize.
// The default is 1000; pass 0 to disable the limit.
func WithMaxRows(n int64) ScanOption {
	return func(cfg *scanConfig) { cfg.maxRows = n }
}

// WithCapacity preallocates the result slice of ScanRows and ScanAll.
func WithCapacity(n int) ScanOption {
	return func(cfg *scanConfig) { cfg.capacity = n }
}

// WithDateTime sets the default timeformat/tz applied to string, int64, and
// time.Time fields matched to a DATETIME column when their own db tag does not
// specify a "timeformat" or "tz" option. timeformat follows the same rules as
// the field-level option (a Go time layout, or one of "ns"/"us"/"ms"/"s" for
// int64 fields); tz is an IANA name, "Local", or "UTC". An empty string leaves
// the corresponding default unchanged. The overall default (with no call to
// WithDateTime) is timeformat "2006-01-02 15:04:05.999" and tz "Local", mirroring
// the machbase-neo HTTP API's timeformat/tz query parameters.
func WithDateTime(timeformat, tz string) ScanOption {
	return func(cfg *scanConfig) {
		if timeformat != "" {
			cfg.dateTimeFormat = timeformat
		}
		if tz != "" {
			loc, err := parseTimeLocation(tz)
			if err != nil {
				cfg.optErr = fmt.Errorf("%w: %v", ErrScanInvalidTagOption, err)
				return
			}
			cfg.dateTimeZone = loc
		}
	}
}

// NameMapperIdentity maps a field name to itself.
func NameMapperIdentity() func(string) string {
	return func(name string) string { return name }
}

// NameMapperSnake maps a field name to its snake_case form.
func NameMapperSnake() func(string) string {
	return func(name string) string {
		var sb strings.Builder
		runes := []rune(name)
		for i, r := range runes {
			if unicode.IsUpper(r) {
				prevIsLower := i > 0 && !unicode.IsUpper(runes[i-1])
				nextIsLower := i+1 < len(runes) && !unicode.IsUpper(runes[i+1])
				if i > 0 && (prevIsLower || nextIsLower) {
					sb.WriteByte('_')
				}
				sb.WriteRune(unicode.ToLower(r))
				continue
			}
			sb.WriteRune(r)
		}
		return sb.String()
	}
}

func (cfg *scanConfig) initialCapacity() int {
	if cfg.capacity > 0 {
		return cfg.capacity
	}
	const capacityHint = 64
	if cfg.maxRows > 0 && cfg.maxRows < capacityHint {
		return int(cfg.maxRows)
	}
	return capacityHint
}

type fieldInfo struct {
	name  string
	index []int
	// time is non-nil for fields eligible for DATETIME conversion (string, int64,
	// time.Time, and their pointer forms), regardless of whether the db tag sets
	// "timeformat"/"tz". This flags the field for the dynamic scan path in
	// scanstruct.go, since the actual column type (DATETIME or not) is only known
	// at scan time.
	time *timeFieldOpts
}

// timeFieldOpts customizes how a DATETIME column is converted into a
// string, int64, or time.Time field via the db tag, e.g.:
//
//	Time  string    `db:"TIME,timeformat=2006-01-02 15:04:05,tz=Local"`
//	Epoch int64     `db:"TIME,timeformat=ms"`
//	At    time.Time `db:"TIME,tz=UTC"`
//
// A zero value means "use the scanConfig default (WithDateTime, or the
// built-in default)".
type timeFieldOpts struct {
	timeformat string
	tz         *time.Location
	// postProcessOnly is true for sql.NullTime/sql.Null[time.Time] fields. The
	// raw pointer scan already works for them (native sql.Scanner), so instead
	// of the any-buffer dynamic path, scanValue lets rows.Scan populate the
	// field directly and applyTZ only reapplies tz on the already-scanned V/Time
	// afterward.
	postProcessOnly bool
}

// normalizeEpochUnit reports whether s (case-insensitively) names a Unix epoch
// unit, matching the machbase-neo HTTP API's timeformat values (ns/us/ms/s).
func normalizeEpochUnit(s string) (string, bool) {
	switch strings.ToLower(s) {
	case "ns", "us", "ms", "s":
		return strings.ToLower(s), true
	default:
		return "", false
	}
}

func epochValue(t time.Time, unit string) int64 {
	switch unit {
	case "ms":
		return t.UnixMilli()
	case "us":
		return t.UnixMicro()
	case "s":
		return t.Unix()
	default: // "ns"
		return t.UnixNano()
	}
}

// apply converts a scanned column value into fv, which must be an addressable
// string, int64, sql.Null[int64], sql.NullString, sql.Null[string], time.Time,
// or a pointer to one of those. val is nil for NULL. When val is not a
// time.Time (i.e. the column is not actually DATETIME), it falls back to the
// standard conversion matrix in scan.go so plain string/int64 columns matched
// to a string/int64 field keep working unaffected. Not used for
// sql.NullTime/sql.Null[time.Time]; see applyTZ for those.
func (o *timeFieldOpts) apply(fv reflect.Value, val any, col string, cfg *scanConfig) error {
	isPtr := fv.Kind() == reflect.Pointer
	if val == nil {
		if isPtr {
			fv.Set(reflect.Zero(fv.Type()))
			return nil
		}
		if isNullWrapperType(fv.Type()) {
			fv.Set(reflect.Zero(fv.Type())) // {V/String: zero, Valid: false}
			return nil
		}
		return fmt.Errorf("%w: column %q", ErrScanNullNotSupported, col)
	}
	target := fv
	if isPtr {
		target = reflect.New(fv.Type().Elem()).Elem()
	}

	t, isDateTime := val.(time.Time)
	switch {
	case !isDateTime:
		if err := Scan(val, target.Addr().Interface(), cfg.dateTimeZone); err != nil {
			return err
		}
	case target.Type() == sqlNullInt64Type:
		unit := o.timeformat
		if unit == "" {
			if u, ok := normalizeEpochUnit(cfg.dateTimeFormat); ok {
				unit = u
			} else {
				unit = "ns"
			}
		}
		target.FieldByName("V").SetInt(epochValue(t, unit))
		target.FieldByName("Valid").SetBool(true)
	case target.Type() == sqlNullStringType || target.Type() == sqlNullStringGenericType:
		tz := o.tz
		if tz == nil {
			tz = cfg.dateTimeZone
		}
		format := o.timeformat
		if format == "" {
			format = cfg.dateTimeFormat
		}
		var formatted string
		if unit, ok := normalizeEpochUnit(format); ok {
			formatted = strconv.FormatInt(epochValue(t, unit), 10)
		} else {
			formatted = t.In(tz).Format(format)
		}
		stringField := "V"
		if target.Type() == sqlNullStringType {
			stringField = "String"
		}
		target.FieldByName(stringField).SetString(formatted)
		target.FieldByName("Valid").SetBool(true)
	default:
		tz := o.tz
		if tz == nil {
			tz = cfg.dateTimeZone
		}
		switch target.Kind() {
		case reflect.String:
			format := o.timeformat
			if format == "" {
				format = cfg.dateTimeFormat
			}
			if unit, ok := normalizeEpochUnit(format); ok {
				target.SetString(strconv.FormatInt(epochValue(t, unit), 10))
			} else {
				target.SetString(t.In(tz).Format(format))
			}
		case reflect.Int64:
			unit := o.timeformat
			if unit == "" {
				if u, ok := normalizeEpochUnit(cfg.dateTimeFormat); ok {
					unit = u
				} else {
					unit = "ns"
				}
			}
			target.SetInt(epochValue(t, unit))
		default: // time.Time
			target.Set(reflect.ValueOf(t.In(tz)))
		}
	}
	if isPtr {
		fv.Set(target.Addr())
	}
	return nil
}

// isNullWrapperType reports whether t is one of the Null[T] wrapper struct
// types that this package resets to its zero value (Valid=false) on NULL,
// rather than requiring a pointer field.
func isNullWrapperType(t reflect.Type) bool {
	return t == sqlNullInt64Type || t == sqlNullStringType || t == sqlNullStringGenericType
}

// applyTZ reapplies tz to an already-scanned sql.NullTime/sql.Null[time.Time]
// field (or pointer to one). Unlike apply, it does not scan; the raw pointer
// path in scanValue already populated fv via the wrapper's own sql.Scanner, so
// this only needs to adjust the location of the value it holds.
func (o *timeFieldOpts) applyTZ(fv reflect.Value, cfg *scanConfig) error {
	if fv.Kind() == reflect.Pointer {
		if fv.IsNil() {
			return nil
		}
		fv = fv.Elem()
	}
	if !fv.FieldByName("Valid").Bool() {
		return nil // NULL; V/Time is already the zero value
	}
	valueField := fv.FieldByName("Time")
	if fv.Type() == sqlNullTimeGenericType {
		valueField = fv.FieldByName("V")
	}
	t, ok := valueField.Interface().(time.Time)
	if !ok {
		return nil
	}
	tz := o.tz
	if tz == nil {
		tz = cfg.dateTimeZone
	}
	valueField.Set(reflect.ValueOf(t.In(tz)))
	return nil
}

type structMap struct {
	fields []fieldInfo
	byName map[string]int
}

func (sm *structMap) lookup(column string) (*fieldInfo, bool) {
	idx, ok := sm.byName[strings.ToLower(column)]
	if !ok {
		return nil, false
	}
	return &sm.fields[idx], true
}

func (sm *structMap) add(name string, index []int, timeOpts *timeFieldOpts) error {
	key := strings.ToLower(name)
	if _, exists := sm.byName[key]; exists {
		return fmt.Errorf("%w: %q is mapped by more than one field", ErrScanDuplicateColumn, name)
	}
	sm.byName[key] = len(sm.fields)
	sm.fields = append(sm.fields, fieldInfo{name: name, index: index, time: timeOpts})
	return nil
}

type structMapKey struct {
	typ            reflect.Type
	tagKey         string
	fallbackTagKey string
}

var structMapCache sync.Map // structMapKey -> *structMap

func (cfg *scanConfig) structMapOf(t reflect.Type) (*structMap, error) {
	key := structMapKey{typ: t, tagKey: cfg.tagKey, fallbackTagKey: cfg.fallbackTagKey}
	if cfg.nameMapper == nil {
		if cached, ok := structMapCache.Load(key); ok {
			return cached.(*structMap), nil
		}
	}
	sm := &structMap{byName: map[string]int{}}
	if err := cfg.walkStruct(sm, t, "", nil); err != nil {
		return nil, err
	}
	if len(sm.fields) == 0 {
		return nil, fmt.Errorf("%w: %s has no %q tagged field; add tags or use WithNameMapper",
			ErrScanNoMappedField, t, cfg.tagKey)
	}
	if cfg.nameMapper != nil {
		return sm, nil
	}
	actual, _ := structMapCache.LoadOrStore(key, sm)
	return actual.(*structMap), nil
}

func (cfg *scanConfig) walkStruct(sm *structMap, t reflect.Type, prefix string, index []int) error {
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" && !field.Anonymous {
			continue
		}
		idx := make([]int, 0, len(index)+1)
		idx = append(idx, index...)
		idx = append(idx, i)

		name, explicit, tagOpts := cfg.fieldName(field)
		if explicit && name == "-" {
			continue
		}
		if field.Anonymous && !explicit && isNestedStruct(field.Type) {
			if err := cfg.walkStruct(sm, field.Type, prefix, idx); err != nil {
				return err
			}
			continue
		}
		if field.PkgPath != "" || name == "" {
			continue
		}
		if isNestedStruct(field.Type) {
			if err := cfg.walkStruct(sm, field.Type, prefix+name+".", idx); err != nil {
				return err
			}
			continue
		}
		timeOpts, err := parseTimeFieldOpts(tagOpts, field.Type, field.Name)
		if err != nil {
			return err
		}
		if err := sm.add(prefix+name, idx, timeOpts); err != nil {
			return err
		}
	}
	return nil
}

// fieldName resolves the column name of a field, reports whether it came from a
// tag, and returns any trailing db tag options (e.g. "unit=ms" in "TIME,unit=ms").
// Tag options are only recognized on the primary tag key (cfg.tagKey), not on
// the fallback tag key.
func (cfg *scanConfig) fieldName(field reflect.StructField) (name string, explicit bool, options string) {
	if cfg.tagKey != "" {
		if tag, ok := field.Tag.Lookup(cfg.tagKey); ok {
			return tagOrFieldName(tag, field.Name), true, tagOptions(tag)
		}
	}
	if cfg.fallbackTagKey != "" {
		if tag, ok := field.Tag.Lookup(cfg.fallbackTagKey); ok {
			return tagOrFieldName(tag, field.Name), true, ""
		}
	}
	if cfg.nameMapper != nil {
		return cfg.nameMapper(field.Name), false, ""
	}
	return "", false, ""
}

func tagName(tag string) string {
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		return tag[:idx]
	}
	return tag
}

// tagOptions returns the portion of a tag after the first comma, if any.
func tagOptions(tag string) string {
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		return tag[idx+1:]
	}
	return ""
}

func tagOrFieldName(tag, fieldName string) string {
	if name := tagName(tag); name != "" {
		return name
	}
	return fieldName
}

// parseTimeFieldOpts parses DATETIME conversion options (timeformat/tz) from
// the portion of a db tag after the column name. For fields eligible for
// DATETIME conversion (string, int64, time.Time, sql.NullTime,
// sql.Null[time.Time], sql.Null[int64], sql.NullString, sql.Null[string], and
// their pointer forms) it always returns a non-nil *timeFieldOpts, even when
// options is empty, so the field is flagged for DATETIME handling regardless
// of whether the tag set anything explicitly. For ineligible fields, a
// non-empty options is an error so typos like `db:"NAME,timeformat=ms"` fail
// loudly instead of being silently ignored.
func parseTimeFieldOpts(options string, fieldType reflect.Type, fieldName string) (*timeFieldOpts, error) {
	elemType := fieldType
	if elemType.Kind() == reflect.Pointer {
		elemType = elemType.Elem()
	}
	isString := elemType.Kind() == reflect.String
	isInt64 := elemType == reflect.TypeOf(int64(0))
	isTime := elemType == timeTimeType
	isNullTime := elemType == sqlNullTimeType || elemType == sqlNullTimeGenericType
	isNullInt64 := elemType == sqlNullInt64Type
	isNullString := elemType == sqlNullStringType || elemType == sqlNullStringGenericType
	eligible := isString || isInt64 || isTime || isNullTime || isNullInt64 || isNullString

	var parts []string
	hasRealOption := false
	for _, part := range strings.Split(options, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		parts = append(parts, part)
		// "omitempty" is a common leftover from copy-pasting a json tag onto db;
		// accept and ignore it instead of failing on borrowed tag syntax.
		if part != "omitempty" {
			hasRealOption = true
		}
	}

	if !hasRealOption {
		if !eligible {
			return nil, nil
		}
		return &timeFieldOpts{postProcessOnly: isNullTime}, nil
	}
	if !eligible {
		return nil, fmt.Errorf("%w: options only apply to string, int64, time.Time, sql.NullTime, sql.Null[time.Time], sql.Null[int64], sql.NullString, or sql.Null[string] fields, not field %s (%s)",
			ErrScanInvalidTagOption, fieldName, fieldType)
	}

	opts := &timeFieldOpts{postProcessOnly: isNullTime}
	for _, part := range parts {
		if part == "omitempty" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return nil, fmt.Errorf("%w: %q on field %s (expected key=value)", ErrScanInvalidTagOption, part, fieldName)
		}
		switch key {
		case "timeformat":
			switch {
			case isTime, isNullTime:
				return nil, fmt.Errorf("%w: \"timeformat\" does not apply to field %s; use \"tz\"", ErrScanInvalidTagOption, fieldName)
			case isInt64, isNullInt64:
				unit, ok := normalizeEpochUnit(value)
				if !ok {
					return nil, fmt.Errorf("%w: timeformat %q must be one of ns, us, ms, s for int64 field %s", ErrScanInvalidTagOption, value, fieldName)
				}
				opts.timeformat = unit
			default: // isString, isNullString
				opts.timeformat = value
			}
		case "tz":
			if !isString && !isTime && !isNullTime && !isNullString {
				return nil, fmt.Errorf("%w: \"tz\" only applies to string, time.Time, sql.NullTime, sql.Null[time.Time], sql.NullString, or sql.Null[string] fields, not field %s", ErrScanInvalidTagOption, fieldName)
			}
			loc, err := parseTimeLocation(value)
			if err != nil {
				return nil, fmt.Errorf("%w: %v (field %s)", ErrScanInvalidTagOption, err, fieldName)
			}
			opts.tz = loc
		default:
			return nil, fmt.Errorf("%w: unknown option %q on field %s", ErrScanInvalidTagOption, key, fieldName)
		}
	}
	return opts, nil
}

func parseTimeLocation(name string) (*time.Location, error) {
	switch name {
	case "UTC":
		return time.UTC, nil
	case "Local":
		return time.Local, nil
	default:
		return time.LoadLocation(name)
	}
}

var (
	timeTimeType             = reflect.TypeOf(time.Time{})
	sqlNullTimeType          = reflect.TypeOf(sql.NullTime{})
	sqlNullTimeGenericType   = reflect.TypeOf(sql.Null[time.Time]{})
	sqlNullInt64Type         = reflect.TypeOf(sql.Null[int64]{})
	sqlNullStringType        = reflect.TypeOf(sql.NullString{})
	sqlNullStringGenericType = reflect.TypeOf(sql.Null[string]{})
	scannerType              = reflect.TypeOf((*sql.Scanner)(nil)).Elem()
	valuerType               = reflect.TypeOf((*driver.Valuer)(nil)).Elem()
)

// isNestedStruct reports whether a struct type should be flattened into columns
// instead of being treated as a single scan destination.
func isNestedStruct(t reflect.Type) bool {
	if t.Kind() != reflect.Struct || t == timeTimeType {
		return false
	}
	return !reflect.PointerTo(t).Implements(scannerType) && !t.Implements(valuerType)
}
