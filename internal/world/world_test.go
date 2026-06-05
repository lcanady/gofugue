package world_test

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/config"
	"github.com/kumakun/gofugue/internal/world"
)

// ---------------------------------------------------------------------------
// Minimal in-process TCP MUD server
// ---------------------------------------------------------------------------

// mudConn is returned by mudListen. Allows the test to write lines to the
// client and read what the client sent back.
type mudConn struct {
	conn net.Conn
	r    *bufio.Reader
}

func (m *mudConn) writeLine(s string) error {
	_, err := fmt.Fprintf(m.conn, "%s\r\n", s)
	return err
}

func (m *mudConn) readLine() (string, error) {
	return m.r.ReadString('\n')
}

func (m *mudConn) close() { m.conn.Close() }

// mudListen starts a one-shot TCP listener. Returns the address and a channel
// that delivers the first accepted connection.
func mudListen(t *testing.T) (addr string, accept <-chan *mudConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mudListen: %v", err)
	}
	ch := make(chan *mudConn, 1)
	go func() {
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		ch <- &mudConn{conn: conn, r: bufio.NewReader(conn)}
	}()
	t.Cleanup(func() { ln.Close() })
	return ln.Addr().String(), ch
}

// waitMudConn blocks until the server accepts a connection or the test times out.
func waitMudConn(t *testing.T, ch <-chan *mudConn) *mudConn {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server to accept connection")
		return nil
	}
}

// subscribe returns a subscription and drains it into a slice under a mutex.
type capture struct {
	mu    sync.Mutex
	lines []bus.WorldLineEvent
	hooks []bus.HookEvent
	stati []bus.StatusEvent
}

func subscribe(b *bus.Bus) *capture {
	c := &capture{}
	sub := b.Subscribe(256, bus.EvWorldLine, bus.EvHook, bus.EvStatus)
	go func() {
		defer sub.Cancel()
		for ev := range sub.C {
			switch e := ev.(type) {
			case bus.WorldLineEvent:
				c.mu.Lock()
				c.lines = append(c.lines, e)
				c.mu.Unlock()
			case bus.HookEvent:
				c.mu.Lock()
				c.hooks = append(c.hooks, e)
				c.mu.Unlock()
			case bus.StatusEvent:
				c.mu.Lock()
				c.stati = append(c.stati, e)
				c.mu.Unlock()
			}
		}
	}()
	return c
}

func (c *capture) waitLine(t *testing.T, substr string, d time.Duration) bus.WorldLineEvent {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, l := range c.lines {
			if strings.Contains(l.Text, substr) {
				c.mu.Unlock()
				return l
			}
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("timeout: no line containing %q", substr)
	return bus.WorldLineEvent{}
}

func (c *capture) waitHook(t *testing.T, name string, d time.Duration) bus.HookEvent {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, h := range c.hooks {
			if h.Name == name {
				c.mu.Unlock()
				return h
			}
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("timeout: no hook %q", name)
	return bus.HookEvent{}
}

func (c *capture) lineCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.lines)
}

func (c *capture) hookNames() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	names := make([]string, len(c.hooks))
	for i, h := range c.hooks {
		names[i] = h.Name
	}
	return names
}

// mudURL converts a raw TCP addr to a mud:// URL.
func mudURL(addr string) string { return "mud://" + addr }

// ---------------------------------------------------------------------------
// Manager.Add / pre-registration
// ---------------------------------------------------------------------------

func TestNewManager(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	if m == nil {
		t.Fatal("expected NewManager to return a non-nil Manager")
	}

	// Verify foreground is empty
	if m.Foreground() != "" {
		t.Errorf("expected empty foreground, got %q", m.Foreground())
	}

	// Verify we can add a world without panicking (ensures worlds map is initialized)
	cfg := config.WorldConfig{Name: "test_init", URL: "tcp://127.0.0.1:1234"}
	m.Add(cfg)
}

