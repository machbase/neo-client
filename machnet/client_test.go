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
	if !conn.closed {
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
