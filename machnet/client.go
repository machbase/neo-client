package machnet

import (
	"bufio"
	"crypto"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/machbase/neo-client/api"
)

type NativeConn struct {
	mu      sync.Mutex
	netConn net.Conn
	br      *bufio.Reader
	bw      *bufio.Writer
	ioBytes *ioByteCounter
	packet  Packet

	host          string
	port          int
	user          string
	password      string
	authMode      string
	authKey       crypto.PrivateKey
	authSigScheme string
	queryTimeout  time.Duration
	fetchRows     int64

	sessionID     uint64
	serverEndian  uint32
	serverVersion uint64
	closed        bool

	stmtMu      sync.Mutex
	stmtCursor  uint32
	stmtUsed    [stmtIDLimit]bool
	stmtUsedCnt int
}

const stmtIDLimit = 1024
const defaultFetchRows int64 = 1000

type StmtExecResult struct {
	stmtType   StmtType
	message    string
	rowCount   int64
	rowID      uint64
	hasRowID   bool
	columns    []ColumnMeta
	paramDesc  []ParamDesc
	rows       [][]any
	lastResult bool
}

func readUIntLE(data []byte) (uint64, bool) {
	switch {
	case len(data) >= 8:
		return binary.LittleEndian.Uint64(data[:8]), true
	case len(data) >= 4:
		return uint64(binary.LittleEndian.Uint32(data[:4])), true
	case len(data) >= 2:
		return uint64(binary.LittleEndian.Uint16(data[:2])), true
	case len(data) >= 1:
		return uint64(data[0]), true
	default:
		return 0, false
	}
}

func countSQLPlaceholders(sql string) int {
	if sql == "" {
		return 0
	}
	cnt := 0
	inSingle := false
	inDouble := false
	for i := 0; i < len(sql); i++ {
		ch := sql[i]
		if inSingle {
			if ch == '\'' {
				if i+1 < len(sql) && sql[i+1] == '\'' {
					i++
					continue
				}
				inSingle = false
			}
			continue
		}
		if inDouble {
			if ch == '"' {
				inDouble = false
			}
			continue
		}
		switch ch {
		case '\'':
			inSingle = true
		case '"':
			inDouble = true
		case '?':
			cnt++
		}
	}
	return cnt
}

func dialNative(host string, port int, user string, password string, authMode string, key crypto.PrivateKey, authSigScheme string, alts []net.TCPAddr, fetchRows int64, trackIOBytes bool) (*NativeConn, error) {
	if fetchRows <= 0 {
		fetchRows = defaultFetchRows
	}
	endpoints := make([]string, 0, 1+len(alts))
	endpoints = append(endpoints, fmt.Sprintf("%s:%d", host, port))
	for _, alt := range alts {
		h := alt.IP.String()
		if h == "<nil>" || h == "" {
			continue
		}
		endpoints = append(endpoints, fmt.Sprintf("%s:%d", h, alt.Port))
	}
	var lastErr error
	for _, ep := range endpoints {
		c, err := net.DialTimeout("tcp", ep, defaultConnectTimeout)
		if err != nil {
			lastErr = err
			continue
		}
		var counter *ioByteCounter
		var br *bufio.Reader
		var bw *bufio.Writer
		if trackIOBytes {
			counter = newIOByteCounter(true)
			br = bufio.NewReaderSize(&countingReader{r: c, counter: counter}, defaultReadBufferSize)
			bw = bufio.NewWriterSize(&countingWriter{w: c, counter: counter}, defaultWriteBufferSize)
		} else {
			br = bufio.NewReaderSize(c, defaultReadBufferSize)
			bw = bufio.NewWriterSize(c, defaultWriteBufferSize)
		}
		nc := &NativeConn{
			netConn:       c,
			br:            br,
			bw:            bw,
			ioBytes:       counter,
			host:          host,
			port:          port,
			user:          user,
			password:      password,
			authMode:      authMode,
			authKey:       key,
			authSigScheme: authSigScheme,
			queryTimeout:  defaultQueryTimeout,
			fetchRows:     fetchRows,
		}
		if err := nc.handshake(); err != nil {
			_ = c.Close()
			lastErr = err
			continue
		}
		if err := nc.connectProtocol(); err != nil {
			_ = c.Close()
			lastErr = err
			continue
		}
		return nc, nil
	}
	if lastErr == nil {
		lastErr = errors.New("connect failed")
	}
	return nil, lastErr
}

