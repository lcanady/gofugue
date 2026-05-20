// Command gf is a terminal MUD client — the Go successor to TinyFugue.
//
// Usage:
//
//	gf [flags]
//	gf --headless --ipc-port 7878 --ipc-ws-port 7879
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gdamore/tcell/v2"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/cmd"
	"github.com/kumakun/gofugue/internal/compat"
	"github.com/kumakun/gofugue/internal/config"
	"github.com/kumakun/gofugue/internal/expr"
	"github.com/kumakun/gofugue/internal/history"
	"github.com/kumakun/gofugue/internal/ipc"
	"github.com/kumakun/gofugue/internal/macro"
	"github.com/kumakun/gofugue/internal/plugin"
	scriptjs "github.com/kumakun/gofugue/internal/script/js"
	scriptpy "github.com/kumakun/gofugue/internal/script/py"
	"github.com/kumakun/gofugue/internal/timers"
	"github.com/kumakun/gofugue/internal/tui"
	"github.com/kumakun/gofugue/internal/world"
)

var (
	flagHeadless  = flag.Bool("headless", false, "run without TUI (IPC-only mode)")
	flagConfig    = flag.String("config", config.DefaultPath(), "path to config.toml")
	flagIPCPort   = flag.Int("ipc-port", 0, "TCP IPC port (overrides config, 0 = use config)")
	flagIPCWsPort = flag.Int("ipc-ws-port", 0, "WebSocket IPC port (overrides config, 0 = use config)")
	flagConnect   = flag.String("connect", "", "connect to a URL on startup (mud://host:port)")
	flagScript    = flag.String("script", "", "load a .tf script on startup")
	flagBridgePy  = flag.String("bridge-py", "", "path to bridge.py (enables /py scripting)")
	flagVersion   = flag.Bool("version", false, "print version and exit")
)

const version = "0.1.0-dev"

