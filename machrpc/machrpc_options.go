package machrpc

import (
	"time"

	"github.com/machbase/neo-engine/spi"
)

type Option func(*Client)

func WithServer(addr string) Option {
	return func(c *Client) {
		c.serverAddr = addr
	}
}

func WithCertificate(clientKeyPath string, clientCertPath string, serverCertPath string) Option {
	return func(c *Client) {
		c.certPath = clientCertPath
		c.keyPath = clientKeyPath
		c.serverCert = serverCertPath
	}
}

func WithQueryTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.queryTimeout = timeout
	}
}

func WithAppendTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.appendTimeout = timeout
	}
}

func WithPassword(username string, password string) spi.ConnectOption {
	return func(c spi.Conn) {
		if conn, ok := c.(*ClientConn); ok {
			conn.dbUser = username
			conn.dbPassword = password
		}
	}
}
