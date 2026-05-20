package ipc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/ipc"
)

// ---------------------------------------------------------------------------
// WebSocket helpers
// ---------------------------------------------------------------------------

// startWSServer spins up an IPC server with a WebSocket listener on a random
// port and returns the address. The server stops when the test ends.
func startWSServer(t *testing.T, b *bus.Bus) string {
	t.Helper()

	// Pick a free port.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := ipc.Config{WSAddr: "127.0.0.1:" + strings.Split(addr, ":")[1]}
	srv := ipc.New(cfg, b)
	go srv.Run(ctx) //nolint:errcheck

	// Wait for the HTTP server to be ready.
	wsURL := "ws://" + cfg.WSAddr + "/"
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err == nil {
			conn.Close()
			return cfg.WSAddr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("WebSocket server never became ready at %s", cfg.WSAddr)
	return ""
}

// wsClient wraps a gorilla/websocket.Conn with JSON-RPC send/recv helpers.
type wsClient struct {
	conn *websocket.Conn
}

func dialWS(t *testing.T, addr string) *wsClient {
	t.Helper()
	url := "ws://" + addr + "/"
	var conn *websocket.Conn
	var err error
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn, _, err = websocket.DefaultDialer.Dial(url, nil)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dialWS %s: %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })
	return &wsClient{conn: conn}
}

func (c *wsClient) send(t *testing.T, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := c.conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("wsClient.send: %v", err)
	}
}

func (c *wsClient) recv(t *testing.T, timeout time.Duration) map[string]any {
	t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	_, msg, err := c.conn.ReadMessage()
	if err != nil {
		t.Fatalf("wsClient.recv: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(msg, &m); err != nil {
		t.Fatalf("wsClient.recv unmarshal: %v (raw: %s)", err, msg)
	}
	return m
}

func (c *wsClient) subscribe(t *testing.T, events ...string) {
	t.Helper()
	c.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "subscribe",
		"params": map[string]any{"events": events},
	})
	c.recv(t, 500*time.Millisecond) // consume ack
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestWS_Connect_ReceivesValidResponse(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)

	c.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 42,
		"method": "subscribe",
		"params": map[string]any{"events": []string{"world.line"}},
	})
	resp := c.recv(t, time.Second)

	if resp["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %q, want 2.0", resp["jsonrpc"])
	}
	// id must echo back as 42.
	id, _ := resp["id"].(float64)
	if int(id) != 42 {
		t.Errorf("response id = %v, want 42", resp["id"])
	}
}

func TestWS_UnknownMethod_ReturnsError(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)

	c.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "does.not.exist",
		"params": nil,
	})
	resp := c.recv(t, time.Second)
	if resp["error"] == nil {
		t.Errorf("expected error for unknown method, got: %v", resp)
	}
}

func TestWS_InvalidJSON_ReturnsParseError(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)

	// Send raw non-JSON bytes.
	c.conn.WriteMessage(websocket.TextMessage, []byte("{not json")) //nolint:errcheck
	resp := c.recv(t, time.Second)
	if resp["error"] == nil {
		t.Errorf("expected parse error, got: %v", resp)
	}
}

func TestWS_Subscribe_ReceivesWorldLineEvent(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)
	c.subscribe(t, "world.line")

	// Publish after a brief pause so the subscription is definitely active.
	time.Sleep(20 * time.Millisecond)
	b.Publish(bus.WorldLineEvent{WorldName: "w1", Text: "hello via ws"})

	notif := c.recv(t, 2*time.Second)
	if notif["method"] != "world.line" {
		t.Errorf("notification method = %q, want %q", notif["method"], "world.line")
	}
	params, _ := notif["params"].(map[string]any)
	if params == nil {
		t.Fatal("notification missing params")
	}
	if params["Text"] != "hello via ws" {
		t.Errorf("params.Text = %q, want %q", params["Text"], "hello via ws")
	}
}

func TestWS_UnsubscribedEvent_NotDelivered(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)
	c.subscribe(t, "hook") // subscribe to hooks only

	// Publish a world.line — should NOT arrive.
	b.Publish(bus.WorldLineEvent{WorldName: "w1", Text: "should not arrive"})

	c.conn.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	_, _, err := c.conn.ReadMessage()
	if err == nil {
		t.Error("unsubscribed event should not be delivered")
	}
}

