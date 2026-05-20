package history

import "sync"

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
