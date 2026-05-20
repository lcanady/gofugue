// Pipeline end-to-end tests: verify that incoming world lines fire macro
// triggers, aliases expand, hooks fire on CONNECT/DISCONNECT, and the
// /connect /dc /sw commands drive world.Manager through the bus.
package integration_test

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/cmd"
	"github.com/kumakun/gofugue/internal/config"
	"github.com/kumakun/gofugue/internal/history"
	"github.com/kumakun/gofugue/internal/macro"
	"github.com/kumakun/gofugue/internal/world"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// pipelineHarness wires bus + world.Manager + macro.Engine + cmd.Dispatcher
// exactly as main.go does, but without IPC/TUI, so tests are fast and focused.
type pipelineHarness struct {
	b          *bus.Bus
	macroEng   *macro.Engine
	worldMgr   *world.Manager
	dispatcher *cmd.Dispatcher
	hist       *history.Buffer
	cmdCtx     *cmd.Context
	cancel     context.CancelFunc
	ctx        context.Context

	// Collected output lines published to "local" world.
	mu      sync.Mutex
	outputs []string
}

func newPipelineHarness(t *testing.T) *pipelineHarness {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)

	h := &pipelineHarness{
		b:          bus.New(),
		macroEng:   macro.New(),
		hist:       history.New(1000),
		dispatcher: cmd.New(),
		cancel:     cancel,
		ctx:        ctx,
	}
	h.worldMgr = world.NewManager(h.b)

	// cmdCtx mirrors what main.go builds.
	h.cmdCtx = &cmd.Context{
		Ctx:   ctx,
		World: "",
		Output: func(text string) {
			h.b.Publish(bus.WorldLineEvent{
				WorldName: "local",
				Text:      text,
				Attrs:     bus.LineAttrs{FG: -1, BG: -1},
			})
		},
		Send: func(wname, text string) error {
			return h.worldMgr.Send(wname, text)
		},
		Connect: func(name, url string) error {
			wcfg := &config.WorldConfig{Name: name, URL: url}
			return h.worldMgr.Connect(ctx, name, wcfg)
		},
		Disconnect: func(name string) error { return h.worldMgr.Disconnect(name) },
		Switch:     func(name string) error { return h.worldMgr.Switch(name) },
		ForegroundWorld: h.worldMgr.Foreground,
		DefMacro:        h.macroEng.Define,
		UndefMacro:      h.macroEng.Undefine,
		ListMacros:      h.macroEng.List,
		Setvar:          func(_, _ string) {},
		Getvar:          func(_ string) (string, bool) { return "", false },
		StartLog:        nil,
		StopLog:         nil,
	}

	// Collect "local" output lines.
	localSub := h.b.Subscribe(512, bus.EvWorldLine)
	go func() {
		defer localSub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-localSub.C:
				if !ok {
					return
				}
				wle := ev.(bus.WorldLineEvent)
				if wle.WorldName == "local" {
					h.mu.Lock()
					h.outputs = append(h.outputs, wle.Text)
					h.mu.Unlock()
				}
			}
		}
	}()

	// Trigger pipeline.
	lineSub := h.b.Subscribe(512, bus.EvWorldLine)
	go func() {
		defer lineSub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-lineSub.C:
				if !ok {
					return
				}
				wle := ev.(bus.WorldLineEvent)
				h.hist.Append(history.Line{
					Text:  wle.Text,
					Attrs: wle.Attrs,
				})
				if body, _ := h.macroEng.MatchTrigger(wle.WorldName, wle.Text); body != "" {
					h.execBody(wle.WorldName, body)
				}
			}
		}
	}()

	// Hook pipeline.
	hookSub := h.b.Subscribe(64, bus.EvHook)
	go func() {
		defer hookSub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-hookSub.C:
				if !ok {
					return
				}
				he := ev.(bus.HookEvent)
				for _, body := range h.macroEng.FireHook(he.WorldName, he.Name) {
					h.execBody(he.WorldName, body)
				}
			}
		}
	}()

	// UserCmd pipeline.
	cmdSub := h.b.Subscribe(64, bus.EvUserCmd)
	go func() {
		defer cmdSub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-cmdSub.C:
				if !ok {
					return
				}
				uce := ev.(bus.UserCmdEvent)
				h.cmdCtx.World = h.worldMgr.Foreground()
				if err := h.dispatcher.Dispatch(h.cmdCtx, uce.Line); err != nil {
					h.cmdCtx.Output("Error: " + err.Error())
				}
			}
		}
	}()

	t.Cleanup(cancel)
	return h
}

