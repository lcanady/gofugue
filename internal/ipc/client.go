package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
)

// Tunables for the per-client write queue. A slow client is dropped once
// either limit is exceeded so it cannot stall faster subscribers.
const (
	// writeQueueSize is the per-client buffered channel depth. Once full,
	// further enqueues are non-blocking drops counted against the client.
	writeQueueSize = 256
	// writeDeadline bounds how long a single conn.Write may take. A timeout
	// causes the client to be force-closed.
	writeDeadline = 2 * time.Second
	// maxConsecutiveDrops is how many in-a-row drops the client may
	// accumulate before it is force-closed for being slow.
	maxConsecutiveDrops = 16
)

// Client represents a single connected IPC frontend.
type Client struct {
	conn   net.Conn
	server *Server

	mu         sync.Mutex
	subscribed map[bus.EventType]struct{}
	allEvents  bool

	writeCh   chan []byte
	closeOnce sync.Once
	closed    chan struct{}

	consecutiveDrops atomic.Int32

	// authenticated tracks whether this client has passed the token auth check.
	// It starts false; set to true after a successful "auth" call. When the
	// server's Token is empty, it is set to true on connection so that no auth
	// step is required (backwards-compatible).
	authenticated bool

	// skipAuth is true for Unix socket clients when Config.UnixSkipsAuth is set.
	// In that case authenticated is also pre-set to true.
	skipAuth bool
}

func newClient(conn net.Conn, s *Server) *Client {
	return newClientWithAuth(conn, s, false)
}

func newClientWithAuth(conn net.Conn, s *Server, skipAuth bool) *Client {
	// Pre-authenticate when: no token configured, or Unix socket with UnixSkipsAuth.
	preAuthed := s.cfg.Token == "" || skipAuth
	c := &Client{
		conn:          conn,
		server:        s,
		subscribed:    make(map[bus.EventType]struct{}),
		writeCh:       make(chan []byte, writeQueueSize),
		closed:        make(chan struct{}),
		authenticated: preAuthed,
		skipAuth:      skipAuth,
	}
	go c.writeLoop()
	return c
}

func (c *Client) isSubscribed(t bus.EventType) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.allEvents {
		return true
	}
	_, ok := c.subscribed[t]
	return ok
}

func (c *Client) subscribe(types ...bus.EventType) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(types) == 0 {
		c.allEvents = true
		return
	}
	for _, t := range types {
		c.subscribed[t] = struct{}{}
	}
}

// write enqueues data for the writer goroutine without blocking. If the
// queue is full the message is dropped and the client's slow-counter is
// bumped. Once a client has dropped too many messages in a row it is
// forcibly closed so the broadcast loop will prune it.
func (c *Client) write(data []byte) {
	select {
	case <-c.closed:
		return
	default:
	}
	select {
	case c.writeCh <- data:
		c.consecutiveDrops.Store(0)
	default:
		if c.consecutiveDrops.Add(1) >= maxConsecutiveDrops {
			c.forceClose()
		}
	}
}

// forceClose closes the underlying conn and signals the writer goroutine to
// exit. Safe to call multiple times.
func (c *Client) forceClose() {
	c.closeOnce.Do(func() {
		close(c.closed)
		_ = c.conn.Close()
	})
}

// writeLoop is the per-client writer goroutine. It owns conn.Write and
// applies a deadline to every write. On any error (including timeout) it
// shuts the client down — the read loop will then see EOF.
func (c *Client) writeLoop() {
	for {
		select {
		case <-c.closed:
			// Drain best-effort then exit.
			return
		case data, ok := <-c.writeCh:
			if !ok {
				return
			}
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
			if _, err := c.conn.Write(data); err != nil {
				c.forceClose()
				return
			}
		}
	}
}

func (c *Client) serve(ctx context.Context) {
	defer func() {
		c.forceClose()
		c.server.remove(c)
	}()

	scanner := bufio.NewScanner(c.conn)
	// Cap single-frame size at 1 MiB to prevent memory exhaustion from
	// oversized IPC messages. The initial buffer is 64 KiB (default).
	scanner.Buffer(make([]byte, 64*1024), 1*1024*1024)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return
		default:
		}

		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			c.sendError(nil, -32700, "parse error")
			continue
		}
		if req.JSONRPC != "2.0" {
			c.sendError(req.ID, -32600, "invalid request")
			continue
		}

		// --- Token authentication gate ---
		// If the server requires auth and this client is not yet authenticated,
		// the ONLY acceptable method is "auth". Anything else gets an auth error
		// and the connection is closed immediately.
		if !c.authenticated {
			if req.Method != "auth" {
				c.sendErrorDirect(req.ID, -32001, "authentication required: send auth first")
				return
			}
			// Handle the auth call inline — do not go through the handler map so
			// that no other code can accidentally bypass the gate.
			var p struct {
				Token string `json:"token"`
			}
			if len(req.Params) > 0 {
				_ = json.Unmarshal(req.Params, &p)
			}
			if p.Token != c.server.cfg.Token {
				c.sendErrorDirect(req.ID, -32001, "authentication failed: bad token")
				return
			}
			c.authenticated = true
			c.sendResult(req.ID, map[string]bool{"ok": true})
			continue
		}

		// Already authenticated — handle normally.
		// "auth" after auth is also allowed (idempotent).
		if req.Method == "auth" {
			c.sendResult(req.ID, map[string]bool{"ok": true})
			continue
		}

		h, ok := c.server.handlers[req.Method]
		if !ok {
			// Sanitize the method name before reflecting it in the error message
			// to prevent log injection via method strings containing newlines or
			// other control characters.
			safeMethod := strings.Map(func(r rune) rune {
				if r < 0x20 || r == 0x7f {
					return '?'
				}
				return r
			}, req.Method)
			c.sendError(req.ID, -32601, "method not found: "+safeMethod)
			continue
		}

		result, err := h(ctx, c, req.Params)
		if err != nil {
			slog.Error("ipc handler error", "method", req.Method, "err", err)
			c.sendError(req.ID, -32000, "internal error")
			continue
		}
		c.sendResult(req.ID, result)
	}
}

func (c *Client) sendResult(id *json.RawMessage, result any) {
	resp := Response{JSONRPC: "2.0", ID: id, Result: result}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	c.write(data)
}

// sendErrorDirect writes an error response synchronously to the connection and
// then force-closes it. Used for auth failures where the write queue may not
// drain in time before close is issued.
func (c *Client) sendErrorDirect(id *json.RawMessage, code int, msg string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	_ = c.conn.SetWriteDeadline(time.Now().Add(writeDeadline))
	_, _ = c.conn.Write(data)
	c.forceClose()
}

func (c *Client) sendError(id *json.RawMessage, code int, msg string) {
	resp := Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: msg},
	}
	data, _ := json.Marshal(resp)
	data = append(data, '\n')
	c.write(data)
}
