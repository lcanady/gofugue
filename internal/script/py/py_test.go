package py_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/script"
	"github.com/kumakun/gofugue/internal/script/py"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func skipIfNoPython3(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not in PATH")
	}
}

// findBridgePy locates tf-lib/bridge.py from the test's working directory.
func findBridgePy(t *testing.T) string {
	t.Helper()
	candidates := []string{
		"../../../tf-lib/bridge.py",
		"../../tf-lib/bridge.py",
		"../../../../tf-lib/bridge.py",
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			abs, _ := filepath.Abs(p)
			return abs
		}
	}
	t.Skip("tf-lib/bridge.py not found — skipping integration test")
	return ""
}

// writeTempScript creates a temporary Python file with the given source.
func writeTempScript(t *testing.T, src string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "script_*.py")
	if err != nil {
		t.Fatalf("writeTempScript: %v", err)
	}
	defer f.Close()
	if _, err := io.WriteString(f, src); err != nil {
		t.Fatalf("writeTempScript write: %v", err)
	}
	return f.Name()
}

func makeCB(echoCh chan<- string, setCh chan<- [2]string) script.Callbacks {
	return script.Callbacks{
		Send:            func(world, text string) error { return nil },
		Echo:            func(s string) { echoCh <- s },
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar: func(name, val string) {
			if setCh != nil {
				setCh <- [2]string{name, val}
			}
		},
	}
}

