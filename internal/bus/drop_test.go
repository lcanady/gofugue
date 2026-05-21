package bus

import (
	"testing"
	"time"
)

// TestDropCounter verifies that a slow subscriber records drops via the
// per-subscription atomic counter (surfaced through Bus.Stats) and that
// Publish never blocks even when the subscriber's channel is full.
func TestDropCounter(t *testing.T) {
	b := New()
	const buf = 2
	sub := b.Subscribe(buf, EvHook)
	defer sub.Cancel()

	done := make(chan struct{})
	go func() {
		for i := 0; i < 100; i++ {
			b.Publish(HookEvent{WorldName: "w", Name: "TICK"})
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked: 100 publishes did not complete within 2s")
	}

	stats := b.Stats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 subscriber stat, got %d", len(stats))
	}
	s := stats[0]
	if s.Capacity != buf {
		t.Errorf("Capacity = %d, want %d", s.Capacity, buf)
	}
	if s.Pending != buf {
		t.Errorf("Pending = %d, want %d (channel should be full)", s.Pending, buf)
	}
	if s.Dropped == 0 {
		t.Errorf("Dropped = 0, want > 0 (publishes should have been dropped)")
	}
	// 100 publishes, buffer 2 → at least 98 drops.
	if got, want := s.Dropped, uint64(98); got < want {
		t.Errorf("Dropped = %d, want >= %d", got, want)
	}
}
