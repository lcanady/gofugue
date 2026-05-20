// mud_e2e_test.go exercises the full stack:
//
//	real TCP mudServer → transport.Dial → proto.Session (Telnet FSM)
//	→ world.Manager readLoop → bus.WorldLineEvent
//	→ macro trigger → body sent back to server
//	→ IPC client receives events
//
// These are the only tests that exercise every layer from raw bytes to
// published events. They deliberately cover paths that unit tests cannot
// reach: MCCP2 mid-stream activation, GMCP subneg round-trip, alias
// expansion before network send, multi-world event tagging.
package integration_test

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
	"github.com/kumakun/gofugue/internal/macro"
)

// ---------------------------------------------------------------------------
// fullStackHarness: pipelineHarness + UserInput pipeline + IPC server
// ---------------------------------------------------------------------------

// fullStackHarness mirrors main.go's run() without TUI, enabling tests that
// exercise alias expansion and IPC event delivery alongside the trigger pipeline.
type fullStackHarness struct {
	*pipelineHarness
	ipcAddr string
}

func newFullStackHarness(t *testing.T) *fullStackHarness {
	t.Helper()
	fh := &fullStackHarness{
		pipelineHarness: newPipelineHarness(t),
	}

	// Wire the UserInput pipeline (alias expansion → world.Send), which
	// pipelineHarness omits because the existing pipeline tests don't need it.
	userInputSub := fh.b.Subscribe(64, bus.EvUserInput)
	go func() {
		defer userInputSub.Cancel()
		for {
			select {
			case <-fh.ctx.Done():
				return
			case ev, ok := <-userInputSub.C:
				if !ok {
					return
				}
				uie := ev.(bus.UserInputEvent)
				fg := fh.worldMgr.Foreground()
				// Check alias first.
				if body, ok := fh.macroEng.MatchAlias(fg, uie.Text); ok {
					fh.execBody(fg, body)
					continue
				}
				fh.worldMgr.Send(fg, uie.Text) //nolint:errcheck
			}
		}
	}()

	// Start an IPC server so tests can exercise the JSON-RPC event delivery path.
	fh.ipcAddr = startTestIPCServer(t, fh.b)

	return fh
}

// connectWorld dials a real TCP mudServer and registers the world under name.
func (fh *fullStackHarness) connectWorld(t *testing.T, name, addr string) {
	t.Helper()
	cfg := &config.WorldConfig{Name: name, URL: "mud://" + addr}
	if err := fh.worldMgr.Connect(fh.ctx, name, cfg); err != nil {
		t.Fatalf("connectWorld %q (%s): %v", name, addr, err)
	}
}

// ---------------------------------------------------------------------------
// lineCollector: subscribes once, accumulates events so no subscription race.
//
// The key invariant: create the collector BEFORE connecting / publishing events
// that you intend to observe. Each call to waitContains scans the accumulated
// slice, so multiple waits on the same stream work correctly.
// ---------------------------------------------------------------------------

type lineCollector struct {
	mu    sync.Mutex
	lines []bus.WorldLineEvent
}

func newLineCollector(b *bus.Bus, ctx context.Context) *lineCollector {
	c := &lineCollector{}
	sub := b.Subscribe(256, bus.EvWorldLine)
	go func() {
		defer sub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub.C:
				if !ok {
					return
				}
				wle := ev.(bus.WorldLineEvent)
				c.mu.Lock()
				c.lines = append(c.lines, wle)
				c.mu.Unlock()
			}
		}
	}()
	return c
}

// waitContains polls until a line from worldName containing substr is found.
func (c *lineCollector) waitContains(t *testing.T, worldName, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, l := range c.lines {
			if (worldName == "" || l.WorldName == worldName) && strings.Contains(l.Text, substr) {
				c.mu.Unlock()
				return l.Text
			}
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("timed out waiting for WorldLineEvent from %q containing %q", worldName, substr)
	return ""
}

// ---------------------------------------------------------------------------
// hookCollector: same pattern for HookEvents.
// ---------------------------------------------------------------------------

type hookCollector struct {
	mu    sync.Mutex
	hooks []bus.HookEvent
}

func newHookCollector(b *bus.Bus, ctx context.Context) *hookCollector {
	c := &hookCollector{}
	sub := b.Subscribe(64, bus.EvHook)
	go func() {
		defer sub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub.C:
				if !ok {
					return
				}
				c.mu.Lock()
				c.hooks = append(c.hooks, ev.(bus.HookEvent))
				c.mu.Unlock()
			}
		}
	}()
	return c
}

