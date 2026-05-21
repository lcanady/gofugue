package cmd_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/cmd"
	"github.com/kumakun/gofugue/internal/macro"
)

// makeCtx builds a minimal Context suitable for most tests.
func makeCtx(output *[]string, sends *[]string) *cmd.Context {
	return &cmd.Context{
		Ctx:   context.Background(),
		World: "testworld",
		Output: func(text string) {
			if output != nil {
				*output = append(*output, text)
			}
		},
		Send: func(world, text string) error {
			if sends != nil {
				*sends = append(*sends, world+":"+text)
			}
			return nil
		},
		Setvar: func(_, _ string) {},
		Getvar: func(_ string) (string, bool) { return "", false },
	}
}

// ---------------------------------------------------------------------------
// Core dispatcher
// ---------------------------------------------------------------------------

func TestNew(t *testing.T) {
	d := cmd.New()
	if d == nil {
		t.Fatal("expected New() to return a non-nil Dispatcher")
	}

	names := d.CommandNames()
	if len(names) == 0 {
		t.Error("expected Dispatcher to have registered builtins")
	}

	required := []string{"connect", "echo", "help", "quit"}
	nameSet := make(map[string]bool)
	for _, n := range names {
		nameSet[n] = true
	}

	for _, req := range required {
		if !nameSet[req] {
			t.Errorf("expected builtin %q to be registered", req)
		}
	}
}

func TestDispatcher_NotACommand_ReturnsError(t *testing.T) {
	d := cmd.New()
	err := d.Dispatch(&cmd.Context{Output: func(string) {}}, "not a command")
	if err == nil {
		t.Fatal("expected error for non-command input")
	}
}

func TestDispatcher_UnknownCommand_ReturnsError(t *testing.T) {
	d := cmd.New()
	err := d.Dispatch(&cmd.Context{Output: func(string) {}}, "/xyzzy")
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !strings.Contains(err.Error(), "xyzzy") {
		t.Errorf("error should mention command name, got: %v", err)
	}
}

func TestDispatcher_CaseInsensitive(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)

	for _, variant := range []string{"/ECHO hello", "/Echo hello", "/echo hello"} {
		output = output[:0]
		if err := d.Dispatch(ctx, variant); err != nil {
			t.Errorf("%q: unexpected error: %v", variant, err)
		}
		if len(output) == 0 {
			t.Errorf("%q: expected output", variant)
		}
	}
}

func TestDispatcher_Register_AddsCommand(t *testing.T) {
	d := cmd.New()
	var called bool
	d.Register("testcmd", func(_ *cmd.Context, _ string) error {
		called = true
		return nil
	})
	if err := d.Dispatch(&cmd.Context{Output: func(string) {}}, "/testcmd"); err != nil {
		t.Fatalf("/testcmd: %v", err)
	}
	if !called {
		t.Error("registered handler was not called")
	}
}

func TestDispatcher_Register_Override_Builtin(t *testing.T) {
	d := cmd.New()
	var override bool
	d.Register("echo", func(_ *cmd.Context, _ string) error {
		override = true
		return nil
	})
	d.Dispatch(&cmd.Context{Output: func(string) {}}, "/echo test") //nolint:errcheck
	if !override {
		t.Error("plugin should be able to override builtin command")
	}
}

// ---------------------------------------------------------------------------
// /echo
// ---------------------------------------------------------------------------

func TestDispatcher_Echo_WritesToOutput(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)

	if err := d.Dispatch(ctx, "/echo hello world"); err != nil {
		t.Fatalf("/echo: %v", err)
	}
	if len(output) != 1 || output[0] != "hello world" {
		t.Errorf("output = %v, want [%q]", output, "hello world")
	}
}

func TestDispatcher_Echo_EmptyArgs(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)
	if err := d.Dispatch(ctx, "/echo"); err != nil {
		t.Fatalf("/echo (no args): %v", err)
	}
	// Should not crash; output may be empty string or nothing.
}

// ---------------------------------------------------------------------------
// /send
// ---------------------------------------------------------------------------

func TestDispatcher_Send_SendsToWorld(t *testing.T) {
	d := cmd.New()
	var sends []string
	ctx := makeCtx(nil, &sends)

	if err := d.Dispatch(ctx, "/send go north"); err != nil {
		t.Fatalf("/send: %v", err)
	}
	if len(sends) != 1 || sends[0] != "testworld:go north" {
		t.Errorf("sends = %v, want [%q]", sends, "testworld:go north")
	}
}