func TestManager_Add_RegistersWorld_NoConnection(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	m.Add(config.WorldConfig{Name: "myworld", URL: "mud://127.0.0.1:9999"})

	st, err := m.WorldState("myworld")
	if err != nil {
		t.Fatalf("WorldState: %v", err)
	}
	if st != world.StateDisconnected {
		t.Errorf("state = %v, want Disconnected", st)
	}
}

func TestManager_WorldState_Unknown_ReturnsError(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	_, err := m.WorldState("nosuch")
	if err == nil {
		t.Fatal("expected error for unknown world")
	}
}

// ---------------------------------------------------------------------------
// Manager.Connect — happy path
// ---------------------------------------------------------------------------

func TestManager_Connect_TransitionsToConnected(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w1", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w1", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w1") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	st, err := m.WorldState("w1")
	if err != nil {
		t.Fatalf("WorldState: %v", err)
	}
	if st != world.StateConnected {
		t.Errorf("state = %v, want Connected", st)
	}
}

func TestManager_Connect_SetsForeground_WhenFirstWorld(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "first", URL: mudURL(addr)}
	if err := m.Connect(ctx, "first", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("first") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	if fg := m.Foreground(); fg != "first" {
		t.Errorf("Foreground = %q, want %q", fg, "first")
	}
}

func TestManager_Connect_PublishesCONNECT_Hook(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	c := subscribe(b)
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	h := c.waitHook(t, "CONNECT", 3*time.Second)
	if h.WorldName != "w" {
		t.Errorf("hook world = %q, want %q", h.WorldName, "w")
	}
}