func (c *NativeConn) ioByteMetrics() (readBytes uint64, writtenBytes uint64, enabled bool) {
	if c == nil || c.ioBytes == nil {
		return 0, 0, false
	}
	r, w := c.ioBytes.snapshot()
	return r, w, c.ioBytes.isEnabled()
}

func (c *NativeConn) resetIOByteMetrics() {
	if c == nil || c.ioBytes == nil {
		return
	}
	c.ioBytes.reset()
}

func (c *NativeConn) close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil
	}
	c.closed = true
	if c.netConn != nil {
		return c.netConn.Close()
	}
	return nil
}

func (c *NativeConn) nextStmtID() (uint32, error) {
	c.stmtMu.Lock()
	defer c.stmtMu.Unlock()

	if c.stmtUsedCnt >= stmtIDLimit {
		return 0, makeClientErr(fmt.Sprintf("Statement ID overflow (Limit = %d, Curr = %d).", stmtIDLimit, c.stmtUsedCnt))
	}
	start := c.stmtCursor % stmtIDLimit
	for i := uint32(0); i < stmtIDLimit; i++ {
		candidate := (start + i) % stmtIDLimit
		if !c.stmtUsed[candidate] {
			c.stmtUsed[candidate] = true
			c.stmtUsedCnt++
			c.stmtCursor = (candidate + 1) % stmtIDLimit
			return candidate, nil
		}
	}
	return 0, makeClientErr(fmt.Sprintf("Statement ID overflow (Limit = %d, Curr = %d).", stmtIDLimit, c.stmtUsedCnt))
}

func (c *NativeConn) releaseStmtID(id uint32) {
	if id >= stmtIDLimit {
		return
	}
	c.stmtMu.Lock()
	defer c.stmtMu.Unlock()
	if c.stmtUsed[id] {
		c.stmtUsed[id] = false
		if c.stmtUsedCnt > 0 {
			c.stmtUsedCnt--
		}
	}
}

func (c *NativeConn) handshake() error {
	payload := []byte(cmiHandshakePayload)
	if len(payload) != cmiProtoCnt {
		return fmt.Errorf("invalid handshake payload size")
	}
	if defaultConnectTimeout > 0 {
		_ = c.netConn.SetWriteDeadline(time.Now().Add(defaultConnectTimeout))
		defer c.netConn.SetWriteDeadline(time.Time{})
		_ = c.netConn.SetReadDeadline(time.Now().Add(defaultConnectTimeout))
		defer c.netConn.SetReadDeadline(time.Time{})
	}
	if err := writePacket(c.bw, payload); err != nil {
		return err
	}
	if err := c.bw.Flush(); err != nil {
		return err
	}
	resp := make([]byte, cmiProtoCnt)
	if _, err := io.ReadFull(c.br, resp); err != nil {
		return err
	}
	if string(resp) != cmiHandshakeReady {
		return fmt.Errorf("handshake failed: %q", string(resp))
	}
	return nil
}

func (c *NativeConn) sendPackets(packets [][]byte, expected byte, timeout time.Duration) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, errors.New("connection closed")
	}
	if timeout > 0 {
		_ = c.netConn.SetWriteDeadline(time.Now().Add(timeout))
		defer c.netConn.SetWriteDeadline(time.Time{})
	}
	for _, p := range packets {
		if err := writePacket(c.bw, p); err != nil {
			return nil, err
		}
	}
	if err := c.bw.Flush(); err != nil {
		return nil, err
	}
	if err := c.packet.Read(c.br); err != nil {
		return nil, err
	}
	if c.packet.protocol != expected {
		return nil, fmt.Errorf("unexpected protocol %d expected %d", c.packet.protocol, expected)
	}
	return c.packet.body, nil
}

func (c *NativeConn) sendPacketsNoResponse(packets [][]byte, timeout time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("connection closed")
	}
	if timeout > 0 {
		_ = c.netConn.SetWriteDeadline(time.Now().Add(timeout))
		defer c.netConn.SetWriteDeadline(time.Time{})
		_ = c.netConn.SetReadDeadline(time.Now().Add(timeout))
		defer c.netConn.SetReadDeadline(time.Time{})
	}
	for _, p := range packets {
		if err := writePacket(c.bw, p); err != nil {
			return err
		}
	}
	if err := c.bw.Flush(); err != nil {
		return err
	}
	return nil
}