func TestDispatcher_Send_NilCallback_ReturnsError(t *testing.T) {
	d := cmd.New()
	ctx := &cmd.Context{World: "w", Output: func(string) {}}
	err := d.Dispatch(ctx, "/send hello")
	if err == nil {
		t.Fatal("expected error when Send callback is nil")
	}
}

// ---------------------------------------------------------------------------
// /connect
// ---------------------------------------------------------------------------

func TestDispatcher_Connect_CallsCallback(t *testing.T) {
	d := cmd.New()
	var mu sync.Mutex
	var connectName, connectURL string
	done := make(chan struct{})
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.Connect = func(name, url string) error {
		mu.Lock()
		connectName = name
		connectURL = url
		mu.Unlock()
		close(done)
		return nil
	}

	if err := d.Dispatch(ctx, "/connect mud://example.com:4000 myworld"); err != nil {
		t.Fatalf("/connect: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Connect callback never fired")
	}
	mu.Lock()
	defer mu.Unlock()
	if connectName != "myworld" {
		t.Errorf("connect name = %q, want %q", connectName, "myworld")
	}
	if connectURL != "mud://example.com:4000" {
		t.Errorf("connect url = %q, want %q", connectURL, "mud://example.com:4000")
	}
}

func TestDispatcher_Connect_AutoName(t *testing.T) {
	d := cmd.New()
	var mu sync.Mutex
	var connectName string
	done := make(chan struct{})
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.Connect = func(name, _ string) error {
		mu.Lock()
		connectName = name
		mu.Unlock()
		close(done)
		return nil
	}

	if err := d.Dispatch(ctx, "/connect mud://example.com:4000"); err != nil {
		t.Fatalf("/connect: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Connect callback never fired")
	}
	mu.Lock()
	defer mu.Unlock()
	if connectName != "example.com" {
		t.Errorf("auto-derived name = %q, want %q", connectName, "example.com")
	}
}

func TestDispatcher_Connect_NoArgs_ReturnsError(t *testing.T) {
	d := cmd.New()
	ctx := makeCtx(nil, nil)
	ctx.Connect = func(_, _ string) error { return nil }
	err := d.Dispatch(ctx, "/connect")
	if err == nil {
		t.Fatal("expected error for /connect with no args")
	}
}

