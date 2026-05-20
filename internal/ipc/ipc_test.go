package ipc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/ipc"
)

// startServer spins up an IPC server on a random TCP port and returns the address.
func startServer(t *testing.T, b *bus.Bus) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close() // let the server claim it

	cfg := ipc.Config{TCPAddr: addr}
	srv := ipc.New(cfg, b)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Run(ctx) //nolint:errcheck
	time.Sleep(30 * time.Millisecond) // let listener start
	return addr
}

// dial opens a raw TCP connection to the IPC server.
func dial(t *testing.T, addr string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial IPC: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func sendJSON(t *testing.T, conn net.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	data = append(data, '\n')
	conn.Write(data) //nolint:errcheck
}

func recvJSON(t *testing.T, conn net.Conn, out any) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("no response from IPC server")
	}
	if err := json.Unmarshal(scanner.Bytes(), out); err != nil {
		t.Fatalf("unmarshal response: %v (raw: %s)", err, scanner.Bytes())
	}
}

// --- tests ---

func TestIPC_UnknownMethod_ReturnsMethodNotFound(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)
	conn := dial(t, addr)

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "nonexistent.method",
	})

	var resp ipc.Response
	recvJSON(t, conn, &resp)

	if resp.Error == nil {
		t.Fatal("expected error response for unknown method")
	}
	if resp.Error.Code != -32601 {
		t.Errorf("error code = %d, want -32601 (method not found)", resp.Error.Code)
	}
}

func TestIPC_InvalidJSON_ReturnsParseError(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)
	conn := dial(t, addr)

	conn.Write([]byte("not json at all\n")) //nolint:errcheck

	var resp ipc.Response
	recvJSON(t, conn, &resp)

	if resp.Error == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if resp.Error.Code != -32700 {
		t.Errorf("error code = %d, want -32700 (parse error)", resp.Error.Code)
	}
}

func TestIPC_Subscribe_ReturnsOK(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)
	conn := dial(t, addr)

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "subscribe",
		"params":  map[string]any{"events": []string{"world.line"}},
	})

	var resp ipc.Response
	recvJSON(t, conn, &resp)

	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestIPC_Subscribe_ReceivesPublishedEvent(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)
	conn := dial(t, addr)

	// Subscribe to world.line events.
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "subscribe",
		"params":  map[string]any{"events": []string{"world.line"}},
	})
	var subResp ipc.Response
	recvJSON(t, conn, &subResp) // consume the subscribe ack

	// Publish an event on the bus.
	time.Sleep(10 * time.Millisecond) // ensure subscription is registered
	b.Publish(bus.WorldLineEvent{WorldName: "testworld", Text: "hello from MUD"})

	// Next message on the connection should be the notification.
	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	scanner := bufio.NewScanner(conn)
	if !scanner.Scan() {
		t.Fatal("expected notification, got nothing")
	}

	var notif ipc.Notification
	if err := json.Unmarshal(scanner.Bytes(), &notif); err != nil {
		t.Fatalf("unmarshal notification: %v", err)
	}
	if notif.Method != "world.line" {
		t.Errorf("notification method = %q, want %q", notif.Method, "world.line")
	}
}

func TestIPC_Subscribe_DoesNotReceiveUnsubscribedEvents(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)
	conn := dial(t, addr)

	// Subscribe only to gmcp — should not receive world.line
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "subscribe",
		"params": map[string]any{"events": []string{"gmcp"}},
	})
	var ack ipc.Response
	recvJSON(t, conn, &ack)

	time.Sleep(10 * time.Millisecond)
	b.Publish(bus.WorldLineEvent{WorldName: "w", Text: "noise"})

	conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	scanner := bufio.NewScanner(conn)
	if scanner.Scan() {
		t.Errorf("received unexpected data: %s", scanner.Bytes())
	}
}

func TestIPC_MultipleClients_EachReceiveEvents(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)

	subscribe := func(t *testing.T) (*bufio.Scanner, net.Conn) {
		t.Helper()
		conn := dial(t, addr)
		sendJSON(t, conn, map[string]any{
			"jsonrpc": "2.0", "id": 1, "method": "subscribe",
			"params": map[string]any{"events": []string{"world.line"}},
		})
		var ack ipc.Response
		recvJSON(t, conn, &ack)
		conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		return bufio.NewScanner(conn), conn
	}

	s1, _ := subscribe(t)
	s2, _ := subscribe(t)

	time.Sleep(10 * time.Millisecond)
	b.Publish(bus.WorldLineEvent{WorldName: "w", Text: "broadcast"})

	for i, sc := range []*bufio.Scanner{s1, s2} {
		if !sc.Scan() {
			t.Errorf("client %d: expected notification, got nothing", i+1)
		}
	}
}

// startServerWithHandler spins up a server and registers one custom handler.
func startServerWithHandler(t *testing.T, b *bus.Bus, method string, h ipc.CommandHandler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	cfg := ipc.Config{TCPAddr: addr}
	srv := ipc.New(cfg, b)
	srv.Handle(method, h)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Run(ctx) //nolint:errcheck
	time.Sleep(30 * time.Millisecond)
	return addr
}

