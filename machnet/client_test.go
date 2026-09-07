package machnet

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func buildStmtResponseBodyForTest(t *testing.T, stmtType StmtType, includeStmtType bool) []byte {
	t.Helper()

	w := newMarshalWriter(0, 0, 0)
	w.addUInt64(cmiRResultID, cmiOKResult)
	if includeStmtType {
		w.addSInt32(cmimIDStmtType, int32(stmtType))
	}
	w.flushCurrent()
	if len(w.bodies) != 1 {
		t.Fatalf("unexpected response body count: got %d, want 1", len(w.bodies))
	}
	return append([]byte(nil), w.bodies[0]...)
}

func buildGeneratedRowIDResponseBodyForTest(t *testing.T, rowID uint64) []byte {
	t.Helper()

	w := newMarshalWriter(0, 0, 0)
	w.addUInt64(cmiRResultID, cmiOKResult)
	w.addUInt64(cmiPGeneratedRowIDID, rowID)
	w.flushCurrent()
	if len(w.bodies) != 1 {
		t.Fatalf("unexpected response body count: got %d, want 1", len(w.bodies))
	}
	return append([]byte(nil), w.bodies[0]...)
}

func TestParseGeneratedRowIDVersionGateAndBits(t *testing.T) {
	for _, value := range []uint64{0, 1, 0x8000000000000000, 0xfffffffffffffffe} {
		body := buildGeneratedRowIDResponseBodyForTest(t, value)
		modern, err := parseStmtResponseVersion(body, "INSERT", nil, true, true, true)
		if err != nil {
			t.Fatalf("parse modern generated ROWID %#x: %v", value, err)
		}
		if !modern.hasRowID || modern.rowID != value {
			t.Fatalf("modern generated ROWID = (%#x, %v), want (%#x, true)", modern.rowID, modern.hasRowID, value)
		}

		legacy, err := parseStmtResponseVersion(body, "INSERT", nil, true, true, false)
		if err != nil {
			t.Fatalf("parse legacy generated ROWID %#x: %v", value, err)
		}
		if legacy.hasRowID || legacy.rowID != 0 {
			t.Fatalf("legacy decoder exposed generated ROWID (%#x, %v)", legacy.rowID, legacy.hasRowID)
		}
	}

	body := buildStmtResponseBodyForTest(t, 0, false)
	result, err := parseStmtResponseVersion(body, "SELECT", nil, true, true, true)
	if err != nil {
		t.Fatalf("parse missing generated ROWID: %v", err)
	}
	if result.hasRowID {
		t.Fatal("missing generated ROWID metadata was reported present")
	}
}

func TestGeneratedRowIDVersionGate(t *testing.T) {
	if got := protocolVersion(); got != cmiArrayVersion {
		t.Fatalf("client protocol version = %#x, want %#x", got, cmiArrayVersion)
	}

	legacy := &NativeConn{serverVersion: (4 << 48) | 2}
	if legacy.supportsGeneratedRowID() {
		t.Fatal("CMI 4.0.2 server must not advertise generated ROWID")
	}

	current := &NativeConn{serverVersion: cmiGeneratedRowIDVersion}
	if !current.supportsGeneratedRowID() {
		t.Fatal("CMI 4.0.3 server must advertise generated ROWID")
	}
}

func TestArrayVersionGate(t *testing.T) {
	legacy := &NativeConn{serverVersion: cmiGeneratedRowIDVersion}
	if legacy.supportsArray() {
		t.Fatal("CMI 4.0.3 server must not advertise ARRAY")
	}
	if _, err := legacy.appendOpen(1, "T", []string{"A[1]"}, 0); err == nil {
		t.Fatal("CMI 4.0.3 indexed append target was not rejected before send")
	}
	current := &NativeConn{serverVersion: cmiArrayVersion}
	if !current.supportsArray() {
		t.Fatal("CMI 4.0.4 server must advertise ARRAY")
	}
}