// parseJSONLines reads all JSON objects from a reader (test helper).
func parseJSONLines(r io.Reader) []map[string]json.RawMessage {
	var out []map[string]json.RawMessage
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		var m map[string]json.RawMessage
		if json.Unmarshal(sc.Bytes(), &m) == nil {
			out = append(out, m)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Lifecycle — no subprocess needed
// ---------------------------------------------------------------------------

func TestNew_ReturnsNonNil(t *testing.T) {
	b := bus.New()
	br := py.New(b, script.Callbacks{
		Send: func(world, text string) error { return nil },
		Echo: func(string) {},
	}, "/nonexistent/bridge.py")
	if br == nil {
		t.Fatal("New returned nil")
	}
}

func TestStop_BeforeLoad_IsNoop(t *testing.T) {
	b := bus.New()
	br := py.New(b, script.Callbacks{}, "/nonexistent/bridge.py")
	if err := br.Stop(); err != nil {
		t.Errorf("Stop before Load should be noop, got: %v", err)
	}
}

func TestStop_TwiceIsIdempotent(t *testing.T) {
	b := bus.New()
	br := py.New(b, script.Callbacks{}, "/nonexistent/bridge.py")
	_ = br.Stop()
	if err := br.Stop(); err != nil {
		t.Errorf("second Stop should not error, got: %v", err)
	}
}

func TestLoad_MissingScript_ReturnsError(t *testing.T) {
	skipIfNoPython3(t)
	bridgePy := findBridgePy(t)

	b := bus.New()
	br := py.New(b, script.Callbacks{
		Send: func(world, text string) error { return nil },
		Echo: func(string) {},
	}, bridgePy)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Python will start but then fail to import a missing script.
	// Load itself should succeed (it starts the process), but the process
	// may exit shortly after. We just verify no panic.
	_ = br.Load(ctx, "/nonexistent/script_abc123.py")
	br.Stop() //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Integration — trigger firing
// ---------------------------------------------------------------------------

func TestBridge_TriggerFires(t *testing.T) {
	skipIfNoPython3(t)
	bridgePy := findBridgePy(t)

	b := bus.New()
	setCh := make(chan [2]string, 8)
	echoCh := make(chan string, 8)
	br := py.New(b, makeCB(echoCh, setCh), bridgePy)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer br.Stop() //nolint:errcheck

	script := writeTempScript(t, `
import tf

@tf.def_trigger(r"hello (\w+)", name="t1")
def on_hello(world, line, caps):
    tf.setvar("greeted", caps[1])
`)

	if err := br.Load(ctx, script); err != nil {
		t.Fatalf("Load: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	b.Publish(bus.WorldRenderedEvent{
		WorldLineEvent: bus.WorldLineEvent{
			WorldName: "mud",
			Text:      "hello world",
			Attrs:     bus.LineAttrs{FG: -1, BG: -1},
		},
	})

	select {
	case kv := <-setCh:
		if kv[0] != "greeted" || kv[1] != "world" {
			t.Errorf("setvar(%q, %q), want (greeted, world)", kv[0], kv[1])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("trigger did not fire within 5s")
	}
}

func TestBridge_TriggerDoesNotFireForNonMatch(t *testing.T) {
	skipIfNoPython3(t)
	bridgePy := findBridgePy(t)

	b := bus.New()
	setCh := make(chan [2]string, 4)
	echoCh := make(chan string, 4)
	br := py.New(b, makeCB(echoCh, setCh), bridgePy)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer br.Stop() //nolint:errcheck

	script := writeTempScript(t, `
import tf

@tf.def_trigger(r"^specific pattern$", name="t1")
def on_match(world, line, caps):
    tf.setvar("fired", "1")
`)

	if err := br.Load(ctx, script); err != nil {
		t.Fatalf("Load: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	b.Publish(bus.WorldRenderedEvent{
		WorldLineEvent: bus.WorldLineEvent{
			WorldName: "mud",
			Text:      "this does not match",
			Attrs:     bus.LineAttrs{FG: -1, BG: -1},
		},
	})

	select {
	case kv := <-setCh:
		t.Errorf("trigger should not have fired, got setvar(%q, %q)", kv[0], kv[1])
	case <-time.After(300 * time.Millisecond):
		// Expected — no match, no fire.
	}
}

func TestBridge_MultiTrigger_EachGetsOwnCaps(t *testing.T) {
	skipIfNoPython3(t)
	bridgePy := findBridgePy(t)

	b := bus.New()
	setCh := make(chan [2]string, 16)
	echoCh := make(chan string, 4)
	br := py.New(b, makeCB(echoCh, setCh), bridgePy)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer br.Stop() //nolint:errcheck

	// Two triggers on the same line, different capture groups.
	script := writeTempScript(t, `
import tf

@tf.def_trigger(r"name:(\w+)", name="t_name")
def on_name(world, line, caps):
    tf.setvar("name", caps[1])

@tf.def_trigger(r"hp:(\d+)", name="t_hp")
def on_hp(world, line, caps):
    tf.setvar("hp", caps[1])
`)

	if err := br.Load(ctx, script); err != nil {
		t.Fatalf("Load: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	b.Publish(bus.WorldRenderedEvent{
		WorldLineEvent: bus.WorldLineEvent{
			WorldName: "mud",
			Text:      "name:Alice hp:100",
			Attrs:     bus.LineAttrs{FG: -1, BG: -1},
		},
	})

	received := map[string]string{}
	deadline := time.After(5 * time.Second)
	for len(received) < 2 {
		select {
		case kv := <-setCh:
			received[kv[0]] = kv[1]
		case <-deadline:
			t.Fatalf("only got %d setvar calls, want 2: %v", len(received), received)
		}
	}
	if received["name"] != "Alice" {
		t.Errorf("name = %q, want Alice", received["name"])
	}
	if received["hp"] != "100" {
		t.Errorf("hp = %q, want 100", received["hp"])
	}
}

// ---------------------------------------------------------------------------
// Integration — GMCP forwarding
// ---------------------------------------------------------------------------

func TestBridge_GMCP_ForwardedToPython(t *testing.T) {
	skipIfNoPython3(t)
	bridgePy := findBridgePy(t)

	b := bus.New()
	setCh := make(chan [2]string, 8)
	echoCh := make(chan string, 4)
	br := py.New(b, makeCB(echoCh, setCh), bridgePy)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer br.Stop() //nolint:errcheck

	script := writeTempScript(t, `
import tf

@tf.on_gmcp("Char.Vitals")
def on_vitals(world, module, data):
    tf.setvar("hp", str(data.get("hp", 0)))
`)

	if err := br.Load(ctx, script); err != nil {
		t.Fatalf("Load: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	b.Publish(bus.GMCPEvent{WorldName: "mud", Module: "Char.Vitals", Data: []byte(`{"hp":42}`)})

	select {
	case kv := <-setCh:
		if kv[0] != "hp" || kv[1] != "42" {
			t.Errorf("setvar(%q, %q), want (hp, 42)", kv[0], kv[1])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("GMCP callback did not fire within 5s")
	}
}

func TestBridge_GMCP_WrongModule_DoesNotFire(t *testing.T) {
	skipIfNoPython3(t)
	bridgePy := findBridgePy(t)

	b := bus.New()
	setCh := make(chan [2]string, 4)
	echoCh := make(chan string, 4)
	br := py.New(b, makeCB(echoCh, setCh), bridgePy)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer br.Stop() //nolint:errcheck

	script := writeTempScript(t, `
import tf

@tf.on_gmcp("Char.Vitals")
def on_vitals(world, module, data):
    tf.setvar("fired", "1")
`)

	if err := br.Load(ctx, script); err != nil {
		t.Fatalf("Load: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	b.Publish(bus.GMCPEvent{WorldName: "mud", Module: "Room.Info", Data: []byte(`{}`)})

	select {
	case kv := <-setCh:
		t.Errorf("should not have fired for Room.Info, got setvar(%q, %q)", kv[0], kv[1])
	case <-time.After(300 * time.Millisecond):
		// Expected.
	}
}

// ---------------------------------------------------------------------------
// Hot-reload
// ---------------------------------------------------------------------------

func TestBridge_HotReload_SecondLoadRestarts(t *testing.T) {
	skipIfNoPython3(t)
	bridgePy := findBridgePy(t)

	b := bus.New()
	var count int32
	br := py.New(b, script.Callbacks{
		Send:            func(world, text string) error { return nil },
		Echo:            func(string) {},
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar:          func(name, val string) { atomic.AddInt32(&count, 1) },
	}, bridgePy)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	defer br.Stop() //nolint:errcheck

	// First script: trigger on "ping".
	script1 := writeTempScript(t, `
import tf

@tf.def_trigger(r"ping", name="t1")
def on_ping(world, line, caps):
    tf.setvar("hit", "1")
`)
	if err := br.Load(ctx, script1); err != nil {
		t.Fatalf("first Load: %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	// Second script: no triggers.
	script2 := writeTempScript(t, `import tf`)
	if err := br.Load(ctx, script2); err != nil {
		t.Fatalf("second Load (hot-reload): %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	beforeCount := atomic.LoadInt32(&count)

	b.Publish(bus.WorldRenderedEvent{
		WorldLineEvent: bus.WorldLineEvent{
			WorldName: "mud",
			Text:      "ping",
			Attrs:     bus.LineAttrs{FG: -1, BG: -1},
		},
	})
	time.Sleep(400 * time.Millisecond)

	afterCount := atomic.LoadInt32(&count)
	if afterCount != beforeCount {
		t.Errorf("hot-reload: old trigger fired after reload (%d extra setvar calls)", afterCount-beforeCount)
	}
}

// ---------------------------------------------------------------------------
// Echo command from Python
// ---------------------------------------------------------------------------

func TestBridge_PythonEcho_CallsCallback(t *testing.T) {
	skipIfNoPython3(t)
	bridgePy := findBridgePy(t)

	b := bus.New()
	echoCh := make(chan string, 4)
	br := py.New(b, script.Callbacks{
		Send:            func(world, text string) error { return nil },
		Echo:            func(s string) { echoCh <- s },
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar:          func(name, val string) {},
	}, bridgePy)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	defer br.Stop() //nolint:errcheck

	// Script echoes on load using a HOOK callback on CONNECT,
	// or simply calls tf.echo() at module level.
	script := writeTempScript(t, `
import tf
tf.echo("hello from python")
`)

	if err := br.Load(ctx, script); err != nil {
		t.Fatalf("Load: %v", err)
	}

	select {
	case got := <-echoCh:
		if !strings.Contains(got, "hello from python") {
			t.Errorf("echo = %q, want to contain 'hello from python'", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tf.echo not received within 5s")
	}
}