func TestDispatcher_Connect_PropagatesError(t *testing.T) {
	d := cmd.New()
	var output []string
	var mu sync.Mutex
	done := make(chan struct{})
	ctx := &cmd.Context{
		Ctx: context.Background(),
		Output: func(s string) {
			mu.Lock()
			output = append(output, s)
			if strings.Contains(s, "Connection error") {
				select {
				case <-done:
				default:
					close(done)
				}
			}
			mu.Unlock()
		},
	}
	ctx.Connect = func(_, _ string) error { return fmt.Errorf("refused") }
	if err := d.Dispatch(ctx, "/connect mud://x.com myworld"); err != nil {
		t.Fatalf("/connect dispatch should not return sync error: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("expected Connection error output, never received")
	}
	mu.Lock()
	defer mu.Unlock()
	found := false
	for _, line := range output {
		if strings.Contains(line, "refused") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected output containing 'refused', got: %v", output)
	}
}

// ---------------------------------------------------------------------------
// /dc
// ---------------------------------------------------------------------------

func TestDispatcher_DC_CallsDisconnect(t *testing.T) {
	d := cmd.New()
	var disconnected string
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.Disconnect = func(name string) error { disconnected = name; return nil }

	if err := d.Dispatch(ctx, "/dc myworld"); err != nil {
		t.Fatalf("/dc: %v", err)
	}
	if disconnected != "myworld" {
		t.Errorf("disconnected = %q, want %q", disconnected, "myworld")
	}
}

func TestDispatcher_DC_NoArgs_UsesForeground(t *testing.T) {
	d := cmd.New()
	var disconnected string
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.ForegroundWorld = func() string { return "fg_world" }
	ctx.Disconnect = func(name string) error { disconnected = name; return nil }

	if err := d.Dispatch(ctx, "/dc"); err != nil {
		t.Fatalf("/dc: %v", err)
	}
	if disconnected != "fg_world" {
		t.Errorf("disconnected = %q, want %q", disconnected, "fg_world")
	}
}

// ---------------------------------------------------------------------------
// /sw
// ---------------------------------------------------------------------------

func TestDispatcher_SW_CallsSwitch(t *testing.T) {
	d := cmd.New()
	var switched string
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.Switch = func(name string) error { switched = name; return nil }

	if err := d.Dispatch(ctx, "/sw otherworld"); err != nil {
		t.Fatalf("/sw: %v", err)
	}
	if switched != "otherworld" {
		t.Errorf("switched = %q, want %q", switched, "otherworld")
	}
}

func TestDispatcher_SW_NoArgs_ReturnsError(t *testing.T) {
	d := cmd.New()
	ctx := makeCtx(nil, nil)
	ctx.Switch = func(_ string) error { return nil }
	if err := d.Dispatch(ctx, "/sw"); err == nil {
		t.Fatal("expected error for /sw with no args")
	}
}

// ---------------------------------------------------------------------------
// /def, /undef, /list
// ---------------------------------------------------------------------------

func TestDispatcher_Def_SimpleBody(t *testing.T) {
	d := cmd.New()
	var defined *macro.Macro
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.DefMacro = func(m *macro.Macro) error { defined = m; return nil }

	if err := d.Dispatch(ctx, "/def myalert=you got a tell"); err != nil {
		t.Fatalf("/def: %v", err)
	}
	if defined == nil || defined.Name != "myalert" {
		t.Fatalf("macro not defined: %v", defined)
	}
	if defined.Body != "you got a tell" {
		t.Errorf("body = %q, want %q", defined.Body, "you got a tell")
	}
}

func TestDispatcher_Def_WithPattern(t *testing.T) {
	d := cmd.New()
	var defined *macro.Macro
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.DefMacro = func(m *macro.Macro) error { defined = m; return nil }

	if err := d.Dispatch(ctx, `/def -t "dragon" hitdragon=/send kill dragon`); err != nil {
		t.Fatalf("/def -t: %v", err)
	}
	if defined.Pattern != "dragon" {
		t.Errorf("pattern = %q, want %q", defined.Pattern, "dragon")
	}
	if defined.Name != "hitdragon" {
		t.Errorf("name = %q, want %q", defined.Name, "hitdragon")
	}
}

func TestDispatcher_Def_WithPriority(t *testing.T) {
	d := cmd.New()
	var defined *macro.Macro
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.DefMacro = func(m *macro.Macro) error { defined = m; return nil }

	if err := d.Dispatch(ctx, "/def -t dragon -p 100 mydef=/send kill dragon"); err != nil {
		t.Fatalf("/def -p: %v", err)
	}
	if defined.Priority != 100 {
		t.Errorf("priority = %d, want 100", defined.Priority)
	}
}

func TestDispatcher_Def_BadPattern_ReturnsError(t *testing.T) {
	d := cmd.New()
	eng := macro.New()
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.DefMacro = eng.Define

	// Invalid regex.
	err := d.Dispatch(ctx, `/def -t "[invalid" bad=body`)
	if err == nil {
		t.Fatal("expected error for invalid regex pattern")
	}
}

func TestDispatcher_Undef_Removes(t *testing.T) {
	d := cmd.New()
	removed := ""
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.UndefMacro = func(name string) bool { removed = name; return true }

	if err := d.Dispatch(ctx, "/undef myalert"); err != nil {
		t.Fatalf("/undef: %v", err)
	}
	if removed != "myalert" {
		t.Errorf("removed = %q, want %q", removed, "myalert")
	}
}

func TestDispatcher_Undef_NotFound_ReturnsError(t *testing.T) {
	d := cmd.New()
	ctx := makeCtx(nil, nil)
	ctx.UndefMacro = func(_ string) bool { return false }
	err := d.Dispatch(ctx, "/undef nosuch")
	if err == nil {
		t.Fatal("expected error when macro not found")
	}
}

func TestDispatcher_List_Empty(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.ListMacros = func() []string { return nil }

	if err := d.Dispatch(ctx, "/list"); err != nil {
		t.Fatalf("/list: %v", err)
	}
	if len(output) == 0 || !strings.Contains(output[0], "No macros") {
		t.Errorf("expected 'No macros' message, got: %v", output)
	}
}

func TestDispatcher_List_ShowsMacros(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.ListMacros = func() []string { return []string{"aaa", "bbb"} }

	if err := d.Dispatch(ctx, "/list"); err != nil {
		t.Fatalf("/list: %v", err)
	}
	if len(output) != 2 {
		t.Errorf("expected 2 macro lines, got %d: %v", len(output), output)
	}
}

// ---------------------------------------------------------------------------
// /set, /unset
// ---------------------------------------------------------------------------

func TestDispatcher_Set_CallsSetvar(t *testing.T) {
	d := cmd.New()
	var setName, setVal string
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.Setvar = func(name, val string) { setName = name; setVal = val }

	if err := d.Dispatch(ctx, "/set myvar=hello"); err != nil {
		t.Fatalf("/set: %v", err)
	}
	if setName != "myvar" || setVal != "hello" {
		t.Errorf("set name=%q val=%q, want myvar/hello", setName, setVal)
	}
}

func TestDispatcher_Unset_ClearsVar(t *testing.T) {
	d := cmd.New()
	var setName, setVal string
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.Setvar = func(name, val string) { setName = name; setVal = val }

	if err := d.Dispatch(ctx, "/unset myvar"); err != nil {
		t.Fatalf("/unset: %v", err)
	}
	if setName != "myvar" || setVal != "" {
		t.Errorf("unset name=%q val=%q, want myvar/empty", setName, setVal)
	}
}

// ---------------------------------------------------------------------------
// /recall
// ---------------------------------------------------------------------------

func TestDispatcher_Recall_ReturnsResults(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.SearchHistory = func(_ string) ([]string, error) {
		return []string{"line one", "line two"}, nil
	}

	if err := d.Dispatch(ctx, "/recall dragon"); err != nil {
		t.Fatalf("/recall: %v", err)
	}
	if len(output) != 2 {
		t.Errorf("expected 2 lines, got %d: %v", len(output), output)
	}
}

func TestDispatcher_Recall_Empty(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.SearchHistory = func(_ string) ([]string, error) { return nil, nil }

	if err := d.Dispatch(ctx, "/recall nothing"); err != nil {
		t.Fatalf("/recall: %v", err)
	}
	if len(output) == 0 || !strings.Contains(output[0], "nothing found") {
		t.Errorf("expected 'nothing found', got: %v", output)
	}
}

// ---------------------------------------------------------------------------
// /log
// ---------------------------------------------------------------------------

func TestDispatcher_Log_StartsLog(t *testing.T) {
	d := cmd.New()
	var logPath string
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.StartLog = func(path string) error { logPath = path; return nil }

	if err := d.Dispatch(ctx, "/log session.log"); err != nil {
		t.Fatalf("/log: %v", err)
	}
	if logPath != "session.log" {
		t.Errorf("log path = %q, want %q", logPath, "session.log")
	}
}

func TestDispatcher_Log_NoArgs_StopsLog(t *testing.T) {
	d := cmd.New()
	var stopped bool
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.StopLog = func() { stopped = true }

	if err := d.Dispatch(ctx, "/log"); err != nil {
		t.Fatalf("/log (stop): %v", err)
	}
	if !stopped {
		t.Error("expected StopLog to be called")
	}
}

// ---------------------------------------------------------------------------
// /quit
// ---------------------------------------------------------------------------

func TestDispatcher_Quit_CallsQuit(t *testing.T) {
	d := cmd.New()
	var quit bool
	ctx := makeCtx(nil, nil)
	ctx.Quit = func() { quit = true }

	if err := d.Dispatch(ctx, "/quit"); err != nil {
		t.Fatalf("/quit: %v", err)
	}
	if !quit {
		t.Error("expected Quit callback to be called")
	}
}

// ---------------------------------------------------------------------------
// /help
// ---------------------------------------------------------------------------

func TestDispatcher_Help_WritesOutput(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)

	if err := d.Dispatch(ctx, "/help"); err != nil {
		t.Fatalf("/help: %v", err)
	}
	if len(output) == 0 {
		t.Error("/help should produce output")
	}
	// Should mention connect and quit at minimum.
	joined := strings.Join(output, "\n")
	for _, kw := range []string{"connect", "quit", "def"} {
		if !strings.Contains(joined, kw) {
			t.Errorf("/help output missing %q keyword", kw)
		}
	}
}