func (c *hookCollector) waitHook(t *testing.T, worldName, hookName string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, h := range c.hooks {
			if (worldName == "" || h.WorldName == worldName) && h.Name == hookName {
				c.mu.Unlock()
				return
			}
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("timed out waiting for %q hook on world %q", hookName, worldName)
}

// ---------------------------------------------------------------------------
// gmcpCollector: same pattern for GMCPEvents.
// ---------------------------------------------------------------------------

type gmcpCollector struct {
	mu     sync.Mutex
	events []bus.GMCPEvent
}

func newGMCPCollector(b *bus.Bus, ctx context.Context) *gmcpCollector {
	c := &gmcpCollector{}
	sub := b.Subscribe(64, bus.EvGMCP)
	go func() {
		defer sub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-sub.C:
				if !ok {
					return
				}
				c.mu.Lock()
				c.events = append(c.events, ev.(bus.GMCPEvent))
				c.mu.Unlock()
			}
		}
	}()
	return c
}

func (c *gmcpCollector) waitModule(t *testing.T, worldName, modulePrefix string, timeout time.Duration) bus.GMCPEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		for _, ge := range c.events {
			if (worldName == "" || ge.WorldName == worldName) &&
				strings.HasPrefix(ge.Module, modulePrefix) {
				c.mu.Unlock()
				return ge
			}
		}
		c.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("timed out waiting for GMCPEvent from %q with module %q", worldName, modulePrefix)
	return bus.GMCPEvent{}
}

// ---------------------------------------------------------------------------
// 1. Plain text lines flow from TCP server → bus.WorldLineEvent
// ---------------------------------------------------------------------------

func TestMUD_E2E_LinesFromServer_ArriveOnBus(t *testing.T) {
	fh := newFullStackHarness(t)
	// Subscribe BEFORE connecting so no events are missed.
	lc := newLineCollector(fh.b, fh.ctx)

	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Welcome to TestMUD!")
		sendLine(conn, "You stand in a sunlit plaza.")
		sendLine(conn, "Obvious exits: north, east.")
		conn.Read(make([]byte, 1)) //nolint:errcheck
	})

	fh.connectWorld(t, "mud1", srv.addr)

	lc.waitContains(t, "mud1", "Welcome to TestMUD!", 3*time.Second)
	lc.waitContains(t, "mud1", "sunlit plaza", 3*time.Second)
	lc.waitContains(t, "mud1", "Obvious exits", 3*time.Second)
}

// ---------------------------------------------------------------------------
// 2. GMCP subnegotiation parsed and published as GMCPEvent
// ---------------------------------------------------------------------------

func TestMUD_E2E_GMCP_ParsedToEvent(t *testing.T) {
	fh := newFullStackHarness(t)
	gc := newGMCPCollector(fh.b, fh.ctx)

	srv := newMudServer(t, func(conn net.Conn) {
		// IAC WILL GMCP (255 251 201)
		conn.Write([]byte{255, 251, 201}) //nolint:errcheck
		time.Sleep(20 * time.Millisecond)
		sendGMCP(conn, "Char.Vitals", map[string]any{"hp": 42, "maxhp": 100})
		conn.Read(make([]byte, 1)) //nolint:errcheck
	})

	fh.connectWorld(t, "gmcp1", srv.addr)

	ge := gc.waitModule(t, "gmcp1", "Char.Vitals", 3*time.Second)
	if ge.Module != "Char.Vitals" {
		t.Errorf("GMCPEvent.Module = %q, want %q", ge.Module, "Char.Vitals")
	}
	if !strings.Contains(string(ge.Data), "42") {
		t.Errorf("GMCPEvent.Data = %s, want hp=42", ge.Data)
	}
}

// ---------------------------------------------------------------------------
// 3. MCCP2: server negotiates compression, client decompresses correctly
// ---------------------------------------------------------------------------

func TestMUD_E2E_MCCP2_CompressedLines_Decompressed(t *testing.T) {
	fh := newFullStackHarness(t)
	lc := newLineCollector(fh.b, fh.ctx)

	srv := newMudServer(t, func(conn net.Conn) {
		zw := sendMCCP2Start(conn)
		fmt.Fprintf(zw, "Compressed welcome!\r\n")
		fmt.Fprintf(zw, "The air shimmers with compressed magic.\r\n")
		zw.Flush()
		conn.Read(make([]byte, 1)) //nolint:errcheck
		zw.Close()
	})

	fh.connectWorld(t, "mccp1", srv.addr)

	lc.waitContains(t, "mccp1", "Compressed welcome", 3*time.Second)
	lc.waitContains(t, "mccp1", "compressed magic", 3*time.Second)
}

