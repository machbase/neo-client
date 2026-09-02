package machnet

import "testing"

func TestSameAppendBindingNamesTracksShape(t *testing.T) {
	if !sameAppendBindingNames(
		[]string{"_ARRIVAL_TIME", "id", `"a"[2]`},
		[]string{"_arrival_time", `"ID"`, "A[2]"},
	) {
		t.Fatal("equivalent append binding names were treated as a new shape")
	}
	if sameAppendBindingNames(
		[]string{"_ARRIVAL_TIME", "ID", "A[2]"},
		[]string{"_ARRIVAL_TIME", "A[2]", "ID"},
	) {
		t.Fatal("reordered append binding names reused stale bindings")
	}
	if sameAppendBindingNames(
		[]string{"_ARRIVAL_TIME", "ID", "A[2]"},
		[]string{"_ARRIVAL_TIME", "ID"},
	) {
		t.Fatal("shorter append binding names reused stale bindings")
	}
}