func main() {
	flag.Parse()

	if *flagVersion {
		fmt.Println("gf", version)
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil && err != context.Canceled {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// --- Config ---
	cfg, err := config.Load(*flagConfig)
	if err != nil {
		slog.Warn("config load failed, using defaults", "path", *flagConfig, "err", err)
		cfg = config.Defaults()
	}

	if *flagIPCPort > 0 {
		cfg.IPC.TCPPort = *flagIPCPort
	}
	if *flagIPCWsPort > 0 {
		cfg.IPC.WSPort = *flagIPCWsPort
	}
	scrollback := cfg.ScrollbackLines
	if scrollback <= 0 {
		scrollback = 5000
	}

	// --- Core subsystems ---
	b := bus.New()
	scope := expr.NewScope()
	macroEng := macro.New()
	worldMgr := world.NewManager(b)

	// Wire expression evaluator for -E condition gates.
	macroEng.SetEvalFunc(func(e string) (bool, error) {
		result, err := expr.Eval(e, scope)
		if err != nil {
			return false, err
		}
		// Truthy: non-empty, non-"0", non-"false".
		switch strings.TrimSpace(strings.ToLower(result)) {
		case "", "0", "false":
			return false, nil
		}
		return true, nil
	})
	dispatcher := cmd.New()
	histMgr := history.NewManager(scrollback)
	timerPool := timers.New()

	// Restriction flags (read by commands that check them in future).
	restrict := struct{ Shell, File, World bool }{}

	// Register worlds from config.
	for name, wcfg := range cfg.Worlds {
		wcfg.Name = name
		worldMgr.Add(wcfg)
	}

	// --- Scripting bridges ---
	scriptEcho := func(text string) {
		b.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{
			WorldName: "local",
			Text:      text,
			Attrs:     bus.LineAttrs{FG: -1, BG: -1},
		}})
	}

	jsBridge := scriptjs.New(b, scriptjs.Callbacks{
		Send:            worldMgr.Send,
		Echo:            scriptEcho,
		ForegroundWorld: worldMgr.Foreground,
		Getvar:          func(name string) (string, bool) { return scope.Get(name) },
		Setvar:          func(name, value string) { scope.Set(name, value) },
	})
	jsBridge.Start(ctx)

	bridgePyPath := *flagBridgePy
	pyBridge := scriptpy.New(b, scriptpy.Callbacks{
		Send:            worldMgr.Send,
		Echo:            scriptEcho,
		ForegroundWorld: worldMgr.Foreground,
		Getvar:          func(name string) (string, bool) { return scope.Get(name) },
		Setvar:          func(name, value string) { scope.Set(name, value) },
	}, bridgePyPath)

	// --- Plugins ---
	for _, p := range plugin.All() {
		slog.Info("plugin registered", "name", p.Name())
	}

	// --- Startup script ---
	startupScript := cfg.StartupScript
	if *flagScript != "" {
		startupScript = *flagScript
	}
	if startupScript != "" {
		if err := loadScript(startupScript, macroEng, scope); err != nil {
			slog.Warn("startup script error", "path", startupScript, "err", err)
		}
	}

	// timerCmdCtx is an indirection so timer callbacks can reference cmdCtx
	// even though the pointer isn't available during struct literal construction.
	var timerCmdCtxPtr *cmd.Context
	timerCmdCtx := func() *cmd.Context { return timerCmdCtxPtr }

	// --- Build the command Context ---
	cmdCtx := &cmd.Context{
		Ctx:   ctx,
		World: worldMgr.Foreground(),
		Output: func(text string) {
			b.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{
				WorldName: "local",
				Text:      text,
				Attrs:     bus.LineAttrs{FG: -1, BG: -1},
			}})
		},
		Send: func(wname, text string) error {
			return worldMgr.Send(wname, text)
		},
		Connect: func(name, url string) error {
			wcfg := &config.WorldConfig{Name: name, URL: url}
			return worldMgr.Connect(ctx, name, wcfg)
		},
		Disconnect: func(name string) error {
			return worldMgr.Disconnect(name)
		},
		Switch: func(name string) error {
			return worldMgr.Switch(name)
		},
		ForegroundWorld: worldMgr.Foreground,
		DefMacro:        macroEng.Define,
		UndefMacro:      macroEng.Undefine,
		ListMacros:      macroEng.List,
		Setvar:          func(name, val string) { scope.Set(name, val) },
		Getvar:          func(name string) (string, bool) { return scope.Get(name) },
		ListVars: func() map[string]string {
			return scope.All()
		},
		StartLog: func(path string) error {
			return histMgr.World(worldMgr.Foreground()).StartLog(path)
		},
		StopLog: func() {
			histMgr.World(worldMgr.Foreground()).StopLog()
		},
		SearchHistory: func(pattern string) ([]string, error) {
			lines := histMgr.SearchAll(pattern)
			out := make([]string, len(lines))
			for i, l := range lines {
				out[i] = l.Text
			}
			return out, nil
		},
		AddTimer: func(duration string, repeat bool, body string) (int, error) {
			d, err := time.ParseDuration(duration)
			if err != nil {
				return 0, fmt.Errorf("invalid duration %q: %w", duration, err)
			}
			// cmdCtx captured after struct is fully constructed (set below).
			id := timerPool.Add(ctx, d, repeat, func() {
				fg := worldMgr.Foreground()
				expanded := expandCaptures(body, nil, scope)
				execBody(fg, expanded, timerCmdCtx(), worldMgr, dispatcher)
			})
			return id, nil
		},
		CancelTimer: timerPool.Cancel,
		ListTimers: func() []cmd.TimerInfo {
			infos := timerPool.List()
			out := make([]cmd.TimerInfo, len(infos))
			for i, t := range infos {
				out[i] = cmd.TimerInfo{
					ID:       t.ID,
					Interval: t.Interval.String(),
					Repeat:   t.Repeat,
					Next:     time.Until(t.Next).Truncate(time.Millisecond).String(),
				}
			}
			return out
		},
		LoadFile: func(path string) error {
			return loadScript(path, macroEng, scope)
		},
		SaveMacros: macroEng.SaveFile,
		LoadJS:     jsBridge.Load,
		EvalJS:     jsBridge.Eval,
		LoadPy: func(path string) error {
			if bridgePyPath == "" {
				return fmt.Errorf("--bridge-py not set; cannot load Python scripts")
			}
			return pyBridge.Load(ctx, path)
		},
		AddWorld: func(name, url, char, pass string) error {
			wcfg := config.WorldConfig{Name: name, URL: url}
			if char != "" {
				wcfg.Char = char
			}
			if pass != "" {
				wcfg.Pass = pass
			}
			worldMgr.Add(wcfg)
			return nil
		},
		ListWorlds: func() []cmd.WorldInfo {
			infos := worldMgr.WorldInfos()
			out := make([]cmd.WorldInfo, len(infos))
			for i, w := range infos {
				out[i] = cmd.WorldInfo{Name: w.Name, URL: w.URL, Connected: w.Connected}
			}
			return out
		},
		SaveWorlds: worldMgr.SaveTOML,
		RemoveWorld: func(name string) error {
			return worldMgr.Remove(name)
		},
		Restrict: func(what string) {
			switch what {
			case "SHELL":
				restrict.Shell = true
			case "FILE":
				restrict.File = true
			case "WORLD":
				restrict.World = true
			}
		},
		IsRestricted: func(what string) bool {
			switch what {
			case "SHELL":
				return restrict.Shell
			case "FILE":
				return restrict.File
			case "WORLD":
				return restrict.World
			}
			return false
		},
		Quit: func() {
			b.Publish(bus.HookEvent{WorldName: "", Name: "QUIT"})
			cancel()
		},
	}

	// --- Pipeline goroutines ---
	timerCmdCtxPtr = cmdCtx // now safe to use in timer callbacks
	startPipelines(ctx, b, worldMgr, macroEng, histMgr, scope, cmdCtx, dispatcher)

	// --- IPC server ---
	ipcCfg := ipc.Config{
		SocketPath: config.DefaultSocketPath(),
	}
	if cfg.IPC.TCPPort > 0 {
		ipcCfg.TCPAddr = fmt.Sprintf("127.0.0.1:%d", cfg.IPC.TCPPort)
	}
	if cfg.IPC.WSPort > 0 {
		ipcCfg.WSAddr = fmt.Sprintf("127.0.0.1:%d", cfg.IPC.WSPort)
	}
	ipcServer := ipc.New(ipcCfg, b)
	ipcServer.SetHistoryFunc(func(n int) []string {
		lines := histMgr.TailAll(n)
		out := make([]string, len(lines))
		for i, l := range lines {
			out[i] = l.Text
		}
		return out
	})

	// input: send raw text to a world (goes through alias/speedwalk pipeline).
	ipcServer.Handle("input", func(_ context.Context, _ *ipc.Client, params json.RawMessage) (any, error) {
		var p struct {
			World string `json:"world"`
			Text  string `json:"text"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		w := p.World
		if w == "" {
			w = worldMgr.Foreground()
		}
		b.Publish(bus.UserInputEvent{WorldName: w, Text: p.Text})
		return map[string]bool{"ok": true}, nil
	})

	// cmd: dispatch a /command line (same as typing /connect etc in the TUI).
	ipcServer.Handle("cmd", func(_ context.Context, _ *ipc.Client, params json.RawMessage) (any, error) {
		var p struct {
			World string `json:"world"`
			Line  string `json:"line"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, err
		}
		w := p.World
		if w == "" {
			w = worldMgr.Foreground()
		}
		b.Publish(bus.UserCmdEvent{WorldName: w, Line: p.Line})
		return map[string]bool{"ok": true}, nil
	})

	// worlds.status: returns current connection state of all registered worlds.
	// Call this after subscribe to hydrate the frontend on reconnect.
	ipcServer.Handle("worlds.status", func(_ context.Context, _ *ipc.Client, _ json.RawMessage) (any, error) {
		infos := worldMgr.WorldInfos()
		type worldStatus struct {
			Name      string `json:"name"`
			Connected bool   `json:"connected"`
		}
		out := make([]worldStatus, len(infos))
		for i, wi := range infos {
			out[i] = worldStatus{Name: wi.Name, Connected: wi.Connected}
		}
		return out, nil
	})

	go func() {
		if err := ipcServer.Run(ctx); err != nil && err != context.Canceled {
			slog.Error("ipc server error", "err", err)
		}
	}()

	slog.Info("gf ready", "version", version, "headless", *flagHeadless)

	// --- TUI or headless ---
	if *flagHeadless {
		// In headless mode we can connect before the event loop.
		if cfg.DefaultWorld != "" {
			if err := worldMgr.Connect(ctx, cfg.DefaultWorld, nil); err != nil {
				slog.Warn("default world connect failed", "world", cfg.DefaultWorld, "err", err)
			}
		}
		if *flagConnect != "" {
			name := world.NameFromURL(*flagConnect)
			wcfg := &config.WorldConfig{Name: name, URL: *flagConnect}
			if err := worldMgr.Connect(ctx, name, wcfg); err != nil {
				slog.Warn("--connect failed", "url", *flagConnect, "err", err)
			}
		}
		<-ctx.Done()
		slog.Info("shutting down")
		return ctx.Err()
	}

	// Redirect slog away from stderr while tcell owns the terminal.
	// Stray writes to stderr corrupt the raw-mode screen; we always redirect
	// to at least io.Discard so no log path can break the display.
	logWriter := io.Writer(io.Discard)
	if logDir, err := os.UserCacheDir(); err == nil {
		logPath := filepath.Join(logDir, "gofugue", "gofugue.log")
		_ = os.MkdirAll(filepath.Dir(logPath), 0o750)
		if lf, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640); err == nil {
			logWriter = lf
			defer lf.Close()
		}
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(logWriter, nil)))

	app := tui.New(b)
	app.SetTheme(themeStyle(cfg.Theme.StatusFG, cfg.Theme.StatusBG, cfg.Theme.StatusBold, cfg.Theme.StatusReverse),
		themeStyle(cfg.Theme.InputFG, cfg.Theme.InputBG, false, cfg.Theme.InputReverse))
	app.SetResizeCallback(func(w, h int) {
		worldMgr.SetWindowSize(uint16(w), uint16(h))
	})
	app.SetCompletionSource(func(prefix string) []string {
		return completionCandidates(prefix, dispatcher, worldMgr, macroEng)
	})

	// Deferred connect: run after the screen is initialised so that
	// StatusEvents are received by the TUI and errors appear in the output
	// pane rather than as raw stderr text that corrupts the display.
	app.SetOnReady(func() {
		connectAndReport := func(name string, wcfg *config.WorldConfig) {
			if err := worldMgr.Connect(ctx, name, wcfg); err != nil {
				cmdCtx.Output("Connection error: " + err.Error())
			}
		}
		if cfg.DefaultWorld != "" {
			connectAndReport(cfg.DefaultWorld, nil)
		}
		if *flagConnect != "" {
			name := world.NameFromURL(*flagConnect)
			connectAndReport(name, &config.WorldConfig{Name: name, URL: *flagConnect})
		}
	})

	return app.Run(ctx)
}