func (c *NativeConn) sendPacketsOptional(packets [][]byte, expected byte, writeTimeout, readTimeout time.Duration) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, false, errors.New("connection closed")
	}
	if writeTimeout > 0 {
		_ = c.netConn.SetWriteDeadline(time.Now().Add(writeTimeout))
		defer c.netConn.SetWriteDeadline(time.Time{})
	}
	for _, p := range packets {
		if err := writePacket(c.bw, p); err != nil {
			return nil, false, err
		}
	}
	if err := c.bw.Flush(); err != nil {
		return nil, false, err
	}
	if readTimeout > 0 {
		_ = c.netConn.SetReadDeadline(time.Now().Add(readTimeout))
		defer c.netConn.SetReadDeadline(time.Time{})
	}
	if err := c.packet.Read(c.br); err != nil {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			return nil, false, nil
		}
		return nil, false, err
	}
	if c.packet.protocol != expected {
		return nil, false, fmt.Errorf("unexpected protocol %d expected %d", c.packet.protocol, expected)
	}
	return c.packet.body, true, nil
}

func (c *NativeConn) connectProtocol() error {
	mode, sigScheme, err := finalizeAuthConnectOptions(c.authMode, c.authKey, c.authSigScheme)
	if err != nil {
		return err
	}

	w := newMarshalWriter(cmiConnectProtocol, 0, 0)
	w.addUInt64(cmiCVersionID, protocolVersion())
	w.addString(cmiCClientID, "CLI")
	w.addString(cmiCDatabaseID, "data")
	w.addString(cmiCUserID, c.user)
	if mode != authModeChallenge {
		w.addString(cmiCPasswordID, c.password)
	}
	if mode != "" {
		w.addString(cmiCAuthModeID, mode)
	}
	if sigScheme != "" {
		w.addString(cmiCAuthSigSchemeID, sigScheme)
	}
	w.addUInt64(cmiCTimeoutID, uint64(defaultQueryTimeout.Seconds()))
	w.addUInt32(cmiCSHCID, 0)
	if la, ok := c.netConn.LocalAddr().(*net.TCPAddr); ok && la.IP != nil {
		w.addString(cmiCIPID, la.IP.String())
	} else {
		w.addString(cmiCIPID, "127.0.0.1")
	}
	body, err := c.sendPackets(w.finalize(), cmiConnectProtocol, defaultConnectTimeout)
	if err != nil {
		return err
	}
	units, err := collectUnits(body)
	if err != nil {
		return err
	}
	result, ok := firstUnit(units, cmiRResultID)
	if !ok || len(result.data) < 8 {
		return errors.New("connect response missing result")
	}
	statusVal := binary.LittleEndian.Uint64(result.data)
	if statusCode(statusVal) != cmiOKResult {
		msg := ""
		if m, ok := firstUnit(units, cmiRMessageID); ok {
			msg = string(m.data)
		}
		return makeServerErr(statusErrNo(statusVal), msg)
	}
	if version, ok := firstUnit(units, cmiCVersionID); ok && len(version.data) >= 8 {
		c.serverVersion = binary.LittleEndian.Uint64(version.data[:8])
	}

	if mode == authModeChallenge {
		nonce, validMs, err := readChallengeFields(units)
		if err != nil {
			return err
		}
		_ = validMs
		signature, err := signAuthNonceWithKey(c.authKey, "AUTH_KEY", sigScheme, nonce)
		if err != nil {
			return err
		}
		w2 := newMarshalWriter(cmiConnectProtocol, 0, 0)
		w2.addUInt64(cmiCVersionID, protocolVersion())
		w2.addString(cmiCClientID, "CLI")
		w2.addString(cmiCDatabaseID, "data")
		w2.addString(cmiCUserID, c.user)
		w2.addString(cmiCAuthModeID, mode)
		w2.addString(cmiCAuthSigSchemeID, sigScheme)
		w2.addBinary(cmiCAuthSignatureID, signature)
		w2.addUInt64(cmiCTimeoutID, uint64(defaultQueryTimeout.Seconds()))
		w2.addUInt32(cmiCSHCID, 0)
		if la, ok := c.netConn.LocalAddr().(*net.TCPAddr); ok && la.IP != nil {
			w2.addString(cmiCIPID, la.IP.String())
		} else {
			w2.addString(cmiCIPID, "127.0.0.1")
		}
		body2, err := c.sendPackets(w2.finalize(), cmiConnectProtocol, defaultConnectTimeout)
		if err != nil {
			return err
		}
		units, err = collectUnits(body2)
		if err != nil {
			return err
		}
		result, ok = firstUnit(units, cmiRResultID)
		if !ok || len(result.data) < 8 {
			return errors.New("connect response missing result")
		}
		statusVal = binary.LittleEndian.Uint64(result.data)
		if statusCode(statusVal) != cmiOKResult {
			msg := ""
			if m, ok := firstUnit(units, cmiRMessageID); ok {
				msg = string(m.data)
			}
			return makeServerErr(statusErrNo(statusVal), msg)
		}
	}

	if sid, ok := firstUnit(units, cmiCSIDID); ok && len(sid.data) >= 8 {
		c.sessionID = binary.LittleEndian.Uint64(sid.data)
	}
	if version, ok := firstUnit(units, cmiCVersionID); ok && len(version.data) >= 8 {
		c.serverVersion = binary.LittleEndian.Uint64(version.data[:8])
	}
	if e, ok := firstUnit(units, cmiCEndianID); ok {
		switch {
		case len(e.data) >= 4:
			c.serverEndian = binary.LittleEndian.Uint32(e.data[:4])
		case len(e.data) >= 1:
			c.serverEndian = uint32(e.data[0])
		default:
			c.serverEndian = 0
		}
	}
	return nil
}

