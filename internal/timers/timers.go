// Package timers provides a goroutine-based timer pool used by /repeat and
// the JS/Python scripting bridges.
package timers

import (
	"context"
	"sync"
	"time"
)

// Info is a snapshot of one active timer for /ps output.
type Info struct {
	ID       int
	Interval time.Duration
	Repeat   bool
	Next     time.Time
}

// Pool manages a set of named timers, each running in its own goroutine.
type Pool struct {
	mu     sync.Mutex
	timers map[int]*entry
	nextID int
}

type entry struct {
	id       int
	interval time.Duration
	repeat   bool
	next     time.Time
	cancel   context.CancelFunc
}

// New returns an empty timer Pool.
func New() *Pool {
	return &Pool{timers: make(map[int]*entry)}
}

// Add schedules fn to fire after delay. If repeat is true, fn fires every
// delay thereafter until cancelled. Returns the timer ID.
func (p *Pool) Add(ctx context.Context, delay time.Duration, repeat bool, fn func()) int {
	p.mu.Lock()
	p.nextID++
	id := p.nextID
	timerCtx, cancel := context.WithCancel(ctx)
	e := &entry{
		id:       id,
		interval: delay,
		repeat:   repeat,
		next:     time.Now().Add(delay),
		cancel:   cancel,
	}
	p.timers[id] = e
	p.mu.Unlock()

	go func() {
		defer func() {
			p.mu.Lock()
			delete(p.timers, id)
			p.mu.Unlock()
		}()
		t := time.NewTimer(delay)
		defer t.Stop()
		for {
			select {
			case <-timerCtx.Done():
				return
			case <-t.C:
				fn()
				if !repeat {
					return
				}
				// Update next-fire snapshot and reset.
				p.mu.Lock()
				if e2, ok := p.timers[id]; ok {
					e2.next = time.Now().Add(delay)
				}
				p.mu.Unlock()
				t.Reset(delay)
			}
		}
	}()

	return id
}

// Cancel stops a timer by ID. Returns true if it existed.
func (p *Pool) Cancel(id int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	e, ok := p.timers[id]
	if !ok {
		return false
	}
	e.cancel()
	delete(p.timers, id)
	return true
}

// List returns a snapshot of all currently active timers.
func (p *Pool) List() []Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Info, 0, len(p.timers))
	for _, e := range p.timers {
		out = append(out, Info{
			ID:       e.id,
			Interval: e.interval,
			Repeat:   e.repeat,
			Next:     e.next,
		})
	}
	return out
}
