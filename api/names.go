package api

import (
	"fmt"
	"regexp"
	"strings"
)

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

// ParseUserName parses the given loginName into a UserName struct.
// It returns the parsed UserName and a boolean indicating whether proxy authentication is allowed.
// The expected format for proxy authentication is "sys as proxy_user" (case-insensitive for "as").
// If the format is correct and the login username is "sys", it allows proxy authentication and returns true.
// If the format is correct but the login username is not "sys", it does not allow proxy authentication and returns false.
// If the format is incorrect, it treats the entire loginName as the Login with no Proxy and returns false.
func ParseUserName(loginName string) (UserName, bool) {
	username := UserName{}
	allowed := username.Parse(loginName)
	return username, allowed
}

type UserName struct {
	// Login is the login username token (the left side of "as") or the raw input when parsing fails.
	Login string
	// Proxy is the proxy username token (the right side of "as") when present.
	Proxy string
}

var usernameProxyPattern = regexp.MustCompile(`(?i)^\s*(\S+)\s+as\s+(\S+)\s*$`)

// Parse attempts to parse the given loginName into Login and Proxy components.
// The expected format is "login as proxy_user" (case-insensitive for "as").
// It returns true if parsing is successful and the login username is "sys" (case-insensitive), allowing proxy authentication.
// On parse failure, it sets Login to the original loginName, Proxy to an empty string, and returns false.
// Example:
//   - Input: "sys as alice" -> Login: "sys", Proxy: "alice", returns true (proxy auth allowed)
//   - Input: "bob as alice" -> Login: "bob", Proxy: "alice", returns false (proxy auth not allowed)
func (u *UserName) Parse(loginName string) bool {
	matches := usernameProxyPattern.FindStringSubmatch(loginName)
	if len(matches) == 3 {
		u.Login = matches[1]
		u.Proxy = matches[2]
		if strings.EqualFold(u.Login, "sys") {
			if strings.EqualFold(u.Proxy, "sys") {
				// treat "sys as sys" as normal "sys" login without proxy
				u.Proxy = ""
				return false
			} else {
				// "sys as proxy_user" format is only allowed when login is "sys""
				return true
			}
		} else {
			// proxy auth not allowed when login is not "sys", even if the format is correct
			return false
		}
	}
	u.Login = loginName
	u.Proxy = ""
	return false
}

// String returns the string representation of the Username.
// It formats as "login as proxy" if Proxy is present, otherwise just "login".
// The returned string is in lowercase for consistency, as login names are typically case-insensitive.
func (u UserName) String() string {
	if u.Proxy != "" {
		return fmt.Sprintf("%s as %s", strings.ToLower(u.Login), strings.ToLower(u.Proxy))
	}
	return strings.ToLower(u.Login)
}