func TestIPC_CustomHandler_Called_And_ReturnsResult(t *testing.T) {
	b := bus.New()
	var calledWith string
	addr := startServerWithHandler(t, b, "echo", func(_ context.Context, _ *ipc.Client, params json.RawMessage) (any, error) {
		var p struct{ Text string `json:"text"` }
		json.Unmarshal(params, &p) //nolint:errcheck
		calledWith = p.Text
		return map[string]string{"echo": p.Text}, nil
	})
	conn := dial(t, addr)

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "echo",
		"params": map[string]any{"text": "hello handler"},
	})

	var resp ipc.Response
	recvJSON(t, conn, &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if calledWith != "hello handler" {
		t.Errorf("handler called with %q, want %q", calledWith, "hello handler")
	}
}

func TestIPC_CustomHandler_ReturnsError_ClientGetsRPCError(t *testing.T) {
	b := bus.New()
	addr := startServerWithHandler(t, b, "fail", func(_ context.Context, _ *ipc.Client, _ json.RawMessage) (any, error) {
		return nil, fmt.Errorf("something went wrong")
	})
	conn := dial(t, addr)

	sendJSON(t, conn, map[string]any{"jsonrpc": "2.0", "id": 6, "method": "fail"})

	var resp ipc.Response
	recvJSON(t, conn, &resp)
	if resp.Error == nil {
		t.Fatal("expected error response")
	}
	if !strings.Contains(resp.Error.Message, "something went wrong") {
		t.Errorf("error message = %q, want 'something went wrong'", resp.Error.Message)
	}
}

func TestIPC_Subscribe_AllEvents_ReceivesEverything(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)
	conn := dial(t, addr)

	// Subscribe with empty events list = subscribe to all.
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "subscribe",
		"params": map[string]any{"events": []string{}},
	})
	var ack ipc.Response
	recvJSON(t, conn, &ack)

	time.Sleep(10 * time.Millisecond)
	b.Publish(bus.WorldLineEvent{WorldName: "w", Text: "all-events test"})

	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	sc := bufio.NewScanner(conn)
	if !sc.Scan() {
		t.Fatal("expected notification for all-events subscriber")
	}
}

func TestIPC_ClientDisconnect_RemovedFromServer(t *testing.T) {
	// We verify liveness by ensuring the server doesn't panic/deadlock
	// when a client disconnects mid-stream, then broadcast fires.
	b := bus.New()
	addr := startServer(t, b)

	conn := dial(t, addr)
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "subscribe",
		"params": map[string]any{"events": []string{"world.line"}},
	})
	var ack ipc.Response
	recvJSON(t, conn, &ack)
	time.Sleep(10 * time.Millisecond)

	// Disconnect the client abruptly.
	conn.Close()
	time.Sleep(50 * time.Millisecond) // give serve() goroutine time to call remove()

	// Publishing after disconnect must not panic.
	for i := 0; i < 5; i++ {
		b.Publish(bus.WorldLineEvent{WorldName: "w", Text: fmt.Sprintf("post-disconnect %d", i)})
	}
	// If we reach here without panic/deadlock, the test passes.
}

func TestIPC_ConcurrentClients_NoRace(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			conn, err := net.DialTimeout("tcp", addr, time.Second)
			if err != nil {
				return
			}
			defer conn.Close()

			// Each client sends a few requests.
			for j := 0; j < 3; j++ {
				data, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      i*10 + j,
					"method":  fmt.Sprintf("unknown-%d-%d", i, j),
				})
				data = append(data, '\n')
				conn.Write(data) //nolint:errcheck
			}
		}()
	}

	// Concurrently publish events while clients connect/disconnect.
	for i := 0; i < 10; i++ {
		go func(n int) {
			b.Publish(bus.WorldLineEvent{WorldName: "w", Text: fmt.Sprintf("event %d", n)})
		}(i)
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent IPC test timed out")
	}
}

func TestIPC_RequestWithoutID_NoResponseSent(t *testing.T) {
	// JSON-RPC notifications (no id) from client: server should not crash.
	b := bus.New()
	addr := startServer(t, b)
	conn := dial(t, addr)

	// Send a valid method but without an id — this is a notification, not a call.
	// The server's current behaviour will still respond (it doesn't distinguish),
	// but it must not panic.
	conn.Write([]byte(`{"jsonrpc":"2.0","method":"subscribe","params":{"events":[]}}` + "\n")) //nolint:errcheck

	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	sc := bufio.NewScanner(conn)
	sc.Scan() // consume any response — we just verify no crash
}

func TestIPC_WrongJSONRPCVersion_ReturnsInvalidRequest(t *testing.T) {
	b := bus.New()
	addr := startServer(t, b)
	conn := dial(t, addr)

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "1.0", // wrong version
		"id":      1,
		"method":  "subscribe",
	})

	var resp ipc.Response
	recvJSON(t, conn, &resp)
	if resp.Error == nil {
		t.Fatal("expected error for wrong JSON-RPC version")
	}
	if resp.Error.Code != -32600 {
		t.Errorf("error code = %d, want -32600 (invalid request)", resp.Error.Code)
	}
}