func TestManager_Connect_PublishesStatusEvent_Connected(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	statusSub := b.Subscribe(8, bus.EvStatus)
	defer statusSub.Cancel()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	select {
	case ev := <-statusSub.C:
		se := ev.(bus.StatusEvent)
		if !se.Connected || se.WorldName != "w" {
			t.Errorf("StatusEvent = %+v, want Connected=true world=w", se)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for StatusEvent")
	}
}

// ---------------------------------------------------------------------------
// Manager.Connect — error cases
// ---------------------------------------------------------------------------

func TestManager_Connect_InvalidURL_ReturnsError(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	ctx := context.Background()

	cfg := &config.WorldConfig{Name: "bad", URL: "mud://127.0.0.1:1"} // port 1 should be refused
	err := m.Connect(ctx, "bad", cfg)
	if err == nil {
		m.Disconnect("bad") //nolint:errcheck
		t.Fatal("expected connect error for refused port")
	}
}

func TestManager_Connect_LeavesDisconnectedOnError(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	ctx := context.Background()

	m.Add(config.WorldConfig{Name: "bad", URL: "mud://127.0.0.1:1"})
	m.Connect(ctx, "bad", nil) //nolint:errcheck — expected to fail

	st, err := m.WorldState("bad")
	if err != nil {
		t.Fatalf("WorldState: %v", err)
	}
	if st != world.StateDisconnected {
		t.Errorf("state after failed connect = %v, want Disconnected", st)
	}
}

func TestManager_Connect_AlreadyConnected_ReturnsError(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("first Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	// Second connect must fail.
	err := m.Connect(ctx, "w", cfg)
	if err == nil {
		t.Fatal("expected error for second Connect on already-connected world")
	}
	if !strings.Contains(err.Error(), "already connected") {
		t.Errorf("error = %q, want 'already connected'", err)
	}
}

func TestManager_Connect_UnknownWorld_NoConfig_ReturnsError(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	err := m.Connect(context.Background(), "nosuch", nil)
	if err == nil {
		t.Fatal("expected error for unknown world with nil config")
	}
}

// ---------------------------------------------------------------------------
// Manager.Disconnect
// ---------------------------------------------------------------------------

func TestManager_Disconnect_TransitionsToDisconnected(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	mc := waitMudConn(t, accept)
	defer mc.close()

	if err := m.Disconnect("w"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	st, err := m.WorldState("w")
	if err != nil {
		t.Fatalf("WorldState: %v", err)
	}
	if st != world.StateDisconnected {
		t.Errorf("state = %v, want Disconnected", st)
	}
}

func TestManager_Disconnect_PublishesDISCONNECT_Hook(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	c := subscribe(b)
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	mc := waitMudConn(t, accept)
	defer mc.close()

	c.waitHook(t, "CONNECT", 3*time.Second)

	if err := m.Disconnect("w"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}

	c.waitHook(t, "DISCONNECT", 3*time.Second)
}

func TestManager_Disconnect_Unknown_ReturnsError(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	err := m.Disconnect("nosuch")
	if err == nil {
		t.Fatal("expected error for unknown world")
	}
}

func TestManager_Disconnect_Idempotent_NotConnected(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	m.Add(config.WorldConfig{Name: "w", URL: "mud://127.0.0.1:9999"})

	// Disconnect on a registered-but-not-connected world should not panic/error.
	if err := m.Disconnect("w"); err != nil {
		t.Errorf("Disconnect on not-connected world: %v", err)
	}
}

// ---------------------------------------------------------------------------
// readLoop — incoming lines from server
// ---------------------------------------------------------------------------

func TestManager_ReadLoop_PublishesWorldLineEvent(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	c := subscribe(b)
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	if err := mc.writeLine("Hello, adventurer!"); err != nil {
		t.Fatalf("writeLine: %v", err)
	}

	ev := c.waitLine(t, "Hello, adventurer!", 3*time.Second)
	if ev.WorldName != "w" {
		t.Errorf("WorldName = %q, want %q", ev.WorldName, "w")
	}
}

func TestManager_ReadLoop_StripsCR(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	c := subscribe(b)
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	// Write raw bytes with \r\n — readLoop should strip \r.
	mc.conn.Write([]byte("line with CR\r\n")) //nolint:errcheck

	ev := c.waitLine(t, "line with CR", 3*time.Second)
	if strings.Contains(ev.Text, "\r") {
		t.Errorf("line contains \\r: %q", ev.Text)
	}
}

func TestManager_ReadLoop_MultipleLines_AllPublished(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	c := subscribe(b)
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	lines := []string{"line one", "line two", "line three"}
	for _, l := range lines {
		mc.writeLine(l) //nolint:errcheck
	}

	for _, l := range lines {
		c.waitLine(t, l, 3*time.Second)
	}
}

func TestManager_ReadLoop_ServerClose_TransitionsToDisconnected(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	c := subscribe(b)
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	mc := waitMudConn(t, accept)
	c.waitHook(t, "CONNECT", 3*time.Second)

	// Server closes connection — readLoop should detect EOF.
	mc.close()

	// World should transition to Disconnected and publish DISCONNECT hook.
	c.waitHook(t, "DISCONNECT", 3*time.Second)

	st, err := m.WorldState("w")
	if err != nil {
		t.Fatalf("WorldState: %v", err)
	}
	if st != world.StateDisconnected {
		t.Errorf("state after server close = %v, want Disconnected", st)
	}
}

func TestManager_ReadLoop_ContextCancel_StopsLoop(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	c := subscribe(b)
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	mc := waitMudConn(t, accept)
	defer mc.close()
	c.waitHook(t, "CONNECT", 3*time.Second)

	// Cancel context — world should disconnect cleanly.
	cancel()

	c.waitHook(t, "DISCONNECT", 3*time.Second)
}

// ---------------------------------------------------------------------------
// Manager.Send
// ---------------------------------------------------------------------------

func TestManager_Send_DeliversTextToServer(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	if err := m.Send("w", "go north"); err != nil {
		t.Fatalf("Send: %v", err)
	}

	line, err := mc.readLine()
	if err != nil {
		t.Fatalf("server readLine: %v", err)
	}
	line = strings.TrimRight(line, "\r\n")
	if line != "go north" {
		t.Errorf("server received %q, want %q", line, "go north")
	}
}

func TestManager_Send_AppendsLineTerminator(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	m.Send("w", "hello") //nolint:errcheck

	// Read raw bytes to verify \r\n terminator.
	buf := make([]byte, 16)
	n, err := mc.conn.Read(buf)
	if err != nil {
		t.Fatalf("raw read: %v", err)
	}
	raw := string(buf[:n])
	if !strings.HasSuffix(raw, "\r\n") {
		t.Errorf("sent %q, want \\r\\n suffix", raw)
	}
}

func TestManager_Send_NotConnected_ReturnsError(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	m.Add(config.WorldConfig{Name: "w", URL: "mud://127.0.0.1:9999"})
	err := m.Send("w", "hello")
	if err == nil {
		t.Fatal("expected error for Send on not-connected world")
	}
}

func TestManager_Send_UnknownWorld_ReturnsError(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	err := m.Send("nosuch", "hello")
	if err == nil {
		t.Fatal("expected error for Send on unknown world")
	}
}

// ---------------------------------------------------------------------------
// Manager.Switch / Foreground
// ---------------------------------------------------------------------------

func TestManager_Switch_UpdatesForeground(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	m.Add(config.WorldConfig{Name: "alpha", URL: "mud://127.0.0.1:9999"})
	m.Add(config.WorldConfig{Name: "beta", URL: "mud://127.0.0.1:9999"})

	if err := m.Switch("alpha"); err != nil {
		t.Fatalf("Switch alpha: %v", err)
	}
	if fg := m.Foreground(); fg != "alpha" {
		t.Errorf("Foreground = %q, want alpha", fg)
	}

	if err := m.Switch("beta"); err != nil {
		t.Fatalf("Switch beta: %v", err)
	}
	if fg := m.Foreground(); fg != "beta" {
		t.Errorf("Foreground = %q, want beta", fg)
	}
}

func TestManager_Switch_Unknown_ReturnsError(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	err := m.Switch("nosuch")
	if err == nil {
		t.Fatal("expected error for Switch to unknown world")
	}
}

func TestManager_Foreground_Empty_BeforeAnyConnect(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	// No world added — foreground should be empty string.
	if fg := m.Foreground(); fg != "" {
		t.Errorf("Foreground = %q, want empty before any connection", fg)
	}
}

// ---------------------------------------------------------------------------
// telnetEnabled — scheme-based detection
// ---------------------------------------------------------------------------

func TestTelnetEnabled_MudScheme_True(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: "mud://" + addr}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect mud://: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	// Connection succeeded — transport accepted mud:// scheme.
	st, _ := m.WorldState("w")
	if st != world.StateConnected {
		t.Errorf("mud:// connection state = %v, want Connected", st)
	}
}

// ---------------------------------------------------------------------------
// Concurrent operations — run with -race
// ---------------------------------------------------------------------------

func TestManager_ConcurrentConnect_OnlyOneSucceeds(t *testing.T) {
	// This test verifies the "already connected" guard is race-free.
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}

	var wg sync.WaitGroup
	errs := make([]error, 3)
	for i := 0; i < 3; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = m.Connect(ctx, "w", cfg)
		}()
	}
	wg.Wait()

	// Drain any accepted connections.
	go func() {
		for {
			select {
			case mc := <-accept:
				mc.close()
			case <-time.After(500 * time.Millisecond):
				return
			}
		}
	}()
	m.Disconnect("w") //nolint:errcheck

	// Exactly one should succeed.
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Errorf("concurrent Connect: %d succeeded, want exactly 1; errs: %v", successes, errs)
	}
}

