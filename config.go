package client

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

type StatementCacheMode int

const (
	StatementCacheOff  StatementCacheMode = 0
	StatementCacheOn   StatementCacheMode = 1
	StatementCacheAuto StatementCacheMode = 2
)

type Config struct {
	Host            string
	Port            int
	User            string
	Password        string
	Database        string
	ProxyUser       string
	AuthMode        string
	AuthKeyFile     string
	AuthKeyPEM      string
	AuthSigScheme   string
	AlternativeHost string
	AlternativePort int
	FetchRows       int64
	StatementCache  StatementCacheMode
	IOMetrics       bool

	statementCacheSet bool
}

func (cfg Config) validate() error {
	if cfg.Host == "" {
		return errors.New("machbase dsn requires host or server")
	}
	if cfg.User == "" {
		return errors.New("machbase dsn requires user")
	}
	authMode := strings.ToUpper(strings.TrimSpace(cfg.AuthMode))
	hasAuthKey := strings.TrimSpace(cfg.AuthKeyFile) != "" || strings.TrimSpace(cfg.AuthKeyPEM) != ""
	if cfg.Password == "" && authMode == "PASSWORD" {
		return errors.New("machbase dsn requires password")
	}
	if cfg.Password == "" && authMode == "" && !hasAuthKey {
		return errors.New("machbase dsn requires password")
	}
	if authMode == "CHALLENGE" && strings.TrimSpace(cfg.AuthKeyFile) == "" && strings.TrimSpace(cfg.AuthKeyPEM) == "" {
		return errors.New("machbase dsn requires auth_key_file or auth_key_pem for auth_mode=CHALLENGE")
	}
	if cfg.Port <= 0 {
		return fmt.Errorf("machbase dsn has invalid port %d", cfg.Port)
	}
	return nil
}

func mergeConfig(base Config, override Config) Config {
	if override.Host != "" {
		base.Host = override.Host
	}
	if override.Port != 0 {
		base.Port = override.Port
	}
	if override.User != "" {
		base.User = override.User
	}
	if override.ProxyUser != "" {
		base.ProxyUser = override.ProxyUser
	}
	if override.Password != "" {
		base.Password = override.Password
	}
	if override.Database != "" {
		base.Database = override.Database
	}
	if override.AuthMode != "" {
		base.AuthMode = override.AuthMode
	}
	if override.AuthKeyFile != "" {
		base.AuthKeyFile = override.AuthKeyFile
	}
	if override.AuthKeyPEM != "" {
		base.AuthKeyPEM = override.AuthKeyPEM
	}
	if override.AuthSigScheme != "" {
		base.AuthSigScheme = override.AuthSigScheme
	}
	if override.AlternativeHost != "" {
		base.AlternativeHost = override.AlternativeHost
	}
	if override.AlternativePort != 0 {
		base.AlternativePort = override.AlternativePort
	}
	if override.FetchRows != 0 {
		base.FetchRows = override.FetchRows
	}
	if override.statementCacheSet {
		base.StatementCache = override.StatementCache
	}
	if override.IOMetrics {
		base.IOMetrics = true
	}
	return base
}

// ParseDSN parses a Machbase DSN string and returns connection config.
//
// Supported syntax:
//
//  1. Server value only
//     - host
//     - host:port
//     - tcp://user:password@host:port/database?as=proxy&fetch_rows=100
//
//  2. Key-value pairs separated by semicolon
//     - key=value;key=value;...
//     - Example: user=sys;password=manager;host=127.0.0.1;port=5656
//
// For key-value syntax, value may be quoted with single or double quotes.
// A semicolon inside quotes is treated as a literal character, not a separator.
// Quotes can be escaped inside quoted values with backslash.
// Examples:
//   - user="sys as demo";password="12;34";host=127.0.0.1;
//   - user='sys as demo';password='12;34';host=127.0.0.1;
//   - password="a\"b";password2='a\'b';
//   - auth_mode=challenge;user=sys;auth_key_pem="-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----";
func ParseDSN(dsn string) (Config, error) {
	var cfg Config
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return cfg, nil
	}
	scheme := strings.Index(dsn, "://")
	separator := strings.IndexByte(dsn, '=')
	if scheme >= 0 && (separator < 0 || separator > scheme) {
		if err := applyServerValue(&cfg, dsn); err != nil {
			return Config{}, err
		}
		return cfg.normalize(), nil
	}
	if strings.Contains(dsn, "=") {
		return parseKeyValueDSN(dsn)
	}
	if err := applyServerValue(&cfg, dsn); err != nil {
		return Config{}, err
	}
	return cfg.normalize(), nil
}

func (cfg Config) normalize() Config {
	if cfg.Port == 0 {
		cfg.Port = defaultPort
	}
	return cfg
}

