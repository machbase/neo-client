package api

import "strings"

type TableName string

func (tn TableName) String() string {
	return strings.ToUpper(string(tn))
}

// Split splits the full table name that consists of database, user, and table name.
func (tn TableName) SplitOr(dbName string, userName string) (string, string, string) {
	tableName := strings.ToUpper(string(tn))
	parts := strings.SplitN(tableName, ".", 3)
	if len(parts) == 2 {
		userName = parts[0]
		tableName = parts[1]
	} else if len(parts) == 3 {
		dbName = parts[0]
		userName = parts[1]
		tableName = parts[2]
	}
	return dbName, userName, tableName
}

// Split splits the full table name that consists of database, user, and table name.
func (tn TableName) Split() (string, string, string) {
	dbName := "MACHBASEDB"
	userName := "SYS"
	tableName := strings.ToUpper(string(tn))
	parts := strings.SplitN(tableName, ".", 3)
	if len(parts) == 2 {
		userName = parts[0]
		tableName = parts[1]
	} else if len(parts) == 3 {
		dbName = parts[0]
		userName = parts[1]
		tableName = parts[2]
	}
	return dbName, userName, tableName
}
