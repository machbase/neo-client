package client

import (
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"reflect"
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
)

const (
	defaultTagKey         = "db"
	defaultFallbackTagKey = "json"
	// defaultMaxRows is a safe upper bound for the helpers that materialize a whole
	// result set. It mirrors defaultFetchRows but does not follow the DSN fetch_rows
	// value, which is unreachable from *sql.Rows.
	defaultMaxRows = int64(defaultFetchRows)
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
}

func newScanConfig(opts []ScanOption) *scanConfig {
	cfg := &scanConfig{
		tagKey:         defaultTagKey,
		fallbackTagKey: defaultFallbackTagKey,
		maxRows:        defaultMaxRows,
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

func (sm *structMap) add(name string, index []int) error {
	key := strings.ToLower(name)
	if _, exists := sm.byName[key]; exists {
		return fmt.Errorf("%w: %q is mapped by more than one field", ErrScanDuplicateColumn, name)
	}
	sm.byName[key] = len(sm.fields)
	sm.fields = append(sm.fields, fieldInfo{name: name, index: index})
	return nil
}

type structMapKey struct {
	typ            reflect.Type
	tagKey         string
	fallbackTagKey string
	nameMapper     uintptr
}

var structMapCache sync.Map // structMapKey -> *structMap

func (cfg *scanConfig) structMapOf(t reflect.Type) (*structMap, error) {
	key := structMapKey{typ: t, tagKey: cfg.tagKey, fallbackTagKey: cfg.fallbackTagKey}
	if cfg.nameMapper != nil {
		key.nameMapper = reflect.ValueOf(cfg.nameMapper).Pointer()
	}
	if cached, ok := structMapCache.Load(key); ok {
		return cached.(*structMap), nil
	}
	sm := &structMap{byName: map[string]int{}}
	if err := cfg.walkStruct(sm, t, "", nil); err != nil {
		return nil, err
	}
	if len(sm.fields) == 0 {
		return nil, fmt.Errorf("%w: %s has no %q tagged field; add tags or use WithNameMapper",
			ErrScanNoMappedField, t, cfg.tagKey)
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

		name, explicit := cfg.fieldName(field)
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
		if err := sm.add(prefix+name, idx); err != nil {
			return err
		}
	}
	return nil
}

// fieldName resolves the column name of a field and reports whether it came from a tag.
func (cfg *scanConfig) fieldName(field reflect.StructField) (string, bool) {
	if cfg.tagKey != "" {
		if tag, ok := field.Tag.Lookup(cfg.tagKey); ok {
			return tagName(tag), true
		}
	}
	if cfg.fallbackTagKey != "" {
		if tag, ok := field.Tag.Lookup(cfg.fallbackTagKey); ok {
			return tagName(tag), true
		}
	}
	if cfg.nameMapper != nil {
		return cfg.nameMapper(field.Name), false
	}
	return "", false
}

func tagName(tag string) string {
	if idx := strings.IndexByte(tag, ','); idx >= 0 {
		return tag[:idx]
	}
	return tag
}

var (
	timeTimeType = reflect.TypeOf(time.Time{})
	scannerType  = reflect.TypeOf((*sql.Scanner)(nil)).Elem()
	valuerType   = reflect.TypeOf((*driver.Valuer)(nil)).Elem()
)

// isNestedStruct reports whether a struct type should be flattened into columns
// instead of being treated as a single scan destination.
func isNestedStruct(t reflect.Type) bool {
	if t.Kind() != reflect.Struct || t == timeTimeType {
		return false
	}
	return !reflect.PointerTo(t).Implements(scannerType) && !t.Implements(valuerType)
}
