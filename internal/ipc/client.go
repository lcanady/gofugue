package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
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
}

func newClient(conn net.Conn, s *Server) *Client {
	c := &Client{
		conn:       conn,
		server:     s,
		subscribed: make(map[bus.EventType]struct{}),
		writeCh:    make(chan []byte, writeQueueSize),
		closed:     make(chan struct{}),
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

		h, ok := c.server.handlers[req.Method]
		if !ok {
			c.sendError(req.ID, -32601, "method not found: "+req.Method)
			continue
		}

		result, err := h(ctx, c, req.Params)
		if err != nil {
			c.sendError(req.ID, -32000, err.Error())
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
