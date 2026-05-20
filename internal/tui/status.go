package tui

import (
	"fmt"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

// statusField is one named, pluggable segment of the status bar.
type statusField struct {
	Name string
	fn   func() string
}

// StatusBar renders a single summary line between output and input.
// Fields can be added or removed at runtime via AddField/RemoveField.
// A set of default fields (world, connected, lag, time) is always present
// unless explicitly removed.
type StatusBar struct {
	mu     sync.Mutex
	width  int
	more   bool // true when scrolled above live view
	style  tcell.Style

	// mutable state read by the default field closures
	world     string
	connected bool
	lagMS     int64

	fields []statusField
}

func newStatusBar(width int) *StatusBar {
	s := &StatusBar{
		width: width,
		style: tcell.StyleDefault,
	}
	s.fields = s.defaultFields()
	return s
}

// SetStyle replaces the style used to render the entire status bar.
func (s *StatusBar) SetStyle(style tcell.Style) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.style = style
}

// NewStatusBar is the exported constructor used by tests.
func NewStatusBar(width int) *StatusBar { return newStatusBar(width) }

// defaultFields returns the built-in status bar segments.
func (s *StatusBar) defaultFields() []statusField {
	return []statusField{
		{Name: "app", fn: func() string { return "GoFugue" }},
		{Name: "world", fn: func() string { return worldOrDash(s.world) }},
		{Name: "conn", fn: func() string {
			if s.connected {
				return "CONNECTED"
			}
			return "──"
		}},
		{Name: "lag", fn: func() string {
			if s.lagMS > 0 {
				return fmt.Sprintf("lag:%dms", s.lagMS)
			}
			return ""
		}},
		{Name: "time", fn: func() string { return time.Now().Format("15:04:05") }},
	}
}

// Update sets the runtime state for the built-in fields.
func (s *StatusBar) Update(world string, connected bool, lagMS int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.world = world
	s.connected = connected
	s.lagMS = lagMS
}

// SetMore controls the [MORE] indicator shown when the user has scrolled up.
func (s *StatusBar) SetMore(more bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.more = more
}

// Resize updates the bar width on terminal resize.
func (s *StatusBar) Resize(width int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.width = width
}

// AddField appends a custom field at the end of the bar (before the time field
// is drawn). If a field with the same name already exists it is replaced.
func (s *StatusBar) AddField(name string, fn func() string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.fields {
		if f.Name == name {
			s.fields[i].fn = fn
			return
		}
	}
	s.fields = append(s.fields, statusField{Name: name, fn: fn})
}

// RemoveField removes a field by name (no-op if not found).
func (s *StatusBar) RemoveField(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, f := range s.fields {
		if f.Name == name {
			s.fields = append(s.fields[:i], s.fields[i+1:]...)
			return
		}
	}
}

// Draw renders the status bar onto three consecutive screen rows starting at
// row: a ─ border line, the content line, then another ─ border line.
func (s *StatusBar) Draw(screen tcell.Screen, row int) {
	s.mu.Lock()
	width := s.width
	more := s.more

	// Collect non-empty field values under the lock.
	parts := make([]string, 0, len(s.fields))
	for _, f := range s.fields {
		v := f.fn()
		if v != "" {
			parts = append(parts, v)
		}
	}
	if more {
		parts = append(parts, "[MORE]")
	}
	s.mu.Unlock()

	// Join segments with │ separator.
	text := " "
	for i, p := range parts {
		if i > 0 {
			text += " │ "
		}
		text += p
	}
	text += " "

	style := s.style

	// Top border row.
	for col := 0; col < width; col++ {
		screen.SetContent(col, row, '─', nil, style)
	}

	// Content row (row+1).
	col := 0
	gr := uniseg.NewGraphemes(text)
	for gr.Next() && col < width {
		runes := gr.Runes()
		if len(runes) == 0 {
			continue
		}
		screen.SetContent(col, row+1, runes[0], runes[1:], style)
		col += gr.Width()
	}
	for ; col < width; col++ {
		screen.SetContent(col, row+1, ' ', nil, style)
	}

	// Bottom border row (row+2).
	for col := 0; col < width; col++ {
		screen.SetContent(col, row+2, '─', nil, style)
	}
}

func worldOrDash(w string) string {
	if w == "" {
		return "(no world)"
	}
	return w
}