func TestParseStmtResponsePreparedExecuteDoesNotInferStmtType(t *testing.T) {
	body := buildStmtResponseBodyForTest(t, 0, false)

	res, err := parseStmtResponseWithStmtTypeFallback(body, "EXEC table_flush(tag_data)", nil, false)
	if err != nil {
		t.Fatalf("parseStmtResponseWithStmtTypeFallback() error = %v", err)
	}
	if res.stmtType != 0 {
		t.Fatalf("stmtType = %d, want 0", res.stmtType)
	}
	if res.stmtType.IsExecRollup() {
		t.Fatalf("stmtType %d should not be classified as rollup", res.stmtType)
	}
}

func TestParseStmtResponseDefaultInfersStmtType(t *testing.T) {
	body := buildStmtResponseBodyForTest(t, 0, false)

	res, err := parseStmtResponse(body, "EXEC table_flush(tag_data)", nil)
	if err != nil {
		t.Fatalf("parseStmtResponse() error = %v", err)
	}
	if res.stmtType != 522 {
		t.Fatalf("stmtType = %d, want 522", res.stmtType)
	}
}

func TestParseStmtResponseUsesServerStmtTypeWithoutFallback(t *testing.T) {
	body := buildStmtResponseBodyForTest(t, 274, true)

	res, err := parseStmtResponseWithStmtTypeFallback(body, "EXEC table_flush(tag_data)", nil, false)
	if err != nil {
		t.Fatalf("parseStmtResponseWithStmtTypeFallback() error = %v", err)
	}
	if res.stmtType != 274 {
		t.Fatalf("stmtType = %d, want 274", res.stmtType)
	}
	if res.stmtType.IsExecRollup() {
		t.Fatalf("stmtType %d should not be classified as rollup", res.stmtType)
	}
}

func TestSendPacketsOptionalUsesIndependentWriteAndReadDeadlines(t *testing.T) {
	client, server := net.Pipe()
	defer client.Close()

	readDone := make(chan error, 1)
	release := make(chan struct{})
	go func() {
		time.Sleep(25 * time.Millisecond)
		packet := make([]byte, packetHeaderSize)
		_, err := io.ReadFull(server, packet)
		readDone <- err
		<-release
		server.Close()
	}()
	defer close(release)

	conn := &NativeConn{
		netConn: client,
		br:      bufio.NewReader(client),
		bw:      bufio.NewWriter(client),
	}
	packet := buildPacket(cmiAppendDataProtocol, 1, 0, 0, nil)
	started := time.Now()
	body, ok, err := conn.sendPacketsOptional(context.Background(), [][]byte{packet}, cmiAppendDataProtocol, 200*time.Millisecond, 10*time.Millisecond)
	if err != nil {
		t.Fatalf("sendPacketsOptional() error = %v", err)
	}
	if ok || body != nil {
		t.Fatalf("sendPacketsOptional() = (%v, %v), want no optional response", body, ok)
	}
	if elapsed := time.Since(started); elapsed < 25*time.Millisecond {
		t.Fatalf("write returned before peer read: elapsed=%v", elapsed)
	}
	if err := <-readDone; err != nil {
		t.Fatalf("server read: %v", err)
	}
}

