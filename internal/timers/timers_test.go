package timers_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/timers"
)

func TestAdd_FiresOnce(t *testing.T) {
	p := timers.New()
	var count int32
	p.Add(context.Background(), 10*time.Millisecond, false, func() {
		atomic.AddInt32(&count, 1)
	})
	time.Sleep(50 * time.Millisecond)
	if n := atomic.LoadInt32(&count); n != 1 {
		t.Errorf("expected 1 fire, got %d", n)
	}
	if len(p.List()) != 0 {
		t.Error("one-shot timer should be removed after firing")
	}
}

func TestAdd_Repeat(t *testing.T) {
	p := timers.New()
	var count int32
	id := p.Add(context.Background(), 10*time.Millisecond, true, func() {
		atomic.AddInt32(&count, 1)
	})
	time.Sleep(55 * time.Millisecond)
	p.Cancel(id)
	n := atomic.LoadInt32(&count)
	if n < 3 {
		t.Errorf("expected ≥3 fires in 55ms at 10ms interval, got %d", n)
	}
}

func TestCancel_StopsFiring(t *testing.T) {
	p := timers.New()
	var count int32
	id := p.Add(context.Background(), 10*time.Millisecond, true, func() {
		atomic.AddInt32(&count, 1)
	})
	time.Sleep(15 * time.Millisecond)
	p.Cancel(id)
	before := atomic.LoadInt32(&count)
	time.Sleep(30 * time.Millisecond)
	after := atomic.LoadInt32(&count)
	if after != before {
		t.Errorf("timer fired after cancel: %d → %d", before, after)
	}
}

func TestList_ShowsActive(t *testing.T) {
	p := timers.New()
	id := p.Add(context.Background(), time.Hour, true, func() {})
	infos := p.List()
	if len(infos) != 1 || infos[0].ID != id {
		t.Errorf("List() = %v, want [{ID:%d}]", infos, id)
	}
	p.Cancel(id)
	if len(p.List()) != 0 {
		t.Error("List should be empty after cancel")
	}
}

func TestContextCancel_StopsTimer(t *testing.T) {
	p := timers.New()
	ctx, cancel := context.WithCancel(context.Background())
	var count int32
	p.Add(ctx, 10*time.Millisecond, true, func() { atomic.AddInt32(&count, 1) })
	time.Sleep(15 * time.Millisecond)
	cancel()
	before := atomic.LoadInt32(&count)
	time.Sleep(30 * time.Millisecond)
	if atomic.LoadInt32(&count) != before {
		t.Error("context cancel should stop timer")
	}
}

func TestCancel_NonExistentID_ReturnsFalse(t *testing.T) {
	p := timers.New()
	// Cancel an ID that was never registered.
	if p.Cancel(99999) {
		t.Error("Cancel of non-existent ID should return false")
	}
}

func TestCancel_AlreadyFired_IsIdempotent(t *testing.T) {
	p := timers.New()
	var count int32
	id := p.Add(context.Background(), 5*time.Millisecond, false, func() {
		atomic.AddInt32(&count, 1)
	})
	// Wait for it to fire and remove itself.
	time.Sleep(30 * time.Millisecond)

	// Now cancel — timer is already gone; should return false without panic.
	result := p.Cancel(id)
	if result {
		t.Error("Cancel of already-fired one-shot timer should return false")
	}
}

func TestAdd_ZeroDuration_FiresImmediately(t *testing.T) {
	p := timers.New()
	var count int32
	p.Add(context.Background(), 0, false, func() {
		atomic.AddInt32(&count, 1)
	})
	// Zero-duration timer should fire nearly immediately.
	time.Sleep(20 * time.Millisecond)
	if n := atomic.LoadInt32(&count); n != 1 {
		t.Errorf("zero-duration timer: expected 1 fire, got %d", n)
	}
}

func TestList_MultipleTimers_AllListed(t *testing.T) {
	p := timers.New()
	ids := make([]int, 3)
	for i := range ids {
		ids[i] = p.Add(context.Background(), time.Hour, true, func() {})
	}
	list := p.List()
	if len(list) != 3 {
		t.Errorf("expected 3 timers in list, got %d", len(list))
	}
	// Cancel all.
	for _, id := range ids {
		p.Cancel(id)
	}
	if len(p.List()) != 0 {
		t.Error("List should be empty after all timers cancelled")
	}
}
