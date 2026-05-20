package ipc

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// listenWS starts an HTTP server that upgrades connections to WebSocket and
// wraps each one as a JSON-RPC IPC client identical to the TCP path.
func (s *Server) listenWS(ctx context.Context, addr string) error {
	upgrader := &websocket.Upgrader{
		// Only allow connections from loopback origins.
		//
		// Non-browser clients (Go, Python, CLI tools) send no Origin header at
		// all — those are always allowed. Electron production builds load from
		// file://, which browsers represent as Origin: null — also allowed.
		// Electron dev server runs on localhost — allowed. Any other origin
		// (e.g. http://evil.com) is rejected with 403.
		CheckOrigin: checkLocalhostOrigin,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		wsConn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		conn := newWSNetConn(wsConn)
		c := newClient(conn, s)
		s.mu.Lock()
		s.clients[c] = struct{}{}
		s.mu.Unlock()
		go c.serve(ctx)
	})

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(shutCtx) //nolint:errcheck
	}()
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// checkLocalhostOrigin returns true when the WebSocket upgrade request comes
// from a safe origin:
//
//   - No Origin header   → non-browser client (Go, Python, CLI); always safe
//   - Origin: null       → Electron file:// renderer; safe
//   - Origin hostname is 127.0.0.1, ::1, or localhost → dev server; safe
//
// Any other origin (e.g. http://evil.com) is rejected, preventing a
// cross-origin WebSocket attack where a browser tab connects to the loopback
// IPC port and reads or writes the user's MUD session.
func checkLocalhostOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" || origin == "null" {
		// Non-browser callers and Electron production (file://) don't send an
		// origin that can be spoofed by a web page.
		return true
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	h := u.Hostname()
	return h == "127.0.0.1" || h == "::1" || h == "localhost"
}

// ---------------------------------------------------------------------------
// wsNetConn adapts a *websocket.Conn to net.Conn so it can be used with the
// existing bufio-based Client read/write path.
//
// Read semantics: each WebSocket text frame is treated as one newline-
// terminated JSON record (the Client's bufio.Scanner expects newlines).
// Write semantics: trailing newline is stripped and the payload is sent as
// a single text frame (mirrors what the TCP path does).
// ---------------------------------------------------------------------------

type wsNetConn struct {
	conn *websocket.Conn
	mu   sync.Mutex // gorilla requires serialised writes
	buf  []byte     // bytes from the current read message not yet consumed
}

func newWSNetConn(conn *websocket.Conn) *wsNetConn {
	return &wsNetConn{conn: conn}
}

func (c *wsNetConn) Read(b []byte) (int, error) {
	// Return any buffered bytes from the previous message first.
	if len(c.buf) > 0 {
		n := copy(b, c.buf)
		c.buf = c.buf[n:]
		return n, nil
	}
	_, msg, err := c.conn.ReadMessage()
	if err != nil {
		return 0, err
	}
	// Append newline so bufio.Scanner in client.serve sees a complete record.
	msg = append(msg, '\n')
	n := copy(b, msg)
	if n < len(msg) {
		c.buf = make([]byte, len(msg)-n)
		copy(c.buf, msg[n:])
	}
	return n, nil
}

func (c *wsNetConn) Write(b []byte) (int, error) {
	// Strip the trailing newline the bufio.Writer appends — WS frames are
	// self-delimiting, so the client doesn't need it.
	payload := b
	if len(payload) > 0 && payload[len(payload)-1] == '\n' {
		payload = payload[:len(payload)-1]
	}
	if len(payload) == 0 {
		return len(b), nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	err := c.conn.WriteMessage(websocket.TextMessage, payload)
	if err != nil {
		return 0, err
	}
	return len(b), nil
}

func (c *wsNetConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn.Close()
}
func (c *wsNetConn) LocalAddr() net.Addr  { return c.conn.LocalAddr() }
func (c *wsNetConn) RemoteAddr() net.Addr { return c.conn.RemoteAddr() }
func (c *wsNetConn) SetDeadline(t time.Time) error {
	if err := c.conn.SetReadDeadline(t); err != nil {
		return err
	}
	return c.conn.SetWriteDeadline(t)
}
func (c *wsNetConn) SetReadDeadline(t time.Time) error  { return c.conn.SetReadDeadline(t) }
func (c *wsNetConn) SetWriteDeadline(t time.Time) error { return c.conn.SetWriteDeadline(t) }