// startPipelines starts the background goroutines routing events between subsystems.
func startPipelines(
	ctx context.Context,
	b *bus.Bus,
	worldMgr *world.Manager,
	macroEng *macro.Engine,
	histMgr *history.Manager,
	scope *expr.Scope,
	cmdCtx *cmd.Context,
	dispatcher *cmd.Dispatcher,
) {
	// UserCmd: /commands from TUI or IPC → dispatcher.
	userCmdSub := b.Subscribe(64, bus.EvUserCmd)
	go func() {
		defer userCmdSub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-userCmdSub.C:
				if !ok {
					return
				}
				uce := ev.(bus.UserCmdEvent)
				cmdCtx.World = worldMgr.Foreground()
				if err := dispatcher.Dispatch(cmdCtx, uce.Line); err != nil {
					slog.Warn("cmd dispatch", "err", err)
					cmdCtx.Output("Error: " + err.Error())
				}
			}
		}
	}()

	// UserInput: plain text → speedwalk → alias check → world.Send + SEND hook.
	userInputSub := b.Subscribe(64, bus.EvUserInput)
	go func() {
		defer userInputSub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-userInputSub.C:
				if !ok {
					return
				}
				uie := ev.(bus.UserInputEvent)
				fg := worldMgr.Foreground()
				text := expandSpeedwalk(uie.Text)

				// SEND hook.
				for _, body := range macroEng.FireHook(fg, "SEND") {
					expanded := expandCaptures(body, nil, scope)
					execBody(fg, expanded, cmdCtx, worldMgr, dispatcher)
				}

				// Alias check.
				if body, ok := macroEng.MatchAlias(fg, text); ok {
					expanded := expandCaptures(body, nil, scope)
					execBody(fg, expanded, cmdCtx, worldMgr, dispatcher)
					continue
				}
				if err := worldMgr.Send(fg, text); err != nil {
					slog.Debug("send failed", "world", fg, "err", err)
				}
			}
		}
	}()

	// WorldLine: raw events → history + triggers → publish rendered event for TUI.
	lineSub := b.Subscribe(512, bus.EvWorldLine)
	go func() {
		defer lineSub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-lineSub.C:
				if !ok {
					return
				}
				wle := ev.(bus.WorldLineEvent)

				// Record in history (plain text).
				histMgr.World(wle.WorldName).Append(history.Line{
					Text:      wle.Text,
					Attrs:     wle.Attrs,
					Timestamp: time.Now(),
				})

				// Fire triggers; apply gag/hilite/substitute transforms.
				result := macroEng.FireTriggers(wle.WorldName, wle.Text)
				if result.Gagged {
					wle.Gagged = true
				}

				for _, fire := range result.Fires {
					switch fire.Macro.Type {
					case macro.TypeTrigger:
						body := expandCaptures(fire.Body, fire.Captures, scope)
						execBody(wle.WorldName, body, cmdCtx, worldMgr, dispatcher)

					case macro.TypeHilite:
						// Apply colour from body (e.g. "red", "bold", "1;32").
						wle.Attrs = parseAttrSpec(fire.Body)

					case macro.TypeSubstitute:
						body := expandCaptures(fire.Body, fire.Captures, scope)
						wle.Text = body
						wle.Spans = nil // clear spans; new text is unstyled
					}
				}

				// Publish processed event for TUI.
				b.Publish(bus.WorldRenderedEvent{WorldLineEvent: wle})
			}
		}
	}()

	// Hook: CONNECT/DISCONNECT/QUIT/PROMPT/ACTIVITY/RESIZE → macro engine.
	hookSub := b.Subscribe(64, bus.EvHook)
	go func() {
		defer hookSub.Cancel()
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-hookSub.C:
				if !ok {
					return
				}
				he := ev.(bus.HookEvent)
				if he.Name == "QUIT" {
					return
				}

				// Auto-login: on CONNECT, send the world's configured login string.
				if he.Name == "CONNECT" {
					if wcfg, ok := worldMgr.WorldConfig(he.WorldName); ok {
						login := wcfg.Login
						if login == "" {
							// Assemble from Char/Pass fields (set via /addworld).
							if wcfg.Char != "" {
								login = wcfg.Char
								pass := wcfg.Pass
								if pass == "" {
									pass = wcfg.Password
								}
								if pass != "" {
									login = wcfg.Char + "\n" + pass
								}
							}
						}
						if login != "" {
							for _, line := range strings.Split(login, "\n") {
								if err := worldMgr.Send(he.WorldName, line); err != nil {
									slog.Warn("autologin", "world", he.WorldName, "err", err)
								}
							}
						}
					}
				}

				for _, body := range macroEng.FireHook(he.WorldName, he.Name) {
					expanded := expandCaptures(body, nil, scope)
					execBody(he.WorldName, expanded, cmdCtx, worldMgr, dispatcher)
				}
			}
		}
	}()
}

