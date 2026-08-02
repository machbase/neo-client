package machbase

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/machbase/neo-client/api"
)

func TestParseDSNKeyValue(t *testing.T) {
	cfg, err := ParseDSN("server=tcp://sys:manager@127.0.0.1:5656?as=user&fetch_rows=777&statement_cache=off&io_metrics=true&alternative_servers=127.0.0.2:5657")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.Host != "127.0.0.1" || cfg.Port != 5656 {
		t.Fatalf("unexpected host/port: %#v", cfg)
	}
	if cfg.User != "sys" || cfg.Password != "manager" {
		t.Fatalf("unexpected credentials: %#v", cfg)
	}
	if cfg.FetchRows != 777 {
		t.Fatalf("unexpected fetch_rows: %d", cfg.FetchRows)
	}
	if cfg.StatementCache != api.StatementCacheOff {
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
	drv := &Driver{Host: "127.0.0.1", Port: 5656, User: "sys", Password: "manager", StatementCache: api.StatementCacheOn}
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
	if cn.cfg.StatementCache != api.StatementCacheOn {
		t.Fatalf("unexpected statement cache: %v", cn.cfg.StatementCache)
	}
}

func TestCheckNamedValue(t *testing.T) {
	ip := net.ParseIP("127.0.0.1")
	tm := time.Unix(0, 1)
	values := []*driver.NamedValue{
		{Ordinal: 1, Value: int32(1)},
		{Ordinal: 2, Value: uint16(2)},
		{Ordinal: 3, Value: ip},
		{Ordinal: 4, Value: tm},
	}
	for _, nv := range values {
		if err := checkNamedValue(nv); err != nil {
			t.Fatalf("checkNamedValue(%T) error = %v", nv.Value, err)
		}
	}
	if _, ok := values[1].Value.(int64); !ok {
		t.Fatalf("expected uint16 to normalize to int64, got %T", values[1].Value)
	}
	named := &driver.NamedValue{Ordinal: 1, Name: "foo", Value: 1}
	if err := checkNamedValue(named); err != nil {
		t.Fatalf("named parameter error = %v", err)
	}
	converted, err := namedValuesToAny([]driver.NamedValue{*named})
	if err != nil {
		t.Fatalf("namedValuesToAny() error = %v", err)
	}
	if param, ok := converted[0].(api.NamedParam); !ok || param.Name != "foo" || param.Value != 1 {
		t.Fatalf("unexpected named conversion: %#v", converted[0])
	}
	if err := checkNamedValue(&driver.NamedValue{Ordinal: 1, Value: true}); err == nil {
		t.Fatalf("expected unsupported bool error")
	}
}

func TestBeginTxOptions(t *testing.T) {
	conn := &Conn{}
	_, err := conn.BeginTx(context.Background(), driver.TxOptions{Isolation: driver.IsolationLevel(1)})
	if err == nil || !strings.Contains(err.Error(), "isolation") {
		t.Fatalf("BeginTx(isolation) error = %v", err)
	}
	_, err = conn.BeginTx(context.Background(), driver.TxOptions{ReadOnly: true})
	if err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Fatalf("BeginTx(read-only) error = %v", err)
	}
	_, err = conn.BeginTx(context.Background(), driver.TxOptions{})
	if !errors.Is(err, driver.ErrBadConn) {
		t.Fatalf("BeginTx(default) error = %v", err)
	}
}

func TestLeadingSQLKeyword(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  string
	}{
		{query: " BEGIN", want: "BEGIN"},
		{query: "begin;", want: "BEGIN"},
		{query: "-- leading comment\n COMMIT", want: "COMMIT"},
		{query: "/* block */ -- line\n rollback", want: "ROLLBACK"},
		{query: "SELECT 'BEGIN'", want: "SELECT"},
		{query: "-- comment only", want: ""},
		{query: "/* unterminated", want: ""},
	} {
		if got := leadingSQLKeyword(tc.query); got != tc.want {
			t.Fatalf("leadingSQLKeyword(%q)=%q, want %q", tc.query, got, tc.want)
		}
	}
}

func TestNormalizeErrorBadConn(t *testing.T) {
	if !errors.Is(normalizeError(errors.New("connection closed")), driver.ErrBadConn) {
		t.Fatalf("expected ErrBadConn for connection closed")
	}
	if errors.Is(normalizeError(errors.New("other error")), driver.ErrBadConn) {
		t.Fatalf("did not expect ErrBadConn for generic error")
	}
}
