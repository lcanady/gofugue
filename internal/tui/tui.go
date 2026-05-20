// Package tui implements the terminal user interface using tcell/v2.
//
// Layout (rows, top-to-bottom):
//
//	┌─────────────────────────────┐
//	│  OutputPane  (scrollable)   │  rows 0 .. height-5
//	├─────────────────────────────┤  ← ─ border  (height-4)
//	│  StatusBar                  │  row height-3
//	├─────────────────────────────┤  ← ─ border  (height-2)
//	│  InputBar   (readline)      │  row height-1
//	└─────────────────────────────┘
//
// The TUI subscribes to the event bus for incoming world lines and status
// updates. User keystrokes are translated to keyboard.Actions, submitted
// lines are published back onto the bus as UserInputEvent / UserCmdEvent.
package tui

import (
	"context"
	"log/slog"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/keyboard"
)

// App is the top-level TUI application.
type App struct {
	bus      *bus.Bus
	screen   tcell.Screen
	bindings []keyboard.Binding

	output *OutputPane
	status *StatusBar
	input  *InputBar
	hist   keyboard.History

	// foregroundWorld is the name of the currently active world,
	// used for routing user input.
	foregroundWorld string

	// themeStatus/themeInput are applied to the bars after initLayout.
	themeStatus tcell.Style
	themeInput  tcell.Style

	// onResize is called with (width, height) when the terminal resizes.
	// Used to propagate NAWS updates to connected sessions.
	onResize func(w, h int)

	// onReady is called once in a goroutine after the screen is initialised
	// and the first draw is complete. Use it for deferred startup work
	// (e.g. auto-connect) that needs the TUI to be live first.
	onReady func()

	// completionSource returns candidates given a prefix for tab completion.
	completionSource func(prefix string) []string
}

// New creates a TUI App backed by the given event bus.
func New(b *bus.Bus) *App {
	return &App{
		bus:         b,
		bindings:    keyboard.DefaultBindings(),
		themeStatus: tcell.StyleDefault,
		themeInput:  tcell.StyleDefault,
	}
}

// SetResizeCallback registers a function to be called when the terminal resizes.
func (a *App) SetResizeCallback(fn func(w, h int)) {
	a.onResize = fn
}

// SetCompletionSource registers a function that returns tab-completion candidates
// for the given prefix. The TUI calls it on Tab key press.
func (a *App) SetCompletionSource(fn func(prefix string) []string) {
	a.completionSource = fn
}

// SetOnReady registers a function to call once after the screen is initialised
// and the first frame is drawn. It runs in its own goroutine so it must not
// call any TUI methods directly; communicate back via the event bus instead.
func (a *App) SetOnReady(fn func()) {
	a.onReady = fn
}

// SetTheme applies colour settings from the config to the status and input bars.
// Call before Run.
func (a *App) SetTheme(statusStyle, inputStyle tcell.Style) {
	if a.status != nil {
		a.status.SetStyle(statusStyle)
	}
	if a.input != nil {
		a.input.SetStyle(inputStyle)
	}
	// Store for use after initLayout (which creates the bars).
	a.themeStatus = statusStyle
	a.themeInput = inputStyle
}

// Run initialises the terminal, starts the event loop, and blocks until ctx
// is cancelled or the user quits. Safe to call from main().
func (a *App) Run(ctx context.Context) error {
	screen, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := screen.Init(); err != nil {
		return err
	}
	defer screen.Fini()

	screen.EnableMouse()
	screen.SetStyle(tcell.StyleDefault)
	screen.Clear()

	return a.RunWithScreen(ctx, screen)
}