// TestSendPacketsContextCancellationAbortsBlockedRead exercises context
// cancellation without any machnet server mock: net.Pipe() supplies both
// ends of the "socket", and a goroutine that only drains writes (never
// replies) is enough to force sendPackets into a blocking Read that only
// context cancellation (via NativeConn.watchContext) can interrupt.
func TestSendPacketsContextCancellationAbortsBlockedRead(t *testing.T) {
	client, server := net.Pipe()
	defer server.Close()

	go func() {
		_, _ = io.Copy(io.Discard, server)
	}()

	conn := &NativeConn{
		netConn: client,
		br:      bufio.NewReader(client),
		bw:      bufio.NewWriter(client),
	}

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	packet := buildPacket(cmiExecDirectProtocol, 1, 0, 0, nil)
	started := time.Now()
	// timeout=0: no fixed deadline is set, so only ctx cancellation can
	// unblock the pending Read.
	_, err := conn.sendPackets(ctx, [][]byte{packet}, cmiExecDirectProtocol, 0)
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("sendPackets() error = nil, want context cancellation error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "connection closed") {
		t.Fatalf("sendPackets() error = %v, want it to mention connection closed", err)
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("sendPackets() error = %v, want context.Canceled in its chain", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("sendPackets() took %v, want a prompt return after ctx cancellation", elapsed)
	}
	if !conn.closed.Load() {
		t.Fatal("connection should be marked closed after a context-aborted I/O")
	}
}

func TestConnHandleSupportsDatabaseMetadata(t *testing.T) {
	if (*ConnHandle)(nil).SupportsDatabaseMetadata() {
		t.Fatal("nil connection reports database metadata support")
	}
	legacy := &ConnHandle{native: &NativeConn{serverVersion: cmiV403MetadataVersion - 1}}
	if legacy.SupportsDatabaseMetadata() {
		t.Fatal("legacy server reports database metadata support")
	}
	current := &ConnHandle{native: &NativeConn{serverVersion: cmiV403MetadataVersion}}
	if !current.SupportsDatabaseMetadata() {
		t.Fatal("CMI 4.0.3 server does not report database metadata support")
	}
}

// newPipeConn returns a NativeConn wired to one end of a net.Pipe, plus the
// peer end standing in for the machbase server.
func newPipeConn(t *testing.T) (*NativeConn, net.Conn) {
	t.Helper()
	client, server := net.Pipe()
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	conn := &NativeConn{
		netConn: client,
		br:      bufio.NewReader(client),
		bw:      bufio.NewWriter(client),
	}
	return conn, server
}

// replyWithProtocol drains one whole request from the peer end and answers with
// a packet carrying the given protocol id, which is how a desynchronized stream
// looks to the client. The request body must be drained too: net.Pipe is
// unbuffered, so a peer that stops reading mid-packet would deadlock the writer.
func replyWithProtocol(t *testing.T, server net.Conn, protocol byte) chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		var header [packetHeaderSize]byte
		_, _, _, _, bodyLen, err := readPacketHeader(server, &header)
		if err != nil {
			done <- err
			return
		}
		if bodyLen > 0 {
			if _, err := io.CopyN(io.Discard, server, int64(bodyLen)); err != nil {
				done <- err
				return
			}
		}
		_, err = server.Write(buildPacket(protocol, 1, 0, 0, nil))
		done <- err
	}()
	return done
}

func requireBroken(t *testing.T, conn *NativeConn, what string) {
	t.Helper()
	if !conn.closed.Load() {
		t.Fatalf("%s: connection was not marked broken", what)
	}
	if (&ConnHandle{native: conn}).IsOpen() {
		t.Fatalf("%s: ConnHandle.IsOpen() = true, want false so database/sql discards it", what)
	}
}

func TestSendPacketsProtocolMismatchMarksConnectionBroken(t *testing.T) {
	conn, server := newPipeConn(t)
	served := replyWithProtocol(t, server, cmiFreeProtocol)

	req := buildPacket(cmiExecDirectProtocol, 1, 0, 0, nil)
	_, err := conn.sendPackets(context.Background(), [][]byte{req}, cmiExecDirectProtocol, 0)
	if err == nil {
		t.Fatal("sendPackets() error = nil, want an unexpected protocol error")
	}
	if !strings.Contains(err.Error(), "unexpected protocol") {
		t.Fatalf("sendPackets() error = %v, want it to mention unexpected protocol", err)
	}
	if serveErr := <-served; serveErr != nil {
		t.Fatalf("peer: %v", serveErr)
	}
	requireBroken(t, conn, "sendPackets protocol mismatch")
}

