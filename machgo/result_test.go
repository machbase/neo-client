package machgo

import "testing"

func TestResultLastInsertIDPreservesUint64Bits(t *testing.T) {
	for _, value := range []uint64{0, 1, 0x8000000000000000, 0xfffffffffffffffe} {
		result := &Result{rowID: value, hasRowID: true}
		got, err := result.LastInsertId()
		if err != nil {
			t.Fatalf("LastInsertId(%#x) error = %v", value, err)
		}
		if uint64(got) != value {
			t.Fatalf("LastInsertId(%#x) bits = %#x", value, uint64(got))
		}
	}

	if _, err := (&Result{}).LastInsertId(); err == nil {
		t.Fatal("LastInsertId() without generated ROWID unexpectedly succeeded")
	}
}