func TestManager_ConcurrentSend_NoRace(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("w") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()

	// Drain the server side so writes don't block.
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := mc.conn.Read(buf); err != nil {
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			m.Send("w", fmt.Sprintf("msg %d", n)) //nolint:errcheck
		}(i)
	}
	wg.Wait()
}

func TestManager_ConcurrentDisconnect_Idempotent(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "w", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	mc := waitMudConn(t, accept)
	defer mc.close()

	// Multiple concurrent Disconnects — should not panic or deadlock.
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			m.Disconnect("w") //nolint:errcheck
		}()
	}

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("concurrent Disconnect deadlocked")
	}
}

// ---------------------------------------------------------------------------
// ErrUnknownWorld
// ---------------------------------------------------------------------------

func TestErrUnknownWorld_Message(t *testing.T) {
	b := bus.New()
	m := world.NewManager(b)
	err := m.Disconnect("ghost")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "ghost") {
		t.Errorf("error %q should mention world name", err)
	}
}

// ---------------------------------------------------------------------------
// Charset transcoding — Latin-1 bytes decoded to UTF-8
// ---------------------------------------------------------------------------

func TestCharset_Latin1_DecodesAccentedChars(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)
	cap := subscribe(b)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Register world with Latin-1 charset.
	cfg := &config.WorldConfig{Name: "latin", URL: mudURL(addr), Charset: "latin-1"}
	if err := m.Connect(ctx, "latin", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	mc := waitMudConn(t, accept)
	defer mc.close()

	// Send a Latin-1 encoded line: "café" = c a f 0xE9
	latin1Line := "caf\xe9\r\n"
	if _, err := mc.conn.Write([]byte(latin1Line)); err != nil {
		t.Fatalf("write latin-1: %v", err)
	}

	// The published WorldLineEvent.Text should be valid UTF-8 "café".
	ev := cap.waitLine(t, "caf", 5*time.Second)
	if !strings.Contains(ev.Text, "café") {
		t.Errorf("Latin-1 'café' not decoded correctly: got %q", ev.Text)
	}
}

