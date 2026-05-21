package js_test

import (
	"context"
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
		Echo: echo,
		Send: func(world, text string) error { return nil },
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
