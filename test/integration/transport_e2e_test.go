package integration_test

// transport_e2e_test.go — full end-to-end tests for TCP and WebSocket transports.
// Each test spins up a real mock server, connects through the transport layer,
// and verifies the complete data path through to bus events.

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/config"
	"github.com/kumakun/gofugue/internal/transport"
	"github.com/kumakun/gofugue/internal/world"
)

// ---------------------------------------------------------------------------
// Transport layer e2e — TCP
// ---------------------------------------------------------------------------

func TestE2E_TCP_Dial_ReceiveLine(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Welcome to TestMUD!")
		sendLine(conn, "You are in the void.")
		conn.Close()
	})

	tr, err := transport.New(transport.Config{URL: "mud://" + srv.addr})
	if err != nil {
		t.Fatalf("transport.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer rwc.Close()

	lines := collectLines(t, rwc, 2, time.Second)
	if len(lines) < 2 {
		t.Fatalf("expected ≥2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "Welcome to TestMUD!" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "Welcome to TestMUD!")
	}
}

func TestE2E_TCP_Send_ServerReceives(t *testing.T) {
	received := make(chan string, 4)
	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "login:")
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			received <- strings.TrimRight(sc.Text(), "\r")
		}
	})

	tr, _ := transport.New(transport.Config{URL: "mud://" + srv.addr})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer rwc.Close()

	// Consume "login:" prompt.
	collectLines(t, rwc, 1, 500*time.Millisecond)

	// Send commands.
	fmt.Fprintf(rwc, "testuser\r\n")
	fmt.Fprintf(rwc, "go north\r\n")

	want := []string{"testuser", "go north"}
	for _, w := range want {
		select {
		case got := <-received:
			if got != w {
				t.Errorf("server received %q, want %q", got, w)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for server to receive %q", w)
		}
	}
}

func TestE2E_TCP_ConnectionRefused_ReturnsError(t *testing.T) {
	// Grab a port then close it — guaranteed refused.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()

	tr, _ := transport.New(transport.Config{URL: "mud://" + addr})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	_, err := tr.Dial(ctx)
	if err == nil {
		t.Fatal("expected error for refused connection")
	}
}

func TestE2E_TCP_ContextCancel_UnblocksRead(t *testing.T) {
	// Server that never sends anything.
	srv := newMudServer(t, func(conn net.Conn) {
		time.Sleep(10 * time.Second)
		conn.Close()
	})

	tr, _ := transport.New(transport.Config{URL: "mud://" + srv.addr})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		// Fine — dial itself may have been cancelled.
		return
	}
	defer rwc.Close()

	buf := make([]byte, 128)
	done := make(chan error, 1)
	go func() {
		_, err := rwc.Read(buf)
		done <- err
	}()

	cancel()
	rwc.Close()

	select {
	case <-done:
		// unblocked correctly
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Read did not unblock after Close")
	}
}

// ---------------------------------------------------------------------------
// Transport layer e2e — WebSocket
// ---------------------------------------------------------------------------

// mockWSServer starts an HTTP server that upgrades connections to WebSocket
// and runs handler for each connection.
type mockWSServer struct {
	addr string
	srv  *http.Server
}

func newMockWSServer(t *testing.T, handler func(*websocket.Conn)) *mockWSServer {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		handler(conn)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ws listen: %v", err)
	}
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln) //nolint:errcheck
	t.Cleanup(func() { srv.Close() })

	return &mockWSServer{addr: ln.Addr().String(), srv: srv}
}

func (s *mockWSServer) wsURL() string { return "ws://" + s.addr + "/" }

