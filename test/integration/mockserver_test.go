// Package integration contains end-to-end tests that spin up real mock MUD
// servers and drive GoFugue subsystems through their full lifecycle.
//
// Run with: go test -race ./test/integration/...
package integration_test

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Mock TCP MUD server
// ---------------------------------------------------------------------------

// mudServer is a lightweight TCP server that speaks Telnet line protocol.
// Tests configure it with a script of responses to send after connection.
type mudServer struct {
	ln      net.Listener
	addr    string
	mu      sync.Mutex
	conns   []net.Conn
	handler func(conn net.Conn) // called in goroutine per accepted connection
}

func newMudServer(t *testing.T, handler func(net.Conn)) *mudServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mudServer listen: %v", err)
	}
	s := &mudServer{ln: ln, addr: ln.Addr().String(), handler: handler}
	go s.serve()
	t.Cleanup(s.close)
	return s
}

func (s *mudServer) serve() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, conn)
		s.mu.Unlock()
		if s.handler != nil {
			go s.handler(conn)
		}
	}
}

func (s *mudServer) close() {
	s.ln.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		c.Close()
	}
}

// sendLine sends a plain text line (LF-terminated) to the client.
func sendLine(conn net.Conn, line string) {
	fmt.Fprintf(conn, "%s\r\n", line)
}

// sendGMCP sends a Telnet GMCP subnegotiation.
// IAC SB GMCP <module> <space> <json> IAC SE
func sendGMCP(conn net.Conn, module string, data any) {
	payload, _ := json.Marshal(data)
	msg := fmt.Sprintf("%s %s", module, payload)
	// Telnet: IAC=255, SB=250, GMCP=201, SE=240
	buf := []byte{255, 250, 201}
	buf = append(buf, []byte(msg)...)
	buf = append(buf, 255, 240)
	conn.Write(buf) //nolint:errcheck
}

// sendMCCP2Negotiation sends the MCCP2 WILL negotiation sequence then
// transitions the connection to zlib-compressed output.
// Returns a zlib writer the caller should use for subsequent output.
func sendMCCP2Start(conn net.Conn) *zlib.Writer {
	// IAC WILL COMPRESS2 (255 251 86)
	conn.Write([]byte{255, 251, 86}) //nolint:errcheck
	// IAC SB COMPRESS2 IAC SE  (255 250 86 255 240)
	conn.Write([]byte{255, 250, 86, 255, 240}) //nolint:errcheck
	zw, _ := zlib.NewWriterLevel(conn, zlib.DefaultCompression)
	return zw
}

// readLines reads all lines sent from client until deadline or conn close.
func readLines(conn net.Conn, timeout time.Duration) []string {
	conn.SetReadDeadline(time.Now().Add(timeout))
	var lines []string
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	return lines
}

// ---------------------------------------------------------------------------
// IPC helpers (reused across tests)
// ---------------------------------------------------------------------------

type ipcConn struct {
	conn    net.Conn
	scanner *bufio.Scanner
	enc     *json.Encoder
}

func dialIPC(t *testing.T, addr string) *ipcConn {
	t.Helper()
	var conn net.Conn
	var err error
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn, err = net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("dialIPC %s: %v", addr, err)
	}
	t.Cleanup(func() { conn.Close() })
	return &ipcConn{
		conn:    conn,
		scanner: bufio.NewScanner(conn),
		enc:     json.NewEncoder(conn),
	}
}

func (c *ipcConn) send(v any) {
	c.enc.Encode(v) //nolint:errcheck
}

func (c *ipcConn) recv(t *testing.T, timeout time.Duration) map[string]any {
	t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(timeout))
	if !c.scanner.Scan() {
		t.Fatalf("ipcConn: no data (err=%v)", c.scanner.Err())
	}
	var m map[string]any
	if err := json.Unmarshal(c.scanner.Bytes(), &m); err != nil {
		t.Fatalf("ipcConn recv unmarshal: %v (raw: %s)", err, c.scanner.Bytes())
	}
	return m
}

func (c *ipcConn) subscribe(t *testing.T, events ...string) {
	t.Helper()
	c.send(map[string]any{
		"jsonrpc": "2.0", "id": 1,
		"method": "subscribe",
		"params": map[string]any{"events": events},
	})
	c.recv(t, 500*time.Millisecond) // consume ack
}

// ---------------------------------------------------------------------------
// waitFor polls fn until it returns true or timeout elapses.
// ---------------------------------------------------------------------------