// ---------------------------------------------------------------------------
// /save
// ---------------------------------------------------------------------------

func TestDispatcher_Save_NilCallback_ReturnsError(t *testing.T) {
	d := cmd.New()
	ctx := makeCtx(nil, nil)
	// SaveMacros is nil by default in makeCtx.
	err := d.Dispatch(ctx, "/save")
	if err == nil {
		t.Fatal("/save with nil SaveMacros should return error")
	}
}

func TestDispatcher_Save_DefaultPath(t *testing.T) {
	d := cmd.New()
	var savedPath string
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.SaveMacros = func(path string) error {
		savedPath = path
		return nil
	}
	if err := d.Dispatch(ctx, "/save"); err != nil {
		t.Fatalf("/save: %v", err)
	}
	if savedPath != "macros.json" {
		t.Errorf("default save path = %q, want %q", savedPath, "macros.json")
	}
	if len(output) == 0 {
		t.Error("/save should produce confirmation output")
	}
}

func TestDispatcher_Save_CustomPath(t *testing.T) {
	d := cmd.New()
	var savedPath string
	ctx := makeCtx(nil, nil)
	ctx.SaveMacros = func(path string) error {
		savedPath = path
		return nil
	}
	if err := d.Dispatch(ctx, "/save /tmp/my-macros.json"); err != nil {
		t.Fatalf("/save: %v", err)
	}
	if savedPath != "/tmp/my-macros.json" {
		t.Errorf("save path = %q, want %q", savedPath, "/tmp/my-macros.json")
	}
}