// ---------------------------------------------------------------------------
// 4. Trigger fires on incoming line → body sent back to server
// ---------------------------------------------------------------------------

func TestMUD_E2E_Trigger_BodySentBackToServer(t *testing.T) {
	clientSent := make(chan string, 4)

	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "A large dragon appears!")
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			clientSent <- strings.TrimRight(sc.Text(), "\r")
		}
	})

	fh := newFullStackHarness(t)

	if err := fh.macroEng.Define(&macro.Macro{
		Name: "dragon_flee", Type: macro.TypeTrigger,
		Pattern: "dragon appears", Body: "flee",
		World: "trig1",
	}); err != nil {
		t.Fatalf("Define trigger: %v", err)
	}

	fh.connectWorld(t, "trig1", srv.addr)

	select {
	case got := <-clientSent:
		if got != "flee" {
			t.Errorf("server received %q, want %q", got, "flee")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for trigger-sent 'flee'")
	}
}

// ---------------------------------------------------------------------------
// 5. Alias expansion: UserInputEvent → alias expands → server receives result
// ---------------------------------------------------------------------------

func TestMUD_E2E_Alias_ExpandsBeforeSend(t *testing.T) {
	clientSent := make(chan string, 8)

	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Connected.")
		sc := bufio.NewScanner(conn)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r")
			clientSent <- line
		}
	})

	fh := newFullStackHarness(t)

	if err := fh.macroEng.Define(&macro.Macro{
		Name: "n", Type: macro.TypeAlias,
		Pattern: "n", Body: "go north",
	}); err != nil {
		t.Fatalf("Define alias: %v", err)
	}

	fh.connectWorld(t, "alias1", srv.addr)
	waitFor(t, 2*time.Second, func() bool {
		return fh.worldMgr.Foreground() == "alias1"
	})

	// User types "n" — alias engine must expand it to "go north" before send.
	fh.b.Publish(bus.UserInputEvent{WorldName: "alias1", Text: "n"})

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case got := <-clientSent:
			if got == "go north" {
				return
			}
			// Other lines (e.g. echo from server) are acceptable noise.
		case <-time.After(time.Until(deadline)):
			t.Fatal("timed out waiting for alias expansion 'go north'")
			return
		}
	}
	t.Fatal("alias expansion 'go north' never received by server")
}

// ---------------------------------------------------------------------------
// 6. IPC: world line events reach a subscribed IPC client
// ---------------------------------------------------------------------------

func TestMUD_E2E_IPC_ReceivesWorldLineEvents(t *testing.T) {
	srv := newMudServer(t, func(conn net.Conn) {
		time.Sleep(50 * time.Millisecond) // let IPC client subscribe first
		sendLine(conn, "IPC delivery test line")
		conn.Read(make([]byte, 1)) //nolint:errcheck
	})

	fh := newFullStackHarness(t)
	fh.connectWorld(t, "ipc1", srv.addr)

	// Connect IPC client and subscribe.
	c := dialIPC(t, fh.ipcAddr)
	c.subscribe(t, "world.line")

	// Receive the notification. The IPC server sends:
	// {"jsonrpc":"2.0","method":"world.line","params":{WorldName,Text,...}}
	msg := c.recv(t, 3*time.Second)

	// method IS the event type (no outer "event" wrapper).
	method, _ := msg["method"].(string)
	if method != "world.line" {
		t.Fatalf("notification method = %q, want %q", method, "world.line")
	}
	// params is the serialized WorldLineEvent (Go field names, no json tags).
	params, _ := msg["params"].(map[string]any)
	if params == nil {
		t.Fatal("notification missing params")
	}
	text, _ := params["Text"].(string)
	if !strings.Contains(text, "IPC delivery test line") {
		t.Errorf("IPC event Text = %q, want 'IPC delivery test line'", text)
	}
}

// ---------------------------------------------------------------------------
// 7. Reconnect: connect → disconnect → reconnect works cleanly
// ---------------------------------------------------------------------------