// expandCaptures replaces %0-%9 references in body with regexp captures,
// then runs ExpandFull for $var and {expr} substitution.
func expandCaptures(body string, captures []string, s *expr.Scope) string {
	if len(captures) > 0 && strings.ContainsAny(body, "%0123456789") {
		for i, c := range captures {
			if i > 9 {
				break
			}
			body = strings.ReplaceAll(body, "%"+strconv.Itoa(i), c)
		}
	}
	result, err := expr.ExpandFull(body, s)
	if err != nil {
		slog.Warn("expand", "body", body, "err", err)
		return body
	}
	return result
}

// execBody executes a macro body in the context of the given world.
func execBody(worldName, body string, cmdCtx *cmd.Context, mgr *world.Manager, d *cmd.Dispatcher) {
	if len(body) == 0 {
		return
	}
	cmdCtx.World = worldName

	// Multiple semicolon-separated commands (after speedwalk expansion is already done).
	parts := strings.Split(body, ";")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part[0] == '/' {
			if err := d.Dispatch(cmdCtx, part); err != nil {
				slog.Warn("macro exec", "world", worldName, "body", part, "err", err)
			}
			continue
		}
		if err := mgr.Send(worldName, part); err != nil {
			slog.Warn("macro send", "world", worldName, "body", part, "err", err)
		}
	}
}

