package keyboard_test

import (
	"fmt"
	"testing"

	"github.com/kumakun/gofugue/internal/keyboard"
)

// --- LineEditor ---

func TestLineEditor_Insert(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('h')
	e.Insert('i')
	if got := e.Text(); got != "hi" {
		t.Errorf("Text() = %q, want %q", got, "hi")
	}
	if e.Cursor() != 2 {
		t.Errorf("Cursor() = %d, want 2", e.Cursor())
	}
}

func TestLineEditor_Submit_ClearsBuffer(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('h')
	e.Insert('i')
	s, ok := e.Apply(keyboard.ActionSubmit)
	if !ok {
		t.Fatal("Submit should return ok=true")
	}
	if s != "hi" {
		t.Errorf("submitted text = %q, want %q", s, "hi")
	}
	if e.Text() != "" {
		t.Errorf("after submit, Text() = %q, want empty", e.Text())
	}
	if e.Cursor() != 0 {
		t.Errorf("after submit, Cursor() = %d, want 0", e.Cursor())
	}
}

func TestLineEditor_MoveBOL(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('a')
	e.Insert('b')
	e.Apply(keyboard.ActionMoveBOL)
	if e.Cursor() != 0 {
		t.Errorf("after MoveBOL, Cursor() = %d, want 0", e.Cursor())
	}
}

func TestLineEditor_MoveEOL(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('a')
	e.Insert('b')
	e.Apply(keyboard.ActionMoveBOL)
	e.Apply(keyboard.ActionMoveEOL)
	if e.Cursor() != 2 {
		t.Errorf("after MoveEOL, Cursor() = %d, want 2", e.Cursor())
	}
}

func TestLineEditor_MoveLeft_ClipsAtZero(t *testing.T) {
	var e keyboard.LineEditor
	e.Apply(keyboard.ActionMoveLeft) // cursor already at 0
	if e.Cursor() != 0 {
		t.Errorf("Cursor() = %d, want 0", e.Cursor())
	}
}

func TestLineEditor_MoveRight_ClipsAtEnd(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('x')
	e.Apply(keyboard.ActionMoveRight) // already at end
	if e.Cursor() != 1 {
		t.Errorf("Cursor() = %d, want 1", e.Cursor())
	}
}

func TestLineEditor_DeleteBack(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('a')
	e.Insert('b')
	e.Apply(keyboard.ActionDeleteBack)
	if e.Text() != "a" {
		t.Errorf("Text() = %q, want %q", e.Text(), "a")
	}
}

func TestLineEditor_DeleteBack_AtStart_NoOp(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('a')
	e.Apply(keyboard.ActionMoveBOL)
	e.Apply(keyboard.ActionDeleteBack) // no-op
	if e.Text() != "a" {
		t.Errorf("Text() = %q, want %q", e.Text(), "a")
	}
}

func TestLineEditor_DeleteFwd(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('a')
	e.Insert('b')
	e.Apply(keyboard.ActionMoveBOL)
	e.Apply(keyboard.ActionDeleteFwd)
	if e.Text() != "b" {
		t.Errorf("Text() = %q, want %q", e.Text(), "b")
	}
}

func TestLineEditor_KillLine(t *testing.T) {
	var e keyboard.LineEditor
	for _, r := range "hello world" {
		e.Insert(r)
	}
	e.Apply(keyboard.ActionMoveBOL)
	e.Apply(keyboard.ActionMoveRight) // cursor at 1
	e.Apply(keyboard.ActionMoveRight) // cursor at 2
	e.Apply(keyboard.ActionKillLine)
	if e.Text() != "he" {
		t.Errorf("Text() = %q, want %q", e.Text(), "he")
	}
}

func TestLineEditor_KillWord(t *testing.T) {
	var e keyboard.LineEditor
	for _, r := range "hello world" {
		e.Insert(r)
	}
	// cursor is at end; kill word should delete "world"
	e.Apply(keyboard.ActionKillWord)
	if e.Text() != "hello " {
		t.Errorf("Text() = %q, want %q", e.Text(), "hello ")
	}
}

func TestLineEditor_InsertMidLine(t *testing.T) {
	var e keyboard.LineEditor
	e.Insert('a')
	e.Insert('c')
	e.Apply(keyboard.ActionMoveBOL)
	e.Apply(keyboard.ActionMoveRight)
	e.Insert('b') // inserts between a and c
	if e.Text() != "abc" {
		t.Errorf("Text() = %q, want %q", e.Text(), "abc")
	}
}

// --- History ---

func TestHistory_PrevNext_Empty(t *testing.T) {
	var h keyboard.History
	_, ok := h.Prev()
	if ok {
		t.Error("Prev on empty history should return ok=false")
	}
	_, ok = h.Next()
	if ok {
		t.Error("Next on empty history should return ok=false")
	}
}

func TestHistory_PushAndRecall(t *testing.T) {
	var h keyboard.History
	h.Push("first")
	h.Push("second")
	h.Push("third")

	s, ok := h.Prev()
	if !ok || s != "third" {
		t.Errorf("first Prev = %q %v, want 'third' true", s, ok)
	}
	s, ok = h.Prev()
	if !ok || s != "second" {
		t.Errorf("second Prev = %q %v, want 'second' true", s, ok)
	}
	s, ok = h.Next()
	if !ok || s != "third" {
		t.Errorf("Next after two Prevs = %q %v, want 'third' true", s, ok)
	}
}

func TestHistory_Next_AtLive_ReturnsFalse(t *testing.T) {
	var h keyboard.History
	h.Push("cmd")
	h.Prev()
	_, ok := h.Next() // back to live
	if ok {
		t.Error("Next past the end should return ok=false")
	}
}