func waitFor(t *testing.T, timeout time.Duration, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for condition")
}

// ---------------------------------------------------------------------------
// Transport-level integration tests
// ---------------------------------------------------------------------------

func TestMockServer_AcceptsConnection(t *testing.T) {
	connected := make(chan struct{}, 1)
	srv := newMudServer(t, func(conn net.Conn) {
		connected <- struct{}{}
		conn.Close()
	})

	conn, err := net.DialTimeout("tcp", srv.addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("server never accepted connection")
	}
}

func TestMockServer_SendsLines_ClientReceives(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Welcome to MockMUD!")
		sendLine(conn, "You are in a dark room.")
		conn.Close()
	})

	conn, _ := net.DialTimeout("tcp", srv.addr, time.Second)
	defer conn.Close()

	lines := readLines(conn, time.Second)
	if len(lines) < 2 {
		t.Fatalf("expected ≥2 lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "Welcome to MockMUD!" {
		t.Errorf("lines[0] = %q, want %q", lines[0], "Welcome to MockMUD!")
	}
}

func TestMockServer_ClientSendsInput_ServerReceives(t *testing.T) {
	received := make(chan string, 1)
	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Password: ")
		sc := bufio.NewScanner(conn)
		if sc.Scan() {
			received <- sc.Text()
		}
		conn.Close()
	})

	conn, _ := net.DialTimeout("tcp", srv.addr, time.Second)
	defer conn.Close()

	readLines(conn, 200*time.Millisecond) // consume "Password: "
	fmt.Fprintf(conn, "sekret\r\n")

	select {
	case got := <-received:
		if got != "sekret" {
			t.Errorf("server received %q, want %q", got, "sekret")
		}
	case <-time.After(time.Second):
		t.Fatal("server never received input")
	}
}

// ---------------------------------------------------------------------------
// GMCP parsing tests (unit-level, using mock server data)
// ---------------------------------------------------------------------------

func TestGMCP_WireFormat_Valid(t *testing.T) {
	pr, pw := io.Pipe()

	go func() {
		// Build GMCP packet manually into the pipe writer.
		payload, _ := json.Marshal(map[string]any{"hp": 100, "maxhp": 200})
		msg := "Char.Vitals " + string(payload)
		buf := []byte{255, 250, 201}
		buf = append(buf, []byte(msg)...)
		buf = append(buf, 255, 240)
		pw.Write(buf) //nolint:errcheck
		pw.Close()
	}()

	data, err := io.ReadAll(pr)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	// IAC SB GMCP ... IAC SE
	if len(data) < 5 {
		t.Fatalf("GMCP packet too short: %d bytes", len(data))
	}
	if data[0] != 255 || data[1] != 250 || data[2] != 201 {
		t.Errorf("missing IAC SB GMCP header: %v", data[:3])
	}
	if data[len(data)-2] != 255 || data[len(data)-1] != 240 {
		t.Errorf("missing IAC SE footer: %v", data[len(data)-2:])
	}

	// payload between the headers
	payload := data[3 : len(data)-2]
	module, jsonData, found := bytes.Cut(payload, []byte(" "))
	if !found {
		t.Fatalf("GMCP payload missing space separator: %s", payload)
	}
	if string(module) != "Char.Vitals" {
		t.Errorf("module = %q, want %q", module, "Char.Vitals")
	}
	var obj map[string]any
	if err := json.Unmarshal(jsonData, &obj); err != nil {
		t.Fatalf("GMCP JSON unmarshal: %v", err)
	}
	if hp, ok := obj["hp"].(float64); !ok || hp != 100 {
		t.Errorf("GMCP hp = %v, want 100", obj["hp"])
	}
}

// ---------------------------------------------------------------------------
// MCCP2 wire format test
// ---------------------------------------------------------------------------

func TestMCCP2_ZlibStream_RoundTrip(t *testing.T) {
	pr, pw := io.Pipe()

	go func() {
		zw, err := zlib.NewWriterLevel(pw, zlib.DefaultCompression)
		if err != nil {
			t.Errorf("zlib.NewWriterLevel: %v", err)
			pw.Close()
			return
		}
		fmt.Fprintf(zw, "compressed line 1\r\n")
		fmt.Fprintf(zw, "compressed line 2\r\n")
		zw.Close()
		pw.Close()
	}()

	zr, err := zlib.NewReader(pr)
	if err != nil {
		t.Fatalf("zlib.NewReader: %v", err)
	}
	defer zr.Close()

	sc := bufio.NewScanner(zr)
	var lines []string
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}

	if len(lines) != 2 {
		t.Fatalf("expected 2 decompressed lines, got %d: %v", len(lines), lines)
	}
	if lines[0] != "compressed line 1" {
		t.Errorf("lines[0] = %q", lines[0])
	}
	if lines[1] != "compressed line 2" {
		t.Errorf("lines[1] = %q", lines[1])
	}
}

