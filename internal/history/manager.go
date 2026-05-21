package history

import (
	"sync"

	"github.com/kumakun/gofugue/internal/bus"
)

// Manager maintains one Buffer per world, created on demand.
// It is safe for concurrent use.
type Manager struct {
	mu       sync.Mutex
	buffers  map[string]*Buffer
	capacity int
}

// NewManager creates a Manager where each per-world buffer holds capacity lines.
func NewManager(capacity int) *Manager {
	return &Manager{
		buffers:  make(map[string]*Buffer),
		capacity: capacity,
	}
}

// World returns (creating if necessary) the Buffer for the named world.
// Use "" for global / system messages.
func (m *Manager) World(name string) *Buffer {
	m.mu.Lock()
	defer m.mu.Unlock()
	if b, ok := m.buffers[name]; ok {
		return b
	}
	b := New(m.capacity)
	m.buffers[name] = b
	return b
}

// SearchAll searches all world buffers and returns matching lines.
func (m *Manager) SearchAll(pattern string) []Line {
	m.mu.Lock()
	bufs := make([]*Buffer, 0, len(m.buffers))
	for _, b := range m.buffers {
		bufs = append(bufs, b)
	}
	m.mu.Unlock()

	var out []Line
	for _, b := range bufs {
		out = append(out, b.Search(pattern)...)
	}
	return out
}

// TailAll returns the last n lines across all worlds (no ordering guarantee).
func (m *Manager) TailAll(n int) []Line {
	m.mu.Lock()
	bufs := make([]*Buffer, 0, len(m.buffers))
	for _, b := range m.buffers {
		bufs = append(bufs, b)
	}
	m.mu.Unlock()

	var out []Line
	for _, b := range bufs {
		out = append(out, b.Tail(n)...)
	}
	return out
}

// WorldLine is a scrollback Line tagged with the world it belongs to. Used by
// the rich history IPC endpoint so frontends can backfill per-world panes with
// full ANSI attributes intact.
type WorldLine struct {
	WorldName string `json:"world"`
	Text      string `json:"text"`
	Attrs     bus.LineAttrs `json:"attrs"`
}

// TailAllRich returns the last n lines per world, world-tagged and including
// attrs. Gagged lines are dropped (consistent with what the user actually saw).
func (m *Manager) TailAllRich(n int) []WorldLine {
	m.mu.Lock()
	type entry struct {
		name string
		buf  *Buffer
	}
	entries := make([]entry, 0, len(m.buffers))
	for name, b := range m.buffers {
		entries = append(entries, entry{name, b})
	}
	m.mu.Unlock()

	var out []WorldLine
	for _, e := range entries {
		for _, l := range e.buf.Tail(n) {
			if l.Gagged {
				continue
			}
			out = append(out, WorldLine{WorldName: e.name, Text: l.Text, Attrs: l.Attrs})
		}
	}
	return out
}
