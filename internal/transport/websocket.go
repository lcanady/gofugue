package transport

import (
	"context"
	"crypto/tls"
	"io"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

type wsTransport struct {
	url string
}

func (t *wsTransport) Dial(ctx context.Context) (io.ReadWriteCloser, error) {
	dialer := websocket.Dialer{
		TLSClientConfig: &tls.Config{},
	}
	conn, _, err := dialer.DialContext(ctx, t.url, http.Header{})
	if err != nil {
		return nil, err
	}
	return &wsConn{conn: conn}, nil
}

// wsConn wraps a *websocket.Conn as io.ReadWriteCloser.
//
// WebSocket is message-framed, not stream-based. We bridge this by:
//   - Read: consume the current message reader; fetch the next message when
//     exhausted. Binary and Text frames are both accepted (MUD servers vary).
//   - Write: send each Write call as a single Binary frame.
//
// Concurrent writes are serialised by writeMu; reads are single-goroutine
// (the proto.Session reader loop) so no read lock is needed.
type wsConn struct {
	conn    *websocket.Conn
	reader  io.Reader // current message reader, nil when exhausted
	writeMu sync.Mutex
}

func (c *wsConn) Read(p []byte) (int, error) {
	for {
		if c.reader != nil {
			n, err := c.reader.Read(p)
			if n > 0 {
				if err == io.EOF {
					c.reader = nil // exhausted; next Read fetches next message
				}
				return n, nil
			}
			if err == io.EOF {
				c.reader = nil
				continue
			}
			return 0, err
		}

		// No current reader — get the next WebSocket message.
		_, r, err := c.conn.NextReader()
		if err != nil {
			return 0, err
		}
		c.reader = r
	}
}

func (c *wsConn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if err := c.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsConn) Close() error {
	// Send a polite close frame before closing the underlying TCP connection.
	c.writeMu.Lock()
	_ = c.conn.WriteMessage(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""),
	)
	c.writeMu.Unlock()
	return c.conn.Close()
}