// ---------------------------------------------------------------------------
// IPC server integration tests
// ---------------------------------------------------------------------------

func TestIPC_HeadlessMode_EventFlow(t *testing.T) {
	// Start the IPC server directly (not via binary — tests the package).
	b := newTestBus(t)
	addr := startTestIPCServer(t, b)

	client := dialIPC(t, addr)
	client.subscribe(t, "world.line", "gmcp", "hook")

	// Simulate a world line arriving on the bus.
	publishWorldLine(b, "testworld", "You see a dragon here.")
	msg := client.recv(t, 500*time.Millisecond)

	if msg["method"] != "world.line" {
		t.Errorf("method = %v, want world.line", msg["method"])
	}
	params, _ := msg["params"].(map[string]any)
	if params == nil {
		t.Fatal("params is nil")
	}
	if params["Text"] != "You see a dragon here." {
		t.Errorf("Text = %v, want %q", params["Text"], "You see a dragon here.")
	}
}

func TestIPC_HeadlessMode_GMCPEventFlow(t *testing.T) {
	b := newTestBus(t)
	addr := startTestIPCServer(t, b)

	client := dialIPC(t, addr)
	client.subscribe(t, "gmcp")

	publishGMCP(b, "testworld", "Char.Vitals", []byte(`{"hp":42,"maxhp":100}`))
	msg := client.recv(t, 500*time.Millisecond)

	if msg["method"] != "gmcp" {
		t.Errorf("method = %v, want gmcp", msg["method"])
	}
}

func TestIPC_HeadlessMode_HookEventFlow(t *testing.T) {
	b := newTestBus(t)
	addr := startTestIPCServer(t, b)

	client := dialIPC(t, addr)
	client.subscribe(t, "hook")

	publishHook(b, "testworld", "CONNECT")
	msg := client.recv(t, 500*time.Millisecond)

	if msg["method"] != "hook" {
		t.Errorf("method = %v, want hook", msg["method"])
	}
	params, _ := msg["params"].(map[string]any)
	if params["Name"] != "CONNECT" {
		t.Errorf("hook Name = %v, want CONNECT", params["Name"])
	}
}

func TestIPC_MultipleClients_IndependentSubscriptions(t *testing.T) {
	b := newTestBus(t)
	addr := startTestIPCServer(t, b)

	// Client A: subscribes to world.line only
	cA := dialIPC(t, addr)
	cA.subscribe(t, "world.line")

	// Client B: subscribes to gmcp only
	cB := dialIPC(t, addr)
	cB.subscribe(t, "gmcp")

	publishWorldLine(b, "w", "line for A")
	publishGMCP(b, "w", "Char.Vitals", []byte(`{}`))

	// A should get the world line
	msgA := cA.recv(t, 500*time.Millisecond)
	if msgA["method"] != "world.line" {
		t.Errorf("client A got %v, want world.line", msgA["method"])
	}

	// B should get the gmcp event
	msgB := cB.recv(t, 500*time.Millisecond)
	if msgB["method"] != "gmcp" {
		t.Errorf("client B got %v, want gmcp", msgB["method"])
	}
}

func TestIPC_ServerContext_Cancel_DisconnectsClients(t *testing.T) {
	b := newTestBus(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())

	// import inline to avoid cycle:
	go func() {
		startIPCServerWithCtx(ctx, addr, b)
	}()
	time.Sleep(30 * time.Millisecond)

	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}

	cancel() // shut down server

	conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err = conn.Read(buf)
	if err == nil {
		t.Error("expected connection to be closed after server context cancel")
	}
}

// ---------------------------------------------------------------------------
// Bus + Macro pipeline integration
// ---------------------------------------------------------------------------

