package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"sync"

	"github.com/kumakun/gofugue/internal/bus"
)

// Client represents a single connected IPC frontend.
type Client struct {
	conn   net.Conn
	server *Server
	w      *bufio.Writer

	mu          sync.Mutex
	subscribed  map[bus.EventType]struct{}
	allEvents   bool
}

func newClient(conn net.Conn, s *Server) *Client {
	return &Client{
		conn:       conn,
		server:     s,
		w:          bufio.NewWriter(conn),
		subscribed: make(map[bus.EventType]struct{}),
	}
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

func (c *Client) write(data []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.w.Write(data) //nolint:errcheck
	c.w.Flush()     //nolint:errcheck
}

func (c *Client) serve(ctx context.Context) {
	defer func() {
		c.conn.Close()
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