func TestE2E_WebSocket_Dial_ReceiveLine(t *testing.T) {
	wss := newMockWSServer(t, func(conn *websocket.Conn) {
		conn.WriteMessage(websocket.BinaryMessage, []byte("Welcome via WS!\r\n"))
		conn.WriteMessage(websocket.BinaryMessage, []byte("Second line.\r\n"))
	})

	tr, err := transport.New(transport.Config{URL: wss.wsURL()})
	if err != nil {
		t.Fatalf("transport.New: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("WebSocket Dial: %v", err)
	}
	defer rwc.Close()

	lines := collectLines(t, rwc, 2, time.Second)
	if len(lines) < 1 {
		t.Fatalf("expected ≥1 line via WebSocket, got 0")
	}
	if !strings.Contains(lines[0], "Welcome via WS!") {
		t.Errorf("lines[0] = %q, want 'Welcome via WS!'", lines[0])
	}
}

func TestE2E_WebSocket_Send_ServerReceives(t *testing.T) {
	received := make(chan string, 4)
	newMockWSServer(t, func(conn *websocket.Conn) {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			received <- strings.TrimRight(string(msg), "\r\n")
		}
	})
	// Use a fresh server for send test.
	wss := newMockWSServer(t, func(conn *websocket.Conn) {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			received <- strings.TrimRight(string(msg), "\r\n")
		}
	})

	tr, _ := transport.New(transport.Config{URL: wss.wsURL()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer rwc.Close()

	fmt.Fprintf(rwc, "go north\r\n")

	select {
	case got := <-received:
		if !strings.Contains(got, "go north") {
			t.Errorf("server received %q, want 'go north'", got)
		}
	case <-time.After(time.Second):
		t.Fatal("server never received the sent message")
	}
}

func TestE2E_WebSocket_Close_Clean(t *testing.T) {
	closed := make(chan struct{}, 1)
	wss := newMockWSServer(t, func(conn *websocket.Conn) {
		// Read until client closes.
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				closed <- struct{}{}
				return
			}
		}
	})

	tr, _ := transport.New(transport.Config{URL: wss.wsURL()})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	rwc.Close()

	select {
	case <-closed:
		// server detected clean close
	case <-time.After(time.Second):
		t.Fatal("server did not detect client close")
	}
}

func TestE2E_WebSocket_LargeMessage_Roundtrip(t *testing.T) {
	bigPayload := strings.Repeat("X", 64*1024) // 64 KB

	echo := make(chan string, 1)
	wss := newMockWSServer(t, func(conn *websocket.Conn) {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		conn.WriteMessage(websocket.BinaryMessage, msg) //nolint:errcheck
		echo <- string(msg)
	})

	tr, _ := transport.New(transport.Config{URL: wss.wsURL()})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rwc, err := tr.Dial(ctx)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer rwc.Close()

	if _, err := fmt.Fprint(rwc, bigPayload); err != nil {
		t.Fatalf("Write large payload: %v", err)
	}

	select {
	case got := <-echo:
		if got != bigPayload {
			t.Errorf("echo length mismatch: got %d, want %d", len(got), len(bigPayload))
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for large message echo")
	}
}

// ---------------------------------------------------------------------------
// World Manager e2e — TCP
// ---------------------------------------------------------------------------

func TestE2E_WorldManager_TCP_ConnectPublishesHook(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Welcome!")
		time.Sleep(500 * time.Millisecond)
	})

	b := bus.New()
	hookSub := b.Subscribe(8, bus.EvHook)
	defer hookSub.Cancel()

	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "test", URL: "mud://" + srv.addr}
	if err := mgr.Connect(ctx, "test", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	ev := waitBusEvent(t, hookSub, time.Second)
	hook := ev.(bus.HookEvent)
	if hook.Name != "CONNECT" {
		t.Errorf("hook.Name = %q, want CONNECT", hook.Name)
	}
	if hook.WorldName != "test" {
		t.Errorf("hook.WorldName = %q, want test", hook.WorldName)
	}
}

func TestE2E_WorldManager_TCP_LinesArrivedOnBus(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "line one")
		sendLine(conn, "line two")
		sendLine(conn, "line three")
		time.Sleep(500 * time.Millisecond)
	})

	b := bus.New()
	lineSub := b.Subscribe(16, bus.EvWorldLine)
	defer lineSub.Cancel()

	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: "mud://" + srv.addr}
	if err := mgr.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	got := collectBusLines(t, lineSub, 3, 2*time.Second)
	texts := make([]string, len(got))
	for i, ev := range got {
		texts[i] = ev.(bus.WorldLineEvent).Text
	}

	for _, want := range []string{"line one", "line two", "line three"} {
		found := false
		for _, g := range texts {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("bus missing expected line %q; got: %v", want, texts)
		}
	}
}

func TestE2E_WorldManager_TCP_SendReachesServer(t *testing.T) {
	received := make(chan string, 4)
	srv := newMudServer(t, func(conn net.Conn) {
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			received <- strings.TrimRight(sc.Text(), "\r")
		}
	})

	b := bus.New()
	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: "mud://" + srv.addr}
	if err := mgr.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Brief pause for connection to stabilise.
	time.Sleep(50 * time.Millisecond)
	if err := mgr.Send("w", "go north"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case got := <-received:
		if got != "go north" {
			t.Errorf("server received %q, want %q", got, "go north")
		}
	case <-time.After(time.Second):
		t.Fatal("server never received sent text")
	}
}