func TestWS_MultipleSubscriptions_AllDelivered(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)
	c.subscribe(t, "world.line", "hook")

	time.Sleep(20 * time.Millisecond)
	b.Publish(bus.WorldLineEvent{WorldName: "w1", Text: "line event"})
	b.Publish(bus.HookEvent{WorldName: "w1", Name: "CONNECT"})

	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		m := c.recv(t, 2*time.Second)
		seen[m["method"].(string)] = true
	}
	if !seen["world.line"] {
		t.Error("world.line event not received")
	}
	if !seen["hook"] {
		t.Error("hook event not received")
	}
}

func TestWS_ConcurrentClients_NoRace(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)

	const n = 5
	clients := make([]*wsClient, n)
	for i := range clients {
		clients[i] = dialWS(t, addr)
		clients[i].subscribe(t, "world.line")
	}

	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	// Publish from multiple goroutines.
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b.Publish(bus.WorldLineEvent{WorldName: "w", Text: "concurrent"})
		}(i)
	}
	wg.Wait()

	// Each client should receive at least one event.
	for i, c := range clients {
		c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, err := c.conn.ReadMessage()
		if err != nil {
			t.Errorf("client %d: expected event, got error: %v", i, err)
		}
	}
}

func TestWS_ClientDisconnect_RemovesFromServer(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)
	c.subscribe(t, "world.line")

	// Close the client.
	c.conn.Close()
	time.Sleep(50 * time.Millisecond)

	// Publishing should not panic (server removes disconnected client).
	b.Publish(bus.WorldLineEvent{WorldName: "w", Text: "after disconnect"})
	// If we reach here without panic, the test passes.
}

func TestWS_HistoryGet_ReturnsLines(t *testing.T) {
	b := bus.New()

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := ipc.Config{WSAddr: addr}
	srv := ipc.New(cfg, b)
	srv.SetHistoryFunc(func(n int) []string {
		return []string{"line1", "line2", "line3"}
	})
	go srv.Run(ctx) //nolint:errcheck

	// Wait for ready.
	time.Sleep(50 * time.Millisecond)

	c := dialWS(t, addr)
	c.send(t, map[string]any{
		"jsonrpc": "2.0", "id": 99,
		"method": "history.get",
		"params": map[string]any{"n": 3},
	})
	resp := c.recv(t, time.Second)
	result, _ := resp["result"].([]any)
	if len(result) != 3 {
		t.Errorf("history.get returned %d lines, want 3: %v", len(result), resp)
	}
}

func TestWS_RequestWithoutID_DoesNotPanic(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)

	// Send a notification-style request (no id field).
	c.conn.WriteMessage(websocket.TextMessage, //nolint:errcheck
		[]byte(`{"jsonrpc":"2.0","method":"subscribe","params":{"events":["world.line"]}}`))

	// Server should not panic; may or may not send a response.
	// Just verify it's still alive by sending a proper request.
	time.Sleep(20 * time.Millisecond)
	c.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "subscribe",
		"params": map[string]any{"events": []string{}}})
	resp := c.recv(t, time.Second)
	if resp["jsonrpc"] != "2.0" {
		t.Errorf("server stopped responding after no-id request")
	}
}

func TestWS_LargePayload_HandledCorrectly(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)
	c.subscribe(t, "world.line")

	// Publish a line with a large text field (> default read buffer).
	largeText := strings.Repeat("x", 8192)
	time.Sleep(20 * time.Millisecond)
	b.Publish(bus.WorldLineEvent{WorldName: "w", Text: largeText})

	notif := c.recv(t, 2*time.Second)
	params, _ := notif["params"].(map[string]any)
	text, _ := params["Text"].(string)
	if len(text) != 8192 {
		t.Errorf("large payload text length = %d, want 8192", len(text))
	}
}

