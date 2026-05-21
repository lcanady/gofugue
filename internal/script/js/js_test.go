package js_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/script"
	scriptjs "github.com/kumakun/gofugue/internal/script/js"
)

func newBridge(t *testing.T, echo func(string)) *scriptjs.Bridge {
	t.Helper()
	b := bus.New()
	cb := script.Callbacks{
		Echo:            echo,
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "test" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar:          func(name, value string) {},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	t.Cleanup(br.Stop)
	return br
}

func TestEval_ArithmeticReturnsResult(t *testing.T) {
	br := newBridge(t, nil)
	result, err := br.Eval("1 + 2")
	if err != nil {
		t.Fatalf("Eval error: %v", err)
	}
	if result != "3" {
		t.Errorf("expected '3', got %q", result)
	}
}

func TestEval_EchoCallback(t *testing.T) {
	ch := make(chan string, 1)
	br := newBridge(t, func(s string) { ch <- s })

	_, err := br.Eval(`tf.echo("hello from js")`)
	if err != nil {
		t.Fatalf("Eval: %v", err)
	}
	select {
	case got := <-ch:
		if got != "hello from js" {
			t.Errorf("echo got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("echo callback not called")
	}
}

func TestEval_SetvarGetvar(t *testing.T) {
	vars := map[string]string{}
	b := bus.New()
	cb := script.Callbacks{
		Echo:            func(s string) {},
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "test" },
		Getvar: func(name string) (string, bool) {
			v, ok := vars[name]
			return v, ok
		},
		Setvar: func(name, value string) { vars[name] = value },
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	if _, err := br.Eval(`tf.setvar("hp", "100")`); err != nil {
		t.Fatalf("setvar: %v", err)
	}
	if vars["hp"] != "100" {
		t.Errorf("setvar: vars[hp] = %q, want 100", vars["hp"])
	}

	if _, err := br.Eval(`tf.setvar("hp2", tf.getvar("hp") + "!")`); err != nil {
		t.Fatalf("getvar chain: %v", err)
	}
	if vars["hp2"] != "100!" {
		t.Errorf("getvar chain: vars[hp2] = %q, want 100!", vars["hp2"])
	}
}

func TestDef_TriggerFires(t *testing.T) {
	echoCh := make(chan string, 4)
	b := bus.New()
	cb := script.Callbacks{
		Echo:            func(s string) { echoCh <- s },
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "test" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar:          func(name, value string) {},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	// Register a trigger.
	_, err := br.Eval(`tf.def("dragon", "dragon", function(world, line, caps) {
		tf.echo("triggered: " + line);
	});`)
	if err != nil {
		t.Fatalf("tf.def: %v", err)
	}

	// Give Start goroutines time to subscribe.
	time.Sleep(20 * time.Millisecond)

	// Publish a matching rendered line.
	b.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{
		WorldName: "test",
		Text:      "a dragon appears",
		Attrs:     bus.LineAttrs{FG: -1, BG: -1},
	}})

	select {
	case got := <-echoCh:
		if got != "triggered: a dragon appears" {
			t.Errorf("trigger echo: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("trigger callback not called within timeout")
	}
}

func TestHookCallback(t *testing.T) {
	echoCh := make(chan string, 4)
	b := bus.New()
	cb := script.Callbacks{
		Echo:            func(s string) { echoCh <- s },
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "test" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar:          func(name, value string) {},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	_, err := br.Eval(`tf.on("CONNECT", function(world) { tf.echo("connected: " + world); });`)
	if err != nil {
		t.Fatalf("tf.on: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	b.Publish(bus.HookEvent{WorldName: "myworld", Name: "CONNECT"})

	select {
	case got := <-echoCh:
		if got != "connected: myworld" {
			t.Errorf("hook echo: %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("hook callback not called")
	}
}

func TestEval_SendCallback(t *testing.T) {
	var gotWorld, gotText string
	b := bus.New()
	cb := script.Callbacks{
		Send: func(world, text string) error {
			gotWorld = world
			gotText = text
			return nil
		},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	if _, err := br.Eval(`tf.send("myworld", "hello world")`); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	if gotWorld != "myworld" || gotText != "hello world" {
		t.Errorf("Send got world=%q text=%q, want myworld and hello world", gotWorld, gotText)
	}
}

func TestEval_SendCallback_Error(t *testing.T) {
	b := bus.New()
	cb := script.Callbacks{
		Send: func(world, text string) error {
			return fmt.Errorf("send error")
		},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	_, err := br.Eval(`tf.send("myworld", "hello world")`)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "send error") {
		t.Errorf("expected error to contain 'send error', got %q", err.Error())
	}
}

func TestEval_LogCallback(t *testing.T) {
	ch := make(chan string, 1)
	b := bus.New()
	cb := script.Callbacks{
		Echo: func(s string) { ch <- s },
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	if _, err := br.Eval(`tf.log("log message")`); err != nil {
		t.Fatalf("Eval: %v", err)
	}
	select {
	case got := <-ch:
		if got != "log message" {
			t.Errorf("log got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("echo/log callback not called")
	}
}

func TestEval_WorldCallback(t *testing.T) {
	b := bus.New()
	cb := script.Callbacks{
		ForegroundWorld: func() string { return "testworld" },
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	result, err := br.Eval(`tf.world()`)
	if err != nil {
		t.Fatalf("Eval error: %v", err)
	}
	if result != "testworld" {
		t.Errorf("expected 'testworld', got %q", result)
	}
}

func TestEval_NilCallbacks(t *testing.T) {
	b := bus.New()
	cb := script.Callbacks{} // All nil
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	// send
	if _, err := br.Eval(`tf.send("w", "t")`); err != nil {
		t.Errorf("send with nil callback returned error: %v", err)
	}
	// echo/log
	if _, err := br.Eval(`tf.echo("t"); tf.log("t")`); err != nil {
		t.Errorf("echo/log with nil callback returned error: %v", err)
	}
	// world
	res, err := br.Eval(`tf.world()`)
	if err != nil {
		t.Errorf("world with nil callback returned error: %v", err)
	}
	if res != "" {
		t.Errorf("expected empty string for nil world callback, got %q", res)
	}
	// getvar
	res, err = br.Eval(`tf.getvar("x")`)
	if err != nil {
		t.Errorf("getvar with nil callback returned error: %v", err)
	}
	if res != "undefined" {
		t.Errorf("expected 'undefined', got %q", res)
	}
	// setvar
	if _, err := br.Eval(`tf.setvar("x", "y")`); err != nil {
		t.Errorf("setvar with nil callback returned error: %v", err)
	}
}