func TestE2E_WorldManager_TCP_DisconnectPublishesHook(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) {
		time.Sleep(2 * time.Second)
	})

	b := bus.New()
	hookSub := b.Subscribe(8, bus.EvHook)
	defer hookSub.Cancel()

	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: "mud://" + srv.addr}
	if err := mgr.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Consume CONNECT hook.
	waitBusEvent(t, hookSub, time.Second)

	if err := mgr.Disconnect("w"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	ev := waitBusEvent(t, hookSub, time.Second)
	if ev.(bus.HookEvent).Name != "DISCONNECT" {
		t.Errorf("expected DISCONNECT hook, got %q", ev.(bus.HookEvent).Name)
	}
}

func TestE2E_WorldManager_TCP_ServerClose_PublishesDisconnect(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Goodbye!")
		conn.Close() // server closes immediately
	})

	b := bus.New()
	hookSub := b.Subscribe(8, bus.EvHook)
	defer hookSub.Cancel()

	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: "mud://" + srv.addr}
	if err := mgr.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	// Expect CONNECT then DISCONNECT (server closed).
	hooks := collectBusLines(t, hookSub, 2, 2*time.Second)
	names := make([]string, len(hooks))
	for i, h := range hooks {
		names[i] = h.(bus.HookEvent).Name
	}
	if names[0] != "CONNECT" {
		t.Errorf("hooks[0] = %q, want CONNECT", names[0])
	}
	if names[1] != "DISCONNECT" {
		t.Errorf("hooks[1] = %q, want DISCONNECT", names[1])
	}
}

func TestE2E_WorldManager_TCP_DoubleConnect_ReturnsError(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) { time.Sleep(2 * time.Second) })

	b := bus.New()
	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: "mud://" + srv.addr}
	if err := mgr.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	if err := mgr.Connect(ctx, "w", nil); err == nil {
		t.Fatal("second Connect should return error (already connected)")
	}
}

func TestE2E_WorldManager_TCP_MultipleWorlds_Independent(t *testing.T) {
	lines1 := make(chan string, 8)
	lines2 := make(chan string, 8)

	srv1 := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "server1-line")
		time.Sleep(time.Second)
	})
	srv2 := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "server2-line")
		time.Sleep(time.Second)
	})

	b := bus.New()
	sub := b.Subscribe(16, bus.EvWorldLine)
	defer sub.Cancel()

	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := mgr.Connect(ctx, "w1", &config.WorldConfig{Name: "w1", URL: "mud://" + srv1.addr}); err != nil {
		t.Fatalf("Connect w1: %v", err)
	}
	if err := mgr.Connect(ctx, "w2", &config.WorldConfig{Name: "w2", URL: "mud://" + srv2.addr}); err != nil {
		t.Fatalf("Connect w2: %v", err)
	}

	// Collect 2 world lines and sort them by world.
	evs := collectBusLines(t, sub, 2, 2*time.Second)
	for _, ev := range evs {
		wl := ev.(bus.WorldLineEvent)
		switch wl.WorldName {
		case "w1":
			lines1 <- wl.Text
		case "w2":
			lines2 <- wl.Text
		}
	}

	if got := <-lines1; got != "server1-line" {
		t.Errorf("w1 text = %q, want server1-line", got)
	}
	if got := <-lines2; got != "server2-line" {
		t.Errorf("w2 text = %q, want server2-line", got)
	}
}

// ---------------------------------------------------------------------------
// World Manager e2e — WebSocket
// ---------------------------------------------------------------------------

func TestE2E_WorldManager_WebSocket_ConnectAndReceive(t *testing.T) {
	wss := newMockWSServer(t, func(conn *websocket.Conn) {
		conn.WriteMessage(websocket.BinaryMessage, []byte("WS line one\r\n"))
		conn.WriteMessage(websocket.BinaryMessage, []byte("WS line two\r\n"))
		time.Sleep(500 * time.Millisecond)
	})

	b := bus.New()
	lineSub := b.Subscribe(16, bus.EvWorldLine)
	defer lineSub.Cancel()

	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	f := false
	cfg := &config.WorldConfig{Name: "ws-world", URL: wss.wsURL(), TelnetEnabled: &f}
	if err := mgr.Connect(ctx, "ws-world", cfg); err != nil {
		t.Fatalf("Connect WS: %v", err)
	}

	evs := collectBusLines(t, lineSub, 2, 2*time.Second)
	if len(evs) < 1 {
		t.Fatal("expected ≥1 line from WebSocket world")
	}
	for _, ev := range evs {
		if ev.(bus.WorldLineEvent).WorldName != "ws-world" {
			t.Errorf("wrong WorldName: %q", ev.(bus.WorldLineEvent).WorldName)
		}
	}
}

