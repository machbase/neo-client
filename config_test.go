package client

import (
	"strings"
	"testing"
)

func TestParseDSNKeyValue(t *testing.T) {
	cfg, err := ParseDSN("server=tcp://sys:manager@127.0.0.1:5656/DB_A?as=user&fetch_rows=777&statement_cache=off&io_metrics=true&alternative_servers=127.0.0.2:5657")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 5656 {
		t.Fatalf("unexpected host/port: %#v", cfg)
	}
	if cfg.User != "sys" || cfg.Password != "manager" {
		t.Fatalf("unexpected credentials: %#v", cfg)
	}
	if cfg.Database != "DB_A" {
		t.Fatalf("unexpected database: %q", cfg.Database)
	}
	if cfg.FetchRows != 777 {
		t.Fatalf("unexpected fetch_rows: %d", cfg.FetchRows)
	}
	if cfg.StatementCache != StatementCacheOff {
		t.Fatalf("unexpected statement cache: %v", cfg.StatementCache)
	}
	if !cfg.IOMetrics {
		t.Fatalf("expected io metrics enabled")
	}
	if cfg.AlternativeHost != "127.0.0.2" || cfg.AlternativePort != 5657 {
		t.Fatalf("unexpected alternative server: %#v", cfg)
	}
	if cfg.ProxyUser != "user" {
		t.Fatalf("unexpected proxy user: %q", cfg.ProxyUser)
	}
}

func TestParseDSNDatabaseForms(t *testing.T) {
	for _, tc := range []struct {
		name string
		dsn  string
		want string
	}{
		{name: "key", dsn: "host=127.0.0.1;database=DATABASE_A", want: "DATABASE_A"},
		{name: "alias", dsn: "host=127.0.0.1;db=DATABASE_B", want: "DATABASE_B"},
		{name: "quoted", dsn: `host=127.0.0.1;database="Database A"`, want: "Database A"},
		{name: "url-path", dsn: "tcp://sys:manager@127.0.0.1:5656/Database%20A", want: "Database A"},
		{name: "url-query", dsn: "tcp://sys:manager@127.0.0.1:5656?database=DATABASE_C", want: "DATABASE_C"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := ParseDSN(tc.dsn)
			if err != nil {
				t.Fatalf("ParseDSN() error = %v", err)
			}
			if cfg.Database != tc.want {
				t.Fatalf("database=%q, want %q", cfg.Database, tc.want)
			}
		})
	}
}

func TestParseDSNAuthKey(t *testing.T) {
	cfg, err := ParseDSN("server=127.0.0.1:5656;user=sys;auth_mode=challenge;auth_key_file=/tmp/machbase_key.pem;auth_sig_scheme=rsa_pss")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.AuthMode != "challenge" {
		t.Fatalf("unexpected auth_mode: %q", cfg.AuthMode)
	}
	if cfg.AuthKeyFile != "/tmp/machbase_key.pem" {
		t.Fatalf("unexpected auth_key_file: %q", cfg.AuthKeyFile)
	}
	if cfg.AuthSigScheme != "rsa_pss" {
		t.Fatalf("unexpected auth_sig_scheme: %q", cfg.AuthSigScheme)
	}
	if cfg.ProxyUser != "" {
		t.Fatalf("unexpected proxy user: %q", cfg.ProxyUser)
	}
}

func TestParseDSNAuthKeyPEM(t *testing.T) {
	dsn := `server=127.0.0.1:5656;user=sys;auth_mode=challenge;auth_key_pem="-----BEGIN PRIVATE KEY-----
MIIA...
-----END PRIVATE KEY-----";auth_sig_scheme=rsa_pss`
	cfg, err := ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.AuthMode != "challenge" {
		t.Fatalf("unexpected auth_mode: %q", cfg.AuthMode)
	}
	if cfg.AuthKeyPEM == "" {
		t.Fatalf("expected auth_key_pem")
	}
	if !strings.Contains(cfg.AuthKeyPEM, "BEGIN PRIVATE KEY") {
		t.Fatalf("unexpected auth_key_pem: %q", cfg.AuthKeyPEM)
	}
	if cfg.AuthSigScheme != "rsa_pss" {
		t.Fatalf("unexpected auth_sig_scheme: %q", cfg.AuthSigScheme)
	}
}

func TestParseDSNAuthKeyProxyLogin(t *testing.T) {
	cfg, err := ParseDSN("server=127.0.0.1:5656;user=sys as user;auth_mode=challenge;auth_key_file=/tmp/machbase_key.pem;auth_sig_scheme=rsa_pss")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.AuthMode != "challenge" {
		t.Fatalf("unexpected auth_mode: %q", cfg.AuthMode)
	}
	if cfg.AuthKeyFile != "/tmp/machbase_key.pem" {
		t.Fatalf("unexpected auth_key_file: %q", cfg.AuthKeyFile)
	}
	if cfg.AuthSigScheme != "rsa_pss" {
		t.Fatalf("unexpected auth_sig_scheme: %q", cfg.AuthSigScheme)
	}
	if cfg.ProxyUser != "user" {
		t.Fatalf("unexpected proxy user: %q", cfg.ProxyUser)
	}
}