func TestDispatcher_Save_CallbackError_Propagated(t *testing.T) {
	d := cmd.New()
	ctx := makeCtx(nil, nil)
	ctx.SaveMacros = func(_ string) error {
		return fmt.Errorf("disk full")
	}
	err := d.Dispatch(ctx, "/save")
	if err == nil {
		t.Fatal("SaveMacros error should propagate")
	}
	if !strings.Contains(err.Error(), "disk full") {
		t.Errorf("error = %q, want 'disk full'", err.Error())
	}
}

func TestDispatcher_Save_OutputMentionsPath(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)
	ctx.SaveMacros = func(_ string) error { return nil }
	d.Dispatch(ctx, "/save custom.json") //nolint:errcheck
	joined := strings.Join(output, "\n")
	if !strings.Contains(joined, "custom.json") {
		t.Errorf("confirmation output %q should mention path", joined)
	}
}

// ---------------------------------------------------------------------------
// /repeat, /cancel, /ps
// ---------------------------------------------------------------------------

// makeTimerCtx creates a Context with a simple in-memory timer stub.
func makeTimerCtx(output *[]string) (*cmd.Context, *[]cmd.TimerInfo) {
	timers := &[]cmd.TimerInfo{}
	nextID := 1

	ctx := makeCtx(output, nil)
	ctx.AddTimer = func(duration string, repeat bool, body string) (int, error) {
		id := nextID
		nextID++
		*timers = append(*timers, cmd.TimerInfo{
			ID:       id,
			Interval: duration,
			Repeat:   repeat,
			Next:     "0s",
		})
		return id, nil
	}
	ctx.CancelTimer = func(id int) bool {
		for i, t := range *timers {
			if t.ID == id {
				*timers = append((*timers)[:i], (*timers)[i+1:]...)
				return true
			}
		}
		return false
	}
	ctx.ListTimers = func() []cmd.TimerInfo {
		cp := make([]cmd.TimerInfo, len(*timers))
		copy(cp, *timers)
		return cp
	}
	return ctx, timers
}

func TestRepeat_AddsTimer(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx, timers := makeTimerCtx(&output)

	if err := d.Dispatch(ctx, "/repeat 5s /echo tick"); err != nil {
		t.Fatalf("/repeat: %v", err)
	}
	if len(*timers) != 1 {
		t.Fatalf("expected 1 timer, got %d", len(*timers))
	}
	if !(*timers)[0].Repeat {
		t.Error("timer should be repeating")
	}
	if (*timers)[0].Interval != "5s" {
		t.Errorf("interval = %q, want 5s", (*timers)[0].Interval)
	}
}

func TestRepeat_NoAddTimer_ReturnsError(t *testing.T) {
	d := cmd.New()
	ctx := makeCtx(nil, nil)
	// AddTimer is nil — should get a clear error.
	err := d.Dispatch(ctx, "/repeat 5s /echo tick")
	if err == nil {
		t.Fatal("/repeat with nil AddTimer should return error")
	}
}

func TestRepeat_MissingArgs_ReturnsError(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx, _ := makeTimerCtx(&output)

	err := d.Dispatch(ctx, "/repeat")
	if err == nil {
		t.Fatal("/repeat with no args should return error")
	}
}