func (h *pipelineHarness) execBody(wname, body string) {
	if len(body) == 0 {
		return
	}
	h.cmdCtx.World = wname
	if body[0] == '/' {
		if err := h.dispatcher.Dispatch(h.cmdCtx, body); err != nil {
			h.cmdCtx.Output("Error: " + err.Error())
		}
		return
	}
	h.worldMgr.Send(wname, body) //nolint:errcheck
}

// waitOutputContaining polls until an output line contains substr, or times out.
func (h *pipelineHarness) waitOutputContaining(t *testing.T, substr string, timeout time.Duration) string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		for _, l := range h.outputs {
			if strings.Contains(l, substr) {
				h.mu.Unlock()
				return l
			}
		}
		h.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Errorf("timed out waiting for output containing %q", substr)
	return ""
}

// ---------------------------------------------------------------------------
// Tests: trigger pipeline
// ---------------------------------------------------------------------------

// TestPipeline_TriggerFires_OnIncomingLine verifies that when the world
// publishes a line matching a trigger pattern, the trigger body is executed
// (as /echo, which writes to local output).
func TestPipeline_TriggerFires_OnIncomingLine(t *testing.T) {
	h := newPipelineHarness(t)

	if err := h.macroEng.Define(&macro.Macro{
		Name:    "dragon_trigger",
		Type:    macro.TypeTrigger,
		Pattern: `[Dd]ragon`,
		Body:    "/echo DRAGON SPOTTED",
	}); err != nil {
		t.Fatalf("define trigger: %v", err)
	}

	// Simulate a world line arriving.
	h.b.Publish(bus.WorldLineEvent{
		WorldName: "myworld",
		Text:      "A red Dragon appears before you!",
		Attrs:     bus.LineAttrs{FG: -1, BG: -1},
	})

	h.waitOutputContaining(t, "DRAGON SPOTTED", 2*time.Second)
}

// TestPipeline_TriggerNotFires_WhenNoMatch verifies non-matching lines
// do not trigger any action.
func TestPipeline_TriggerNotFires_WhenNoMatch(t *testing.T) {
	h := newPipelineHarness(t)

	if err := h.macroEng.Define(&macro.Macro{
		Name:    "dragon_trigger",
		Type:    macro.TypeTrigger,
		Pattern: `[Dd]ragon`,
		Body:    "/echo DRAGON SPOTTED",
	}); err != nil {
		t.Fatalf("define trigger: %v", err)
	}

	h.b.Publish(bus.WorldLineEvent{
		WorldName: "myworld",
		Text:      "A peaceful meadow stretches before you.",
		Attrs:     bus.LineAttrs{FG: -1, BG: -1},
	})

	time.Sleep(100 * time.Millisecond)
	h.mu.Lock()
	for _, l := range h.outputs {
		if strings.Contains(l, "DRAGON") {
			h.mu.Unlock()
			t.Errorf("unexpected trigger output: %q", l)
			return
		}
	}
	h.mu.Unlock()
}

// TestPipeline_TriggerWorldScoped fires only on the correct world.
func TestPipeline_TriggerWorldScoped(t *testing.T) {
	h := newPipelineHarness(t)

	if err := h.macroEng.Define(&macro.Macro{
		Name:    "scoped_trigger",
		Type:    macro.TypeTrigger,
		Pattern: `hello`,
		World:   "world_a",
		Body:    "/echo HELLO_FROM_A",
	}); err != nil {
		t.Fatalf("define trigger: %v", err)
	}

	// Same pattern, different world — should NOT fire.
	h.b.Publish(bus.WorldLineEvent{
		WorldName: "world_b",
		Text:      "hello there",
		Attrs:     bus.LineAttrs{FG: -1, BG: -1},
	})
	time.Sleep(80 * time.Millisecond)

	h.mu.Lock()
	for _, l := range h.outputs {
		if strings.Contains(l, "HELLO_FROM_A") {
			h.mu.Unlock()
			t.Errorf("trigger fired for wrong world: %q", l)
			return
		}
	}
	h.mu.Unlock()

	// Now publish on the correct world.
	h.b.Publish(bus.WorldLineEvent{
		WorldName: "world_a",
		Text:      "hello there",
		Attrs:     bus.LineAttrs{FG: -1, BG: -1},
	})
	h.waitOutputContaining(t, "HELLO_FROM_A", 2*time.Second)
}

