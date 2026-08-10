package machnet

import (
	"bufio"
	"io"
	"net"
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
	body, ok, err := conn.sendPacketsOptional([][]byte{packet}, cmiAppendDataProtocol, 200*time.Millisecond, 10*time.Millisecond)
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
