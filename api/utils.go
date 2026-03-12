package api

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

func FormatIntWithCommas(value int64) string {
	digits := strconv.FormatInt(value, 10)
	start := 0
	if digits[0] == '-' {
		start = 1
	}
	if len(digits)-start <= 3 {
		return digits
	}

	var builder strings.Builder
	builder.Grow(len(digits) + (len(digits)-start-1)/3)
	if start == 1 {
		builder.WriteByte('-')
	}

	head := (len(digits) - start) % 3
	if head == 0 {
		head = 3
	}
	builder.WriteString(digits[start : start+head])
	for index := start + head; index < len(digits); index += 3 {
		builder.WriteByte(',')
		builder.WriteString(digits[index : index+3])
	}
	return builder.String()
}

func ErrIncompatible(dstType string, src any) error {
	return fmt.Errorf("incompatible conv '%v' (%T) to %s", src, src, dstType)
}

func ToInt8(one any) (int8, error) {
	v, err := ToInt64(one)
	if err != nil {
		return 0, err
	}
	return int8(v), nil
}

func ToInt16(one any) (int16, error) {
	v, err := ToInt64(one)
	if err != nil {
		return 0, err
	}
	return int16(v), nil
}

func ToUint16(one any) (uint16, error) {
	v, err := ToInt64(one)
	if err != nil {
		return 0, err
	}
	return uint16(v), nil
}

func ToInt32(one any) (int32, error) {
	v, err := ToInt64(one)
	if err != nil {
		return 0, err
	}
	return int32(v), nil
}

func ToUint32(one any) (uint32, error) {
	v, err := ToInt64(one)
	if err != nil {
		return 0, err
	}
	return uint32(v), nil
}

func ToInt64(one any) (int64, error) {
	switch val := one.(type) {
	case string:
		if v, err := strconv.ParseInt(val, 10, 64); err != nil {
			if f, err := ToFloat64(val); err != nil {
				return 0, ErrIncompatible("int64", val)
			} else {
				return int64(f), nil
			}
		} else {
			return v, nil
		}
	case *string:
		if v, err := strconv.ParseInt(*val, 10, 64); err != nil {
			if f, err := ToFloat64(*val); err != nil {
				return 0, ErrIncompatible("int64", val)
			} else {
				return int64(f), nil
			}
		} else {
			return v, nil
		}
	case float32:
		return int64(val), nil
	case *float32:
		return int64(*val), nil
	case float64:
		return int64(val), nil
	case *float64:
		return int64(*val), nil
	case int64:
		return val, nil
	case *int64:
		return *val, nil
	case int:
		return int64(val), nil
	case *int:
		return int64(*val), nil
	case int32:
		return int64(val), nil
	case *int32:
		return int64(*val), nil
	case int16:
		return int64(val), nil
	case *int16:
		return int64(*val), nil
	case int8:
		return int64(val), nil
	case *int8:
		return int64(*val), nil
	case json.Number:
		if v, err := val.Int64(); err != nil {
			return 0, fmt.Errorf("incompatible conv '%v' (%T) to int64, %s", val, val, err.Error())
		} else {
			return v, nil
		}
	default:
		return 0, ErrIncompatible("int64", val)
	}
}

func ToUint64(one any) (uint64, error) {
	switch val := one.(type) {
	case string:
		if v, err := strconv.ParseUint(val, 10, 64); err != nil {
			if f, err := ToFloat64(val); err != nil {
				return 0, ErrIncompatible("int64", val)
			} else {
				return uint64(f), nil
			}
		} else {
			return v, nil
		}
	case *string:
		if v, err := strconv.ParseUint(*val, 10, 64); err != nil {
			if f, err := ToFloat64(*val); err != nil {
				return 0, ErrIncompatible("int64", val)
			} else {
				return uint64(f), nil
			}
		} else {
			return v, nil
		}
	case float32:
		return uint64(val), nil
	case *float32:
		return uint64(*val), nil
	case float64:
		return uint64(val), nil
	case *float64:
		return uint64(*val), nil
	case int64:
		return uint64(val), nil
	case *int64:
		return uint64(*val), nil
	case int:
		return uint64(val), nil
	case *int:
		return uint64(*val), nil
	case int32:
		return uint64(val), nil
	case *int32:
		return uint64(*val), nil
	case int16:
		return uint64(val), nil
	case *int16:
		return uint64(*val), nil
	case int8:
		return uint64(val), nil
	case *int8:
		return uint64(*val), nil
	case json.Number:
		if v, err := val.Int64(); err != nil {
			return 0, fmt.Errorf("incompatible conv '%v' (%T) to int64, %s", val, val, err.Error())
		} else {
			return uint64(v), nil
		}
	default:
		return 0, ErrIncompatible("int64", val)
	}
}

