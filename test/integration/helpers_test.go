package integration_test

// helpers_test.go wires real internal packages into the integration tests
// without exposing them through the public API. All helpers are test-only.

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/ipc"
	"github.com/kumakun/gofugue/internal/macro"
)

// ---------------------------------------------------------------------------
// Type aliases so tests don't import bus directly for type assertions.
// ---------------------------------------------------------------------------

type worldLineEvent = bus.WorldLineEvent
type hookEvent = bus.HookEvent

const (
	busEvWorldLine = bus.EvWorldLine
	busEvHook      = bus.EvHook
)

// ---------------------------------------------------------------------------
// Bus helpers
// ---------------------------------------------------------------------------

func newTestBus(t *testing.T) *bus.Bus {
	t.Helper()
	return bus.New()
}

func publishWorldLine(b *bus.Bus, world, text string) {
	b.Publish(bus.WorldLineEvent{WorldName: world, Text: text})
}

func publishGMCP(b *bus.Bus, world, module string, data []byte) {
	b.Publish(bus.GMCPEvent{WorldName: world, Module: module, Data: data})
}

func publishHook(b *bus.Bus, world, name string) {
	b.Publish(bus.HookEvent{WorldName: world, Name: name})
}

// subChan extracts the underlying channel from a *bus.Subscription for select.
func subChan(s interface{ Cancel() }) <-chan bus.Event {
	// We can't access .C directly on the interface; use type assertion.
	if sub, ok := s.(*bus.Subscription); ok {
		return sub.C
	}
	return nil
}

// waitBusEvent waits up to timeout for the next event on sub.C.
func waitBusEvent(t *testing.T, sub *bus.Subscription, timeout time.Duration) bus.Event {
	t.Helper()
	select {
	case ev := <-sub.C:
		return ev
	case <-time.After(timeout):
		t.Fatal("timed out waiting for bus event")
		return nil
	}
}

// ---------------------------------------------------------------------------
// IPC server helpers
// ---------------------------------------------------------------------------

func startTestIPCServer(t *testing.T, b *bus.Bus) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	cfg := ipc.Config{TCPAddr: addr}
	srv := ipc.New(cfg, b)
	go srv.Run(ctx) //nolint:errcheck

	// Wait for listener to be ready.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err == nil {
			conn.Close()
			return addr
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("IPC server never became ready at %s", addr)
	return ""
}

func startIPCServerWithCtx(ctx context.Context, addr string, b *bus.Bus) {
	cfg := ipc.Config{TCPAddr: addr}
	srv := ipc.New(cfg, b)
	// Wait briefly for ln to be freed.
	time.Sleep(20 * time.Millisecond)
	srv.Run(ctx) //nolint:errcheck
}

// ---------------------------------------------------------------------------
// Macro engine helpers
// ---------------------------------------------------------------------------

func newTestMacroEngine(t *testing.T) *macro.Engine {
	t.Helper()
	return macro.New()
}

func defineTrigger(e *macro.Engine, name, pattern, body string) {
	if err := e.Define(&macro.Macro{
		Name: name, Type: macro.TypeTrigger,
		Pattern: pattern, Body: body,
	}); err != nil {
		panic(fmt.Sprintf("defineTrigger %q: %v", name, err))
	}
}

func defineTriggerWorld(e *macro.Engine, name, pattern, body, world string) {
	if err := e.Define(&macro.Macro{
		Name: name, Type: macro.TypeTrigger,
		Pattern: pattern, Body: body, World: world,
	}); err != nil {
		panic(fmt.Sprintf("defineTriggerWorld %q: %v", name, err))
	}
}

func defineHook(e *macro.Engine, name, hookName, body string) {
	if err := e.Define(&macro.Macro{
		Name: name, Type: macro.TypeHook,
		Pattern: hookName, Body: body,
	}); err != nil {
		panic(fmt.Sprintf("defineHook %q: %v", name, err))
	}
}

func matchTrigger(e *macro.Engine, world, text string) (string, []string) {
	return e.MatchTrigger(world, text)
}

func fireHook(e *macro.Engine, world, hookName string) []string {
	return e.FireHook(world, hookName)
}