func TestRepeat_OutputContainsTimerID(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx, _ := makeTimerCtx(&output)

	if err := d.Dispatch(ctx, "/repeat 1s /echo hi"); err != nil {
		t.Fatalf("/repeat: %v", err)
	}
	joined := strings.Join(output, "\n")
	// Output should mention the timer ID (1).
	if !strings.Contains(joined, "1") {
		t.Errorf("/repeat output %q should mention timer ID", joined)
	}
}

func TestCancel_RemovesTimer(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx, timers := makeTimerCtx(&output)

	// First add a timer.
	if err := d.Dispatch(ctx, "/repeat 1s /echo hi"); err != nil {
		t.Fatalf("/repeat: %v", err)
	}
	id := (*timers)[0].ID

	// Cancel it.
	if err := d.Dispatch(ctx, fmt.Sprintf("/cancel %d", id)); err != nil {
		t.Fatalf("/cancel: %v", err)
	}
	if len(*timers) != 0 {
		t.Errorf("expected 0 timers after cancel, got %d", len(*timers))
	}
}

func TestCancel_NonExistentID_ReturnsError(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx, _ := makeTimerCtx(&output)

	err := d.Dispatch(ctx, "/cancel 9999")
	if err == nil {
		t.Fatal("/cancel 9999 should return error for unknown id")
	}
}

func TestCancel_NoCancelTimer_ReturnsError(t *testing.T) {
	d := cmd.New()
	ctx := makeCtx(nil, nil)
	err := d.Dispatch(ctx, "/cancel 1")
	if err == nil {
		t.Fatal("/cancel with nil CancelTimer should return error")
	}
}

func TestPS_ListsTimers(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx, _ := makeTimerCtx(&output)

	d.Dispatch(ctx, "/repeat 5s /echo a") //nolint:errcheck
	d.Dispatch(ctx, "/repeat 1s /echo b") //nolint:errcheck

	output = nil // clear so we only capture /ps output
	ctx.Output = func(s string) { output = append(output, s) }

	if err := d.Dispatch(ctx, "/ps"); err != nil {
		t.Fatalf("/ps: %v", err)
	}
	joined := strings.Join(output, "\n")
	if !strings.Contains(joined, "5s") || !strings.Contains(joined, "1s") {
		t.Errorf("/ps output %q should list both timers", joined)
	}
}

func TestPS_EmptyList(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx, _ := makeTimerCtx(&output)

	if err := d.Dispatch(ctx, "/ps"); err != nil {
		t.Fatalf("/ps: %v", err)
	}
	joined := strings.Join(output, "\n")
	// Should indicate no timers.
	if !strings.Contains(joined, "No") && !strings.Contains(joined, "no") && !strings.Contains(joined, "0") {
		t.Errorf("/ps empty list output = %q, expected indication of no timers", joined)
	}
}

func TestPS_NoListTimers_ReturnsNilError(t *testing.T) {
	d := cmd.New()
	ctx := makeCtx(nil, nil)
	// ListTimers is nil — /ps should handle gracefully.
	err := d.Dispatch(ctx, "/ps")
	// Either error or empty output — just must not panic.
	_ = err
}

// ---------------------------------------------------------------------------
// CommandNames
// ---------------------------------------------------------------------------

func TestCommandNames_ReturnsAllBuiltins(t *testing.T) {
	d := cmd.New()
	names := d.CommandNames()
	if len(names) == 0 {
		t.Fatal("CommandNames returned empty slice")
	}

	// Check a representative set of expected commands.
	required := []string{"connect", "dc", "echo", "help", "quit", "repeat", "cancel", "ps", "def"}
	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	for _, req := range required {
		if !nameSet[req] {
			t.Errorf("CommandNames missing %q; got: %v", req, names)
		}
	}
}

func TestCommandNames_IsSorted(t *testing.T) {
	d := cmd.New()
	names := d.CommandNames()
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("CommandNames not sorted at index %d: %q > %q", i, names[i-1], names[i])
		}
	}
}

func TestCommandNames_AllHaveSlashPrefix_WhenDispatched(t *testing.T) {
	d := cmd.New()
	var output []string
	ctx := makeCtx(&output, nil)

	// Every name returned by CommandNames should be dispatchable via /name.
	// We just verify dispatch doesn't return "not a command" errors.
	for _, name := range d.CommandNames() {
		output = nil
		err := d.Dispatch(ctx, "/"+name)
		if err != nil && strings.Contains(err.Error(), "not a command") {
			t.Errorf("CommandNames returned %q but /%-s is not dispatchable", name, name)
		}
	}
}
