package tui_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/tui"
)

// simScreen returns a tcell SimulationScreen sized to w×h.
func simScreen(t *testing.T, w, h int) tcell.SimulationScreen {
	t.Helper()
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatalf("SimScreen.Init: %v", err)
	}
	s.SetSize(w, h)
	t.Cleanup(s.Fini)
	return s
}

// screenText reads all content from the simulation screen into a slice of
// row strings, each exactly w runes wide.
func screenText(s tcell.SimulationScreen) []string {
	cells, w, h := s.GetContents()
	rows := make([]string, h)
	for i, cell := range cells {
		row := i / w
		if len(cell.Runes) > 0 {
			rows[row] += string(cell.Runes[0])
		} else {
			rows[row] += " "
		}
	}
	return rows
}

// ---------------------------------------------------------------------------
// App.RunWithScreen — event loop tests
// ---------------------------------------------------------------------------

// runApp starts App.RunWithScreen in a goroutine. Returns the cancel func and
// a channel that receives the return error when RunWithScreen exits.
// A t.Cleanup is registered that cancels and waits for the goroutine to stop
// BEFORE the SimulationScreen's Fini cleanup runs.
func runApp(t *testing.T, b *bus.Bus, s tcell.SimulationScreen) (cancel context.CancelFunc, done <-chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ch := make(chan error, 1)
	app := tui.New(b)
	go func() {
		ch <- app.RunWithScreen(ctx, s)
	}()
	// This cleanup runs BEFORE the simScreen's t.Cleanup(s.Fini), ensuring
	// the app goroutine has fully exited before the screen is torn down.
	t.Cleanup(func() {
		cancel()
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
		}
	})
	return cancel, ch
}

// injectKey posts a synthetic key event into the SimulationScreen's event queue.
func injectKey(s tcell.SimulationScreen, key tcell.Key, r rune) {
	s.InjectKey(key, r, tcell.ModNone)
}

// waitScreen polls until pred(screenText(s)) is true or the deadline passes.
func waitScreen(t *testing.T, s tcell.SimulationScreen, pred func(rows []string) bool, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		s.Show()
		rows := screenText(s)
		if pred(rows) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	s.Show()
	t.Errorf("waitScreen: condition not met within %v\nscreen:\n%v", d, screenText(s))
}

func TestApp_Run_ContextCancel_ExitsCleanly(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, done := runApp(t, b, s)

	// Give the loop time to start.
	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil && err != context.Canceled && err != context.DeadlineExceeded {
			t.Errorf("RunWithScreen returned unexpected error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithScreen did not exit after context cancel")
	}
}

func TestApp_WorldLineEvent_AppearsOnScreen(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, done := runApp(t, b, s)

	time.Sleep(20 * time.Millisecond)
	b.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{
		WorldName: "w",
		Text:      "A dragon roars loudly!",
		Attrs:     bus.LineAttrs{FG: -1, BG: -1},
	}})

	// Give the event loop time to process and render.
	time.Sleep(80 * time.Millisecond)

	// Stop the app so there's no concurrent render during GetContents.
	cancel()
	<-done

	s.Show() // final sync
	rows := screenText(s)
	found := false
	for _, r := range rows {
		if strings.Contains(r, "dragon roars") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("'dragon roars' not found on screen after world line event:\n%v", rows)
	}
}

func TestApp_MultipleWorldLines_AllRendered(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, done := runApp(t, b, s)
	time.Sleep(20 * time.Millisecond)

	lines := []string{"first line", "second line", "third line"}
	for _, l := range lines {
		b.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{
			WorldName: "w", Text: l, Attrs: bus.LineAttrs{FG: -1, BG: -1},
		}})
	}
	time.Sleep(80 * time.Millisecond)

	cancel()
	<-done

	s.Show()
	rows := screenText(s)
	joined := strings.Join(rows, "\n")
	for _, l := range lines {
		if !strings.Contains(joined, l) {
			t.Errorf("line %q not found on screen", l)
		}
	}
}

func TestApp_StatusEvent_UpdatesPromptAndStatusBar(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, done := runApp(t, b, s)
	time.Sleep(20 * time.Millisecond)

	b.Publish(bus.StatusEvent{WorldName: "myworld", Connected: true, LagMS: 0})
	time.Sleep(80 * time.Millisecond)

	cancel()
	<-done

	s.Show()
	rows := screenText(s)
	found := false
	for _, r := range rows {
		if strings.Contains(r, "myworld") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("'myworld' not found on screen after status event:\n%v", rows)
	}
}