func (c *NativeConn) supportsV403() bool {
	return protocolVersion() >= cmiV403MetadataVersion && c.serverVersion >= cmiV403MetadataVersion
}

func (c *NativeConn) supportsGeneratedRowID() bool {
	return protocolVersion() >= cmiGeneratedRowIDVersion && c.serverVersion >= cmiGeneratedRowIDVersion
}

func parseStmtResponse(body []byte, sql string, fallbackCols []ColumnMeta) (*StmtExecResult, error) {
	return parseStmtResponseVersion(body, sql, fallbackCols, true, false, false)
}

func parseStmtResponseWithStmtTypeFallback(body []byte, sql string, fallbackCols []ColumnMeta, useStmtTypeFallback bool) (*StmtExecResult, error) {
	return parseStmtResponseVersion(body, sql, fallbackCols, useStmtTypeFallback, false, false)
}

func parseStmtResponseVersion(body []byte, sql string, fallbackCols []ColumnMeta, useStmtTypeFallback, v403, generatedRowID bool) (*StmtExecResult, error) {
	units, err := collectUnits(body)
	if err != nil {
		return nil, err
	}
	ret := &StmtExecResult{}
	if useStmtTypeFallback {
		ret.stmtType = inferStmtType(sql)
	}

	if m, ok := firstUnit(units, cmiRMessageID); ok {
		ret.message = string(m.data)
	}
	if rc, ok := firstUnit(units, cmiPRowsID); ok {
		if v, ok := readUIntLE(rc.data); ok {
			ret.rowCount = int64(v)
		}
	}
	if generatedRowID {
		if rowID, ok := firstUnit(units, cmiPGeneratedRowIDID); ok {
			if len(rowID.data) < 8 {
				return nil, fmt.Errorf("malformed generated ROWID metadata")
			}
			ret.rowID = binary.LittleEndian.Uint64(rowID.data[:8])
			ret.hasRowID = true
		}
	}
	if st, ok := firstUnit(units, cmimIDStmtType); ok {
		if len(st.data) >= 4 {
			ret.stmtType = StmtType(int32(binary.LittleEndian.Uint32(st.data[:4])))
		}
	}

	paramTypeUnits := units[cmiPParamTypeID]
	paramCount := len(paramTypeUnits)
	if binds, ok := firstUnit(units, cmiPBindsID); ok {
		if len(binds.data) < 8 {
			return nil, fmt.Errorf("malformed parameter count metadata")
		}
		value := binary.LittleEndian.Uint64(binds.data[:8])
		if value > 0xffff {
			return nil, fmt.Errorf("parameter count exceeds protocol limit")
		}
		paramCount = int(value)
	}
	switch {
	case paramCount > 0:
		ret.paramDesc = buildParamDesc(units, paramCount, v403)
		if v403 {
			if err := applyParamMetadataV2(ret.paramDesc, units); err != nil {
				return nil, err
			}
		}
	default:
		qCount := countSQLPlaceholders(sql)
		if qCount > 0 {
			ret.paramDesc = make([]ParamDesc, qCount)
			for i := range ret.paramDesc {
				ret.paramDesc[i] = ParamDesc{Type: api.SqlTypeString, Nullability: api.NullabilityUnknown, Ordinal: i + 1}
			}
		}
	}

	ret.columns = buildColumns(units, v403)
	if len(ret.columns) == 0 && len(fallbackCols) > 0 {
		ret.columns = append([]ColumnMeta(nil), fallbackCols...)
	}
	if v := units[cmiFValueID]; len(v) > 0 && len(ret.columns) > 0 {
		rows, deErr := decodeRowsFromUnits(v, ret.columns)
		if deErr != nil {
			return nil, deErr
		}
		ret.rows = append(ret.rows, rows...)
	}

	if results := units[cmiRResultID]; len(results) > 0 {
		for _, result := range results {
			if len(result.data) < 8 {
				continue
			}
			statusVal := binary.LittleEndian.Uint64(result.data)
			st := statusCode(statusVal)
			if st == cmiLastResult {
				ret.lastResult = true
			}
			if st != cmiOKResult && st != cmiLastResult {
				errMsg := ""
				if em, ok := firstUnit(units, cmiREMessageID); ok {
					errMsg = string(em.data)
				}
				msg := ret.message
				if errMsg != "" {
					if msg == "" {
						msg = errMsg
					} else {
						msg = msg + "; " + errMsg
					}
				}
				return nil, makeServerErr(statusErrNo(statusVal), msg)
			}
		}
	}
	return ret, nil
}

