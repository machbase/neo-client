package client

import (
	"testing"

	"github.com/machbase/neo-client/v2/machnet"
)

func TestDatabaseStmtTypeResultMessages(t *testing.T) {
	tests := []struct {
		stmtType machnet.StmtType
		want     string
	}{
		{stmtType: machnet.QPP_STMT_TYPE_CREATE_DATABASE, want: "database created."},
		{stmtType: machnet.QPP_STMT_TYPE_DROP_DATABASE, want: "database dropped."},
		{stmtType: machnet.QPP_STMT_TYPE_ALTER_DATABASE, want: "database altered."},
	}

	for _, tc := range tests {
		if got := formatResultMessage(nil, tc.stmtType, 0); got != tc.want {
			t.Fatalf("formatResultMessage(%d) = %q, want %q", tc.stmtType, got, tc.want)
		}
	}
}

func TestDatabaseStmtTypesInvalidateCatalog(t *testing.T) {
	for _, stmtType := range []machnet.StmtType{
		machnet.QPP_STMT_TYPE_ALTER_SESSION_SET,
		machnet.QPP_STMT_TYPE_CONNECT_USER,
		machnet.QPP_STMT_TYPE_CREATE_DATABASE,
		machnet.QPP_STMT_TYPE_DROP_DATABASE,
		machnet.QPP_STMT_TYPE_ALTER_DATABASE,
	} {
		if !stmtTypeInvalidatesCatalog(stmtType) {
			t.Fatalf("statement type %d should invalidate catalog state", stmtType)
		}
	}

	if stmtTypeInvalidatesCatalog(machnet.QPP_STMT_TYPE_SELECT) {
		t.Fatal("select should not invalidate catalog state")
	}
}
