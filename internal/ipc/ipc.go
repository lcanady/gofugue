// Package ipc provides the JSON-RPC 2.0 IPC server that exposes GoFugue's
// event bus and command interface to external frontends.
//
// Three listeners run simultaneously:
//   - Unix domain socket  (~/.config/gofugue/gofugue.sock)
//   - TCP loopback        (127.0.0.1:7878, for Tcl/Tk and Windows clients)
//   - WebSocket           (ws://127.0.0.1:7879, for Electron/React)
//
// All three use identical newline-delimited JSON-RPC 2.0 framing.
package ipc

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
)

// Request is an incoming JSON-RPC 2.0 call from a client.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response is an outgoing JSON-RPC 2.0 result or error.
type Response struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  any              `json:"result,omitempty"`
	Error   *RPCError        `json:"error,omitempty"`
}

// Notification is a server-push JSON-RPC 2.0 message (no ID).
type Notification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

// RPCError encodes a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Config controls which listeners the Server starts.
type Config struct {
	SocketPath string // Unix domain socket path (empty = disabled)
	TCPAddr    string // e.g. "127.0.0.1:7878" (empty = disabled)
	WSAddr     string // e.g. "127.0.0.1:7879" (empty = disabled)

	// Token is the shared secret clients must present via the "auth" method as
	// their first RPC message. Empty string disables authentication entirely
	// (backwards-compatible default).
	Token string

	// UnixSkipsAuth, when true, allows Unix socket connections to bypass the
	// token check. The socket file is already restricted to mode 0600 (owner
	// only), so file-system permissions are the auth mechanism for Unix paths.
	// TCP and WebSocket connections always require the token when Token != "".
	UnixSkipsAuth bool

	// MaxConnections is the maximum number of simultaneously connected IPC
	// clients across all transports. Zero means use the default (64). If a
	// new connection would exceed this limit it is rejected with a JSON-RPC
	// error and immediately closed.
	MaxConnections int
}

// CommandHandler is called when a client sends a known method.
type CommandHandler func(ctx context.Context, client *Client, params json.RawMessage) (any, error)

// defaultMaxConnections is used when Config.MaxConnections is zero.
const defaultMaxConnections = 64

// Server manages IPC listeners and connected clients.
type Server struct {
	cfg      Config
	bus      *bus.Bus
	handlers map[string]CommandHandler

	mu      sync.RWMutex
	clients map[*Client]struct{}

	connCount atomic.Int64

	// historyTail is an optional callback used by the history.get handler.
	// Set it via SetHistoryFunc before calling Run.
	historyTail func(n int) []string

	// historyTailRich is an optional callback used by history.tail; returns
	// world-tagged lines with ANSI attrs preserved (any so this package stays
	// free of an internal/history import).
	historyTailRich func(n int) any
}

// New creates an IPC Server. Register handlers before calling Run.
func New(cfg Config, b *bus.Bus) *Server {
	s := &Server{
		cfg:      cfg,
		bus:      b,
		handlers: make(map[string]CommandHandler),
		clients:  make(map[*Client]struct{}),
	}
	s.registerBuiltins()
	return s
}

// Handle registers a JSON-RPC method handler.
func (s *Server) Handle(method string, h CommandHandler) {
	s.handlers[method] = h
}

// SetHistoryFunc wires the history.get handler to a real scrollback accessor.
// fn receives the requested line count and returns text lines newest-last.
func (s *Server) SetHistoryFunc(fn func(n int) []string) {
	s.historyTail = fn
}

// SetHistoryRichFunc wires the history.tail handler. fn should return a JSON-
// serialisable slice of world-tagged entries with text and ANSI attrs.
func (s *Server) SetHistoryRichFunc(fn func(n int) any) {
	s.historyTailRich = fn
}

// Run starts all configured listeners and blocks until ctx is cancelled.
func (s *Server) Run(ctx context.Context) error {
	var wg sync.WaitGroup
	errCh := make(chan error, 4) // enough for all listeners

	if s.cfg.SocketPath != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.listenUnix(ctx, s.cfg.SocketPath); err != nil {
				errCh <- err
			}
		}()
	}
	if s.cfg.TCPAddr != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.listenTCP(ctx, s.cfg.TCPAddr); err != nil {
				errCh <- err
			}
		}()
	}
	if s.cfg.WSAddr != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.listenWS(ctx, s.cfg.WSAddr); err != nil {
				errCh <- err
			}
		}()
	}

	// Forward bus events to subscribed clients.
	sub := s.bus.Subscribe(256)
	defer sub.Cancel()
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case ev := <-sub.C:
				s.broadcast(ev)
			}
		}
	}()

	// Wait for context cancellation or a listener error.
	var err error
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-ctx.Done():
		err = ctx.Err()
	case err = <-errCh:
	case <-done:
	}

	return err
}