func TestApp_CtrlC_ExitsEventLoop(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	_, done := runApp(t, b, s)

	time.Sleep(30 * time.Millisecond)
	injectKey(s, tcell.KeyCtrlC, 0)

	select {
	case err := <-done:
		// Ctrl-C returns nil (graceful exit), not a context error.
		if err != nil && err != context.Canceled {
			t.Errorf("unexpected error on Ctrl-C exit: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithScreen did not exit after Ctrl-C")
	}
}

func TestApp_Keyboard_TypeAndSubmit_PublishesUserInputEvent(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, _ := runApp(t, b, s)
	defer cancel()

	sub := b.Subscribe(8, bus.EvUserInput)
	defer sub.Cancel()

	time.Sleep(20 * time.Millisecond)

	// Type "go north" then Enter.
	for _, r := range "go north" {
		injectKey(s, tcell.KeyRune, r)
	}
	injectKey(s, tcell.KeyEnter, 0)

	select {
	case ev := <-sub.C:
		uie := ev.(bus.UserInputEvent)
		if uie.Text != "go north" {
			t.Errorf("UserInputEvent.Text = %q, want %q", uie.Text, "go north")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for UserInputEvent")
	}
}

func TestApp_Keyboard_SlashCommand_PublishesUserCmdEvent(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, _ := runApp(t, b, s)
	defer cancel()

	sub := b.Subscribe(8, bus.EvUserCmd)
	defer sub.Cancel()

	time.Sleep(20 * time.Millisecond)

	for _, r := range "/connect mud://example.com" {
		injectKey(s, tcell.KeyRune, r)
	}
	injectKey(s, tcell.KeyEnter, 0)

	select {
	case ev := <-sub.C:
		uce := ev.(bus.UserCmdEvent)
		if !strings.HasPrefix(uce.Line, "/connect") {
			t.Errorf("UserCmdEvent.Line = %q, want /connect prefix", uce.Line)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for UserCmdEvent")
	}
}

func TestApp_Keyboard_CtrlA_MovesBOL(t *testing.T) {
	// Type some text, then Ctrl-A — editor cursor should move to BOL.
	// We verify this doesn't panic and the app stays running.
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, done := runApp(t, b, s)
	defer cancel()
	time.Sleep(20 * time.Millisecond)

	for _, r := range "hello" {
		injectKey(s, tcell.KeyRune, r)
	}
	injectKey(s, tcell.KeyCtrlA, 0)

	// App should still be running.
	select {
	case err := <-done:
		t.Fatalf("app exited unexpectedly: %v", err)
	case <-time.After(100 * time.Millisecond):
		// Good — still running.
	}
}

func TestApp_Keyboard_ScrollUp_DoesNotCrash(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, done := runApp(t, b, s)
	defer cancel()
	time.Sleep(20 * time.Millisecond)

	injectKey(s, tcell.KeyPgUp, 0)
	injectKey(s, tcell.KeyPgDn, 0)

	select {
	case err := <-done:
		t.Fatalf("app exited unexpectedly after scroll: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestApp_Resize_ReflowsLayout(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, done := runApp(t, b, s)
	defer cancel()
	time.Sleep(20 * time.Millisecond)

	// Simulate resize by changing sim screen size and posting resize event.
	s.SetSize(40, 12)
	s.PostEventWait(tcell.NewEventResize(40, 12))

	time.Sleep(50 * time.Millisecond)

	select {
	case err := <-done:
		t.Fatalf("app exited after resize: %v", err)
	default:
		// Good — still running.
	}
}

func TestApp_ConcurrentBusEvents_NoRace(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, _ := runApp(t, b, s)
	defer cancel()
	time.Sleep(20 * time.Millisecond)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{
				WorldName: "w",
				Text:      strings.Repeat("X", i%30+1),
				Attrs:     bus.LineAttrs{FG: -1, BG: -1},
			}})
		}()
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond) // let render goroutine process
}

func TestApp_EmptySubmit_DoesNotPublish(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, _ := runApp(t, b, s)
	defer cancel()

	sub := b.Subscribe(4, bus.EvUserInput, bus.EvUserCmd)
	defer sub.Cancel()

	time.Sleep(20 * time.Millisecond)

	// Submit with no text.
	injectKey(s, tcell.KeyEnter, 0)
	// Also submit whitespace only.
	injectKey(s, tcell.KeyRune, ' ')
	injectKey(s, tcell.KeyEnter, 0)

	time.Sleep(80 * time.Millisecond)

	select {
	case ev := <-sub.C:
		t.Errorf("unexpected event for empty submit: %T %+v", ev, ev)
	default:
		// Good — nothing published.
	}
}

func TestApp_HistoryPrevNext_CyclesEntries(t *testing.T) {
	s := simScreen(t, 80, 24)
	b := bus.New()
	cancel, done := runApp(t, b, s)

	sub := b.Subscribe(8, bus.EvUserInput)
	defer sub.Cancel()

	time.Sleep(20 * time.Millisecond)

	// Submit two lines.
	for _, cmd := range []string{"look", "north"} {
		for _, r := range cmd {
			injectKey(s, tcell.KeyRune, r)
		}
		injectKey(s, tcell.KeyEnter, 0)
		time.Sleep(20 * time.Millisecond)
	}

	// Drain the two UserInputEvents.
	for i := 0; i < 2; i++ {
		select {
		case <-sub.C:
		case <-time.After(time.Second):
			t.Fatalf("missing UserInputEvent %d", i)
		}
	}

	// Up arrow should recall "north" (most recent).
	injectKey(s, tcell.KeyUp, 0)
	time.Sleep(80 * time.Millisecond)

	// Stop the app, then inspect the final screen state.
	cancel()
	<-done

	s.Show()
	rows := screenText(s)
	if len(rows) == 0 || !strings.Contains(rows[len(rows)-1], "north") {
		t.Errorf("input row should show 'north' after Up arrow; last row = %q", rows[len(rows)-1])
	}
}

// ---------------------------------------------------------------------------
// OutputPane tests
// ---------------------------------------------------------------------------

func TestNewOutputPane(t *testing.T) {
	pane := tui.NewOutputPane(80, 24)
	if pane == nil {
		t.Fatal("NewOutputPane returned nil")
	}

	if !pane.AtBottom() {
		t.Error("newly created OutputPane should be at the bottom")
	}

	// Verify Draw does not panic on an empty newly created pane
	s := simScreen(t, 80, 24)
	pane.Draw(s, 0)
	s.Show()
}

func TestOutputPane_AppendAndRender(t *testing.T) {
	s := simScreen(t, 40, 10)
	pane := tui.NewOutputPane(40, 8)
	pane.Append(tui.MakeLogicalLine("hello world", bus.LineAttrs{}))
	pane.Draw(s, 0)
	s.Show()

	rows := screenText(s)
	// The last visible row before status/input should contain the text.
	found := false
	for _, row := range rows {
		if strings.Contains(row, "hello world") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("'hello world' not found in rendered output:\n%v", rows)
	}
}

func TestOutputPane_Wrapping_LongLine(t *testing.T) {
	const width = 20
	s := simScreen(t, width, 10)
	pane := tui.NewOutputPane(width, 8)

	// A line longer than the pane width should wrap.
	long := strings.Repeat("A", 45)
	pane.Append(tui.MakeLogicalLine(long, bus.LineAttrs{}))
	pane.Draw(s, 0)
	s.Show()

	rows := screenText(s)
	// Should occupy at least 3 physical rows.
	nonEmpty := 0
	for _, r := range rows {
		if strings.ContainsAny(r, "A") {
			nonEmpty++
		}
	}
	if nonEmpty < 3 {
		t.Errorf("expected ≥3 wrapped rows for 45-char line in width-%d pane, got %d", width, nonEmpty)
	}
}

func TestOutputPane_Scroll_Up_Down(t *testing.T) {
	s := simScreen(t, 40, 5)
	pane := tui.NewOutputPane(40, 3) // 3 visible rows

	for i := 0; i < 10; i++ {
		pane.Append(tui.MakeLogicalLine(strings.Repeat("X", 5), bus.LineAttrs{}))
	}

	// Scroll up — should not panic.
	pane.ScrollUp(3)
	if pane.AtBottom() {
		t.Error("after ScrollUp, AtBottom should be false")
	}

	pane.Draw(s, 0)
	s.Show()

	// Scroll back down.
	pane.ScrollDown(9999)
	if !pane.AtBottom() {
		t.Error("after ScrollDown(9999), AtBottom should be true")
	}
}

func TestOutputPane_GaggedLines_NotRendered(t *testing.T) {
	s := simScreen(t, 40, 10)
	pane := tui.NewOutputPane(40, 8)

	pane.Append(tui.MakeLogicalLine("visible line", bus.LineAttrs{}))
	pane.Append(tui.LogicalLine{
		Spans:  []tui.Span{{Text: "secret gagged line", Attrs: bus.LineAttrs{}}},
		Gagged: true,
	})
	pane.Draw(s, 0)
	s.Show()

	rows := screenText(s)
	for _, row := range rows {
		if strings.Contains(row, "secret") {
			t.Error("gagged line should not appear in rendered output")
		}
	}
}

func TestOutputPane_Resize_DoesNotPanic(t *testing.T) {
	pane := tui.NewOutputPane(80, 24)
	for i := 0; i < 50; i++ {
		pane.Append(tui.MakeLogicalLine("line content", bus.LineAttrs{}))
	}
	// Resize to smaller — should not panic.
	pane.Resize(40, 12)
	pane.Resize(10, 5)
	pane.Resize(80, 24)
}

func TestOutputPane_Empty_DrawDoesNotPanic(t *testing.T) {
	s := simScreen(t, 40, 10)
	pane := tui.NewOutputPane(40, 8)
	pane.Draw(s, 0) // no lines — should not panic
	s.Show()
}

// ---------------------------------------------------------------------------
// StatusBar tests
// ---------------------------------------------------------------------------

func TestNewStatusBar(t *testing.T) {
	sb := tui.NewStatusBar(80)
	if sb == nil {
		t.Fatal("NewStatusBar returned nil")
	}
	s := simScreen(t, 80, 5)
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	// Status bar draws on rows y=1, y=2, y=3
	// So rows[2] is the middle content line.
	if !strings.Contains(rows[2], "GoFugue") {
		t.Errorf("expected default field 'GoFugue' to be drawn, got: %q", rows[2])
	}
}

func TestStatusBar_SetStyle(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)

	style := tcell.StyleDefault.Background(tcell.ColorRed).Foreground(tcell.ColorWhite)
	sb.SetStyle(style)

	sb.Draw(s, 1)
	s.Show()

	// Content row is drawn at y=2, borders at y=1 and y=3.
	_, _, gotStyle, _ := s.GetContent(0, 1)
	if gotStyle != style {
		t.Errorf("expected border style %v, got %v", style, gotStyle)
	}
	_, _, gotStyleContent, _ := s.GetContent(0, 2)
	if gotStyleContent != style {
		t.Errorf("expected content style %v, got %v", style, gotStyleContent)
	}
}

