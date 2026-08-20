package api

import "testing"

func TestTransactionColumnFlagValues(t *testing.T) {
	tests := []struct {
		name string
		got  ColumnFlag
		want ColumnFlag
		text string
	}{
		{name: "auto increment", got: ColumnFlagAutoIncrement, want: 0x00100000, text: "auto increment"},
		{name: "primary key", got: ColumnFlagPrimaryKey, want: 0x00400000, text: "primary key"},
		{name: "not null", got: ColumnFlagNotNull, want: 0x00800000, text: "not null"},
		{name: "default", got: ColumnFlagDefault, want: 0x40000000, text: "default"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Fatalf("column flag = %#x, want %#x", tc.got, tc.want)
			}
			if got := tc.got.String(); got != tc.text {
				t.Fatalf("ColumnFlag.String() = %q, want %q", got, tc.text)
			}
		})
	}
}

func TestTransactionAutoIncrementFlagCombination(t *testing.T) {
	flag := ColumnFlag(0x00D00000)

	for _, required := range []ColumnFlag{
		ColumnFlagAutoIncrement,
		ColumnFlagPrimaryKey,
		ColumnFlagNotNull,
	} {
		if flag&required != required {
			t.Fatalf("flag %#x does not contain %#x", flag, required)
		}
	}
	if flag&ColumnFlagDefault != 0 {
		t.Fatalf("flag %#x unexpectedly contains default %#x", flag, ColumnFlagDefault)
	}
}
