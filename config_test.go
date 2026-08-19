package client

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeValidTestAuthKeyPEMFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "machbase_key.pem")
	if err := os.WriteFile(path, []byte(validTestAuthKeyPEM), 0600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	return path
}

const validTestAuthKeyPEM = `-----BEGIN PRIVATE KEY-----
MIIEvgIBADANBgkqhkiG9w0BAQEFAASCBKgwggSkAgEAAoIBAQCnafJLe8SpObNR
13B5Uhfkfwd9vckQ77ii3qC993uKuvS4HgsepeQTk/+OR4Fsw+htGfjEEB8m+Qlr
xGabMSQpsC04jY6Y2X2vaaDMNQQuTdNbCmlyLtC10VVTYTLGn4NpiHk5SzcXcUMW
LyxzNrbhFP5RUAAKjcpcBh3tKxaOnAO3GCyYh7nEY7SNjwkkpiKgx0xWcQlfzP+A
myxbwk6MTOTtFhOGgXiSMYcWzkQuUsGfypgTjGfL+u96NOjAcVTmJOBmtVKTV2eX
gsd8gFtbH+D6PE9+P9R6kvpKhiS4ouHH/0TO+qQEu0xOvThF9WFKPliEPAxYMpNY
i0EisUBfAgMBAAECggEANZSb/nqjS4HzGVt5XOrgSLo7PIw0QN5oWoAkNAh0GseR
MSg0aN+xKm7wmKncC2J8DgcE7kM2pTOJR1t5d2v35fvDzVjI7bSWHEETPKgvKV6x
KW8gpnHDTJ2t0FzIcnd1CJ6sJaEkBbWzQfNhJ5K4Xztn1cBj8vzEakVu6Iwk0Qkr
BjAAiMvWZ2xvAl4gXtVWGciraN1TF4VrAga34KMsX7al9J7aTHGEEpv7vdknaPpx
RxhLcUC9vezzAoAs7AFJl2IXQwRp4x6rj9sG1ML7r2SnKZTNw0VpGRDdSGsx928p
CbwadGuhJTfVB9ywWcZj5/nHCetCVAtp/0TmGrINUQKBgQDCmNbqTMCEM7BnhdW2
d3QvPOyzLQcx3O7G8EW+P97NekMpWwHw9NlIyyloxz6iBJb3uDHg8zqY+IQUmNzg
isg4sTPsRMU5mm76qwDsGf/jN5KZoUj4N8UEuWXo1ZfN2fiWO+9jHzu8ixk8a5KV
EwNTQpmK6AOGNp9+QUWT+Pwd5wKBgQDcPU3boUUKWBh2gVU7nyzWcDVKlFUhZvYQ
JcmP2fgFSeZqWvBTl8YhpH2pGJELy7U+YfgaZSQvh8XJGrFkV87BXnRO6X5ief5T
PPSZnVUM9k3E7yIxFb43HOvp4noxGY2Fqyu+SRzgNwr2B1h6Fj4Va++oAqQ+CFA1
7R4Kzf9KyQKBgQCcPjNw9Ccu/oGI3UB2vPqgYv557pF0S7u8J3cYBhhSSvRZ5CRu
32kGtXiOFEwJsj20sEP8Jc7Ku97w2rud3lBclIroDV99nK22vk6DQ2zdduVSTNlV
0xFxdZqJk9XLBlQ96+mNYKqJ+/VLOeP7pcRpuXOmwBr0TC9LJAVFhgiHyQKBgQCF
k/UWAcFDHd1wes78Q3XJdfMMkdz0TmNttc2DnztL0d+boB5lRQeZvg+tMMZAdkQu
WvNE5xVEcr/mUndHGe6/348BkaLjDYTQbYcZaJB+NSFEEZoWVU6yVKtNhtx/zTTF
3uTAG84Uu629PQVPvw/WpEmOCFQff6FOo8t12C0/6QKBgHU93YHFqzN8D77rvBRK
VTvfahW+zpOrUuS+OOXfSixozEc9AzyWeRev1+jay5HQHxcdl22ayZkOyHjY4UF+
O3JVOOLlcZVdeQI8JGfDjAoDm7ClUU6yzAV104q4InhsHhljSFQSKhD1VfevOF6y
7hd0f++JbEQFif2qQ9mDhFGI
-----END PRIVATE KEY-----`

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
	if len(cfg.AlternativeServers) != 1 || cfg.AlternativeServers[0] != "127.0.0.2:5657" {
		t.Fatalf("unexpected alternative server list: %#v", cfg.AlternativeServers)
	}
	if cfg.ProxyUser != "user" {
		t.Fatalf("unexpected proxy user: %q", cfg.ProxyUser)
	}
}