func TestStatusBar_ShowsWorldName(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	sb.Update("mymud", true, 42)
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	if !strings.Contains(rows[2], "mymud") {
		t.Errorf("status bar should contain world name 'mymud': %q", rows[2])
	}
}

func TestStatusBar_ShowsConnected(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	sb.Update("w", true, 0)
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	if !strings.Contains(rows[2], "CONNECTED") {
		t.Errorf("status bar should show CONNECTED: %q", rows[2])
	}
}

func TestStatusBar_NoWorld_ShowsDash(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	sb.Update("", false, 0)
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	if !strings.Contains(rows[2], "no world") {
		t.Errorf("status bar should show '(no world)': %q", rows[2])
	}
}

func TestStatusBar_Resize_DoesNotPanic(t *testing.T) {
	sb := tui.NewStatusBar(80)
	sb.Resize(40)
	sb.Resize(200)
}

// ---------------------------------------------------------------------------
// InputBar tests
// ---------------------------------------------------------------------------

func TestInputBar_ShowsText(t *testing.T) {
	s := simScreen(t, 40, 3)
	ib := tui.NewInputBar(40)
	for _, r := range "go north" {
		ib.Editor().Insert(r)
	}
	ib.Draw(s, 2)
	s.Show()

	rows := screenText(s)
	if !strings.Contains(rows[2], "go north") {
		t.Errorf("input bar should display 'go north': %q", rows[2])
	}
}