func parseKeyValueDSN(dsn string) (Config, error) {
	var cfg Config
	parts, err := splitDSNSegments(dsn)
	if err != nil {
		return Config{}, err
	}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			return Config{}, fmt.Errorf("invalid dsn segment %q", part)
		}
		key = strings.ToLower(strings.TrimSpace(key))
		value = strings.TrimSpace(value)
		value, err = unquoteDSNValue(value)
		if err != nil {
			return Config{}, fmt.Errorf("invalid value for %q: %w", key, err)
		}
		switch key {
		case "server":
			if err := applyServerValue(&cfg, value); err != nil {
				return Config{}, err
			}
		case "host":
			cfg.Host = value
		case "port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid port %q", value)
			}
			cfg.Port = port
		case "user", "uid":
			username, proxyname, proxied := parseUserName(value)
			cfg.User = username
			if proxied && proxyname != "" {
				cfg.ProxyUser = proxyname
			}
		case "password", "pwd":
			cfg.Password = value
		case "database", "db":
			cfg.Database = value
		case "auth_mode":
			cfg.AuthMode = value
		case "auth_key_file":
			cfg.AuthKeyFile = value
		case "auth_key_pem":
			cfg.AuthKeyPEM = value
		case "auth_sig_scheme":
			cfg.AuthSigScheme = value
		case "fetch_rows", "fetchrows":
			rows, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return Config{}, fmt.Errorf("invalid fetch_rows %q", value)
			}
			cfg.FetchRows = rows
		case "statement_cache", "statementcache":
			mode, err := parseStatementCacheMode(value)
			if err != nil {
				return Config{}, err
			}
			cfg.StatementCache = mode
			cfg.statementCacheSet = true
		case "io_metrics", "iometrics":
			enabled, err := strconv.ParseBool(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid io_metrics %q", value)
			}
			cfg.IOMetrics = enabled
		case "alternative_servers":
			if err := applyAlternativeServers(&cfg, value); err != nil {
				return Config{}, err
			}
		case "alternative_host":
			cfg.AlternativeHost = value
		case "alternative_port":
			port, err := strconv.Atoi(value)
			if err != nil {
				return Config{}, fmt.Errorf("invalid alternative_port %q", value)
			}
			cfg.AlternativePort = port
		default:
			return Config{}, fmt.Errorf("unsupported dsn key %q", key)
		}
	}
	return cfg.normalize(), nil
}

func splitDSNSegments(dsn string) ([]string, error) {
	parts := make([]string, 0)
	var current strings.Builder
	var quote rune
	escaped := false

	for _, ch := range dsn {
		if escaped {
			current.WriteRune(ch)
			escaped = false
			continue
		}
		switch ch {
		case '\\':
			current.WriteRune(ch)
			if quote != 0 {
				escaped = true
			}
		case '\'', '"':
			switch quote {
			case 0:
				quote = ch
			case ch:
				quote = 0
			}
			current.WriteRune(ch)
		case ';':
			if quote != 0 {
				current.WriteRune(ch)
				continue
			}
			parts = append(parts, current.String())
			current.Reset()
		default:
			current.WriteRune(ch)
		}
	}

	if quote != 0 {
		return nil, errors.New("unterminated quoted value")
	}
	if escaped {
		return nil, errors.New("unterminated escape in quoted value")
	}
	parts = append(parts, current.String())
	return parts, nil
}

func unquoteDSNValue(value string) (string, error) {
	if len(value) < 2 {
		return value, nil
	}
	if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
		quote := rune(value[0])
		content := value[1 : len(value)-1]
		var out strings.Builder
		escaped := false
		for _, ch := range content {
			if escaped {
				if ch == quote || ch == '\\' {
					out.WriteRune(ch)
				} else {
					out.WriteRune('\\')
					out.WriteRune(ch)
				}
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			out.WriteRune(ch)
		}
		if escaped {
			return "", errors.New("unterminated escape sequence")
		}
		return out.String(), nil
	}
	if value[0] == '"' || value[0] == '\'' || value[len(value)-1] == '"' || value[len(value)-1] == '\'' {
		return "", errors.New("mismatched quotes")
	}
	return value, nil
}

func parseStatementCacheMode(value string) (StatementCacheMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return StatementCacheAuto, nil
	case "on", "true", "1":
		return StatementCacheOn, nil
	case "off", "false", "0":
		return StatementCacheOff, nil
	default:
		return StatementCacheAuto, fmt.Errorf("invalid statement_cache %q", value)
	}
}

