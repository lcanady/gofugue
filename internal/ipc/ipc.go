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
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
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
}

// CommandHandler is called when a client sends a known method.
type CommandHandler func(ctx context.Context, client *Client, params json.RawMessage) (any, error)

// Server manages IPC listeners and connected clients.
type Server struct {
	cfg      Config
	bus      *bus.Bus
	handlers map[string]CommandHandler

	mu      sync.RWMutex
	clients map[*Client]struct{}

	// historyTail is an optional callback used by the history.get handler.
	// Set it via SetHistoryFunc before calling Run.
	historyTail func(n int) []string
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

// Broadcast pushes a bus event to all clients that have subscribed to its type.
func (s *Server) broadcast(ev bus.Event) {
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

	s.mu.RLock()
	defer s.mu.RUnlock()
	for c := range s.clients {
		if c.isSubscribed(ev.Type()) {
			c.write(data)
		}
	}
}

func (s *Server) listenUnix(ctx context.Context, path string) error {
	// Ensure the parent directory exists. Without this, a fresh install
	// (no ~/.config/gofugue) fails to bind and historically cascaded into
	// process exit.
	if dir := filepath.Dir(path); dir != "" {
		_ = os.MkdirAll(dir, 0o700)
	}
	// Clean up a stale socket from a previous ungraceful exit. We do this
	// only if no process is listening on it — probe by dialing.
	if _, err := os.Stat(path); err == nil {
		if conn, derr := net.DialTimeout("unix", path, 100*time.Millisecond); derr == nil {
			conn.Close()
			return fmt.Errorf("listen unix %s: another gofugue is already running", path)
		}
		_ = os.Remove(path)
	}
	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	// Restrict socket to the owner. Without this the file inherits the
	// process umask (commonly 0755), letting any local user issue
	// unauthenticated /cmd calls over IPC.
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return err
	}
	return s.accept(ctx, l)
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
		s.mu.Lock()
		s.clients[c] = struct{}{}
		s.mu.Unlock()
		go c.serve(ctx)
	}
}

func (s *Server) remove(c *Client) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
}

// registerBuiltins wires the standard JSON-RPC methods.
func (s *Server) registerBuiltins() {
	s.Handle("subscribe", handleSubscribe)
	s.Handle("history.get", s.handleHistoryGet)
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

func (s *Server) handleHistoryGet(_ context.Context, _ *Client, params json.RawMessage) (any, error) {
	var p struct {
		N int `json:"n"`
	}
	p.N = 100 // default
	if len(params) > 0 {
		_ = json.Unmarshal(params, &p)
	}
	if s.historyTail == nil {
		return []string{}, nil
	}
	return s.historyTail(p.N), nil
}