func TestMUD_E2E_Reconnect_Works(t *testing.T) {
	connected := make(chan struct{}, 4)
	srv := newMudServer(t, func(conn net.Conn) {
		connected <- struct{}{}
		sendLine(conn, "Session started.")
		conn.Read(make([]byte, 1)) //nolint:errcheck
	})

	fh := newFullStackHarness(t)
	lc := newLineCollector(fh.b, fh.ctx)
	// Subscribe to hooks BEFORE any connect/disconnect so we don't miss events.
	hc := newHookCollector(fh.b, fh.ctx)

	// First connect.
	fh.connectWorld(t, "rc1", srv.addr)
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("first connect: server never accepted")
	}
	lc.waitContains(t, "rc1", "Session started", 2*time.Second)

	// Disconnect.
	if err := fh.worldMgr.Disconnect("rc1"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	hc.waitHook(t, "rc1", "DISCONNECT", 2*time.Second)

	// Reconnect.
	cfg2 := &config.WorldConfig{Name: "rc1", URL: "mud://" + srv.addr}
	if err := fh.worldMgr.Connect(fh.ctx, "rc1", cfg2); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	select {
	case <-connected:
	case <-time.After(2 * time.Second):
		t.Fatal("reconnect: server never accepted second connection")
	}
	lc.waitContains(t, "rc1", "Session started", 2*time.Second)
}

// ---------------------------------------------------------------------------
// 8. Telnet IAC bytes interleaved with text don't corrupt world lines
// ---------------------------------------------------------------------------

func TestMUD_E2E_TelnetIAC_StrippedFromLines(t *testing.T) {
	fh := newFullStackHarness(t)
	lc := newLineCollector(fh.b, fh.ctx)

	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Before telnet bytes")
		conn.Write([]byte{255, 253, 1}) // IAC DO ECHO — consumed by FSM
		sendLine(conn, "After telnet bytes")
		conn.Read(make([]byte, 1)) //nolint:errcheck
	})

	fh.connectWorld(t, "iac1", srv.addr)

	line1 := lc.waitContains(t, "iac1", "Before telnet bytes", 3*time.Second)
	if strings.ContainsAny(line1, "\xff\xfd") {
		t.Errorf("IAC bytes leaked into first line: %q", line1)
	}
	line2 := lc.waitContains(t, "iac1", "After telnet bytes", 3*time.Second)
	if strings.ContainsAny(line2, "\xff\xfd") {
		t.Errorf("IAC bytes leaked into second line: %q", line2)
	}
}

// ---------------------------------------------------------------------------
// 9. Multi-world: events from each world are tagged with the correct WorldName
// ---------------------------------------------------------------------------

func TestMUD_E2E_MultiWorld_EventsTaggedCorrectly(t *testing.T) {
	fh := newFullStackHarness(t)
	lc := newLineCollector(fh.b, fh.ctx)

	srvA := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Hello from alpha")
		conn.Read(make([]byte, 1)) //nolint:errcheck
	})
	srvB := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Hello from beta")
		conn.Read(make([]byte, 1)) //nolint:errcheck
	})

	fh.connectWorld(t, "alpha", srvA.addr)
	fh.connectWorld(t, "beta", srvB.addr)

	// Wait for both greetings.
	lc.waitContains(t, "alpha", "Hello from alpha", 3*time.Second)
	lc.waitContains(t, "beta", "Hello from beta", 3*time.Second)

	// Verify no cross-contamination: alpha lines never tagged as beta, vice versa.
	lc.mu.Lock()
	defer lc.mu.Unlock()
	for _, l := range lc.lines {
		if l.WorldName == "alpha" && strings.Contains(l.Text, "beta") {
			t.Errorf("alpha line contains 'beta': %q", l.Text)
		}
		if l.WorldName == "beta" && strings.Contains(l.Text, "alpha") {
			t.Errorf("beta line contains 'alpha': %q", l.Text)
		}
	}
}

// ---------------------------------------------------------------------------
// 10. Disconnect tears down the connection so the server sees EOF
// ---------------------------------------------------------------------------

func TestMUD_E2E_Disconnect_ServerSeesEOF(t *testing.T) {
	serverClosed := make(chan struct{})

	srv := newMudServer(t, func(conn net.Conn) {
		sendLine(conn, "Connected.")
		buf := make([]byte, 1)
		conn.Read(buf) //nolint:errcheck — blocks until client disconnects
		close(serverClosed)
	})

	fh := newFullStackHarness(t)
	lc := newLineCollector(fh.b, fh.ctx)
	hc := newHookCollector(fh.b, fh.ctx)

	fh.connectWorld(t, "eof1", srv.addr)
	lc.waitContains(t, "eof1", "Connected.", 2*time.Second)

	if err := fh.worldMgr.Disconnect("eof1"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	hc.waitHook(t, "eof1", "DISCONNECT", 2*time.Second)

	select {
	case <-serverClosed:
	case <-time.After(2 * time.Second):
		t.Error("server never saw EOF after client disconnect")
	}
}