func TestInputBar_SetPrompt_Shown(t *testing.T) {
	s := simScreen(t, 60, 3)
	ib := tui.NewInputBar(60)
	ib.SetPrompt("[mymud] ")
	ib.Draw(s, 2)
	s.Show()

	rows := screenText(s)
	if !strings.Contains(rows[2], "mymud") {
		t.Errorf("input bar should display prompt with world name: %q", rows[2])
	}
}

func TestInputBar_Resize_DoesNotPanic(t *testing.T) {
	ib := tui.NewInputBar(80)
	ib.Resize(40)
	ib.Resize(200)
}

// ---------------------------------------------------------------------------
// attrToStyle / style conversion tests
// ---------------------------------------------------------------------------

func TestAttrToStyle_Bold(t *testing.T) {
	style := tui.AttrToStyle(bus.LineAttrs{FG: -1, BG: -1, Bold: true})
	_, _, attrs := style.Decompose()
	if attrs&tcell.AttrBold == 0 {
		t.Error("expected Bold attribute in style")
	}
}

func TestAttrToStyle_Foreground(t *testing.T) {
	style := tui.AttrToStyle(bus.LineAttrs{FG: 31, BG: -1})
	fg, _, _ := style.Decompose()
	if fg != tcell.PaletteColor(31) {
		t.Errorf("FG = %v, want palette color 31", fg)
	}
}

