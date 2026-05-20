package transport

import (
	"context"
	"crypto/tls"
	"io"
	"net"
)

type tcpTransport struct {
	host       string
	tls        bool
	skipVerify bool
}

func (t *tcpTransport) Dial(ctx context.Context) (io.ReadWriteCloser, error) {
	d := &net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp", t.host)
	if err != nil {
		return nil, err
	}
	if t.tls {
		tlsCfg := &tls.Config{
			InsecureSkipVerify: t.skipVerify, //nolint:gosec // user opt-in for self-signed
			ServerName:         hostOnly(t.host),
		}
		tlsConn := tls.Client(conn, tlsCfg)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, err
		}
		return tlsConn, nil
	}
	return conn, nil
}

func hostOnly(hostport string) string {
	h, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return h
}