func TestPipeline_TriggerFiresOnWorldLine(t *testing.T) {
	b := newTestBus(t)
	engine := newTestMacroEngine(t)

	// Define a trigger that matches incoming lines.
	defineTrigger(engine, "dragon-trigger", `(?i)dragon`, "say I see a dragon!")

	// Subscribe to the bus to capture world lines.
	sub := b.Subscribe(16, busEvWorldLine)
	defer sub.Cancel()

	// Publish a world line.
	publishWorldLine(b, "w", "A red dragon appears.")

	// Receive and run through trigger engine.
	ev := waitBusEvent(t, sub, 500*time.Millisecond)
	body, caps := matchTrigger(engine, ev.World(), ev.(worldLineEvent).Text)
	if body == "" {
		t.Fatal("trigger should have matched the dragon line")
	}
	if len(caps) == 0 {
		t.Fatal("expected capture groups")
	}
}

func TestPipeline_TriggerDoesNotFireOnMismatch(t *testing.T) {
	b := newTestBus(t)
	engine := newTestMacroEngine(t)
	defineTrigger(engine, "t", `dragon`, "body")

	sub := b.Subscribe(16, busEvWorldLine)
	defer sub.Cancel()
	publishWorldLine(b, "w", "A goblin scurries past.")

	ev := waitBusEvent(t, sub, 200*time.Millisecond)
	body, _ := matchTrigger(engine, ev.World(), ev.(worldLineEvent).Text)
	if body != "" {
		t.Errorf("trigger should not fire for goblin line, got body %q", body)
	}
}

func TestPipeline_HookFiresOnConnect(t *testing.T) {
	b := newTestBus(t)
	engine := newTestMacroEngine(t)
	defineHook(engine, "on-connect", "CONNECT", "say Connected!")

	sub := b.Subscribe(16, busEvHook)
	defer sub.Cancel()
	publishHook(b, "testworld", "CONNECT")

	ev := waitBusEvent(t, sub, 200*time.Millisecond)
	bodies := fireHook(engine, ev.World(), ev.(hookEvent).Name)
	if len(bodies) == 0 {
		t.Fatal("hook should fire on CONNECT")
	}
	if bodies[0] != "say Connected!" {
		t.Errorf("hook body = %q, want %q", bodies[0], "say Connected!")
	}
}

func TestPipeline_MultipleWorlds_EventsAreScopedCorrectly(t *testing.T) {
	b := newTestBus(t)
	engine := newTestMacroEngine(t)

	// Trigger scoped to world-A only.
	defineTriggerWorld(engine, "scoped", `test`, "body", "world-a")

	sub := b.Subscribe(16, busEvWorldLine)
	defer sub.Cancel()

	publishWorldLine(b, "world-a", "test line")
	evA := waitBusEvent(t, sub, 200*time.Millisecond)
	body, _ := matchTrigger(engine, evA.World(), evA.(worldLineEvent).Text)
	if body == "" {
		t.Error("trigger should fire for world-a")
	}

	publishWorldLine(b, "world-b", "test line")
	evB := waitBusEvent(t, sub, 200*time.Millisecond)
	body, _ = matchTrigger(engine, evB.World(), evB.(worldLineEvent).Text)
	if body != "" {
		t.Errorf("trigger should not fire for world-b, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// Stress / concurrency integration tests
// ---------------------------------------------------------------------------

func TestStress_BusUnderLoad_NoDeadlock(t *testing.T) {
	b := newTestBus(t)
	const (
		publishers   = 5
		subscribers  = 10
		msgsEach     = 200
		testDuration = 2 * time.Second
	)

	ctx, cancel := context.WithTimeout(context.Background(), testDuration)
	defer cancel()

	var wg sync.WaitGroup

	for i := 0; i < subscribers; i++ {
		sub := b.Subscribe(64, busEvWorldLine)
		wg.Add(1)
		go func(s interface{ Cancel() }) {
			defer wg.Done()
			defer s.Cancel()
			for {
				select {
				case <-ctx.Done():
					return
				case <-subChan(s):
				}
			}
		}(sub)
	}

	for i := 0; i < publishers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < msgsEach; j++ {
				select {
				case <-ctx.Done():
					return
				default:
					publishWorldLine(b, fmt.Sprintf("world-%d", id),
						fmt.Sprintf("message %d", j))
				}
			}
		}(i)
	}

	wg.Wait()
}

func TestStress_IPC_ManyClients(t *testing.T) {
	b := newTestBus(t)
	addr := startTestIPCServer(t, b)

	const numClients = 20
	var wg sync.WaitGroup

	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := dialIPC(t, addr)
			c.subscribe(t, "world.line")
		}()
	}
	wg.Wait()

	// Publish one event — all clients should receive it without deadlock.
	publishWorldLine(b, "w", "broadcast to all")
	time.Sleep(100 * time.Millisecond) // give clients time to drain
}
