package machnet

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/machbase/neo-client/v2/api"
)

// cmi protocol version: 4.0.3
const (
	cmiProtocolMajor = 4
	cmiProtocolMinor = 0
	cmiProtocolFix   = 3
)

const cmiV403MetadataVersion uint64 = (4 << 48) | 3
const cmiGeneratedRowIDVersion uint64 = (4 << 48) | 3

const (
	cmiPacketMaxBody = 64 * 1024

	cmiProtoCnt         = 9
	cmiHandshakePrefix  = "CMI_INET"
	cmiHandshakeEndian  = "0"
	cmiHandshakeReady   = "CMI_READY"
	cmiHandshakePayload = cmiHandshakePrefix + cmiHandshakeEndian
)

const (
	cmiConnectProtocol     = 0
	cmiDisconnectProtocol  = 1
	cmiPrepareProtocol     = 6
	cmiExecuteProtocol     = 7
	cmiExecDirectProtocol  = 8
	cmiFetchProtocol       = 9
	cmiFreeProtocol        = 10
	cmiAppendOpenProtocol  = 11
	cmiAppendDataProtocol  = 12
	cmiAppendCloseProtocol = 13
)

const (
	cmiCVersionID       = 0x00000001
	cmiCClientID        = 0x00000002
	cmiCDatabaseID      = 0x00000004
	cmiCEndianID        = 0x00000005
	cmiCUserID          = 0x00000006
	cmiCPasswordID      = 0x00000007
	cmiCTimeoutID       = 0x00000008
	cmiCAuthModeID      = 0x0000000A
	cmiCAuthNonceID     = 0x0000000C
	cmiCAuthValidMsID   = 0x0000000D
	cmiCAuthSignatureID = 0x0000000E
	cmiCSIDID           = 0x00000040
	cmiCSHCID           = 0x00000041
	cmiCIPID            = 0x00000042
	cmiCAuthSigSchemeID = 0x00000043
	cmiCTimezoneID      = 0x00000070

	cmiRResultID   = 0x00000010
	cmiRMessageID  = 0x00000011
	cmiREMessageID = 0x00000012

	cmiPStatementID      = 0x00000020
	cmiPBindsID          = 0x00000021
	cmiPIDID             = 0x00000022
	cmiPRowsID           = 0x00000023
	cmiPColumnsID        = 0x00000024
	cmiPTableID          = 0x00000025
	cmiPColNameID        = 0x00000026
	cmiPColTypeID        = 0x00000027
	cmiPParamTypeID      = 0x00000029
	cmiPParamMetaV2ID    = 0x0000002A
	cmiPGeneratedRowIDID = 0x0000002C

	cmiEParamID   = 0x00000031
	cmiEParamV2ID = 0x00000033
	cmiEEndianID  = 0x00000034

	cmiDStatementID = 0x00000040

	cmiFIDID    = 0x00000050
	cmiFRowsID  = 0x00000051
	cmiFValueID = 0x00000052

	cmiXIDID            = 0x00000060
	cmiXAppendSuccessID = 0x00000061
	cmiXAppendFailureID = 0x00000062
)

const (
	cmimIDStmtType = 200
)

const (
	cmiStringType = 0x00000002
	cmiBinaryType = 0x00000003
	cmiSCharType  = 0x00000004
	cmiUCharType  = 0x00000005
	cmiSShortType = 0x00000006
	cmiUShortType = 0x00000007
	cmiSIntType   = 0x00000008
	cmiUIntType   = 0x00000009
	cmiSLongType  = 0x0000000a
	cmiULongType  = 0x0000000b
	cmiDateType   = 0x0000000c
	cmiRowsType   = 0x0000000d
	cmiTNumType   = 0x000000f1
	cmiNumType    = 0x000000f2
)