func TestParseDSNAlternativeServers(t *testing.T) {
	cfg, err := ParseDSN("host=127.0.0.1;alternative_servers=backup.example.com:5657,[::1]:5658")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	want := []string{"backup.example.com:5657", "[::1]:5658"}
	if !reflect.DeepEqual(cfg.AlternativeServers, want) {
		t.Fatalf("alternative servers=%v, want %v", cfg.AlternativeServers, want)
	}

	cn := &Connector{cfg: cfg}
	connStr := cn.connectionString()
	if !strings.Contains(connStr, "ALTERNATIVE_SERVERS=backup.example.com:5657,[::1]:5658") {
		t.Fatalf("connection string omitted alternative servers: %q", connStr)
	}
}

func TestParseDSNRejectsRemovedAlternativeServerKeys(t *testing.T) {
	for _, key := range []string{"alternative_host", "alternative_port"} {
		_, err := ParseDSN("host=127.0.0.1;" + key + "=backup.example.com")
		if err == nil {
			t.Fatalf("ParseDSN() with %s unexpectedly succeeded", key)
		}
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

func TestParseDSNStatementCacheDefaults(t *testing.T) {
	for _, dsn := range []string{
		"host=127.0.0.1",
		"tcp://sys:manager@127.0.0.1:5656",
		"127.0.0.1:5656",
	} {
		t.Run(dsn, func(t *testing.T) {
			cfg, err := ParseDSN(dsn)
			if err != nil {
				t.Fatalf("ParseDSN() error = %v", err)
			}
			if cfg.StatementCache != StatementCacheAuto {
				t.Fatalf("statement cache=%v, want auto", cfg.StatementCache)
			}
		})
	}
}

func TestParseDSNDefaultsDatabase(t *testing.T) {
	cfg, err := ParseDSN("host=127.0.0.1")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.Database != defaultDatabase {
		t.Fatalf("database=%q, want %q", cfg.Database, defaultDatabase)
	}
}

func TestParseDSNAuthKey(t *testing.T) {
	keyFile := writeValidTestAuthKeyPEMFile(t)
	cfg, err := ParseDSN("server=127.0.0.1:5656;user=sys;auth_mode=challenge;auth_key_file=" + keyFile + ";auth_sig_scheme=rsa_pss")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.AuthMode != "CHALLENGE" {
		t.Fatalf("unexpected auth_mode: %q", cfg.AuthMode)
	}
	if cfg.AuthKeyFile != keyFile {
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
	keyFile := writeValidTestAuthKeyPEMFile(t)
	dsn := "server=127.0.0.1:5656;user=sys;auth_mode=challenge;auth_key_file=" + keyFile + ";auth_sig_scheme=rsa_pss"
	cfg, err := ParseDSN(dsn)
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.AuthMode != "CHALLENGE" {
		t.Fatalf("unexpected auth_mode: %q", cfg.AuthMode)
	}
	if cfg.AuthKeyFile != keyFile {
		t.Fatalf("unexpected auth_key_file: %q", cfg.AuthKeyFile)
	}
	if cfg.AuthSigScheme != "rsa_pss" {
		t.Fatalf("unexpected auth_sig_scheme: %q", cfg.AuthSigScheme)
	}
}

func TestParseDSNAuthKeyProxyLogin(t *testing.T) {
	keyFile := writeValidTestAuthKeyPEMFile(t)
	cfg, err := ParseDSN("server=127.0.0.1:5656;user=sys as user;auth_mode=challenge;auth_key_file=" + keyFile + ";auth_sig_scheme=rsa_pss")
	if err != nil {
		t.Fatalf("ParseDSN() error = %v", err)
	}
	if cfg.AuthMode != "CHALLENGE" {
		t.Fatalf("unexpected auth_mode: %q", cfg.AuthMode)
	}
	if cfg.AuthKeyFile != keyFile {
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
	cfg := Config{Host: "127.0.0.1", Port: 5656, User: "sys", AuthMode: "CHALLENGE", AuthKeyPEM: validTestAuthKeyPEM}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestConfigValidateImplicitChallengeWithAuthKeyPEM(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 5656, User: "sys", AuthKeyPEM: validTestAuthKeyPEM}
	if err := cfg.validate(); err != nil {
		t.Fatalf("validate() error = %v", err)
	}
}

func TestDriverOpenConnectorMergesDefaults(t *testing.T) {
	drv := &Driver{}
	connector, err := drv.OpenConnector("host=127.0.0.1; port=5656; user=sys; password=manager; database=DEFAULT_DB; statement_cache=on; fetch_rows=500")
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