// RunWithScreen runs the event loop using the provided (possibly simulated)
// screen. The screen must already be initialised. Used in tests.
func (a *App) RunWithScreen(ctx context.Context, screen tcell.Screen) error {
	a.screen = screen
	w, h := screen.Size()
	a.initLayout(w, h)

	// Subscribe to bus events from other goroutines.
	// Use EvWorldLineRendered so gag/hilite/substitute are already applied.
	worldSub := a.bus.Subscribe(256, bus.EvWorldLineRendered)
	statusSub := a.bus.Subscribe(32, bus.EvStatus)
	defer worldSub.Cancel()
	defer statusSub.Cancel()

	a.draw()

	if a.onReady != nil {
		go a.onReady()
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case ev := <-worldSub.C:
			wl := ev.(bus.WorldRenderedEvent)
			a.output.Append(fromWorldLine(wl.WorldLineEvent))
			a.draw()

		case ev := <-statusSub.C:
			st := ev.(bus.StatusEvent)
			a.status.Update(st.WorldName, st.Connected, st.LagMS)
			if st.WorldName != "" {
				a.foregroundWorld = st.WorldName
			}
			a.draw()

		default:
			// Poll tcell for terminal events with a short timeout so the
			// select above stays responsive.
			if !screen.HasPendingEvent() {
				continue
			}
			tev := screen.PollEvent()
			if tev == nil {
				continue
			}
			if done := a.handleTcellEvent(tev); done {
				return nil
			}
			a.draw()
		}
	}
}

// initLayout creates the three panes based on current terminal size.
func (a *App) initLayout(w, h int) {
	outH := h - 4 // leave 3 rows for status bar (borders + content), 1 for input
	if outH < 1 {
		outH = 1
	}
	a.output = newOutputPane(w, outH)
	a.status = newStatusBar(w)
	a.input = newInputBar(w)
	a.status.SetStyle(a.themeStatus)
	a.input.SetStyle(a.themeInput)
}

// draw renders the full screen.
func (a *App) draw() {
	if a.screen == nil {
		return
	}
	_, h := a.screen.Size()
	outH := h - 4
	if outH < 1 {
		outH = 1
	}

	a.output.Draw(a.screen, 0)
	a.status.SetMore(!a.output.AtBottom())
	a.status.Draw(a.screen, outH)
	a.input.Draw(a.screen, h-1)
	a.screen.Show()
}

// handleTcellEvent processes one tcell event. Returns true if the app should exit.
func (a *App) handleTcellEvent(tev tcell.Event) bool {
	switch ev := tev.(type) {
	case *tcell.EventResize:
		w, h := ev.Size()
		a.screen.Clear()
		a.output.Resize(w, h-4)
		a.status.Resize(w)
		a.input.Resize(w)
		a.screen.Sync()
		if a.onResize != nil {
			a.onResize(w, h)
		}
		a.bus.Publish(bus.HookEvent{WorldName: a.foregroundWorld, Name: "RESIZE"})

	case *tcell.EventKey:
		return a.handleKey(ev)

	case *tcell.EventMouse:
		switch ev.Buttons() {
		case tcell.WheelUp:
			a.output.ScrollUp(3)
		case tcell.WheelDown:
			a.output.ScrollDown(3)
		}
	}
	return false
}

