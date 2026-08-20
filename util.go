package client

import (
	"database/sql/driver"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/machbase/neo-client/v2/api"
	"github.com/machbase/neo-client/v2/machnet"
)

func queryHead(query string) string {
	parts := strings.Fields(query)
	if len(parts) == 0 {
		return ""
	}
	return strings.ToUpper(parts[0])
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func namedValuesToAny(args []driver.NamedValue) ([]any, error) {
	vals := make([]any, len(args))
	for i := range args {
		arg := args[i]
		if err := checkNamedValue(&arg); err != nil {
			return nil, err
		}
		if arg.Name != "" {
			vals[i] = arg
		} else {
			vals[i] = arg.Value
		}
	}
	return vals, nil
}

func valuesToAny(args []driver.Value) []any {
	vals := make([]any, len(args))
	for i := range args {
		vals[i] = args[i]
	}
	return vals
}

func checkNamedValue(nv *driver.NamedValue) error {
	if nv == nil {
		return nil
	}
	value, err := normalizeNamedValue(nv.Value)
	if err != nil {
		return err
	}
	nv.Value = value
	return nil
}

func normalizeNamedValue(value any) (any, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case int:
		if v > math.MaxInt32 || v < math.MinInt32 {
			return int64(v), nil
		}
		return v, nil
	case int16, *int16, int32, *int32, int64, *int64, float32, *float32, float64, *float64, string, *string, []byte, time.Time, *time.Time, net.IP, api.Decimal, *api.Decimal:
		return v, nil
	case *int:
		if v == nil {
			return nil, nil
		}
		if *v > math.MaxInt32 || *v < math.MinInt32 {
			return int64(*v), nil
		}
		return *v, nil
	case bool:
		return nil, fmt.Errorf("machbase does not support bool parameter type")
	case driver.Valuer:
		resolved, err := v.Value()
		if err != nil {
			return nil, err
		}
		return normalizeNamedValue(resolved)
	case uint:
		if uint64(v) > math.MaxInt64 {
			return nil, fmt.Errorf("uint value %d overflows int64", v)
		}
		return int64(v), nil
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return nil, fmt.Errorf("uint64 value %d overflows int64", v)
		}
		return int64(v), nil
	case *uint:
		if v == nil {
			return nil, nil
		}
		return normalizeNamedValue(*v)
	case *uint8:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint16:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint32:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint64:
		if v == nil {
			return nil, nil
		}
		return normalizeNamedValue(*v)
	default:
		return nil, fmt.Errorf("machbase does not support parameter type %T", value)
	}
}

// normalizeError classifies an already-formatted error (typically the output
// of ErrorOf/formatMachcliError) and promotes network/connection-death text
// to driver.ErrBadConn so database/sql evicts and retries; it never formats
// machcli (code, msg) pairs itself, that's ErrorOf's job. Every driver-facing
// entry point (Conn/Stmt/Rows methods that implement database/sql/driver
// interfaces) must wrap its final error in normalizeError exactly once right
// before returning to database/sql; internal helpers that only stash an
// ErrorOf result in a Result/Row/Rows field for later inspection should call
// ErrorOf alone and let the eventual driver-facing caller normalize it.
func normalizeError(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "connection closed") ||
		strings.Contains(msg, "invalid connection") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "unexpected eof") ||
		strings.Contains(msg, "eof") {
		return driver.ErrBadConn
	}
	return err
}