func TestCharset_Latin1_MultipleAccented(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)
	cap := subscribe(b)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg2 := &config.WorldConfig{Name: "latin2", URL: mudURL(addr), Charset: "latin-1"}
	if err := m.Connect(ctx, "latin2", cfg2); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	mc := waitMudConn(t, accept)
	defer mc.close()

	// "Müll" in Latin-1: M \xfc l l
	if _, err := mc.conn.Write([]byte("M\xfcll\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := cap.waitLine(t, "M", 5*time.Second)
	if !strings.Contains(ev.Text, "Müll") {
		t.Errorf("Latin-1 'Müll' not decoded correctly: got %q", ev.Text)
	}
}

func TestCharset_UTF8_PassThrough(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)
	cap := subscribe(b)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Default charset (UTF-8 passthrough).
	cfg3 := &config.WorldConfig{Name: "utf8w", URL: mudURL(addr)}
	if err := m.Connect(ctx, "utf8w", cfg3); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	mc := waitMudConn(t, accept)
	defer mc.close()

	// Send valid UTF-8 (dragon emoji as UTF-8 bytes).
	if _, err := mc.conn.Write([]byte("龙\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := cap.waitLine(t, "龙", 5*time.Second)
	if !strings.Contains(ev.Text, "龙") {
		t.Errorf("UTF-8 pass-through failed: got %q", ev.Text)
	}
}

func TestCharset_NoCharset_PlainASCII_Unaffected(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)
	cap := subscribe(b)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cfg4 := &config.WorldConfig{Name: "ascii", URL: mudURL(addr)}
	if err := m.Connect(ctx, "ascii", cfg4); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	mc := waitMudConn(t, accept)
	defer mc.close()

	if _, err := mc.conn.Write([]byte("hello world\r\n")); err != nil {
		t.Fatalf("write: %v", err)
	}

	ev := cap.waitLine(t, "hello world", 5*time.Second)
	if ev.Text != "hello world" {
		t.Errorf("plain ASCII changed: got %q", ev.Text)
	}
}

// ---------------------------------------------------------------------------
// Security: TLSSkipVerify warning (M3)
// ---------------------------------------------------------------------------

// TestConnect_TLSSkipVerify_EmitsLocalWarning is the exploit proof for M3.
//
// When a user connects with tls_skip_verify = true, certificate verification is
// silently disabled. A user who set this once for a dev server and forgets is
// vulnerable to MITM on subsequent TLS connections. The fix: emit a visible
// warning line to the local echo buffer on every such connection.
func TestConnect_TLSSkipVerify_EmitsLocalWarning(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	m := world.NewManager(b)

	// Capture EvWorldLineRendered events — warnings use the rendered pipeline
	// so they appear in both the TUI and IPC frontends.
	var localLines []string
	var mu sync.Mutex
	sub := b.Subscribe(32, bus.EvWorldLineRendered)
	go func() {
		defer sub.Cancel()
		for ev := range sub.C {
			if e, ok := ev.(bus.WorldRenderedEvent); ok && e.WorldName == "local" {
				mu.Lock()
				localLines = append(localLines, e.Text)
				mu.Unlock()
			}
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &config.WorldConfig{
		Name:          "skipworld",
		URL:           mudURL(addr),
		TLSSkipVerify: true,
	}
	if err := m.Connect(ctx, "skipworld", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	mc := waitMudConn(t, accept)
	defer mc.close()

	// Give the warning a moment to be published.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, line := range localLines {
			upper := strings.ToUpper(line)
			if strings.Contains(upper, "TLS") && (strings.Contains(upper, "SKIP") || strings.Contains(upper, "VERIF") || strings.Contains(upper, "WARN")) {
				mu.Unlock()
				return // warning found — test passes
			}
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}

	mu.Lock()
	got := localLines
	mu.Unlock()
	t.Errorf("expected a TLS skip-verify warning in local echo, got local lines: %v", got)
}

// ---------------------------------------------------------------------------
// NameFromURL
// ---------------------------------------------------------------------------

func TestNameFromURL(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"mud://example.com:4000", "example.com"},
		{"ws://example.com/path", "example.com"},
		{"wss://example.com:443/path", "example.com"},
		{"example.com:4000", "example.com"},
		{"mud://example.com", "example.com"},
		{"example.com", "example.com"},
		{"", "world"},
		{"mud://127.0.0.1:9999", "127.0.0.1"},
		{"wt://[::1]:4000", "[::1]"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			got := world.NameFromURL(tt.url)
			if got != tt.want {
				t.Errorf("NameFromURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// M-4: Sensitive lines (Telnet IAC WILL ECHO / WONT ECHO)
// ---------------------------------------------------------------------------

// TestSensitiveLines verifies that lines received while the server has Telnet
// ECHO active (IAC WILL ECHO) are marked Sensitive=true, and that lines before
// and after the echo window are Sensitive=false.
//
// Protocol flow:
//  1. Server sends IAC WILL ECHO  → EchoEnabled = true (password prompt window)
//  2. Server sends "Password: "   → must arrive as Sensitive=true
//  3. Server sends IAC WONT ECHO  → EchoEnabled = false (normal output resumes)
//  4. Server sends "Welcome back" → must arrive as Sensitive=false
func TestSensitiveLines(t *testing.T) {
	addr, accept := mudListen(t)
	b := bus.New()
	c := subscribe(b)
	m := world.NewManager(b)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg := &config.WorldConfig{Name: "s", URL: mudURL(addr)}
	if err := m.Connect(ctx, "s", cfg); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer m.Disconnect("s") //nolint:errcheck

	mc := waitMudConn(t, accept)
	defer mc.close()
	c.waitHook(t, "CONNECT", 3*time.Second)

	// Telnet constants.
	const (
		IAC  = byte(255)
		WILL = byte(251)
		WONT = byte(252)
		ECHO = byte(1)
	)

	// 1. Activate server-echo (password window).
	mc.conn.Write([]byte{IAC, WILL, ECHO}) //nolint:errcheck

	// 2. Send the "password" prompt line inside the echo window.
	mc.conn.Write([]byte("Password: \r\n")) //nolint:errcheck

	// Wait for the line to arrive.
	passwordLine := c.waitLine(t, "Password:", 3*time.Second)

	// 3. Close the echo window.
	mc.conn.Write([]byte{IAC, WONT, ECHO}) //nolint:errcheck

	// 4. Send a normal line after the echo window closes.
	mc.conn.Write([]byte("Welcome back.\r\n")) //nolint:errcheck
	normalLine := c.waitLine(t, "Welcome back", 3*time.Second)

	// Assertions.
	if !passwordLine.Sensitive {
		t.Errorf("expected Password line to be Sensitive=true, got Sensitive=false (Text=%q)", passwordLine.Text)
	}
	if normalLine.Sensitive {
		t.Errorf("expected Welcome line to be Sensitive=false, got Sensitive=true (Text=%q)", normalLine.Text)
	}
}
