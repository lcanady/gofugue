package bus_test

import (
	"sync"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
)

// --- helpers ---

func worldLine(world, text string) bus.WorldLineEvent {
	return bus.WorldLineEvent{WorldName: world, Text: text}
}

func publish(b *bus.Bus, ev bus.Event) {
	b.Publish(ev)
}

// waitEvent waits up to 100 ms for an event on sub.C, returns it or fails.
func waitEvent(t *testing.T, sub *bus.Subscription) bus.Event {
	t.Helper()
	select {
	case ev := <-sub.C:
		return ev
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for event")
		return nil
	}
}

// ---

func TestBus_PublishToTypedSubscriber(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(4, bus.EvWorldLine)
	defer sub.Cancel()

	publish(b, worldLine("myworld", "hello"))

	ev := waitEvent(t, sub)
	wl, ok := ev.(bus.WorldLineEvent)
	if !ok {
		t.Fatalf("expected WorldLineEvent, got %T", ev)
	}
	if wl.Text != "hello" {
		t.Errorf("got text %q, want %q", wl.Text, "hello")
	}
	if wl.WorldName != "myworld" {
		t.Errorf("got world %q, want %q", wl.WorldName, "myworld")
	}
}

func TestBus_TypedSubscriberDoesNotReceiveOtherTypes(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(4, bus.EvGMCP)
	defer sub.Cancel()

	// publish a WorldLine — should NOT arrive on GMCP subscriber
	publish(b, worldLine("w", "noise"))

	select {
	case got := <-sub.C:
		t.Fatalf("received unexpected event: %v", got)
	case <-time.After(20 * time.Millisecond):
		// correct — nothing received
	}
}

func TestBus_WildcardSubscriberReceivesAll(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(8) // no types = wildcard
	defer sub.Cancel()

	publish(b, worldLine("w", "line"))
	publish(b, bus.GMCPEvent{WorldName: "w", Module: "Char.Vitals", Data: []byte("{}")})
	publish(b, bus.HookEvent{WorldName: "w", Name: "CONNECT"})

	for i := 0; i < 3; i++ {
		waitEvent(t, sub)
	}
}

func TestBus_MultipleSubscribers_AllReceive(t *testing.T) {
	b := bus.New()
	s1 := b.Subscribe(4, bus.EvWorldLine)
	s2 := b.Subscribe(4, bus.EvWorldLine)
	defer s1.Cancel()
	defer s2.Cancel()

	publish(b, worldLine("w", "broadcast"))

	waitEvent(t, s1)
	waitEvent(t, s2)
}

func TestBus_Cancel_StopsDelivery(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(4, bus.EvWorldLine)
	sub.Cancel()

	publish(b, worldLine("w", "after cancel"))

	select {
	case ev := <-sub.C:
		t.Fatalf("received event after Cancel: %v", ev)
	case <-time.After(20 * time.Millisecond):
		// correct
	}
}

func TestBus_SlowSubscriber_DoesNotBlockPublisher(t *testing.T) {
	b := bus.New()
	// buffer=1 so second publish would block a blocking send
	slow := b.Subscribe(1, bus.EvWorldLine)
	defer slow.Cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 100; i++ {
			publish(b, worldLine("w", "flood"))
		}
	}()

	select {
	case <-done:
		// publisher finished without blocking
	case <-time.After(500 * time.Millisecond):
		t.Fatal("publisher blocked on slow subscriber")
	}
}

func TestBus_ConcurrentPublishSubscribe_RaceDetector(t *testing.T) {
	b := bus.New()
	var wg sync.WaitGroup

	// 10 publishers, 10 subscribers, all concurrent
	for i := 0; i < 10; i++ {
		sub := b.Subscribe(32, bus.EvWorldLine)
		wg.Add(1)
		go func(s *bus.Subscription) {
			defer wg.Done()
			defer s.Cancel()
			timeout := time.After(200 * time.Millisecond)
			for {
				select {
				case <-s.C:
				case <-timeout:
					return
				}
			}
		}(sub)
	}
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				publish(b, worldLine("w", "concurrent"))
			}
		}()
	}

	wg.Wait()
}

func TestBus_EventTypes(t *testing.T) {
	tests := []struct {
		ev   bus.Event
		want bus.EventType
	}{
		{bus.WorldLineEvent{}, bus.EvWorldLine},
		{bus.GMCPEvent{}, bus.EvGMCP},
		{bus.HookEvent{}, bus.EvHook},
		{bus.StatusEvent{}, bus.EvStatus},
		{bus.UserInputEvent{}, bus.EvUserInput},
		{bus.UserCmdEvent{}, bus.EvUserCmd},
	}
	for _, tt := range tests {
		if got := tt.ev.Type(); got != tt.want {
			t.Errorf("%T.Type() = %q, want %q", tt.ev, got, tt.want)
		}
	}
}

func TestBus_WorldField(t *testing.T) {
	tests := []struct {
		ev    bus.Event
		world string
	}{
		{bus.WorldLineEvent{WorldName: "alpha"}, "alpha"},
		{bus.GMCPEvent{WorldName: "beta"}, "beta"},
		{bus.HookEvent{WorldName: "gamma"}, "gamma"},
		{bus.StatusEvent{WorldName: "delta"}, "delta"},
		{bus.UserInputEvent{WorldName: "epsilon"}, "epsilon"},
		{bus.UserCmdEvent{WorldName: "zeta"}, "zeta"},
	}
	for _, tt := range tests {
		if got := tt.ev.World(); got != tt.world {
			t.Errorf("%T.World() = %q, want %q", tt.ev, got, tt.world)
		}
	}
}
