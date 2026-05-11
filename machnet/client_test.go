package machnet

import "testing"

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
