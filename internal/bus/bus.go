// Package bus provides the central typed event bus. All components communicate
// exclusively through this bus — no direct cross-package calls at runtime.
package bus

import (
	"context"
	"sync"
)

// EventType identifies the kind of event.
type EventType string

const (
	EvWorldLine         EventType = "world.line"          // raw from world connection
	EvWorldLineRendered EventType = "world.line.rendered" // after gag/hilite/substitute
	EvGMCP              EventType = "gmcp"
	EvHook              EventType = "hook"
	EvStatus            EventType = "status"
	EvUserInput         EventType = "user.input"
	EvUserCmd           EventType = "user.cmd"
)

// Event is the common interface for all bus events.
type Event interface {
	Type() EventType
	World() string // empty string = global / not world-scoped
}

// Span is a run of text sharing a single style, used to represent
// mid-line colour changes from MUD server ANSI sequences.
type Span struct {
	Text  string
	Attrs LineAttrs
}

// WorldLineEvent carries a single line of output from a world connection.
// Spans is populated by the ANSI parser when the line contains colour codes;
// if empty, Text+Attrs represent a plain single-span line.
type WorldLineEvent struct {
	WorldName string
	Text      string   // plain text (ANSI stripped); always set
	Attrs     LineAttrs
	Spans     []Span   // non-empty when line has mid-line colour changes
	Gagged    bool     // trigger marked this line as suppressed
}

func (e WorldLineEvent) Type() EventType { return EvWorldLine }
func (e WorldLineEvent) World() string   { return e.WorldName }

// WorldRenderedEvent is a WorldLineEvent after gag/hilite/substitute processing.
// The TUI subscribes to this instead of WorldLineEvent so it sees processed output.
type WorldRenderedEvent struct{ WorldLineEvent }

func (e WorldRenderedEvent) Type() EventType { return EvWorldLineRendered }
func (e WorldRenderedEvent) World() string   { return e.WorldName }

// LineAttrs holds ANSI display attributes for a line or span.
type LineAttrs struct {
	FG, BG    int      // palette index, -1 = default
	FGRGB     [3]byte  // true-colour FG; active when FG == -2
	BGRGB     [3]byte  // true-colour BG; active when BG == -2
	Bold      bool
	Underline bool
	Reverse   bool
	Italic    bool
}

// GMCPEvent carries a parsed GMCP package from a world.
type GMCPEvent struct {
	WorldName string
	Module    string // e.g. "Char.Vitals"
	Data      []byte // raw JSON
}

func (e GMCPEvent) Type() EventType { return EvGMCP }
func (e GMCPEvent) World() string   { return e.WorldName }

// HookEvent fires at named lifecycle points (CONNECT, DISCONNECT, ACTIVITY, …).
type HookEvent struct {
	WorldName string
	Name      string // hook name
}

func (e HookEvent) Type() EventType { return EvHook }
func (e HookEvent) World() string   { return e.WorldName }

// StatusEvent carries connection state for a world (lag, connected flag, etc.).
type StatusEvent struct {
	WorldName string
	Connected bool
	LagMS     int64
}

func (e StatusEvent) Type() EventType { return EvStatus }
func (e StatusEvent) World() string   { return e.WorldName }

// UserInputEvent carries raw text entered by the user.
type UserInputEvent struct {
	WorldName string
	Text      string
}

func (e UserInputEvent) Type() EventType { return EvUserInput }
func (e UserInputEvent) World() string   { return e.WorldName }

// UserCmdEvent carries a parsed /command line from the user or a macro.
type UserCmdEvent struct {
	WorldName string
	Line      string // full command line including leading /
}

func (e UserCmdEvent) Type() EventType { return EvUserCmd }
func (e UserCmdEvent) World() string   { return e.WorldName }

// ---------------------------------------------------------------------------

// Subscription is returned by Subscribe and closed by calling Cancel.
type Subscription struct {
	C      <-chan Event
	cancel func()
}

// Cancel unsubscribes and drains the channel.
func (s *Subscription) Cancel() { s.cancel() }

// Bus is a fan-out event dispatcher. Subscribers receive events on a buffered
// channel. Slow subscribers are dropped (non-blocking send) to prevent the
// bus from stalling the rest of the system.
type Bus struct {
	mu   sync.RWMutex
	subs map[EventType][]*subscription
}

type subscription struct {
	ch     chan Event
	filter EventType // empty = all
}

// New returns a ready-to-use Bus.
func New() *Bus {
	return &Bus{subs: make(map[EventType][]*subscription)}
}

// Subscribe returns a Subscription that receives events of the given types.
// Pass no types to receive all events.
func (b *Bus) Subscribe(bufSize int, types ...EventType) *Subscription {
	ch := make(chan Event, bufSize)
	subs := make([]*subscription, 0, len(types))

	b.mu.Lock()
	if len(types) == 0 {
		s := &subscription{ch: ch}
		b.subs[""] = append(b.subs[""], s)
		subs = append(subs, s)
	} else {
		for _, t := range types {
			s := &subscription{ch: ch, filter: t}
			b.subs[t] = append(b.subs[t], s)
			subs = append(subs, s)
		}
	}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		for _, s := range subs {
			key := s.filter
			sl := b.subs[key]
			for i, sub := range sl {
				if sub == s {
					b.subs[key] = append(sl[:i], sl[i+1:]...)
					break
				}
			}
		}
		b.mu.Unlock()
		// drain so no goroutine leaks waiting on ch
		for len(ch) > 0 {
			<-ch
		}
	}

	return &Subscription{C: ch, cancel: cancel}
}

// Publish sends ev to all matching subscribers. Non-blocking: slow subscribers
// miss the event rather than blocking the publisher.
func (b *Bus) Publish(ev Event) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	send := func(s *subscription) {
		select {
		case s.ch <- ev:
		default:
		}
	}
	for _, s := range b.subs[ev.Type()] {
		send(s)
	}
	for _, s := range b.subs[""] {
		send(s)
	}
}

// PublishCtx publishes ev, respecting ctx cancellation on a best-effort basis.
func (b *Bus) PublishCtx(ctx context.Context, ev Event) {
	select {
	case <-ctx.Done():
	default:
		b.Publish(ev)
	}
}