const (
	cmdFixFlag  = 0x0000
	cmdVarFlag  = 0x0001
	cmdTimeFlag = 0x0002

	cmdVarcharType = (0x0001 << 2) | cmdVarFlag
	cmdDateType    = (0x0001 << 2) | cmdTimeFlag
	cmdInt16Type   = (0x0001 << 2) | cmdFixFlag
	cmdInt32Type   = (0x0002 << 2) | cmdFixFlag
	cmdInt64Type   = (0x0003 << 2) | cmdFixFlag
	cmdFlt32Type   = (0x0004 << 2) | cmdFixFlag
	cmdFlt64Type   = (0x0005 << 2) | cmdFixFlag
	cmdNulType     = (0x0006 << 2) | cmdFixFlag
	cmdIpv4Type    = (0x0008 << 2) | cmdFixFlag
	cmdIpv6Type    = (0x0009 << 2) | cmdFixFlag
	cmdBoolType    = (0x000a << 2) | cmdFixFlag
	cmdCharType    = (0x000b << 2) | cmdVarFlag
	cmdTextType    = (0x000c << 2) | cmdVarFlag
	cmdClobType    = (0x000d << 2) | cmdVarFlag
	cmdBlobType    = (0x000e << 2) | cmdVarFlag
	cmdJSONType    = (0x000f << 2) | cmdVarFlag
	cmdBinaryType  = (0x0018 << 2) | cmdVarFlag
	cmdIPNetType   = (0x0019 << 2) | cmdVarFlag
	cmdUInt16Type  = (0x001a << 2) | cmdFixFlag
	cmdUInt32Type  = (0x001b << 2) | cmdFixFlag
	cmdUInt64Type  = (0x001c << 2) | cmdFixFlag
	cmdDecimalType = (0x0021 << 2) | cmdFixFlag
)

const (
	cmiOKResult      uint64 = 0x724f4b5f00000000
	cmiCMErrorResult uint64 = 0x72434d5f00000000
	cmiLastResult    uint64 = 0x724c535400000000
)

const (
	shortNull    = int16(-32768)
	ushortNull   = uint16(0xffff)
	intNull      = int32(-2147483648)
	uintNull     = uint32(0xffffffff)
	longNull     = int64(-9223372036854775808)
	ulongNull    = uint64(0xffffffffffffffff)
	floatNull    = float32(3.402823466e+38)
	doubleNull   = float64(1.7976931348623158e+308)
	datetimeNull = uint64(0xffffffffffffffff)
)

const (
	sqlParamInput byte = 1
)

const (
	defaultConnectTimeout  = 5 * time.Second
	defaultQueryTimeout    = 60 * time.Second
	defaultReadBufferSize  = 128 * 1024
	defaultWriteBufferSize = 128 * 1024
)

func align8(v int) int {
	return (v + 7) &^ 7
}

type StmtType int

func (typ StmtType) IsSelect() bool       { return typ == 512 }
func (typ StmtType) IsDDL() bool          { return typ >= 1 && typ <= 255 }
func (typ StmtType) IsAlterSystem() bool  { return typ >= 256 && typ <= 511 }
func (typ StmtType) IsInsert() bool       { return typ == 513 }
func (typ StmtType) IsDelete() bool       { return typ >= 514 && typ <= 518 }
func (typ StmtType) IsInsertSelect() bool { return typ == 519 }
func (typ StmtType) IsUpdate() bool       { return typ == 520 }
func (typ StmtType) IsExecRollup() bool   { return typ >= 522 && typ <= 524 }

const (
	QPP_STMT_TYPE_CREATE_TABLESPACE StmtType = iota + 1
	QPP_STMT_TYPE_DROP_TABLESPACE
	QPP_STMT_TYPE_ALTER_TABLESPACE_MODIFY_DATADISK
	QPP_STMT_TYPE_CREATE_TABLE
	QPP_STMT_TYPE_DROP_TABLE
	QPC_STMT_TYPE_CREATE_ROLLUP
	QPC_STMT_TYPE_DROP_ROLLUP
	QPC_STMT_TYPE_CREATE_RETENTION
	QPC_STMT_TYPE_DROP_RETENTION
	QPP_STMT_TYPE_CREATE_INDEX
	QPP_STMT_TYPE_DROP_INDEX
	QPP_STMT_TYPE_CREATE_USER
	QPP_STMT_TYPE_DROP_USER
	QPP_STMT_TYPE_ALTER_USER
	QPP_STMT_TYPE_GRANT_USER
	QPP_STMT_TYPE_REVOKE_USER
	QPP_STMT_TYPE_ALTER_TABLE_ADD_COL
	QPP_STMT_TYPE_ALTER_TABLE_DROP_COL
	QPP_STMT_TYPE_ALTER_TABLE_RENAME_COL
	QPP_STMT_TYPE_ALTER_TABLE_MODIFY_COL
	QPC_STMT_TYPE_ALTER_TABLE_AUTO_DEL_UNUSED
	QPP_STMT_TYPE_ALTER_TABLE_RENAME
	QPC_STMT_TYPE_ALTER_TABLE_ADD_RETENTION
	QPC_STMT_TYPE_ALTER_TABLE_DROP_RETENTION
	QPP_STMT_TYPE_TRUNCATE_TABLE
	QPP_STMT_TYPE_ALTER_TABLE_SET_PROP
	QPP_STMT_TYPE_ALTER_INDEX
	QPP_STMT_TYPE_CREATE_VIEW
	QPP_STMT_TYPE_DROP_VIEW
)

