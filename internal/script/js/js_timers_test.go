package js_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/script"
	scriptjs "github.com/kumakun/gofugue/internal/script/js"
)

// newBridgeWithBus returns a bridge and its underlying bus (needed for GMCP tests).
func newBridgeWithBus(t *testing.T, echo func(string)) (*scriptjs.Bridge, *bus.Bus) {
	t.Helper()
	b := bus.New()
	cb := script.Callbacks{
		Echo:            echo,
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar:          func(name, value string) {},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	t.Cleanup(br.Stop)
	return br, b
}

// ---------------------------------------------------------------------------
// tf.setTimeout
// ---------------------------------------------------------------------------

func TestSetTimeout_FiresOnce(t *testing.T) {
	ch := make(chan struct{}, 4)
	br, _ := newBridgeWithBus(t, nil)
	_, err := br.Eval(`tf.setTimeout(20, function(){ tf.setvar("fired","1"); });`)
	if err != nil {
		t.Fatalf("setTimeout eval: %v", err)
	}
	// Give callback time to fire via timer pool + task channel.
	time.Sleep(150 * time.Millisecond)
	_ = ch

	var count int32
	br2, _ := newBridgeWithBus(t, nil)
	vars := map[string]string{}
	_ = br2
	b2 := bus.New()
	cb2 := script.Callbacks{
		Echo:            func(string) {},
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { v, ok := vars[name]; return v, ok },
		Setvar: func(name, value string) {
			vars[name] = value
			atomic.AddInt32(&count, 1)
		},
	}
	br3 := scriptjs.New(b2, cb2)
	br3.Start(context.Background())
	defer br3.Stop()

	if _, err := br3.Eval(`tf.setTimeout(20, function(){ tf.setvar("t","fired"); });`); err != nil {
		t.Fatalf("setTimeout: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	if n := atomic.LoadInt32(&count); n != 1 {
		t.Errorf("setTimeout fired %d times, want exactly 1", n)
	}
}

func TestSetTimeout_ClearBeforeFiring(t *testing.T) {
	var fired int32
	b := bus.New()
	cb := script.Callbacks{
		Echo:            func(string) {},
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar: func(name, value string) {
			if name == "fired" {
				atomic.AddInt32(&fired, 1)
			}
		},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	// Schedule timer then immediately cancel it.
	result, err := br.Eval(`var id = tf.setTimeout(200, function(){ tf.setvar("fired","1"); }); tf.clearTimeout(id); id`)
	if err != nil {
		t.Fatalf("clearTimeout: %v", err)
	}
	_ = result

	time.Sleep(400 * time.Millisecond)
	if n := atomic.LoadInt32(&fired); n != 0 {
		t.Errorf("clearTimeout: callback fired %d times after cancel, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// tf.setInterval
// ---------------------------------------------------------------------------

func TestSetInterval_FiresRepeatedly(t *testing.T) {
	var count int32
	b := bus.New()
	cb := script.Callbacks{
		Echo:            func(string) {},
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar: func(name, value string) {
			if name == "tick" {
				atomic.AddInt32(&count, 1)
			}
		},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	if _, err := br.Eval(`tf.setInterval(20, function(){ tf.setvar("tick","1"); });`); err != nil {
		t.Fatalf("setInterval: %v", err)
	}
	time.Sleep(150 * time.Millisecond)

	n := atomic.LoadInt32(&count)
	if n < 3 {
		t.Errorf("setInterval: expected ≥3 fires in 150ms at 20ms interval, got %d", n)
	}
}

func TestSetInterval_ClearStopsFiring(t *testing.T) {
	var count int32
	b := bus.New()
	cb := script.Callbacks{
		Echo:            func(string) {},
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar: func(name, value string) {
			if name == "tick" {
				atomic.AddInt32(&count, 1)
			}
		},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	result, err := br.Eval(`var id = tf.setInterval(20, function(){ tf.setvar("tick","1"); }); id`)
	if err != nil {
		t.Fatalf("setInterval: %v", err)
	}
	_ = result

	time.Sleep(60 * time.Millisecond)
	if _, err := br.Eval(`tf.clearInterval(` + result + `)`); err != nil {
		t.Fatalf("clearInterval: %v", err)
	}

	before := atomic.LoadInt32(&count)
	time.Sleep(60 * time.Millisecond)
	after := atomic.LoadInt32(&count)

	if after != before {
		t.Errorf("clearInterval: timer fired %d more times after cancel", after-before)
	}
}

// ---------------------------------------------------------------------------
// tf.undef removes a trigger
// ---------------------------------------------------------------------------

func TestUndef_StopsTrigger(t *testing.T) {
	echoCh := make(chan string, 8)
	b := bus.New()
	cb := script.Callbacks{
		Echo:            func(s string) { echoCh <- s },
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "mud" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar:          func(name, value string) {},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	if _, err := br.Eval(`tf.def("t1","troll",function(w,l,c){ tf.echo("troll!"); });`); err != nil {
		t.Fatalf("tf.def: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// First fire — should trigger.
	b.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{WorldName: "mud", Text: "a troll attacks", Attrs: bus.LineAttrs{FG: -1, BG: -1}}})
	select {
	case <-echoCh:
	case <-time.After(time.Second):
		t.Fatal("trigger should have fired before undef")
	}

	// Undef the trigger.
	if _, err := br.Eval(`tf.undef("t1");`); err != nil {
		t.Fatalf("tf.undef: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// Second fire — should NOT trigger.
	b.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{WorldName: "mud", Text: "a troll attacks", Attrs: bus.LineAttrs{FG: -1, BG: -1}}})
	select {
	case got := <-echoCh:
		t.Errorf("trigger fired after undef: %q", got)
	case <-time.After(150 * time.Millisecond):
		// Expected: nothing.
	}
}

// ---------------------------------------------------------------------------
// safeRun: panicking task doesn't crash the event loop
// ---------------------------------------------------------------------------

func TestSafeRun_PanicDoesNotCrashLoop(t *testing.T) {
	echoCh := make(chan string, 4)
	br, _ := newBridgeWithBus(t, func(s string) { echoCh <- s })

	// A JS error (bad call) should not crash the loop.
	_, _ = br.Eval(`null.boom()`) // throws TypeError — goja recovers via safeRun

	// Loop must still be alive and functional.
	if _, err := br.Eval(`tf.echo("still alive")`); err != nil {
		t.Fatalf("event loop died after panic: %v", err)
	}
	select {
	case got := <-echoCh:
		if got != "still alive" {
			t.Errorf("got %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("event loop unresponsive after panic recovery")
	}
}

// ---------------------------------------------------------------------------
// GMCP callbacks
// ---------------------------------------------------------------------------

func TestGMCP_SpecificModule_Fires(t *testing.T) {
	echoCh := make(chan string, 4)
	br, b := newBridgeWithBus(t, func(s string) { echoCh <- s })

	if _, err := br.Eval(`tf.on("GMCP:Char.Vitals", function(world,mod,data){ tf.echo("vitals:"+data.hp); });`); err != nil {
		t.Fatalf("tf.on GMCP: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	b.Publish(bus.GMCPEvent{WorldName: "mud", Module: "Char.Vitals", Data: []byte(`{"hp":100}`)})

	select {
	case got := <-echoCh:
		if got != "vitals:100" {
			t.Errorf("GMCP callback: got %q, want vitals:100", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("GMCP specific callback not called")
	}
}

func TestGMCP_WildcardReceivesAllModules(t *testing.T) {
	echoCh := make(chan string, 8)
	br, b := newBridgeWithBus(t, func(s string) { echoCh <- s })

	if _, err := br.Eval(`tf.on("GMCP", function(world,mod,data){ tf.echo("mod:"+mod); });`); err != nil {
		t.Fatalf("tf.on GMCP wildcard: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	b.Publish(bus.GMCPEvent{WorldName: "mud", Module: "Char.Vitals", Data: []byte(`{}`)})
	b.Publish(bus.GMCPEvent{WorldName: "mud", Module: "Room.Info", Data: []byte(`{}`)})

	got := make([]string, 0, 2)
	deadline := time.After(2 * time.Second)
	for len(got) < 2 {
		select {
		case s := <-echoCh:
			got = append(got, s)
		case <-deadline:
			t.Fatalf("GMCP wildcard: only received %v, want 2 events", got)
		}
	}
	if got[0] != "mod:Char.Vitals" {
		t.Errorf("first event: %q", got[0])
	}
	if got[1] != "mod:Room.Info" {
		t.Errorf("second event: %q", got[1])
	}
}

func TestGMCP_SpecificModule_DoesNotFireForOther(t *testing.T) {
	echoCh := make(chan string, 4)
	br, b := newBridgeWithBus(t, func(s string) { echoCh <- s })

	// Only listen for Char.Vitals.
	if _, err := br.Eval(`tf.on("GMCP:Char.Vitals", function(w,m,d){ tf.echo("vitals"); });`); err != nil {
		t.Fatalf("tf.on: %v", err)
	}
	time.Sleep(20 * time.Millisecond)

	// Publish a different module.
	b.Publish(bus.GMCPEvent{WorldName: "mud", Module: "Room.Info", Data: []byte(`{}`)})

	select {
	case got := <-echoCh:
		t.Errorf("should not have fired for Room.Info, got %q", got)
	case <-time.After(150 * time.Millisecond):
		// Expected.
	}
}

// ---------------------------------------------------------------------------
// tf.world()
// ---------------------------------------------------------------------------

func TestWorld_ReturnsForeground(t *testing.T) {
	b := bus.New()
	cb := script.Callbacks{
		Echo:            func(string) {},
		Send:            func(world, text string) error { return nil },
		ForegroundWorld: func() string { return "myworld" },
		Getvar:          func(name string) (string, bool) { return "", false },
		Setvar:          func(name, value string) {},
	}
	br := scriptjs.New(b, cb)
	br.Start(context.Background())
	defer br.Stop()

	result, err := br.Eval(`tf.world()`)
	if err != nil {
		t.Fatalf("tf.world(): %v", err)
	}
	if result != "myworld" {
		t.Errorf("tf.world() = %q, want myworld", result)
	}
}