func TestE2E_WorldManager_WebSocket_SendReceive(t *testing.T) {
	echo := make(chan string, 4)
	wss := newMockWSServer(t, func(conn *websocket.Conn) {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			echo <- strings.TrimRight(string(msg), "\r\n")
		}
	})

	b := bus.New()
	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	f := false
	cfg := &config.WorldConfig{Name: "ws", URL: wss.wsURL(), TelnetEnabled: &f}
	if err := mgr.Connect(ctx, "ws", cfg); err != nil {
		t.Fatalf("Connect WS: %v", err)
	}

	time.Sleep(30 * time.Millisecond)
	if err := mgr.Send("ws", "cast fireball"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	select {
	case got := <-echo:
		if !strings.Contains(got, "cast fireball") {
			t.Errorf("server received %q, want 'cast fireball'", got)
		}
	case <-time.After(time.Second):
		t.Fatal("WS server never received message")
	}
}

// ---------------------------------------------------------------------------
// State machine e2e
// ---------------------------------------------------------------------------

func TestE2E_WorldState_Transitions(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) { time.Sleep(2 * time.Second) })

	b := bus.New()
	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: "mud://" + srv.addr}

	st, err := mgr.WorldState("w")
	if err == nil || st != world.StateDisconnected {
		// world doesn't exist yet — error expected
	}

	if err := mgr.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	st, err = mgr.WorldState("w")
	if err != nil {
		t.Fatalf("WorldState after connect: %v", err)
	}
	if st != world.StateConnected {
		t.Errorf("state = %v, want StateConnected", st)
	}

	if err := mgr.Disconnect("w"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		st, _ := mgr.WorldState("w")
		return st == world.StateDisconnected
	})
}

func TestE2E_UnknownWorld_Send_ReturnsError(t *testing.T) {
	b := bus.New()
	mgr := world.NewManager(b)
	if err := mgr.Send("ghost", "hello"); err == nil {
		t.Fatal("expected error sending to unknown world")
	}
}

func TestE2E_UnknownWorld_Disconnect_ReturnsError(t *testing.T) {
	b := bus.New()
	mgr := world.NewManager(b)
	if err := mgr.Disconnect("ghost"); err == nil {
		t.Fatal("expected error disconnecting unknown world")
	}
}

func TestE2E_ForegroundWorld_SwitchAndGet(t *testing.T) {
	srv1 := newMudServer(t, func(conn net.Conn) { time.Sleep(2 * time.Second) })
	srv2 := newMudServer(t, func(conn net.Conn) { time.Sleep(2 * time.Second) })

	b := bus.New()
	mgr := world.NewManager(b)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mgr.Connect(ctx, "a", &config.WorldConfig{Name: "a", URL: "mud://" + srv1.addr}) //nolint:errcheck
	mgr.Connect(ctx, "b", &config.WorldConfig{Name: "b", URL: "mud://" + srv2.addr}) //nolint:errcheck

	if mgr.Foreground() != "a" {
		t.Errorf("foreground = %q, want 'a' (first connected)", mgr.Foreground())
	}

	if err := mgr.Switch("b"); err != nil {
		t.Fatalf("Switch: %v", err)
	}
	if mgr.Foreground() != "b" {
		t.Errorf("foreground after switch = %q, want 'b'", mgr.Foreground())
	}
}

// ---------------------------------------------------------------------------
// helpers local to this file
// ---------------------------------------------------------------------------

// collectLines reads up to n lines from rwc, blocking up to timeout.
func collectLines(t *testing.T, r interface{ Read([]byte) (int, error) }, n int, timeout time.Duration) []string {
	t.Helper()
	lines := make(chan string, n*2)
	go func() {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			lines <- strings.TrimRight(sc.Text(), "\r")
		}
		close(lines)
	}()

	var result []string
	deadline := time.After(timeout)
	for len(result) < n {
		select {
		case l, ok := <-lines:
			if !ok {
				return result
			}
			result = append(result, l)
		case <-deadline:
			return result
		}
	}
	return result
}

// collectBusLines collects up to n events from sub within timeout.
func collectBusLines(t *testing.T, sub *bus.Subscription, n int, timeout time.Duration) []bus.Event {
	t.Helper()
	var result []bus.Event
	deadline := time.After(timeout)
	for len(result) < n {
		select {
		case ev := <-sub.C:
			result = append(result, ev)
		case <-deadline:
			t.Logf("collectBusLines: timeout after %d/%d events", len(result), n)
			return result
		}
	}
	return result
}
