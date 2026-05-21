// Package transport provides a uniform Dial interface over TCP/TLS, WebSocket,
// and WebTransport. Upper layers receive an io.ReadWriteCloser and are
// transport-agnostic.
package transport

import (
	"context"
	"fmt"
	"io"
	"net/url"
)

// Transport dials a remote endpoint and returns a bidirectional byte stream.
type Transport interface {
	Dial(ctx context.Context) (io.ReadWriteCloser, error)
}

// Config holds the URL and optional TLS settings for a world connection.
type Config struct {
	// URL selects the transport and endpoint:
	//   mud://host:port        plain TCP
	//   muds://host:port       TCP + TLS
	//   ws://host/path         WebSocket
	//   wss://host/path        WebSocket + TLS
	//   wt://host/path         WebTransport (HTTP/3 + QUIC)
	URL string

	// TelnetEnabled controls whether the Telnet FSM wraps this transport.
	// Some WebSocket MUD servers speak raw text frames with no IAC bytes.
	TelnetEnabled bool
}

// New returns the appropriate Transport implementation for cfg.URL.
func New(cfg Config) (Transport, error) {
	u, err := url.Parse(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("transport: invalid URL %q: %w", cfg.URL, err)
	}
	switch u.Scheme {
	case "mud":
		return &tcpTransport{host: u.Host, tls: false}, nil
	case "muds":
		return &tcpTransport{host: u.Host, tls: true}, nil
	case "ws", "wss":
		return &wsTransport{url: cfg.URL}, nil
	case "wt":
		return &wtTransport{url: cfg.URL}, nil
	default:
		return nil, fmt.Errorf("transport: unknown scheme %q (use mud/muds/ws/wss/wt)", u.Scheme)
	}
}