// TestPipeline_TriggerPriority fires highest-priority trigger first.
func TestPipeline_TriggerPriority(t *testing.T) {
	h := newPipelineHarness(t)

	if err := h.macroEng.Define(&macro.Macro{
		Name: "low_prio", Type: macro.TypeTrigger,
		Pattern: `test`, Priority: 10, Body: "/echo PRIO_LOW",
	}); err != nil {
		t.Fatalf("define low: %v", err)
	}
	if err := h.macroEng.Define(&macro.Macro{
		Name: "high_prio", Type: macro.TypeTrigger,
		Pattern: `test`, Priority: 100, Body: "/echo PRIO_HIGH",
	}); err != nil {
		t.Fatalf("define high: %v", err)
	}

	// MatchTrigger returns the FIRST (highest priority) match only.
	body, _ := h.macroEng.MatchTrigger("myworld", "this is a test")
	if !strings.Contains(body, "PRIO_HIGH") {
		t.Errorf("expected high-priority body, got %q", body)
	}
}

// TestPipeline_MultipleLines_AllMatched verifies each incoming line is checked.
func TestPipeline_MultipleLines_AllMatched(t *testing.T) {
	h := newPipelineHarness(t)

	if err := h.macroEng.Define(&macro.Macro{
		Name: "counter", Type: macro.TypeTrigger,
		Pattern: `ping`, Body: "/echo PONG",
	}); err != nil {
		t.Fatalf("define: %v", err)
	}

	for i := 0; i < 3; i++ {
		h.b.Publish(bus.WorldLineEvent{
			WorldName: "w", Text: fmt.Sprintf("ping %d", i),
			Attrs: bus.LineAttrs{FG: -1, BG: -1},
		})
	}

	// Wait for all three PONG outputs.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		count := 0
		for _, l := range h.outputs {
			if l == "PONG" {
				count++
			}
		}
		h.mu.Unlock()
		if count >= 3 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.Lock()
	count := 0
	for _, l := range h.outputs {
		if l == "PONG" {
			count++
		}
	}
	h.mu.Unlock()
	if count < 3 {
		t.Errorf("expected 3 PONGs, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Tests: hook pipeline
// ---------------------------------------------------------------------------

// TestPipeline_HookFires_OnConnect verifies CONNECT hook executes trigger body.
func TestPipeline_HookFires_OnConnect(t *testing.T) {
	h := newPipelineHarness(t)

	if err := h.macroEng.Define(&macro.Macro{
		Name:    "on_connect",
		Type:    macro.TypeHook,
		Pattern: "CONNECT",
		Body:    "/echo CONNECTED_HOOK",
	}); err != nil {
		t.Fatalf("define hook: %v", err)
	}

	h.b.Publish(bus.HookEvent{WorldName: "myworld", Name: "CONNECT"})
	h.waitOutputContaining(t, "CONNECTED_HOOK", 2*time.Second)
}

// TestPipeline_HookFires_OnDisconnect verifies DISCONNECT hook.
func TestPipeline_HookFires_OnDisconnect(t *testing.T) {
	h := newPipelineHarness(t)

	if err := h.macroEng.Define(&macro.Macro{
		Name:    "on_disconnect",
		Type:    macro.TypeHook,
		Pattern: "DISCONNECT",
		Body:    "/echo DISCONNECTED_HOOK",
	}); err != nil {
		t.Fatalf("define hook: %v", err)
	}

	h.b.Publish(bus.HookEvent{WorldName: "myworld", Name: "DISCONNECT"})
	h.waitOutputContaining(t, "DISCONNECTED_HOOK", 2*time.Second)
}

// ---------------------------------------------------------------------------
// Tests: cmd pipeline (via UserCmdEvent)
// ---------------------------------------------------------------------------

// TestPipeline_UserCmd_EchoViabus verifies /echo dispatched from bus.
func TestPipeline_UserCmd_EchoViaBus(t *testing.T) {
	h := newPipelineHarness(t)

	h.b.Publish(bus.UserCmdEvent{
		WorldName: "",
		Line:      "/echo hello from bus",
	})

	h.waitOutputContaining(t, "hello from bus", 2*time.Second)
}

// TestPipeline_UserCmd_Connect_And_Disconnect uses mock TCP server.
func TestPipeline_UserCmd_Connect_And_Disconnect(t *testing.T) {
	// Spin up a minimal TCP MUD server that accepts one connection and blocks.
	ready := make(chan struct{})
	srv := newMudServer(t, func(conn net.Conn) {
		close(ready)
		conn.Read(make([]byte, 1)) //nolint:errcheck // block until closed
	})

	h := newPipelineHarness(t)

	// /connect via bus command.
	url := fmt.Sprintf("mud://%s", srv.addr)
	h.b.Publish(bus.UserCmdEvent{Line: "/connect " + url + " testconn"})

	// Wait for CONNECT hook (world.Manager publishes it on successful connect).
	hookSub := h.b.Subscribe(8, bus.EvHook)
	defer hookSub.Cancel()

	select {
	case ev := <-hookSub.C:
		he := ev.(bus.HookEvent)
		if he.Name != "CONNECT" || he.WorldName != "testconn" {
			t.Errorf("unexpected hook: %+v", he)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for CONNECT hook")
	}

	// /dc.
	h.b.Publish(bus.UserCmdEvent{Line: "/dc testconn"})

	// Wait for DISCONNECT.
	select {
	case ev := <-hookSub.C:
		he := ev.(bus.HookEvent)
		if he.Name != "DISCONNECT" {
			t.Errorf("expected DISCONNECT hook, got %q", he.Name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for DISCONNECT hook")
	}
}

// TestPipeline_UserCmd_SW_SwitchesForeground verifies /sw changes foreground.
func TestPipeline_UserCmd_SW_SwitchesForeground(t *testing.T) {
	h := newPipelineHarness(t)

	// Pre-register two worlds (no actual connection needed for switch test).
	h.worldMgr.Add(config.WorldConfig{Name: "alpha", URL: "mud://127.0.0.1:9999"})
	h.worldMgr.Add(config.WorldConfig{Name: "beta", URL: "mud://127.0.0.1:9998"})

	// First connect sets foreground.
	_ = h.worldMgr.Switch("alpha")
	if fg := h.worldMgr.Foreground(); fg != "alpha" {
		t.Fatalf("foreground = %q, want alpha", fg)
	}

	h.b.Publish(bus.UserCmdEvent{Line: "/sw beta"})

	// Give the goroutine time to process.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if h.worldMgr.Foreground() == "beta" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("foreground still %q after /sw beta", h.worldMgr.Foreground())
}

// ---------------------------------------------------------------------------
// Tests: history integration
// ---------------------------------------------------------------------------

// TestPipeline_History_CapturesLines verifies every world line lands in history.
func TestPipeline_History_CapturesLines(t *testing.T) {
	h := newPipelineHarness(t)

	lines := []string{"first line", "second line", "third line"}
	for _, l := range lines {
		h.b.Publish(bus.WorldLineEvent{
			WorldName: "w", Text: l,
			Attrs: bus.LineAttrs{FG: -1, BG: -1},
		})
	}

	time.Sleep(100 * time.Millisecond)

	tail := h.hist.Tail(10)
	got := make(map[string]bool)
	for _, hl := range tail {
		got[hl.Text] = true
	}
	for _, l := range lines {
		if !got[l] {
			t.Errorf("history missing line %q", l)
		}
	}
}

// TestPipeline_History_Search returns matching lines.
func TestPipeline_History_Search(t *testing.T) {
	h := newPipelineHarness(t)

	h.b.Publish(bus.WorldLineEvent{WorldName: "w", Text: "dragons are scary", Attrs: bus.LineAttrs{FG: -1, BG: -1}})
	h.b.Publish(bus.WorldLineEvent{WorldName: "w", Text: "wolves howl", Attrs: bus.LineAttrs{FG: -1, BG: -1}})
	h.b.Publish(bus.WorldLineEvent{WorldName: "w", Text: "a dragon sleeps", Attrs: bus.LineAttrs{FG: -1, BG: -1}})

	time.Sleep(100 * time.Millisecond)

	results := h.hist.Search("dragon")
	if len(results) != 2 {
		t.Errorf("expected 2 dragon matches, got %d: %v", len(results), results)
	}
}

// ---------------------------------------------------------------------------
// Tests: macro List
// ---------------------------------------------------------------------------

// TestMacroEngine_List_ReturnsAllMacros verifies List() after multiple defines.
func TestMacroEngine_List_ReturnsAllMacros(t *testing.T) {
	eng := macro.New()
	for _, name := range []string{"a", "b", "c"} {
		eng.Define(&macro.Macro{ //nolint:errcheck
			Name: name, Type: macro.TypeTrigger,
			Pattern: "x", Body: "/echo " + name,
		})
	}
	list := eng.List()
	if len(list) != 3 {
		t.Errorf("expected 3 items, got %d: %v", len(list), list)
	}
	joined := strings.Join(list, "\n")
	for _, name := range []string{"a", "b", "c"} {
		if !strings.Contains(joined, name) {
			t.Errorf("list missing macro %q", name)
		}
	}
}

// TestMacroEngine_List_Empty_ReturnsEmpty verifies empty engine returns empty slice.
func TestMacroEngine_List_Empty_ReturnsEmpty(t *testing.T) {
	eng := macro.New()
	if list := eng.List(); len(list) != 0 {
		t.Errorf("expected empty list for empty engine, got %v", list)
	}
}

// ---------------------------------------------------------------------------
// Tests: cmd /def + /undef roundtrip via dispatcher
// ---------------------------------------------------------------------------

// TestPipeline_Def_Then_Trigger_Fires verifies the full def→trigger loop.
func TestPipeline_Def_Then_Trigger_Fires(t *testing.T) {
	h := newPipelineHarness(t)

	// Define a trigger via /def command.
	h.b.Publish(bus.UserCmdEvent{
		Line: `/def -t "treasure" found_treasure=/echo YOU_FOUND_TREASURE`,
	})
	time.Sleep(50 * time.Millisecond)

	// Simulate matching line from world.
	h.b.Publish(bus.WorldLineEvent{
		WorldName: "w",
		Text:      "You see a glimmer of treasure on the floor.",
		Attrs:     bus.LineAttrs{FG: -1, BG: -1},
	})

	h.waitOutputContaining(t, "YOU_FOUND_TREASURE", 2*time.Second)
}

// TestPipeline_Undef_Stops_Trigger verifies /undef removes a trigger.
func TestPipeline_Undef_Stops_Trigger(t *testing.T) {
	h := newPipelineHarness(t)

	h.macroEng.Define(&macro.Macro{ //nolint:errcheck
		Name: "to_remove", Type: macro.TypeTrigger,
		Pattern: "remove_me", Body: "/echo SHOULD_NOT_FIRE",
	})

	h.b.Publish(bus.UserCmdEvent{Line: "/undef to_remove"})
	time.Sleep(50 * time.Millisecond)

	h.b.Publish(bus.WorldLineEvent{
		WorldName: "w", Text: "remove_me now",
		Attrs: bus.LineAttrs{FG: -1, BG: -1},
	})
	time.Sleep(100 * time.Millisecond)

	h.mu.Lock()
	for _, l := range h.outputs {
		if strings.Contains(l, "SHOULD_NOT_FIRE") {
			h.mu.Unlock()
			t.Error("trigger fired after /undef")
			return
		}
	}
	h.mu.Unlock()
}