func (c *NativeConn) fetchRowsChunk(stmtID uint32, columns []ColumnMeta, fetchRows int64) ([][]any, bool, error) {
	if fetchRows <= 0 {
		fetchRows = c.fetchRows
		if fetchRows <= 0 {
			fetchRows = defaultFetchRows
		}
	}
	w := newMarshalWriter(cmiFetchProtocol, stmtID, 0)
	w.addUInt32(cmiFIDID, stmtID)
	w.addSInt64(cmiFRowsID, fetchRows)
	body, err := c.sendPackets(w.finalize(), cmiFetchProtocol, c.queryTimeout)
	if err != nil {
		return nil, false, err
	}
	units, err := collectUnits(body)
	if err != nil {
		return nil, false, err
	}

	last := false
	if results := units[cmiRResultID]; len(results) > 0 {
		for _, result := range results {
			if len(result.data) < 8 {
				continue
			}
			statusVal := binary.LittleEndian.Uint64(result.data)
			st := statusCode(statusVal)
			if st == cmiLastResult {
				last = true
			}
			if st != cmiOKResult && st != cmiLastResult {
				msg := ""
				if m, ok := firstUnit(units, cmiRMessageID); ok {
					msg = string(m.data)
				}
				return nil, false, makeServerErr(statusErrNo(statusVal), msg)
			}
		}
	}

	var rows [][]any
	if vals := units[cmiFValueID]; len(vals) > 0 {
		decoded, deErr := decodeRowsFromUnits(vals, columns)
		if deErr != nil {
			return nil, false, deErr
		}
		rows = append(rows, decoded...)
	}

	// Some server paths can return an empty fetch block without explicit LAST.
	if !last && len(rows) == 0 {
		if r, ok := firstUnit(units, cmiFRowsID); ok {
			if v, ok := readUIntLE(r.data); ok && int64(v) == 0 {
				last = true
			}
		}
	}

	return rows, last, nil
}

func (c *NativeConn) execDirect(stmtID uint32, sql string) (*StmtExecResult, error) {
	w := newMarshalWriter(cmiExecDirectProtocol, stmtID, 0)
	w.addString(cmiDStatementID, sql)
	w.addUInt64(cmiPIDID, uint64(stmtID))
	w.addSInt64(cmiFRowsID, c.fetchRows)
	body, err := c.sendPackets(w.finalize(), cmiExecDirectProtocol, c.queryTimeout)
	if err != nil {
		return nil, err
	}
	ret, err := parseStmtResponseVersion(body, sql, nil, true, c.supportsV403(), c.supportsGeneratedRowID())
	if err != nil {
		return nil, err
	}
	if ret.lastResult && ret.rowCount == 0 && len(ret.columns) > 0 {
		ret.rowCount = int64(len(ret.rows))
	}
	if ret.stmtType == 0 {
		ret.stmtType = inferStmtType(sql)
	}
	return ret, nil
}