func TestParseDSNPassowrdProxyLogin(t *testing.T) {
	cfg, err := ParseDSN("server=127.0.0.1:5656;user=sys as demo;password=manager;fetch_rows=100")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 5656 {
		t.Fatalf("unexpected host or port: %#v", cfg)
	}
	if cfg.User != "sys" || cfg.Password != "manager" {
		t.Fatalf("unexpected credentials: %#v", cfg)
	}
	if cfg.ProxyUser != "demo" {
		t.Fatalf("unexpected proxy user: %q", cfg.ProxyUser)
	}
	if cfg.FetchRows != 100 {
		t.Fatalf("unexpected fetch_rows: %d", cfg.FetchRows)
	}
	if cfg.ProxyUser != "demo" {
		t.Fatalf("unexpected proxy user: %q", cfg.ProxyUser)
	}
}

func TestParseDSNKeyValueQuotedValues(t *testing.T) {
	cfg, err := ParseDSN("user=\"sys as demo\";password=\"12;34\";host=127.0.0.1;")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.User != "sys" {
		t.Fatalf("unexpected user: %q", cfg.User)
	}
	if cfg.ProxyUser != "demo" {
		t.Fatalf("unexpected proxy user: %q", cfg.ProxyUser)
	}
	if cfg.Password != "12;34" {
		t.Fatalf("unexpected password: %q", cfg.Password)
	}
	if cfg.Host != "127.0.0.1" {
		t.Fatalf("unexpected host: %q", cfg.Host)
	}
}

func TestParseDSNKeyValueSingleQuotedValues(t *testing.T) {
	cfg, err := ParseDSN("user='sys as demo';password='12;34';host=127.0.0.1;")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.User != "sys" {
		t.Fatalf("unexpected user: %q", cfg.User)
	}
	if cfg.ProxyUser != "demo" {
		t.Fatalf("unexpected proxy user: %q", cfg.ProxyUser)
	}
	if cfg.Password != "12;34" {
		t.Fatalf("unexpected password: %q", cfg.Password)
	}
}

func TestParseDSNKeyValueQuotedValueErrors(t *testing.T) {
	if _, err := ParseDSN("user=\"sys;password=manager;host=127.0.0.1"); err == nil {
		t.Fatalf("expected unterminated quote error")
	}
	if _, err := ParseDSN("user=\"sys';password=manager;host=127.0.0.1"); err == nil {
		t.Fatalf("expected mismatched quote error")
	}
}

func TestParseDSNKeyValueQuotedValueEscapes(t *testing.T) {
	cfg, err := ParseDSN(`user="sys as demo";password="12\";34\\x";host=127.0.0.1;`)
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.Password != `12";34\x` {
		t.Fatalf("unexpected password: %q", cfg.Password)
	}

	cfg, err = ParseDSN(`user='sys as demo';password='12\';34\\x';host=127.0.0.1;`)
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.Password != `12';34\x` {
		t.Fatalf("unexpected password: %q", cfg.Password)
	}
}

func TestParseDSNKeyValueQuotedValueEscapeErrors(t *testing.T) {
	if _, err := ParseDSN("user=\"sys\";password=\"abc\\\";host=127.0.0.1;"); err == nil {
		t.Fatalf("expected unterminated escape error")
	}
}

func TestConfigValidateChallengeRequiresKeyFile(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 5656, User: "sys", AuthMode: "CHALLENGE"}
	if err := cfg.validate(); err == nil {
		t.Fatalf("expected validate() error")
	}
}

func TestConfigValidateChallengeWithAuthKeyPEM(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 5656, User: "sys", AuthMode: "CHALLENGE", AuthKeyPEM: "-----BEGIN PRIVATE KEY-----\nX\n-----END PRIVATE KEY-----"}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestConfigValidateImplicitChallengeWithAuthKeyPEM(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 5656, User: "sys", AuthKeyPEM: "-----BEGIN PRIVATE KEY-----\nX\n-----END PRIVATE KEY-----"}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestDriverOpenConnectorMergesDefaults(t *testing.T) {
	drv := &Driver{Host: "127.0.0.1", Port: 5656, User: "sys", Password: "manager", Database: "DEFAULT_DB", StatementCache: StatementCacheOn}
	connector, err := drv.OpenConnector("fetch_rows=500")
	if err != nil {
		t.Fatalf("OpenConnector() error = %v", err)
	}
	cn, ok := connector.(*Connector)
	if !ok {
		t.Fatalf("unexpected connector type %T", connector)
	}
	if cn.cfg.Host != "127.0.0.1" || cn.cfg.User != "sys" || cn.cfg.Password != "manager" {
		t.Fatalf("defaults were not merged: %#v", cn.cfg)
	}
	if cn.cfg.FetchRows != 500 {
		t.Fatalf("unexpected fetch rows: %d", cn.cfg.FetchRows)
	}
	if cn.cfg.StatementCache != StatementCacheOn {
		t.Fatalf("unexpected statement cache: %v", cn.cfg.StatementCache)
	}
	if cn.cfg.Database != "DEFAULT_DB" {
		t.Fatalf("unexpected database: %q", cn.cfg.Database)
	}
}