const (
	// backup relative
	QPP_STMT_TYPE_BACKUP_DATABASE StmtType = iota + 100
	QPP_STMT_TYPE_RESTORE_DATABASE
	QPP_STMT_TYPE_MOUNT_DATABASE
	QPP_STMT_TYPE_UMOUNT_DATABASE
	QPP_STMT_TYPE_MOUNT_TABLE
	QPP_STMT_TYPE_UMOUNT_TABLE
	QPP_STMT_TYPE_BACKUP_TABLE
	// incremental backups
	QPP_STMT_TYPE_INC_BACKUP_DATABASE
	QPP_STMT_TYPE_INC_BACKUP_TABLE
)

const (
	QPP_STMT_TYPE_CONNECT_USER StmtType = iota + 256
	QPP_STMT_TYPE_ALTER_SYSTEM_KILL_SESSION
	QPP_STMT_TYPE_ALTER_SYSTEM_CANCEL_SESSION
	QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_AGER
	QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_RESULT_CACHE
	QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_PVO_CACHE
	QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_SYS_STAT
	QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_PAGE_CACHE
	QPP_STMT_TYPE_ALTER_SYSTEM_FLUSH_KV_CACHE
	QPP_STMT_TYPE_ALTER_SYSTEM_CHECK_DISK_USAGE
	QPP_STMT_TYPE_ALTER_SYSTEM_SWIPE
	QPP_STMT_TYPE_ALTER_SYSTEM_INSTALL
	QPP_STMT_TYPE_ALTER_SYSTEM_CHECKPOINT
	QPP_STMT_TYPE_ALTER_SYSTEM_SET
	QPP_STMT_TYPE_ALTER_SYSTEM_UNSET
	QPP_STMT_TYPE_ALTER_SESSION_SET
	QPP_STMT_TYPE_COMMIT
	QPP_STMT_TYPE_ROLLBACK
	QPP_STMT_TYPE_TABLE_FLUSH
	QPP_STMT_TYPE_INDEX_FLUSH
	QPP_STMT_TYPE_TABLE_REFRESH
	QPP_STMT_TYPE_TABLE_FREEZE_TAG_INDEX
	QPP_STMT_TYPE_TABLE_UNFREEZE_TAG_INDEX
)

const (
	QPP_STMT_TYPE_CREATE_DATABASE StmtType = iota + 279
	QPP_STMT_TYPE_DROP_DATABASE
	QPP_STMT_TYPE_ALTER_DATABASE
)

const (
	QPP_STMT_TYPE_CREATE_CQL StmtType = iota + 490
	QPP_STMT_TYPE_DROP_CQL
	QPP_STMT_TYPE_START_CQL
	QPP_STMT_TYPE_STOP_CQL
	QPP_STMT_TYPE_SEEK_CQL
	QPP_STMT_TYPE_SUBSCRIBE_CQL
	QPP_STMT_TYPE_RUN_CQL
)