func (c *NativeConn) prepare(stmtID uint32, sql string) (*StmtExecResult, error) {
	w := newMarshalWriter(cmiPrepareProtocol, stmtID, 0)
	w.addUInt64(cmiPIDID, uint64(stmtID))
	w.addString(cmiPStatementID, sql)
	body, err := c.sendPackets(w.finalize(), cmiPrepareProtocol, c.queryTimeout)
	if err != nil {
		return nil, err
	}
	ret, err := parseStmtResponseVersion(body, sql, nil, true, c.supportsV403(), c.supportsGeneratedRowID())
	if err != nil {
		return nil, err
	}
	if ret.stmtType == 0 {
		ret.stmtType = inferStmtType(sql)
	}
	return ret, nil
}

func (c *NativeConn) executePrepared(stmtID uint32, sql string, params []BoundParam, preparedCols []ColumnMeta) (*StmtExecResult, error) {
	w := newMarshalWriter(cmiExecuteProtocol, stmtID, 0)
	w.addUInt64(cmiPIDID, uint64(stmtID))
	w.addSInt64(cmiFRowsID, c.fetchRows)
	if len(params) > 0 {
		v403 := c.supportsV403()
		p, err := encodeParams(params, v403)
		if err != nil {
			return nil, err
		}
		if len(p) > 0 {
			if v403 {
				w.addBinary(cmiEParamV2ID, p)
			} else {
				w.addBinary(cmiEParamID, p)
			}
		}
	}
	body, err := c.sendPackets(w.finalize(), cmiExecuteProtocol, c.queryTimeout)
	if err != nil {
		return nil, err
	}
	ret, err := parseStmtResponseVersion(body, sql, preparedCols, false, c.supportsV403(), c.supportsGeneratedRowID())
	if err != nil {
		return nil, err
	}
	if ret.lastResult && ret.rowCount == 0 && len(ret.columns) > 0 {
		ret.rowCount = int64(len(ret.rows))
	}
	return ret, nil
}

func (c *NativeConn) free(stmtID uint32) error {
	w := newMarshalWriter(cmiFreeProtocol, stmtID, 0)
	w.addUInt64(cmiXIDID, uint64(stmtID))
	body, err := c.sendPackets(w.finalize(), cmiFreeProtocol, c.queryTimeout)
	if err != nil {
		if strings.Contains(err.Error(), "unexpected protocol") {
			return nil
		}
		return err
	}
	units, err := collectUnits(body)
	if err != nil {
		return err
	}
	if result, ok := firstUnit(units, cmiRResultID); ok && len(result.data) >= 8 {
		statusVal := binary.LittleEndian.Uint64(result.data)
		st := statusCode(statusVal)
		if st != cmiOKResult && st != cmiLastResult {
			msg := ""
			if m, ok := firstUnit(units, cmiRMessageID); ok {
				msg = string(m.data)
			}
			return makeServerErr(statusErrNo(statusVal), msg)
		}
	}
	return nil
}

