package ipc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/ipc"
)

// startAuthServer creates an IPC server with token authentication enabled.
func startAuthServer(t *testing.T, b *bus.Bus, token string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	cfg := ipc.Config{TCPAddr: addr, Token: token}
	srv := ipc.New(cfg, b)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Run(ctx) //nolint:errcheck
	time.Sleep(30 * time.Millisecond)
	return addr
}

// TestAuth_NoAuthMessage_ConnectionClosed verifies that a client that sends any
// non-auth message as its first message is rejected with an auth error and then
// the connection is closed.
func TestAuth_NoAuthMessage_ConnectionClosed(t *testing.T) {
	b := bus.New()
	addr := startAuthServer(t, b, "supersecrettoken")
	conn := dial(t, addr)

	// Send a subscribe request WITHOUT authenticating first.
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "subscribe",
		"params":  map[string]any{"events": []string{"world.line"}},
	})

	// The server must respond with an auth error.
	var resp ipc.Response
	recvJSON(t, conn, &resp)

	if resp.Error == nil {
		t.Fatal("expected auth error response, got no error")
	}
	if resp.Error.Code != -32001 {
		t.Errorf("error code = %d, want -32001 (auth required)", resp.Error.Code)
	}

	// After the error the server must close the connection. The next read should
	// return EOF or a timeout / empty scan.
	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	sc := bufio.NewScanner(conn)
	if sc.Scan() {
		t.Errorf("expected connection closed, but got extra data: %s", sc.Bytes())
	}
}

// TestAuth_WrongToken_ConnectionClosed verifies that an auth message with the
// wrong token is rejected.
func TestAuth_WrongToken_ConnectionClosed(t *testing.T) {
	b := bus.New()
	addr := startAuthServer(t, b, "correcttoken")
	conn := dial(t, addr)

	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "auth",
		"params":  map[string]any{"token": "wrongtoken"},
	})

	var resp ipc.Response
	recvJSON(t, conn, &resp)

	if resp.Error == nil {
		t.Fatal("expected auth error response for wrong token")
	}
	if resp.Error.Code != -32001 {
		t.Errorf("error code = %d, want -32001 (auth required)", resp.Error.Code)
	}

	// Connection must be closed after auth failure.
	conn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	sc := bufio.NewScanner(conn)
	if sc.Scan() {
		t.Errorf("expected connection closed after wrong token, got: %s", sc.Bytes())
	}
}

// TestAuth_CorrectToken_CanSubscribe verifies that a client that sends the
// correct token as its first message is admitted and can use the API normally.
func TestAuth_CorrectToken_CanSubscribe(t *testing.T) {
	b := bus.New()
	const tok = "correcttoken"
	addr := startAuthServer(t, b, tok)
	conn := dial(t, addr)

	// Authenticate first.
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "auth",
		"params":  map[string]any{"token": tok},
	})
	var authResp ipc.Response
	recvJSON(t, conn, &authResp)
	if authResp.Error != nil {
		t.Fatalf("auth failed: %+v", authResp.Error)
	}

	// Now subscribe — should work.
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      2,
		"method":  "subscribe",
		"params":  map[string]any{"events": []string{"world.line"}},
	})
	var subResp ipc.Response
	recvJSON(t, conn, &subResp)
	if subResp.Error != nil {
		t.Fatalf("subscribe after auth failed: %+v", subResp.Error)
	}
}

// TestAuth_NoToken_ServerSkipsAuth verifies that when no token is configured,
// the server admits clients without requiring auth (backwards-compatible).
func TestAuth_NoToken_ServerSkipsAuth(t *testing.T) {
	b := bus.New()
	// startServer from ipc_test.go uses an empty token (no auth).
	addr := startServer(t, b)
	conn := dial(t, addr)

	// Send subscribe directly without auth — must succeed.
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "subscribe",
		"params":  map[string]any{"events": []string{"world.line"}},
	})
	var resp ipc.Response
	recvJSON(t, conn, &resp)
	if resp.Error != nil {
		t.Fatalf("unexpected error when no token configured: %+v", resp.Error)
	}
}

// TestAuth_SecondAuthCall_AfterAuthSucceeded verifies that calling auth again
// after already authenticated is treated as a normal method (idempotent or
// handled gracefully) rather than closing the connection.
func TestAuth_SecondAuthCall_AfterAuthSucceeded(t *testing.T) {
	b := bus.New()
	const tok = "mytoken"
	addr := startAuthServer(t, b, tok)
	conn := dial(t, addr)

	// First auth.
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "auth",
		"params": map[string]any{"token": tok},
	})
	var r1 ipc.Response
	recvJSON(t, conn, &r1)
	if r1.Error != nil {
		t.Fatalf("first auth failed: %+v", r1.Error)
	}

	// Second auth with correct token — should succeed (idempotent).
	sendJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "auth",
		"params": map[string]any{"token": tok},
	})
	var r2 ipc.Response
	recvJSON(t, conn, &r2)
	if r2.Error != nil {
		t.Fatalf("second auth failed unexpectedly: %+v", r2.Error)
	}
}

// TestAuth_Unix_SkipsAuth verifies that Unix socket connections skip token
// authentication (file permissions on the socket are sufficient).
func TestAuth_Unix_SkipsAuth(t *testing.T) {
	b := bus.New()
	tmpDir := t.TempDir()
	sockPath := tmpDir + "/test_auth.sock"

	// Unix socket server with a token configured — Unix path should still bypass auth.
	cfg := ipc.Config{SocketPath: sockPath, Token: "sometoken", UnixSkipsAuth: true}
	srv := ipc.New(cfg, b)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Run(ctx) //nolint:errcheck

	// Retry dial.
	var conn net.Conn
	var err error
	for i := 0; i < 10; i++ {
		conn, err = net.Dial("unix", sockPath)
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dial unix: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	// Subscribe without auth — must succeed on Unix socket.
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "subscribe",
		"params": map[string]any{"events": []string{"world.line"}},
	})
	data = append(data, '\n')
	conn.Write(data) //nolint:errcheck

	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	sc := bufio.NewScanner(conn)
	if !sc.Scan() {
		t.Fatal("expected response, got nothing")
	}
	var resp ipc.Response
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Error != nil {
		t.Fatalf("unexpected error on Unix socket without auth: %+v", resp.Error)
	}
}