const (
	// DML
	QPP_STMT_TYPE_SELECT StmtType = iota + 512
	QPP_STMT_TYPE_INSERT
	QPP_STMT_TYPE_DELETE
	QPP_STMT_TYPE_DELETE_WHERE
	QPP_STMT_TYPE_DELETE_ROLLUP
	QPP_STMT_TYPE_DELETE_WHERE_ROLLUP
	QPP_STMT_TYPE_PARTIAL_DELETE_ROLLUP
	QPP_STMT_TYPE_INSERT_SELECT
	QPP_STMT_TYPE_UPDATE
	QPP_STMT_TYPE_DESC
)
const (
	// load
	QPP_STMT_TYPE_LOAD_INFILE StmtType = iota + 600
)

const (
	// ROLLUP
	QPP_STMT_TYPE_START_ALL_ROLLUP StmtType = iota + 1000
	QPP_STMT_TYPE_STOP_ALL_ROLLUP
	QPP_STMT_TYPE_COLLECT_ALL_ROLLUP
	QPP_STMT_TYPE_SET_ROLLUP_WAKEUP
	QPP_STMT_TYPE_WAKEUP_ROLLUP
)

const (
	// snapshot

	QPP_STMT_TYPE_ALTER_SYSTEM_SNAPSHOT StmtType = iota + 1100
	QPP_STMT_TYPE_ALTER_SYSTEM_RECOVER
	QPP_STMT_TYPE_ALTER_SYSTEM_ID
)

const (
	// procedure
	QPP_STMT_TYPE_EXECUTE_PROCEDURE StmtType = iota + 1200
	QPP_STMT_TYPE_ROLLUP_REBUILD
)

const (
	QPC_STMT_TYPE_APPEND StmtType = iota + 2000
)

type ParamDesc struct {
	Type        api.SqlType
	Precision   int
	Scale       int
	Nullable    bool
	Nullability api.Nullability
	Ordinal     int
	Name        string
}

type StatusError struct {
	code int
	msg  string
}

func (e *StatusError) Error() string {
	if e.msg == "" {
		return fmt.Sprintf("server error code=%d", e.code)
	}
	if e.code > 0 {
		return fmt.Sprintf("server error code=%d message=%s", e.code, e.msg)
	}
	return e.msg
}

func (st *StatusError) setErr(err error) {
	if st == nil {
		return
	}
	if err == nil {
		st.code = 0
		st.msg = ""
		return
	}
	var se *StatusError
	if errors.As(err, &se) {
		st.code = se.code
		st.msg = se.msg
		if st.msg == "" {
			st.msg = err.Error()
		}
		return
	}
	st.code = 0
	st.msg = err.Error()
}

func protocolVersion() uint64 {
	return (uint64(cmiProtocolMajor&0xffff) << 48) |
		(uint64(cmiProtocolMinor&0xffff) << 32) |
		uint64(cmiProtocolFix&0xffffffff)
}

func makeClientErr(msg string) error {
	if msg == "" {
		msg = "unknown client error"
	}
	return &StatusError{code: 0, msg: msg}
}

func makeServerErr(code int, msg string) error {
	return &StatusError{code: code, msg: msg}
}

func sqlTypeToCmdType(sqlType api.SqlType) int {
	switch sqlType {
	case api.SqlTypeInt16:
		return cmdInt16Type
	case api.SqlTypeUInt16:
		return cmdUInt16Type
	case api.SqlTypeInt32:
		return cmdInt32Type
	case api.SqlTypeUInt32:
		return cmdUInt32Type
	case api.SqlTypeInt64:
		return cmdInt64Type
	case api.SqlTypeUInt64:
		return cmdUInt64Type
	case api.SqlTypeDatetime:
		return cmdDateType
	case api.SqlTypeFloat:
		return cmdFlt32Type
	case api.SqlTypeDouble:
		return cmdFlt64Type
	case api.SqlTypeIPv4:
		return cmdIpv4Type
	case api.SqlTypeIPv6:
		return cmdIpv6Type
	case api.SqlTypeBinary:
		return cmdBinaryType
	case api.SqlTypeJSON:
		return cmdJSONType
	case api.SqlTypeDecimal:
		return cmdDecimalType
	default:
		return cmdVarcharType
	}
}