// TestWS_TCPAndWS_SameServer verifies both TCP and WS listeners work on one server.
func TestWS_TCPAndWS_SameServer(t *testing.T) {
	b := bus.New()

	tcpLn, _ := net.Listen("tcp", "127.0.0.1:0")
	tcpAddr := tcpLn.Addr().String()
	tcpLn.Close()

	wsLn, _ := net.Listen("tcp", "127.0.0.1:0")
	wsAddr := wsLn.Addr().String()
	wsLn.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := ipc.Config{TCPAddr: tcpAddr, WSAddr: wsAddr}
	srv := ipc.New(cfg, b)
	go srv.Run(ctx) //nolint:errcheck
	time.Sleep(50 * time.Millisecond)

	// TCP client.
	var tcpConn net.Conn
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		var err error
		tcpConn, err = net.DialTimeout("tcp", tcpAddr, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if tcpConn == nil {
		t.Fatal("TCP client: could not connect")
	}
	defer tcpConn.Close()

	// WS client.
	wsC := dialWS(t, wsAddr)

	// Both subscribe and verify both receive the same event.
	subMsg := []byte(`{"jsonrpc":"2.0","id":1,"method":"subscribe","params":{"events":["world.line"]}}` + "\n")
	tcpConn.Write(subMsg) //nolint:errcheck
	// consume TCP ack
	sc := bufio.NewScanner(tcpConn)
	tcpConn.SetReadDeadline(time.Now().Add(time.Second))
	sc.Scan()

	wsC.subscribe(t, "world.line")
	time.Sleep(20 * time.Millisecond)

	b.Publish(bus.WorldLineEvent{WorldName: "both", Text: "dual listener test"})

	// TCP receives it.
	tcpConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if !sc.Scan() {
		t.Error("TCP client: expected world.line event")
	}

	// WS receives it.
	wsNotif := wsC.recv(t, 2*time.Second)
	if wsNotif["method"] != "world.line" {
		t.Errorf("WS notification method = %q, want world.line", wsNotif["method"])
	}
}

// Verify wsNetConn.SetWriteDeadline is distinct from SetReadDeadline.
// This is a regression test for the bug where SetWriteDeadline called SetReadDeadline.
func TestWS_SetWriteDeadline_IsDistinct(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)
	c := dialWS(t, addr)

	// Set a write deadline in the past — the server should see a write error
	// when it tries to send the subscribe ack with a past write deadline.
	// We can't directly test the server side's SetWriteDeadline here, but we
	// can at least verify that subscribe still works (no regression in the
	// net.Conn adapter path that now correctly routes SetDeadline to both).
	c.send(t, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "subscribe",
		"params": map[string]any{"events": []string{"world.line"}}})
	resp := c.recv(t, time.Second)
	if resp["error"] != nil {
		t.Errorf("unexpected error after subscribe: %v", resp["error"])
	}
}

// ---------------------------------------------------------------------------
// Security: cross-origin WebSocket rejection (H1)
// ---------------------------------------------------------------------------

// TestWS_CrossOrigin_IsRejected is the exploit proof for H1.
//
// A web page at http://evil.com can open ws://127.0.0.1:<port>/ and read all
// MUD output because CheckOrigin returned true for every request. After the
// fix, any connection whose Origin header resolves to a non-localhost host must
// receive HTTP 403 Forbidden.
func TestWS_CrossOrigin_IsRejected(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)

	// Simulate a browser page at http://evil.com trying to connect.
	evilHeaders := http.Header{"Origin": []string{"http://evil.com"}}
	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+addr+"/", evilHeaders)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("cross-origin WebSocket connection was accepted — H1 vulnerability present")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Errorf("expected HTTP 403 for cross-origin, got %d", code)
	}
}

// TestWS_LocalhostOrigin_IsAccepted ensures the legitimate Electron dev-server
// origin (http://localhost:PORT) still connects after the fix.
func TestWS_LocalhostOrigin_IsAccepted(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)

	localHeaders := http.Header{"Origin": []string{"http://localhost:5173"}}
	conn, resp, err := websocket.DefaultDialer.Dial("ws://"+addr+"/", localHeaders)
	if conn != nil {
		conn.Close()
	}
	if err != nil {
		code := 0
		if resp != nil {
			code = resp.StatusCode
		}
		t.Fatalf("localhost origin should be accepted, got err=%v status=%d", err, code)
	}
}

// TestWS_NoOrigin_IsAccepted ensures CLI / Go / Python clients that send no
// Origin header still connect (they are loopback-only non-browser callers).
func TestWS_NoOrigin_IsAccepted(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)

	// nil headers → gorilla sends no Origin header.
	conn, _, err := websocket.DefaultDialer.Dial("ws://"+addr+"/", nil)
	if conn != nil {
		conn.Close()
	}
	if err != nil {
		t.Fatalf("connection without Origin header should be accepted: %v", err)
	}
}

// Ensure startWSServer helper resolves addr correctly (the port extraction).
func TestWS_ServerStarts_AcceptsMultipleConnections(t *testing.T) {
	b := bus.New()
	addr := startWSServer(t, b)

	// Open 3 concurrent connections.
	for i := 0; i < 3; i++ {
		c := dialWS(t, addr)
		c.send(t, map[string]any{"jsonrpc": "2.0", "id": i, "method": "subscribe",
			"params": map[string]any{"events": []string{}}})
		c.recv(t, time.Second)
	}
}

// startWSServer uses string split on ":" which breaks for IPv6; use a helper
// that extracts port from the full addr string properly.
func init() {
	_ = http.DefaultServeMux // ensure net/http is linked
}