// expandSpeedwalk converts speedwalk notation like "3n2ew" into "north;north;north;east;east;west".
// Only expands when the input looks like a pure speedwalk (digits + direction letters only).
func expandSpeedwalk(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	dirMap := map[byte]string{
		'n': "north", 's': "south", 'e': "east", 'w': "west",
		'u': "up", 'd': "down",
		'N': "north", 'S': "south", 'E': "east", 'W': "west",
		'U': "up", 'D': "down",
	}
	// Validate: only digits and direction letters.
	for i := 0; i < len(text); i++ {
		c := text[i]
		if c >= '0' && c <= '9' {
			continue
		}
		if _, ok := dirMap[c]; ok {
			continue
		}
		return text // not a speedwalk — return unchanged
	}
	// Expand.
	var dirs []string
	i := 0
	for i < len(text) {
		count := 0
		for i < len(text) && text[i] >= '0' && text[i] <= '9' {
			count = count*10 + int(text[i]-'0')
			i++
		}
		if i >= len(text) {
			break
		}
		dir, ok := dirMap[text[i]]
		if !ok {
			return text // fallback
		}
		if count == 0 {
			count = 1
		}
		for j := 0; j < count; j++ {
			dirs = append(dirs, dir)
		}
		i++
	}
	if len(dirs) == 0 {
		return text
	}
	return strings.Join(dirs, ";")
}