// broadcast pushes a bus event to all clients that have subscribed to its type.
// Events carrying Sensitive=true (e.g. lines received during Telnet ECHO suppression,
// which indicates a password prompt) are silently dropped to prevent credential leakage
// over the IPC channel.
func (s *Server) broadcast(ev bus.Event) {
	// Guard: never send sensitive lines to IPC subscribers.
	switch e := ev.(type) {
	case bus.WorldLineEvent:
		if e.Sensitive {
			return
		}
	case bus.WorldRenderedEvent:
		if e.Sensitive {
			return
		}
	}

	n := Notification{
		JSONRPC: "2.0",
		Method:  string(ev.Type()),
		Params:  ev,
	}
	data, err := json.Marshal(n)
	if err != nil {
		return
	}
	data = append(data, '\n')

	// Snapshot subscribers under the read lock so we don't hold s.mu while
	// performing per-client I/O. Each c.write is a non-blocking enqueue
	// onto the client's writer goroutine.
	s.mu.RLock()
	subs := make([]*Client, 0, len(s.clients))
	for c := range s.clients {
		if c.isSubscribed(ev.Type()) {
			subs = append(subs, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range subs {
		c.write(data)
	}
}

func (s *Server) listenUnixConn(ctx context.Context, l net.Listener) error {
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		// Unix socket connections may skip token auth when UnixSkipsAuth is set;
		// file-system permissions (mode 0600) on the socket are the gate.
		skipAuth := s.cfg.UnixSkipsAuth
		c := newClientWithAuth(conn, s, skipAuth)
		if !s.addClientGuarded(c) {
			continue
		}
		go c.serve(ctx)
	}
}

func (s *Server) listenUnix(ctx context.Context, path string) error {
	// Ensure the parent directory exists. Without this, a fresh install
	// (no ~/.config/gofugue) fails to bind and historically cascaded into
	// process exit.
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	// Unconditionally remove any stale socket. Ignoring errors covers both
	// "no such file" (first start) and other transient failures. This avoids
	// the TOCTOU race that a stat+dial+remove sequence introduces.
	_ = os.Remove(path)

	// Set umask to 0177 before Listen so the kernel creates the socket with
	// mode 0600 (owner r/w only) atomically. This eliminates the window that
	// a post-Listen os.Chmod would leave open. We restore the previous umask
	// immediately after the file is created.
	old := syscall.Umask(0o177)
	l, err := net.Listen("unix", path)
	syscall.Umask(old)
	if err != nil {
		return err
	}
	return s.listenUnixConn(ctx, l)
}

func (s *Server) listenTCP(ctx context.Context, addr string) error {
	l, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	return s.accept(ctx, l)
}


func (s *Server) accept(ctx context.Context, l net.Listener) error {
	go func() {
		<-ctx.Done()
		l.Close()
	}()
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		c := newClient(conn, s)
		if !s.addClientGuarded(c) {
			continue
		}
		go c.serve(ctx)
	}
}

func (s *Server) remove(c *Client) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
	s.connCount.Add(-1)
}

// maxConns returns the effective connection limit.
func (s *Server) maxConns() int64 {
	if s.cfg.MaxConnections > 0 {
		return int64(s.cfg.MaxConnections)
	}
	return defaultMaxConnections
}

// addClientGuarded registers a client if the connection limit allows it.
// Returns true if the client was accepted, false if rejected (and closed).
func (s *Server) addClientGuarded(c *Client) bool {
	if s.connCount.Add(1) > s.maxConns() {
		s.connCount.Add(-1)
		// Send a JSON-RPC error then drop.
		resp := Response{
			JSONRPC: "2.0",
			Error:   &RPCError{Code: -32000, Message: "too many connections"},
		}
		data, _ := json.Marshal(resp)
		data = append(data, '\n')
		_ = c.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
		_, _ = c.conn.Write(data)
		c.forceClose()
		return false
	}
	s.mu.Lock()
	s.clients[c] = struct{}{}
	s.mu.Unlock()
	return true
}

// registerBuiltins wires the standard JSON-RPC methods.
func (s *Server) registerBuiltins() {
	s.Handle("subscribe", handleSubscribe)
	s.Handle("history.get", s.handleHistoryGet)
	s.Handle("history.tail", s.handleHistoryTail)
	// input and cmd handlers are registered by the application layer
	// (they need access to WorldManager and MacroEngine).
}

func handleSubscribe(_ context.Context, c *Client, params json.RawMessage) (any, error) {
	var p struct {
		Events []bus.EventType `json:"events"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	c.subscribe(p.Events...)
	return map[string]bool{"ok": true}, nil
}

// maxHistoryN is the maximum number of lines a client may request in a single
// history.get or history.tail call. Requests above this are silently clamped.
const maxHistoryN = 10000

func (s *Server) handleHistoryGet(_ context.Context, _ *Client, params json.RawMessage) (any, error) {
	var p struct {
		N int `json:"n"`
	}
	p.N = 100 // default
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	if p.N < 0 {
		p.N = 0
	}
	if p.N > maxHistoryN {
		p.N = maxHistoryN
	}
	if s.historyTail == nil {
		return []string{}, nil
	}
	return s.historyTail(p.N), nil
}

func (s *Server) handleHistoryTail(_ context.Context, _ *Client, params json.RawMessage) (any, error) {
	var p struct {
		N int `json:"n"`
	}
	p.N = 200
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	if p.N < 0 {
		p.N = 0
	}
	if p.N > maxHistoryN {
		p.N = maxHistoryN
	}
	if s.historyTailRich == nil {
		return []any{}, nil
	}
	return s.historyTailRich(p.N), nil
}