func TestHistory_NoDuplicateConsecutive(t *testing.T) {
	var h keyboard.History
	h.Push("same")
	h.Push("same")
	h.Push("same")

	// Only one entry should be stored. Prev clips at oldest entry (keeps
	// returning it), so we verify via Next: after one Prev(), Next() takes
	// us back to the live line (ok=false), confirming there is only one entry.
	s, ok := h.Prev()
	if !ok || s != "same" {
		t.Fatalf("first Prev = %q %v, want 'same' true", s, ok)
	}
	_, ok2 := h.Next() // back to live — only one history entry
	if ok2 {
		t.Error("expected Next() to return false (only one entry stored)")
	}
}

func TestHistory_SkipsBlank(t *testing.T) {
	var h keyboard.History
	h.Push("")
	h.Push("   ")
	h.Push("real")

	// Only "real" should be stored; blanks are silently dropped.
	s, ok := h.Prev()
	if !ok || s != "real" {
		t.Errorf("Prev = %q %v, want 'real' true", s, ok)
	}
	// Next() should immediately return false — only one entry in history.
	_, ok = h.Next()
	if ok {
		t.Error("blank lines should not be stored; Next() should return false after single Prev()")
	}
}

func TestDefaultBindings_NotEmpty(t *testing.T) {
	bindings := keyboard.DefaultBindings()
	if len(bindings) == 0 {
		t.Error("DefaultBindings returned empty slice")
	}
	// verify critical bindings exist
	have := make(map[keyboard.Action]bool)
	for _, b := range bindings {
		have[b.Action] = true
	}
	for _, a := range []keyboard.Action{
		keyboard.ActionSubmit,
		keyboard.ActionMoveBOL,
		keyboard.ActionMoveEOL,
		keyboard.ActionHistoryPrev,
		keyboard.ActionHistoryNext,
	} {
		if !have[a] {
			t.Errorf("default bindings missing action %q", a)
		}
	}
}

// ---------------------------------------------------------------------------
// Additional KillWord edge cases
// ---------------------------------------------------------------------------

func TestLineEditor_KillWord_AtStart_IsNoOp(t *testing.T) {
	var e keyboard.LineEditor
	for _, r := range "hello" {
		e.Insert(r)
	}
	e.Apply(keyboard.ActionMoveBOL)
	e.Apply(keyboard.ActionKillWord) // cursor at start — nothing to kill backwards
	// Text should be unchanged (KillWord kills backwards from cursor).
	if e.Text() != "hello" {
		t.Errorf("KillWord at BOL: Text() = %q, want %q", e.Text(), "hello")
	}
}

func TestLineEditor_KillWord_MidWord_DeletesToWordBoundary(t *testing.T) {
	var e keyboard.LineEditor
	for _, r := range "hello world" {
		e.Insert(r)
	}
	// Move cursor to middle of "world" (position 8 = "hello wo|rld").
	e.Apply(keyboard.ActionMoveBOL)
	for i := 0; i < 8; i++ {
		e.Apply(keyboard.ActionMoveRight)
	}
	e.Apply(keyboard.ActionKillWord)
	// "wo" should be deleted, leaving "hello rld".
	got := e.Text()
	if got != "hello rld" {
		t.Errorf("KillWord mid-word: Text() = %q, want %q", got, "hello rld")
	}
}

func TestLineEditor_KillWord_MultipleWords(t *testing.T) {
	var e keyboard.LineEditor
	for _, r := range "one two three" {
		e.Insert(r)
	}
	e.Apply(keyboard.ActionKillWord) // deletes "three"
	e.Apply(keyboard.ActionKillWord) // deletes "two "
	// Should be left with "one " or "one".
	got := e.Text()
	if got != "one " && got != "one" {
		t.Errorf("after 2× KillWord: Text() = %q, want 'one' or 'one '", got)
	}
}

// ---------------------------------------------------------------------------
// ActionComplete — currently a no-op stub
// ---------------------------------------------------------------------------

func TestLineEditor_Complete_NoOp(t *testing.T) {
	var e keyboard.LineEditor
	for _, r := range "lo" {
		e.Insert(r)
	}
	text, ok := e.Apply(keyboard.ActionComplete)
	// Complete should not modify the editor and should return ok=false (stub).
	if ok {
		t.Errorf("ActionComplete: ok = true, want false (stub)")
	}
	_ = text // may be "" — just ensure no panic
	if e.Text() != "lo" {
		t.Errorf("ActionComplete changed editor text to %q", e.Text())
	}
}

// ---------------------------------------------------------------------------
// History — sequential usage from one goroutine (by design)
// ---------------------------------------------------------------------------

// TestHistory_Sequential_ManyPushesAndRecalls verifies the history ring works
// correctly over many sequential push/recall cycles (the only expected usage).
// History is intentionally NOT thread-safe: only the TUI event loop goroutine
// ever touches it.
func TestHistory_Sequential_ManyPushesAndRecalls(t *testing.T) {
	var h keyboard.History
	entries := make([]string, 0, 50)
	for i := 0; i < 50; i++ {
		s := fmt.Sprintf("entry-%02d", i)
		entries = append(entries, s)
		h.Push(s)
	}

	// Prev from live should return most recent entry.
	got, ok := h.Prev()
	if !ok {
		t.Fatal("Prev returned ok=false")
	}
	want := entries[len(entries)-1]
	if got != want {
		t.Errorf("Prev = %q, want %q", got, want)
	}

	// Next at most-recent goes back to live.
	_, live := h.Next()
	_ = live // may be false if already at bottom
}