func ToFloat32(one any) (float32, error) {
	switch val := one.(type) {
	case string:
		return ParseFloat32(val)
	case *string:
		return ParseFloat32(*val)
	case float32:
		return val, nil
	case *float32:
		return *val, nil
	case float64:
		return float32(val), nil
	case *float64:
		return float32(*val), nil
	case int:
		return float32(val), nil
	case *int:
		return float32(*val), nil
	case json.Number:
		if v, err := val.Float64(); err != nil {
			return 0, fmt.Errorf("incompatible conv '%v' (%T) to float32, %s", val, val, err.Error())
		} else {
			return float32(v), nil
		}
	default:
		return 0, ErrIncompatible("float32", val)
	}
}

func ParseFloat32(val string) (float32, error) {
	if val, err := strconv.ParseFloat(val, 32); err != nil {
		return 0, fmt.Errorf("incompatible conv '%v' (%T) to float32, %s", val, val, err.Error())
	} else {
		return float32(val), nil
	}
}

func ParseFloat64(val string) (float64, error) {
	if val, err := strconv.ParseFloat(val, 64); err != nil {
		return 0, fmt.Errorf("incompatible conv '%v' (%T) to float64, %s", val, val, err.Error())
	} else {
		return val, nil
	}
}

func ParseBoolean(val string) (bool, error) {
	return strconv.ParseBool(val)
}

func ToFloat64(one any) (float64, error) {
	switch val := one.(type) {
	case string:
		return ParseFloat64(val)
	case *string:
		return ParseFloat64(*val)
	case float32:
		return float64(val), nil
	case *float32:
		return float64(*val), nil
	case float64:
		return val, nil
	case *float64:
		return *val, nil
	case int:
		return float64(val), nil
	case *int:
		return float64(*val), nil
	case json.Number:
		if v, err := val.Float64(); err != nil {
			return 0, fmt.Errorf("incompatible conv '%v' (%T) to float64, %s", val, val, err.Error())
		} else {
			return v, nil
		}
	default:
		return 0, ErrIncompatible("float64", val)
	}
}

func ToDuration(one any) (time.Duration, error) {
	switch val := one.(type) {
	case time.Duration:
		return val, nil
	case string:
		return ParseDuration(val)
	case *string:
		return ParseDuration(*val)
	case float64:
		return time.Duration(int64(val)), nil
	case *float64:
		return time.Duration(int64(*val)), nil
	case float32:
		return time.Duration(int64(val)), nil
	case *float32:
		return time.Duration(int64(*val)), nil
	case int64:
		return time.Duration(val), nil
	case *int64:
		return time.Duration(*val), nil
	case int32:
		return time.Duration(val), nil
	case *int32:
		return time.Duration(*val), nil
	case int16:
		return time.Duration(val), nil
	case *int16:
		return time.Duration(*val), nil
	case int8:
		return time.Duration(val), nil
	case *int8:
		return time.Duration(*val), nil
	case int:
		return time.Duration(val), nil
	case *int:
		return time.Duration(*val), nil
	default:
		return 0, ErrIncompatible("time.Duration", val)
	}
}

func ParseDuration(val string) (time.Duration, error) {
	if i := strings.IndexRune(val, 'd'); i > 0 {
		var day time.Duration = 0
		digit := val[0:i]
		str := val[i+1:]
		d, err := strconv.ParseInt(digit, 10, 64)
		if err != nil {
			return 0, ErrIncompatible("time.Duration", val)
		}
		day = time.Duration(d) * 24 * time.Hour
		if len(str) > 0 {
			if dur, err := time.ParseDuration(str); err != nil {
				return 0, ErrIncompatible("time.Duration", val)
			} else if day >= 0 {
				return day + dur, nil
			} else {
				return day - dur, nil
			}
		} else {
			return day, nil
		}
	}
	if d, err := time.ParseDuration(val); err != nil {
		return 0, err
	} else {
		return d, nil
	}
}

func ParseIP(val string) (net.IP, error) {
	addr := net.ParseIP(val)
	if addr == nil {
		return nil, fmt.Errorf("incompatible conv '%v' (%T) to IP", val, val)
	}
	return addr, nil
}

var StandardTimeNow func() time.Time = time.Now