func TestAttrToStyle_Background(t *testing.T) {
	style := tui.AttrToStyle(bus.LineAttrs{FG: -1, BG: 44})
	_, bg, _ := style.Decompose()
	if bg != tcell.PaletteColor(44) {
		t.Errorf("BG = %v, want palette color 44", bg)
	}
}

func TestAttrToStyle_DefaultColors(t *testing.T) {
	style := tui.AttrToStyle(bus.LineAttrs{FG: -1, BG: -1})
	fg, bg, _ := style.Decompose()
	if fg != tcell.ColorDefault {
		t.Errorf("default FG: got %v, want ColorDefault", fg)
	}
	if bg != tcell.ColorDefault {
		t.Errorf("default BG: got %v, want ColorDefault", bg)
	}
}

// ---------------------------------------------------------------------------
// Word-wrap at word boundary
// ---------------------------------------------------------------------------

func TestOutputPane_WordWrap_SplitsAtSpace(t *testing.T) {
	const width = 10
	s := simScreen(t, width, 10)
	pane := tui.NewOutputPane(width, 8)

	// "hello world" is 11 chars — must wrap after "hello" (5 chars + space),
	// not mid-word.
	pane.Append(tui.MakeLogicalLine("hello world", bus.LineAttrs{}))
	pane.Draw(s, 0)
	s.Show()

	rows := screenText(s)
	// Find the row containing "hello" — it must NOT contain "world" on same row.
	// And a separate row must contain "world".
	helloRow := -1
	worldRow := -1
	for i, r := range rows {
		if strings.Contains(r, "hello") {
			helloRow = i
		}
		if strings.Contains(r, "world") {
			worldRow = i
		}
	}
	if helloRow == -1 {
		t.Fatal("'hello' not found in output")
	}
	if worldRow == -1 {
		t.Fatal("'world' not found in output")
	}
	if helloRow == worldRow {
		t.Errorf("'hello' and 'world' on same row %d — expected word-boundary wrap", helloRow)
	}
}

func TestOutputPane_WordWrap_NoMidWordBreak(t *testing.T) {
	const width = 8
	s := simScreen(t, width, 10)
	pane := tui.NewOutputPane(width, 8)

	// "abcdefg hijklmn" — both words are 7 chars, width is 8.
	// "abcdefg" fits (7 < 8), "hijklmn" wraps to next row.
	pane.Append(tui.MakeLogicalLine("abcdefg hijklmn", bus.LineAttrs{}))
	pane.Draw(s, 0)
	s.Show()

	rows := screenText(s)
	joined := strings.Join(rows, "|")
	// "abcdefg" should appear whole on one row.
	if !strings.Contains(joined, "abcdefg") {
		t.Errorf("'abcdefg' not found in output: %q", joined)
	}
	// "hijklmn" should appear on a different row.
	for i, r := range rows {
		if strings.Contains(r, "abcdefg") && strings.Contains(r, "hijklmn") {
			t.Errorf("row %d contains both words — expected wrap: %q", i, r)
		}
	}
}