func (c *NativeConn) appendOpen(stmtID uint32, table string, errCheckCount int) (*StmtExecResult, error) {
	_ = errCheckCount
	w := newMarshalWriter(cmiAppendOpenProtocol, stmtID, 0)
	w.addUInt64(cmiPIDID, uint64(stmtID))
	w.addString(cmiPTableID, table)
	w.addUInt64(cmiEEndianID, 0)
	body, err := c.sendPackets(w.finalize(), cmiAppendOpenProtocol, c.queryTimeout)
	if err != nil {
		return nil, err
	}
	ret, err := parseStmtResponseVersion(body, "APPEND "+table, nil, true, c.supportsV403(), false)
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func parseAppendDataResponse(body []byte) error {
	units, err := collectUnits(body)
	if err != nil {
		return err
	}
	if results := units[cmiRResultID]; len(results) > 0 {
		for _, result := range results {
			if len(result.data) < 8 {
				continue
			}
			statusVal := binary.LittleEndian.Uint64(result.data)
			st := statusCode(statusVal)
			if st != cmiOKResult && st != cmiLastResult {
				msg := ""
				if m, ok := firstUnit(units, cmiRMessageID); ok {
					msg = string(m.data)
				}
				if em, ok := firstUnit(units, cmiREMessageID); ok {
					if msg == "" {
						msg = string(em.data)
					} else {
						msg += "; " + string(em.data)
					}
				}
				return makeServerErr(statusErrNo(statusVal), msg)
			}
		}
	}
	if fail, ok := firstUnit(units, cmiXAppendFailureID); ok && len(fail.data) >= 8 {
		failCnt := binary.LittleEndian.Uint64(fail.data[:8])
		if failCnt > 0 {
			msg := ""
			if m, ok := firstUnit(units, cmiRMessageID); ok {
				msg = string(m.data)
			}
			if msg == "" {
				msg = fmt.Sprintf("append data failed rows=%d", failCnt)
			}
			return makeServerErr(0, msg)
		}
	}
	return nil
}

func (c *NativeConn) appendData(stmtID uint32, rows [][]byte, checkResponse bool) error {
	if len(rows) == 0 {
		return nil
	}
	w := newMarshalWriter(cmiAppendDataProtocol, stmtID, uint16(stmtID&0xffff))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		w.addBinary(cmiPRowsID, row)
	}
	packets := w.finalize()
	if !checkResponse {
		return c.sendPacketsNoResponse(packets, c.queryTimeout)
	}
	// CAUTION: This is a short timeout for low latency append. But prone to cause false-timeout.
	timeout := 5 * time.Millisecond
	if c.queryTimeout > 0 && timeout > c.queryTimeout {
		timeout = c.queryTimeout
	}
	body, ok, err := c.sendPacketsOptional(packets, cmiAppendDataProtocol, c.queryTimeout, timeout)
	if err != nil {
		return err
	}
	if !ok || len(body) == 0 {
		return nil
	}
	return parseAppendDataResponse(body)
}

func parseAppendCloseResponse(body []byte) (int64, int64, error) {
	units, err := collectUnits(body)
	if err != nil {
		return 0, 0, err
	}
	if result, ok := firstUnit(units, cmiRResultID); ok && len(result.data) >= 8 {
		statusVal := binary.LittleEndian.Uint64(result.data)
		st := statusCode(statusVal)
		if st != cmiOKResult && st != cmiLastResult {
			msg := ""
			if m, ok := firstUnit(units, cmiRMessageID); ok {
				msg = string(m.data)
			}
			return 0, 0, makeServerErr(statusErrNo(statusVal), msg)
		}
	}
	var success int64
	var fail int64
	if v, ok := firstUnit(units, cmiXAppendSuccessID); ok && len(v.data) >= 8 {
		success = int64(binary.LittleEndian.Uint64(v.data))
	}
	if v, ok := firstUnit(units, cmiXAppendFailureID); ok && len(v.data) >= 8 {
		fail = int64(binary.LittleEndian.Uint64(v.data))
	}
	return success, fail, nil
}

func (c *NativeConn) appendClose(stmtID uint32) (int64, int64, error) {
	w := newMarshalWriter(cmiAppendCloseProtocol, stmtID, 0)
	w.addUInt64(cmiPIDID, uint64(stmtID))
	packets := w.finalize()

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return 0, 0, errors.New("connection closed")
	}

	if c.queryTimeout > 0 {
		_ = c.netConn.SetWriteDeadline(time.Now().Add(c.queryTimeout))
		defer c.netConn.SetWriteDeadline(time.Time{})
	}
	for _, p := range packets {
		if err := writePacket(c.bw, p); err != nil {
			return 0, 0, err
		}
	}
	if err := c.bw.Flush(); err != nil {
		return 0, 0, err
	}

	for {
		if c.queryTimeout > 0 {
			_ = c.netConn.SetReadDeadline(time.Now().Add(c.queryTimeout))
		}
		err := c.packet.Read(c.br)
		if c.queryTimeout > 0 {
			c.netConn.SetReadDeadline(time.Time{})
		}

		if err != nil {
			return 0, 0, err
		}
		switch c.packet.protocol {
		case cmiAppendDataProtocol:
			if err := parseAppendDataResponse(c.packet.body); err != nil {
				return 0, 0, err
			}
		case cmiAppendCloseProtocol:
			return parseAppendCloseResponse(c.packet.body)
		default:
			return 0, 0, fmt.Errorf("unexpected protocol %d expected %d", c.packet.protocol, cmiAppendCloseProtocol)
		}
	}
}