// formatMachcliError builds the error returned by Connector/Conn/Stmt.ErrorOf
// from the machcli (code, msg) pair reported by the underlying handle, plus
// an optional cause from the Go-level call that triggered the lookup. It only
// formats a message; it never decides driver.ErrBadConn promotion, see
// normalizeError above for that half of the split and the required call
// order. (Full static enforcement of the ErrorOf -> normalizeError order
// isn't practical without a broader refactor of every driver-interface
// method into a single choke point; this comment plus grepping for
// `.ErrorOf(` call sites is the current safeguard.)
func formatMachcliError(code int, msg string, cause error) error {
	if code == 0 && msg == "" && cause == nil {
		// no error
		return nil
	}
	if code == 0 {
		// code == 0 means client-side error
		if cause == nil {
			return fmt.Errorf("MACHCLI %s", msg)
		}
		return fmt.Errorf("MACHCLI %s, %s", msg, cause.Error())
	}
	// code > 0 means server-side error: msg already carries the full
	// server-reported text (cause is typically the very same error that
	// populated code/msg in the first place), so cause is intentionally
	// not appended here to avoid duplicating it.
	return fmt.Errorf("MACHCLI-ERR-%d, %s", code, msg)
}

func leadingSQLKeyword(query string) string {
	remaining := strings.TrimSpace(query)
	for remaining != "" {
		switch {
		case strings.HasPrefix(remaining, "--"):
			newline := strings.IndexByte(remaining, '\n')
			if newline < 0 {
				return ""
			}
			remaining = strings.TrimSpace(remaining[newline+1:])
		case strings.HasPrefix(remaining, "/*"):
			end := strings.Index(remaining[2:], "*/")
			if end < 0 {
				return ""
			}
			remaining = strings.TrimSpace(remaining[end+4:])
		default:
			end := 0
			for end < len(remaining) {
				ch := remaining[end]
				if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') ||
					(ch >= '0' && ch <= '9') || ch == '_' || ch == '$') {
					break
				}
				end++
			}
			return strings.ToUpper(remaining[:end])
		}
	}
	return ""
}

func toDriverValue(value any) (driver.Value, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case *bool:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case *int16:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint16:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *int32:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *uint32:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *int64:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case *uint64:
		if v == nil {
			return nil, nil
		}
		return int64(*v), nil
	case *float32:
		if v == nil {
			return nil, nil
		}
		return float64(*v), nil
	case *float64:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case *string:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case *[]byte:
		if v == nil {
			return nil, nil
		}
		buf := make([]byte, len(*v))
		copy(buf, *v)
		return buf, nil
	case *time.Time:
		if v == nil {
			return nil, nil
		}
		return *v, nil
	case time.Time:
		return v, nil
	case *net.IP:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	case net.IP:
		return v, nil
	case api.Decimal:
		return v.String(), nil
	case *api.Decimal:
		if v == nil {
			return nil, nil
		}
		return v.String(), nil
	case int:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case float32:
		return float64(v), nil
	case float64:
		return v, nil
	case string:
		return v, nil
	case []byte:
		buf := make([]byte, len(v))
		copy(buf, v)
		return buf, nil
	default:
		return nil, fmt.Errorf("machbase cannot convert row value %T to driver.Value", value)
	}
}