func TestOutputPane_WordWrap_HardWrapFallback(t *testing.T) {
	const width = 5
	s := simScreen(t, width, 10)
	pane := tui.NewOutputPane(width, 8)

	// A word longer than the pane — must hard-wrap (no space to break at).
	pane.Append(tui.MakeLogicalLine("ABCDEFGHIJ", bus.LineAttrs{}))
	pane.Draw(s, 0)
	s.Show()

	rows := screenText(s)
	// All chars must appear, spread across at least 2 rows.
	allChars := ""
	for _, r := range rows {
		allChars += strings.TrimRight(r, " ")
	}
	for _, ch := range "ABCDEFGHIJ" {
		if !strings.ContainsRune(allChars, ch) {
			t.Errorf("char %q missing from output after hard-wrap", ch)
		}
	}
}

func TestOutputPane_WordWrap_NewlineBreaksRow(t *testing.T) {
	const width = 40
	s := simScreen(t, width, 10)
	pane := tui.NewOutputPane(width, 8)

	// Embedded newline must produce a row break regardless of width.
	pane.Append(tui.MakeLogicalLine("line one\nline two", bus.LineAttrs{}))
	pane.Draw(s, 0)
	s.Show()

	rows := screenText(s)
	oneRow, twoRow := -1, -1
	for i, r := range rows {
		if strings.Contains(r, "line one") {
			oneRow = i
		}
		if strings.Contains(r, "line two") {
			twoRow = i
		}
	}
	if oneRow == -1 || twoRow == -1 {
		t.Fatalf("expected both rows; oneRow=%d twoRow=%d, screen:\n%v", oneRow, twoRow, rows)
	}
	if oneRow == twoRow {
		t.Errorf("newline did not produce row break: both on row %d", oneRow)
	}
}

// ---------------------------------------------------------------------------
// StatusBar.AddField / RemoveField / SetMore
// ---------------------------------------------------------------------------

func TestStatusBar_AddField_AppearsInRender(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	sb.AddField("hp", func() string { return "HP:99" })
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	if !strings.Contains(rows[2], "HP:99") {
		t.Errorf("custom field 'HP:99' not found in status bar: %q", rows[2])
	}
}

func TestStatusBar_AddField_UpdatesExisting(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	sb.AddField("hp", func() string { return "HP:50" })
	sb.AddField("hp", func() string { return "HP:100" }) // replace
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	if strings.Contains(rows[2], "HP:50") {
		t.Errorf("old field value should be replaced; still shows HP:50: %q", rows[2])
	}
	if !strings.Contains(rows[2], "HP:100") {
		t.Errorf("updated field 'HP:100' not found: %q", rows[2])
	}
}

func TestStatusBar_RemoveField_DisappearsFromRender(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	sb.AddField("hp", func() string { return "HP:42" })
	sb.RemoveField("hp")
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	if strings.Contains(rows[2], "HP:42") {
		t.Errorf("removed field should not appear: %q", rows[2])
	}
}

func TestStatusBar_RemoveField_Nonexistent_IsNoop(t *testing.T) {
	sb := tui.NewStatusBar(80)
	// Must not panic.
	sb.RemoveField("doesnotexist")
}

func TestStatusBar_SetMore_ShowsMoreIndicator(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	sb.Update("mud", true, 0)
	sb.SetMore(true)
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	if !strings.Contains(rows[2], "MORE") {
		t.Errorf("SetMore(true) should show [MORE] indicator: %q", rows[2])
	}
}

func TestStatusBar_SetMore_False_HidesIndicator(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	sb.Update("mud", true, 0)
	sb.SetMore(true)
	sb.SetMore(false)
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	if strings.Contains(rows[2], "MORE") {
		t.Errorf("SetMore(false) should hide [MORE] indicator: %q", rows[2])
	}
}

func TestStatusBar_AddField_EmptyValue_NotShown(t *testing.T) {
	s := simScreen(t, 80, 5)
	sb := tui.NewStatusBar(80)
	// Field returns empty string — should not leave an extra separator.
	sb.AddField("empty", func() string { return "" })
	sb.Draw(s, 1)
	s.Show()

	rows := screenText(s)
	// Should still render without panic; world name should still be present.
	if !strings.Contains(rows[2], "GoFugue") {
		t.Errorf("status bar broken after empty field: %q", rows[2])
	}
}