// handleKey maps a tcell key event to a keyboard.Action and applies it.
func (a *App) handleKey(ev *tcell.EventKey) bool {
	action := a.resolveAction(ev)

	switch action {
	case keyboard.ActionScrollUp:
		a.output.ScrollUp(5)
		return false
	case keyboard.ActionScrollDown:
		a.output.ScrollDown(5)
		return false
	case keyboard.ActionHistoryPrev:
		if s, ok := a.hist.Prev(); ok {
			replaceEditorText(a.input.Editor(), s)
		}
		return false
	case keyboard.ActionHistoryNext:
		s, _ := a.hist.Next()
		replaceEditorText(a.input.Editor(), s)
		return false
	case keyboard.ActionComplete:
		a.handleComplete()
		return false
	case keyboard.ActionSubmit:
		text, ok := a.input.Editor().Apply(keyboard.ActionSubmit)
		if !ok || strings.TrimSpace(text) == "" {
			return false
		}
		a.hist.Push(text)
		// Scroll back to live view on submit.
		for !a.output.AtBottom() {
			a.output.ScrollDown(9999)
		}
		if strings.HasPrefix(text, "/") {
			a.bus.Publish(bus.UserCmdEvent{WorldName: a.foregroundWorld, Line: text})
		} else {
			a.bus.Publish(bus.UserInputEvent{WorldName: a.foregroundWorld, Text: text})
		}
		return false
	}

	// For quit (Ctrl-C / Ctrl-Q) return true to exit.
	if ev.Key() == tcell.KeyCtrlC || ev.Key() == tcell.KeyCtrlQ {
		return true
	}

	// Pass printable runes and editing keys to the line editor.
	if action != "" {
		a.input.Editor().Apply(action)
	} else if ev.Key() == tcell.KeyRune {
		a.input.Editor().Insert(ev.Rune())
	}
	return false
}

// resolveAction maps a tcell key event to a keyboard.Action using the
// binding table.
func (a *App) resolveAction(ev *tcell.EventKey) keyboard.Action {
	keyName := tcellKeyName(ev)
	for _, b := range a.bindings {
		if b.Key == keyName {
			return b.Action
		}
	}
	return ""
}

// tcellKeyName converts a tcell key event to the canonical key name used in
// the binding table (e.g. "ctrl-a", "up", "enter").
func tcellKeyName(ev *tcell.EventKey) string {
	switch ev.Key() {
	case tcell.KeyEnter:
		return "enter"
	case tcell.KeyUp:
		return "up"
	case tcell.KeyDown:
		return "down"
	case tcell.KeyLeft:
		return "left"
	case tcell.KeyRight:
		return "right"
	case tcell.KeyPgUp:
		return "pgup"
	case tcell.KeyPgDn:
		return "pgdn"
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		return "backspace"
	case tcell.KeyDelete:
		return "delete"
	case tcell.KeyTab:
		return "tab"
	case tcell.KeyCtrlA:
		return "ctrl-a"
	case tcell.KeyCtrlE:
		return "ctrl-e"
	case tcell.KeyCtrlK:
		return "ctrl-k"
	case tcell.KeyCtrlW:
		return "ctrl-w"
	default:
		return ""
	}
}

// handleComplete implements Tab completion.
// If the input starts with '/', complete against command names.
// Otherwise, complete against macro names and world names (via completionSource).
func (a *App) handleComplete() {
	ed := a.input.Editor()
	text := ed.Text()
	if text == "" {
		return
	}

	var candidates []string
	if a.completionSource != nil {
		candidates = a.completionSource(text)
	}
	if len(candidates) == 0 {
		return
	}
	if len(candidates) == 1 {
		// Unique match — complete in place.
		replaceEditorText(ed, candidates[0])
		return
	}
	// Multiple matches — show them in output.
	a.output.Append(LogicalLine{Spans: []Span{{
		Text:  strings.Join(candidates, "  "),
		Attrs: bus.LineAttrs{FG: 6, BG: -1}, // cyan
	}}})
}

// replaceEditorText clears the editor and sets it to text.
func replaceEditorText(e *keyboard.LineEditor, text string) {
	e.Apply(keyboard.ActionMoveBOL)
	e.Apply(keyboard.ActionKillLine)
	for _, r := range text {
		e.Insert(r)
	}
}

// Echo appends a local (non-world) line to the output pane, styled in dim white.
// Used for /echo output, system messages, and script feedback.
func (a *App) Echo(text string) {
	if a == nil {
		slog.Info(text) // headless fallback
		return
	}
	ll := LogicalLine{Spans: []Span{{
		Text:  text,
		Attrs: bus.LineAttrs{FG: 7, BG: -1}, // white on default
	}}}
	a.output.Append(ll)
	a.draw()
}