func formatResultMessage(err error, stmtType machnet.StmtType, rowCount int64) string {
	if err != nil {
		return err.Error()
	}
	switch stmtType {
	case machnet.QPP_STMT_TYPE_CREATE_TABLE:
		return "table created."
	case machnet.QPP_STMT_TYPE_DROP_TABLE:
		return "table dropped."
	case machnet.QPC_STMT_TYPE_CREATE_ROLLUP:
		return "rollup created."
	case machnet.QPC_STMT_TYPE_DROP_ROLLUP:
		return "rollup dropped."
	case machnet.QPC_STMT_TYPE_CREATE_RETENTION:
		return "retention created."
	case machnet.QPC_STMT_TYPE_DROP_RETENTION:
		return "retention dropped."
	case machnet.QPP_STMT_TYPE_CREATE_INDEX:
		return "index created."
	case machnet.QPP_STMT_TYPE_DROP_INDEX:
		return "index dropped."
	case machnet.QPP_STMT_TYPE_ALTER_INDEX:
		return "index altered."
	case machnet.QPP_STMT_TYPE_CREATE_USER:
		return "user created."
	case machnet.QPP_STMT_TYPE_DROP_USER:
		return "user dropped."
	case machnet.QPP_STMT_TYPE_ALTER_USER:
		return "user altered."
	case machnet.QPP_STMT_TYPE_GRANT_USER:
		return "user granted."
	case machnet.QPP_STMT_TYPE_REVOKE_USER:
		return "user revoked."
	case machnet.QPP_STMT_TYPE_CREATE_VIEW:
		return "view created."
	case machnet.QPP_STMT_TYPE_DROP_VIEW:
		return "view dropped."
	case machnet.QPP_STMT_TYPE_CREATE_DATABASE:
		return "database created."
	case machnet.QPP_STMT_TYPE_DROP_DATABASE:
		return "database dropped."
	case machnet.QPP_STMT_TYPE_ALTER_DATABASE:
		return "database altered."
	case machnet.QPP_STMT_TYPE_BACKUP_DATABASE:
		return "database backup completed."
	case machnet.QPP_STMT_TYPE_RESTORE_DATABASE:
		return "database restore completed."
	case machnet.QPP_STMT_TYPE_MOUNT_DATABASE:
		return "database mounted."
	case machnet.QPP_STMT_TYPE_UMOUNT_DATABASE:
		return "database unmounted."
	case machnet.QPP_STMT_TYPE_MOUNT_TABLE:
		return "table mounted."
	case machnet.QPP_STMT_TYPE_UMOUNT_TABLE:
		return "table unmounted."
	case machnet.QPP_STMT_TYPE_BACKUP_TABLE:
		return "table backup completed."
	case machnet.QPP_STMT_TYPE_INC_BACKUP_DATABASE:
		return "incremental database backup completed."
	case machnet.QPP_STMT_TYPE_INC_BACKUP_TABLE:
		return "incremental table backup completed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_KILL_SESSION:
		return "session killed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_CANCEL_SESSION:
		return "session canceled."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_AGER:
		return "ager flushed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_RESULT_CACHE:
		return "result cache flushed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_PVO_CACHE:
		return "pvo cache flushed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_SYS_STAT:
		return "system statistics flushed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_PAGE_CACHE:
		return "page cache flushed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_KV_CACHE:
		return "kv cache flushed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_CHECK_DISK_USAGE:
		return "disk usage checked."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_SWIPE:
		return "system swiped."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_INSTALL:
		return "system installed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_CHECKPOINT:
		return "system checkpointed."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_SET:
		return "system set."
	case machnet.QPP_STMT_TYPE_ALTER_SYSTEM_UNSET:
		return "system unset."
	case machnet.QPP_STMT_TYPE_ALTER_SESSION_SET:
		return "session set."
	case machnet.QPP_STMT_TYPE_COMMIT:
		return "transaction committed."
	case machnet.QPP_STMT_TYPE_ROLLBACK:
		return "transaction rolled back."
	case machnet.QPP_STMT_TYPE_TABLE_FLUSH:
		return "table flushed."
	case machnet.QPP_STMT_TYPE_INDEX_FLUSH:
		return "index flushed."
	case machnet.QPP_STMT_TYPE_TABLE_REFRESH:
		return "table refreshed."
	case machnet.QPP_STMT_TYPE_TABLE_FREEZE_TAG_INDEX:
		return "table tag index frozen."
	case machnet.QPP_STMT_TYPE_TABLE_UNFREEZE_TAG_INDEX:
		return "table tag index unfrozen."
	}

	verb := ""
	if stmtType.IsSelect() {
		verb = "selected."
	} else if stmtType.IsInsert() {
		verb = "inserted."
	} else if stmtType.IsDelete() {
		verb = "deleted."
	} else if stmtType.IsInsertSelect() {
		verb = "inserted from select."
	} else if stmtType.IsUpdate() {
		verb = "updated."
	} else if stmtType.IsExecRollup() {
		verb = "rollup executed."
	} else {
		return fmt.Sprintf("executed (%d).", stmtType)
	}

	switch rowCount {
	case 0:
		return "no rows " + verb
	case 1:
		return "a row " + verb
	default:
		return formatIntWithCommas(rowCount) + " rows " + verb
	}
}

func formatIntWithCommas(value int64) string {
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
