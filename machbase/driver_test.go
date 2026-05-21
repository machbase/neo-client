package machbase

import (
	"context"
	"database/sql/driver"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/machbase/neo-client/api"
)

func TestParseDSNKeyValue(t *testing.T) {
	cfg, err := ParseDSN("server=tcp://sys:manager@127.0.0.1:5656;fetch_rows=777;statement_cache=off;io_metrics=true;alternative_servers=127.0.0.2:5657")
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
}

func TestConfigValidateChallengeRequiresKeyFile(t *testing.T) {
	cfg := Config{Host: "127.0.0.1", Port: 5656, User: "sys", AuthMode: "CHALLENGE"}
	if err := cfg.validate(); err == nil {
		t.Fatalf("expected validate() error")
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
	if err := checkNamedValue(&driver.NamedValue{Ordinal: 1, Name: "foo", Value: 1}); err == nil {
		t.Fatalf("expected named parameter error")
	}
	if err := checkNamedValue(&driver.NamedValue{Ordinal: 1, Value: true}); err == nil {
		t.Fatalf("expected unsupported bool error")
	}
}

func TestBeginTxUnsupported(t *testing.T) {
	conn := &Conn{}
	_, err := conn.BeginTx(context.Background(), driver.TxOptions{})
	if !errors.Is(err, errTransactionsUnsupported) {
		t.Fatalf("BeginTx() error = %v", err)
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
