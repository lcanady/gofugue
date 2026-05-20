// Package keyboard handles user input: readline-style line editing,
// configurable keybindings, and input history.
package keyboard

import (
	"strings"
)

// Action is a named editing operation (e.g. "move-bol", "kill-word").
type Action string

const (
	ActionSubmit       Action = "submit"
	ActionMoveBOL      Action = "move-bol"      // Ctrl-A
	ActionMoveEOL      Action = "move-eol"      // Ctrl-E
	ActionMoveLeft     Action = "move-left"      // ←
	ActionMoveRight    Action = "move-right"     // →
	ActionDeleteBack   Action = "delete-back"    // Backspace
	ActionDeleteFwd    Action = "delete-fwd"     // Delete
	ActionKillLine     Action = "kill-line"      // Ctrl-K
	ActionKillWord     Action = "kill-word"      // Ctrl-W
	ActionHistoryPrev  Action = "history-prev"   // ↑
	ActionHistoryNext  Action = "history-next"   // ↓
	ActionScrollUp     Action = "scroll-up"      // PgUp
	ActionScrollDown   Action = "scroll-down"    // PgDn
	ActionComplete     Action = "complete"        // Tab
)

// Binding maps a key sequence string to an Action.
type Binding struct {
	Key    string // e.g. "ctrl-a", "up", "f1"
	Action Action
}

// DefaultBindings returns the standard emacs-style key map.
func DefaultBindings() []Binding {
	return []Binding{
		{Key: "enter", Action: ActionSubmit},
		{Key: "ctrl-a", Action: ActionMoveBOL},
		{Key: "ctrl-e", Action: ActionMoveEOL},
		{Key: "left", Action: ActionMoveLeft},
		{Key: "right", Action: ActionMoveRight},
		{Key: "backspace", Action: ActionDeleteBack},
		{Key: "delete", Action: ActionDeleteFwd},
		{Key: "ctrl-k", Action: ActionKillLine},
		{Key: "ctrl-w", Action: ActionKillWord},
		{Key: "up", Action: ActionHistoryPrev},
		{Key: "down", Action: ActionHistoryNext},
		{Key: "pgup", Action: ActionScrollUp},
		{Key: "pgdn", Action: ActionScrollDown},
		{Key: "tab", Action: ActionComplete},
	}
}

// LineEditor maintains the current input line state.
type LineEditor struct {
	buf    []rune
	cursor int // position in buf (rune index)
}

// Insert adds a rune at the cursor position.
func (e *LineEditor) Insert(r rune) {
	e.buf = append(e.buf[:e.cursor], append([]rune{r}, e.buf[e.cursor:]...)...)
	e.cursor++
}

// Apply executes an editing Action.
func (e *LineEditor) Apply(a Action) (submitted string, ok bool) {
	switch a {
	case ActionSubmit:
		s := string(e.buf)
		e.buf = e.buf[:0]
		e.cursor = 0
		return s, true
	case ActionMoveBOL:
		e.cursor = 0
	case ActionMoveEOL:
		e.cursor = len(e.buf)
	case ActionMoveLeft:
		if e.cursor > 0 {
			e.cursor--
		}
	case ActionMoveRight:
		if e.cursor < len(e.buf) {
			e.cursor++
		}
	case ActionDeleteBack:
		if e.cursor > 0 {
			e.buf = append(e.buf[:e.cursor-1], e.buf[e.cursor:]...)
			e.cursor--
		}
	case ActionDeleteFwd:
		if e.cursor < len(e.buf) {
			e.buf = append(e.buf[:e.cursor], e.buf[e.cursor+1:]...)
		}
	case ActionKillLine:
		e.buf = e.buf[:e.cursor]
	case ActionKillWord:
		// delete backwards to previous word boundary
		end := e.cursor
		for end > 0 && e.buf[end-1] == ' ' {
			end--
		}
		for end > 0 && e.buf[end-1] != ' ' {
			end--
		}
		e.buf = append(e.buf[:end], e.buf[e.cursor:]...)
		e.cursor = end
	}
	return "", false
}

// Text returns the current input line as a string.
func (e *LineEditor) Text() string { return string(e.buf) }

// Cursor returns the current cursor position (rune index).
func (e *LineEditor) Cursor() int { return e.cursor }

// History stores submitted input lines for recall.
type History struct {
	lines []string
	pos   int // -1 = not in history (live input)
}

// Push records a submitted line.
func (h *History) Push(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	// avoid consecutive duplicates
	if len(h.lines) > 0 && h.lines[len(h.lines)-1] == line {
		return
	}
	h.lines = append(h.lines, line)
	h.pos = -1
}

// Prev returns the previous history entry (↑).
func (h *History) Prev() (string, bool) {
	if len(h.lines) == 0 {
		return "", false
	}
	if h.pos < 0 {
		h.pos = len(h.lines) - 1
	} else if h.pos > 0 {
		h.pos--
	}
	return h.lines[h.pos], true
}

// Next returns the next history entry (↓); returns "", false at the live line.
func (h *History) Next() (string, bool) {
	if h.pos < 0 {
		return "", false
	}
	h.pos++
	if h.pos >= len(h.lines) {
		h.pos = -1
		return "", false
	}
	return h.lines[h.pos], true
}