func TestSendPacketsOptionalProtocolMismatchMarksConnectionBroken(t *testing.T) {
	conn, server := newPipeConn(t)
	served := replyWithProtocol(t, server, cmiFreeProtocol)

	req := buildPacket(cmiAppendDataProtocol, 1, 0, 0, nil)
	_, ok, err := conn.sendPacketsOptional(context.Background(), [][]byte{req}, cmiAppendDataProtocol, time.Second, time.Second)
	if err == nil {
		t.Fatal("sendPacketsOptional() error = nil, want an unexpected protocol error")
	}
	if ok {
		t.Fatal("sendPacketsOptional() reported a usable response for a mismatched protocol")
	}
	if serveErr := <-served; serveErr != nil {
		t.Fatalf("peer: %v", serveErr)
	}
	requireBroken(t, conn, "sendPacketsOptional protocol mismatch")
}

// free() intentionally reports success on a protocol mismatch because its
// caller (statement cleanup) has nothing to do with the error. The connection
// must still be taken out of service, otherwise the desynchronized stream is
// handed to the next borrower of the pooled connection.
func TestFreeSwallowsProtocolMismatchButMarksConnectionBroken(t *testing.T) {
	conn, server := newPipeConn(t)
	served := replyWithProtocol(t, server, cmiExecDirectProtocol)

	if err := conn.free(1); err != nil {
		t.Fatalf("free() error = %v, want nil", err)
	}
	if serveErr := <-served; serveErr != nil {
		t.Fatalf("peer: %v", serveErr)
	}
	requireBroken(t, conn, "free protocol mismatch")
}

// The fixed queryTimeout deadline is not derived from ctx, so ctx.Err() is nil
// when it fires; the connection must be retired all the same because the
// request was only partially written.
func TestSendPacketsWriteTimeoutMarksConnectionBroken(t *testing.T) {
	conn, _ := newPipeConn(t)

	req := buildPacket(cmiExecDirectProtocol, 1, 0, 0, make([]byte, 64*1024))
	_, err := conn.sendPackets(context.Background(), [][]byte{req}, cmiExecDirectProtocol, 20*time.Millisecond)
	if err == nil {
		t.Fatal("sendPackets() error = nil, want a write timeout")
	}
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("sendPackets() error = %v, want a timeout error", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "connection closed") {
		t.Fatalf("sendPackets() error = %v, want the original error so it is not promoted to driver.ErrBadConn", err)
	}
	requireBroken(t, conn, "sendPackets write timeout")
}

// The append fast path deliberately treats a read timeout as "no response yet"
// rather than a failure, so it must not retire the connection.
func TestSendPacketsOptionalReadTimeoutKeepsConnectionUsable(t *testing.T) {
	conn, server := newPipeConn(t)
	drained := make(chan error, 1)
	go func() {
		header := make([]byte, packetHeaderSize)
		_, err := io.ReadFull(server, header)
		drained <- err
	}()

	req := buildPacket(cmiAppendDataProtocol, 1, 0, 0, nil)
	body, ok, err := conn.sendPacketsOptional(context.Background(), [][]byte{req}, cmiAppendDataProtocol, time.Second, 20*time.Millisecond)
	if err != nil {
		t.Fatalf("sendPacketsOptional() error = %v, want nil", err)
	}
	if ok || body != nil {
		t.Fatalf("sendPacketsOptional() = (%v, %v), want no optional response", body, ok)
	}
	if drainErr := <-drained; drainErr != nil {
		t.Fatalf("peer: %v", drainErr)
	}
	if conn.closed.Load() {
		t.Fatal("append read timeout must not retire the connection")
	}
	if !(&ConnHandle{native: conn}).IsOpen() {
		t.Fatal("ConnHandle.IsOpen() = false after a benign append read timeout")
	}
}

func TestConnHandleIsOpenTracksNativeConn(t *testing.T) {
	if (*ConnHandle)(nil).IsOpen() {
		t.Fatal("nil connection reports open")
	}
	if (&ConnHandle{}).IsOpen() {
		t.Fatal("connection without a native socket reports open")
	}
	conn, _ := newPipeConn(t)
	handle := &ConnHandle{native: conn}
	if !handle.IsOpen() {
		t.Fatal("fresh connection reports closed")
	}
	conn.mu.Lock()
	conn.markBrokenLocked()
	conn.mu.Unlock()
	if handle.IsOpen() {
		t.Fatal("broken connection still reports open")
	}
}