func applyServerValue(cfg *Config, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("server value is empty")
	}
	if strings.Contains(value, "://") {
		u, err := url.Parse(value)
		if err != nil {
			return fmt.Errorf("invalid server %q", value)
		}
		if u.Host == "" {
			return fmt.Errorf("invalid server %q", value)
		}
		cfg.Host = u.Hostname()
		if port := u.Port(); port != "" {
			parsedPort, err := strconv.Atoi(port)
			if err != nil {
				return fmt.Errorf("invalid server port %q", port)
			}
			cfg.Port = parsedPort
		}
		if u.User != nil {
			if user := u.User.Username(); user != "" {
				cfg.User = user
			}
			if pass, ok := u.User.Password(); ok {
				cfg.Password = pass
			}
		}
		if u.Path != "" && u.Path != "/" {
			cfg.Database = strings.TrimPrefix(u.Path, "/")
		}
		for key, values := range u.Query() {
			switch strings.ToLower(key) {
			case "as":
				if len(values) > 0 {
					cfg.ProxyUser = values[0]
				}
			case "database", "db":
				if len(values) > 0 {
					cfg.Database = values[0]
				}
			case "auth_mode":
				cfg.AuthMode = values[0]
			case "auth_key_file":
				cfg.AuthKeyFile = values[0]
			case "auth_key_pem":
				cfg.AuthKeyPEM = values[0]
			case "auth_sig_scheme":
				cfg.AuthSigScheme = values[0]
			case "fetch_rows", "fetchrows":
				rows, err := strconv.ParseInt(values[0], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid fetch_rows %q", values[0])
				}
				cfg.FetchRows = rows
			case "statement_cache", "statementcache":
				mode, err := parseStatementCacheMode(values[0])
				if err != nil {
					return err
				}
				cfg.StatementCache = mode
				cfg.statementCacheSet = true
			case "io_metrics", "iometrics":
				enabled, err := strconv.ParseBool(values[0])
				if err != nil {
					return fmt.Errorf("invalid io_metrics %q", values[0])
				}
				cfg.IOMetrics = enabled
			case "alternative_servers":
				if err := applyAlternativeServers(cfg, values[0]); err != nil {
					return err
				}
			case "alternative_host":
				cfg.AlternativeHost = values[0]
			case "alternative_port":
				port, err := strconv.Atoi(values[0])
				if err != nil {
					return fmt.Errorf("invalid alternative_port %q", values[0])
				}
				cfg.AlternativePort = port
			}
		}
		return nil
	}
	host, port, err := net.SplitHostPort(value)
	if err == nil {
		cfg.Host = host
		parsedPort, convErr := strconv.Atoi(port)
		if convErr != nil {
			return fmt.Errorf("invalid server port %q", port)
		}
		cfg.Port = parsedPort
		return nil
	}
	if strings.Contains(err.Error(), "missing port in address") {
		cfg.Host = value
		return nil
	}
	return fmt.Errorf("invalid server %q", value)
}

func applyAlternativeServers(cfg *Config, value string) error {
	entries := strings.Split(value, ",")
	for _, entry := range entries {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		host, port, err := net.SplitHostPort(entry)
		if err != nil {
			return fmt.Errorf("invalid alternative server %q", entry)
		}
		parsedPort, err := strconv.Atoi(port)
		if err != nil {
			return fmt.Errorf("invalid alternative server port %q", port)
		}
		cfg.AlternativeHost = host
		cfg.AlternativePort = parsedPort
		return nil
	}
	return nil
}

var usernameProxyPattern = regexp.MustCompile(`(?i)^\s*(\S+)\s+as\s+(\S+)\s*$`)

// Parse attempts to parse the given loginName into Login and Proxy components.
// The expected format is "login as proxy_user" (case-insensitive for "as").
// It returns true if parsing is successful and the login username is "sys" (case-insensitive), allowing proxy authentication.
// On parse failure, it sets Login to the original loginName, Proxy to an empty string, and returns false.
// Example:
//   - Input: "sys as alice" -> Login: "sys", Proxy: "alice", returns true (proxy auth allowed)
//   - Input: "bob as alice" -> Login: "bob", Proxy: "alice", returns false (proxy auth not allowed)
func parseUserName(loginName string) (login string, proxy string, proxied bool) {
	matches := usernameProxyPattern.FindStringSubmatch(loginName)
	if len(matches) == 3 {
		login = matches[1]
		proxy = matches[2]
		if strings.EqualFold(login, "sys") {
			if strings.EqualFold(proxy, "sys") {
				// treat "sys as sys" as normal "sys" login without proxy
				proxy = ""
				proxied = false
				return
			} else {
				// "sys as proxy_user" format is only allowed when login is "sys""
				proxied = true
				return
			}
		} else {
			// proxy auth not allowed when login is not "sys", even if the format is correct
			proxied = false
			return
		}
	}
	login = loginName
	proxy = ""
	proxied = false
	return
}

// Split splits the full table name that consists of database, user, and table name.
func parseTableName(rawTableName string, dbName string, userName string) (string, string, string) {
	tableName := strings.ToUpper(string(rawTableName))
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