func ParseTime(strVal string, format string, location *time.Location) (time.Time, error) {
	var baseTime time.Time
	strVal = strings.TrimSpace(strVal)
	if strings.HasPrefix(strVal, "now") {
		baseTime = StandardTimeNow()
		sig := time.Duration(1)
		remain := strings.TrimSpace(strVal[3:])
		if len(remain) == 0 {
			return baseTime, nil
		}
		if strings.HasPrefix(remain, "+") {
			remain = strings.TrimSpace(remain[1:])
		} else if strings.HasPrefix(remain, "-") {
			sig = time.Duration(-1)
			remain = strings.TrimSpace(remain[1:])
		} else {
			return baseTime, ErrIncompatible("time.Time", strVal)
		}
		dur, err := ToDuration(remain)
		if err != nil {
			return baseTime, fmt.Errorf("incompatible conv '%s', %s", strVal, err.Error())
		}
		baseTime = baseTime.Add(dur * sig)
		return baseTime, nil
	}
	if format == "" {
		return baseTime, ErrIncompatible("time.Time", strVal)
	}

	timeLayout := GetTimeformat(format)
	var ts int64
	var err error
	switch timeLayout {
	case "s":
		if ts, err = ToInt64(strVal); err != nil {
			return time.Time{}, fmt.Errorf("unable parse time in timeformat, %s", err.Error())
		}
		return time.Unix(ts, 0), nil
	case "ms":
		if ts, err = ToInt64(strVal); err != nil {
			return time.Time{}, fmt.Errorf("unable parse time in timeformat, %s", err.Error())
		}
		return time.Unix(0, ts*int64(time.Millisecond)), nil
	case "us":
		if ts, err = ToInt64(strVal); err != nil {
			return time.Time{}, fmt.Errorf("unable parse time in timeformat, %s", err.Error())
		}
		return time.Unix(0, ts*int64(time.Microsecond)), nil
	case "ns":
		if ts, err = ToInt64(strVal); err != nil {
			return time.Time{}, fmt.Errorf("unable parse time in timeformat, %s", err.Error())
		}
		return time.Unix(0, ts), nil
	default:
		baseTime, err = time.ParseInLocation(timeLayout, strVal, location)
		if err != nil {
			return baseTime, fmt.Errorf("%s, %s", ErrIncompatible("time.Time", strVal).Error(), err)
		}
	}
	return baseTime, nil
}

func GetTimeformat(f string) string {
	if m, ok := _predefinedFormats[strings.ToUpper(f)]; ok {
		return m
	}
	return f
}

// Refer: https://gosamples.dev/date-time-format-cheatsheet/
var _predefinedFormats = map[string]string{
	"-":           "2006-01-02 15:04:05.999",
	"DEFAULT":     "2006-01-02 15:04:05.999",
	"DEFAULT_MS":  "2006-01-02 15:04:05.999",
	"DEFAULT_US":  "2006-01-02 15:04:05.999999",
	"DEFAULT_NS":  "2006-01-02 15:04:05.999999999",
	"DEFAULT.MS":  "2006-01-02 15:04:05.000",
	"DEFAULT.US":  "2006-01-02 15:04:05.000000",
	"DEFAULT.NS":  "2006-01-02 15:04:05.000000000",
	"NUMERIC":     "01/02 03:04:05PM '06 -0700", // The reference time, in numerical order.
	"ANSIC":       "Mon Jan _2 15:04:05 2006",
	"UNIX":        "Mon Jan _2 15:04:05 MST 2006",
	"RUBY":        "Mon Jan 02 15:04:05 -0700 2006",
	"RFC822":      "02 Jan 06 15:04 MST",
	"RFC822Z":     "02 Jan 06 15:04 -0700", // RFC822 with numeric zone
	"RFC850":      "Monday, 02-Jan-06 15:04:05 MST",
	"RFC1123":     "Mon, 02 Jan 2006 15:04:05 MST",
	"RFC1123Z":    "Mon, 02 Jan 2006 15:04:05 -0700", // RFC1123 with numeric zone
	"RFC3339":     "2006-01-02T15:04:05Z07:00",
	"RFC3339NANO": "2006-01-02T15:04:05.999999999Z07:00",
	"KITCHEN":     "3:04:05PM",
	"STAMP":       "Jan _2 15:04:05",
	"STAMPMILLI":  "Jan _2 15:04:05.000",
	"STAMPMICRO":  "Jan _2 15:04:05.000000",
	"STAMPNANO":   "Jan _2 15:04:05.000000000",
	"S_NS":        "05.999999999",
	"S_US":        "05.999999",
	"S_MS":        "05.999",
	"S.NS":        "05.000000000",
	"S.US":        "05.000000",
	"S.MS":        "05.000",
}