// parseAttrSpec converts a colour/attr spec string (e.g. "red", "bold", "1;32")
// into a bus.LineAttrs for hilite macros.
func parseAttrSpec(spec string) bus.LineAttrs {
	attrs := bus.LineAttrs{FG: -1, BG: -1}
	spec = strings.ToLower(strings.TrimSpace(spec))

	namedColours := map[string]int{
		"black": 0, "red": 1, "green": 2, "yellow": 3,
		"blue": 4, "magenta": 5, "cyan": 6, "white": 7,
		"bblack": 8, "bred": 9, "bgreen": 10, "byellow": 11,
		"bblue": 12, "bmagenta": 13, "bcyan": 14, "bwhite": 15,
	}

	// Named colour shortcuts.
	if n, ok := namedColours[spec]; ok {
		attrs.FG = n
		return attrs
	}
	if strings.HasPrefix(spec, "bg") {
		if n, ok := namedColours[spec[2:]]; ok {
			attrs.BG = n
			return attrs
		}
	}

	// SGR numeric codes.
	for _, part := range strings.Split(spec, ";") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		switch {
		case n == 1:
			attrs.Bold = true
		case n == 3:
			attrs.Italic = true
		case n == 4:
			attrs.Underline = true
		case n == 7:
			attrs.Reverse = true
		case n >= 30 && n <= 37:
			attrs.FG = n - 30
		case n >= 40 && n <= 47:
			attrs.BG = n - 40
		case n >= 90 && n <= 97:
			attrs.FG = n - 90 + 8
		case n >= 100 && n <= 107:
			attrs.BG = n - 100 + 8
		}
	}
	return attrs
}