func spinerTypeToSqlType(spinerType int) api.SqlType {
	switch spinerType {
	case cmdBoolType, cmdInt16Type:
		return api.SqlTypeInt16
	case cmdUInt16Type:
		return api.SqlTypeUInt16
	case cmdInt32Type:
		return api.SqlTypeInt32
	case cmdUInt32Type:
		return api.SqlTypeUInt32
	case cmdInt64Type:
		return api.SqlTypeInt64
	case cmdUInt64Type:
		return api.SqlTypeUInt64
	case cmdDateType:
		return api.SqlTypeDatetime
	case cmdFlt32Type:
		return api.SqlTypeFloat
	case cmdFlt64Type:
		return api.SqlTypeDouble
	case cmdIpv4Type:
		return api.SqlTypeIPv4
	case cmdIpv6Type:
		return api.SqlTypeIPv6
	case cmdBinaryType, cmdBlobType:
		return api.SqlTypeBinary
	case cmdJSONType:
		return api.SqlTypeJSON
	case cmdDecimalType:
		return api.SqlTypeDecimal
	default:
		return api.SqlTypeString
	}
}

func inferStmtType(sql string) StmtType {
	t := strings.ToUpper(stripLeadingSQLComments(sql))
	if t == "" {
		return 0
	}
	parts := strings.Fields(t)
	if len(parts) == 0 {
		return 0
	}
	head := parts[0]
	switch head {
	case "SELECT":
		return 512
	case "INSERT":
		if strings.Contains(t, "SELECT") {
			return 519
		}
		return 513
	case "DELETE":
		return 514
	case "UPDATE":
		return 520
	case "ALTER":
		if strings.HasPrefix(t, "ALTER SYSTEM") {
			return 256
		}
		return 1
	case "CREATE", "DROP", "TRUNCATE":
		return 1
	case "EXEC":
		return 522
	case "BEGIN":
		return 489
	default:
		return 0
	}
}

func stripLeadingSQLComments(sql string) string {
	t := strings.TrimSpace(sql)
	for t != "" {
		if strings.HasPrefix(t, "--") {
			idx := strings.IndexByte(t, '\n')
			if idx < 0 {
				return ""
			}
			t = strings.TrimSpace(t[idx+1:])
			continue
		}
		if strings.HasPrefix(t, "/*") {
			idx := strings.Index(t, "*/")
			if idx < 0 {
				return ""
			}
			t = strings.TrimSpace(t[idx+2:])
			continue
		}
		break
	}
	return t
}

func isVariableSpinerType(spinerType int) bool {
	return (spinerType & cmdVarFlag) == cmdVarFlag
}

func computeColumnLength(spinerType int, precision int) int {
	switch spinerType {
	case cmdInt16Type, cmdUInt16Type:
		return 2
	case cmdInt32Type, cmdUInt32Type:
		return 4
	case cmdInt64Type, cmdUInt64Type:
		return 8
	case cmdFlt32Type:
		return 4
	case cmdFlt64Type:
		return 8
	case cmdDateType:
		return 8
	case cmdIpv4Type:
		return 5
	case cmdIpv6Type:
		return 17
	case cmdBoolType:
		return 2
	case cmdNulType:
		return 0
	case cmdDecimalType:
		if size, err := decimalSize(precision); err == nil {
			return size
		}
		return 0
	default:
		return precision
	}
}

func extractSpinerType(cmType uint64) int {
	return int((cmType >> 56) & 0xff)
}

func extractPrecision(cmType uint64) int {
	return int((cmType >> 28) & 0x0fffffff)
}

func extractScale(cmType uint64, v403 bool) int {
	if v403 {
		return int((cmType >> 23) & 0x1f)
	}
	return int(cmType & 0x0fffffff)
}

func extractNullability(cmType uint64, v403 bool) api.Nullability {
	if !v403 {
		return api.NullabilityUnknown
	}
	flags := cmType & 0x007fffff
	if flags&0x4 != 0 {
		return api.NullabilityUnknown
	}
	if flags&0x2 != 0 {
		return api.NullabilityNullable
	}
	return api.NullabilityNoNulls
}

func extractPrimaryKey(cmType uint64, v403 bool) bool {
	return v403 && cmType&0x1 != 0
}

func statusCode(v uint64) uint64 {
	return v & 0xffffffff00000000
}

func statusErrNo(v uint64) int {
	return int(v & 0xffffffff)
}
