package api

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseTableName(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		expectedDB      string
		expectedUser    string
		expectedTable   string
		expectedOrDB    string
		expectedOrUser  string
		expectedOrTable string
	}{
		{
			name:            "table_only",
			input:           "example",
			expectedDB:      "MACHBASEDB",
			expectedUser:    "SYS",
			expectedTable:   "EXAMPLE",
			expectedOrDB:    "db0",
			expectedOrUser:  "user0",
			expectedOrTable: "EXAMPLE",
		},
		{
			name:            "user.table",
			input:           "sys.example",
			expectedDB:      "MACHBASEDB",
			expectedUser:    "SYS",
			expectedTable:   "EXAMPLE",
			expectedOrDB:    "db0",
			expectedOrUser:  "SYS",
			expectedOrTable: "EXAMPLE",
		},
		{
			name:            "db.user.table",
			input:           "testdb.sys.example",
			expectedDB:      "TESTDB",
			expectedUser:    "SYS",
			expectedTable:   "EXAMPLE",
			expectedOrDB:    "TESTDB",
			expectedOrUser:  "SYS",
			expectedOrTable: "EXAMPLE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, user, table := TableName(tt.input).Split()
			require.Equal(t, tt.expectedDB, db)
			require.Equal(t, tt.expectedUser, user)
			require.Equal(t, tt.expectedTable, table)
			db, user, table = TableName(tt.input).SplitOr("db0", "user0")
			require.Equal(t, tt.expectedOrDB, db)
			require.Equal(t, tt.expectedOrUser, user)
			require.Equal(t, tt.expectedOrTable, table)
		})
	}
}

func TestParseProxyUserName(t *testing.T) {
	tests := []struct {
		name              string
		input             string
		expectedLoginName string
		expectedProxyUser string
		expectedAllowed   bool
		expectedString    string
	}{
		{
			name:              "no proxy user",
			input:             "alice",
			expectedLoginName: "alice",
			expectedProxyUser: "",
			expectedAllowed:   false,
			expectedString:    "alice",
		},
		{
			name:              "with proxy user",
			input:             "Sys As proxy",
			expectedLoginName: "Sys",
			expectedProxyUser: "proxy",
			expectedAllowed:   true,
			expectedString:    "sys as proxy",
		},
		{
			name:              "proxy user same as login",
			input:             "sys as sys",
			expectedLoginName: "sys",
			expectedProxyUser: "",
			expectedAllowed:   false,
			expectedString:    "sys",
		},
		{
			name:              "non-sys login with proxy format",
			input:             "Proxy as other",
			expectedLoginName: "Proxy",
			expectedProxyUser: "other",
			expectedAllowed:   false,
			expectedString:    "proxy as other",
		},
		{
			name:              "invalid format",
			input:             "PROXY other",
			expectedLoginName: "PROXY other",
			expectedProxyUser: "",
			expectedAllowed:   false,
			expectedString:    "proxy other",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			un := &UserName{}
			allowed := un.Parse(tt.input)
			require.Equal(t, tt.expectedLoginName, un.Login)
			require.Equal(t, tt.expectedProxyUser, un.Proxy)
			require.Equal(t, tt.expectedAllowed, allowed)
			require.Equal(t, tt.expectedString, un.String())
		})
	}
}