// completionCandidates returns tab-completion candidates for the given prefix.
// For /command prefixes it returns matching command names.
// For plain text it returns matching macro names and world names.
func completionCandidates(prefix string, d *cmd.Dispatcher, mgr *world.Manager, eng *macro.Engine) []string {
	var out []string
	if strings.HasPrefix(prefix, "/") {
		// Command completion: match registered command names.
		cmdPrefix := strings.ToLower(prefix[1:])
		for _, name := range d.CommandNames() {
			if strings.HasPrefix(name, cmdPrefix) {
				out = append(out, "/"+name)
			}
		}
	} else {
		// Macro and world name completion.
		for _, desc := range eng.List() {
			// List() returns "[type] name /pattern/ = body" — extract name.
			parts := strings.Fields(desc)
			if len(parts) >= 2 && strings.HasPrefix(parts[1], prefix) {
				out = append(out, parts[1])
			}
		}
		for _, wi := range mgr.WorldInfos() {
			if strings.HasPrefix(wi.Name, prefix) {
				out = append(out, wi.Name)
			}
		}
	}
	// Sort and deduplicate.
	seen := make(map[string]bool)
	var deduped []string
	for _, c := range out {
		if !seen[c] {
			seen[c] = true
			deduped = append(deduped, c)
		}
	}
	sort.Strings(deduped)
	return deduped
}

// themeStyle builds a tcell.Style from config colour name strings and flags.
// fg/bg are tcell colour names (e.g. "navy", "white") or empty for default.
func themeStyle(fg, bg string, bold, reverse bool) tcell.Style {
	s := tcell.StyleDefault
	if fg != "" {
		if c, ok := tcell.ColorNames[strings.ToLower(fg)]; ok {
			s = s.Foreground(c)
		}
	}
	if bg != "" {
		if c, ok := tcell.ColorNames[strings.ToLower(bg)]; ok {
			s = s.Background(c)
		}
	}
	if bold {
		s = s.Bold(true)
	}
	if reverse {
		s = s.Reverse(true)
	}
	return s
}

// loadScript parses a .tf file and defines all macros and applies settings.
func loadScript(path string, eng *macro.Engine, scope *expr.Scope) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	res, err := compat.ImportReader(f)
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		slog.Warn("script", "warning", w, "path", path)
	}
	for _, m := range res.Macros {
		if err := eng.Define(m); err != nil {
			slog.Warn("macro define", "name", m.Name, "err", err)
		}
	}
	// Apply /set variables from script.
	for k, v := range res.Settings {
		scope.Set(k, v)
	}
	// Apply /key bindings as TypeKeybind macros.
	for _, kb := range res.KeyBindings {
		key := strings.ToLower(kb.Key)
		m := &macro.Macro{
			Name:    "key_" + key,
			Type:    macro.TypeKeybind,
			Pattern: key,
			Body:    kb.Body,
		}
		if err := eng.Define(m); err != nil {
			slog.Warn("keybind define", "key", key, "err", err)
		}
	}
	return nil
}

