// Package cmd implements the builtin /command dispatcher and alias expansion.
package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/kumakun/gofugue/internal/macro"
)

// Handler is the function signature for a builtin command.
type Handler func(ctx *Context, args string) error

// Context carries the runtime state a command handler needs.
// All function fields are optional; commands check for nil before calling.
type Context struct {
	// Ctx is the application lifecycle context (used for Connect calls).
	Ctx context.Context

	// World is the current foreground world name.
	World string

	// Output writes a line to the local output pane.
	Output func(text string)

	// Send sends text to the named world.
	Send func(world, text string) error

	// Connect dials a new world by URL and optional name.
	// If name == "", an auto-name is derived from the URL.
	Connect func(name, url string) error

	// Disconnect closes the named world (or foreground world if "").
	Disconnect func(name string) error

	// Switch changes the foreground world.
	Switch func(name string) error

	// ForegroundWorld returns the current foreground world name.
	ForegroundWorld func() string

	// DefMacro defines or replaces a macro.
	DefMacro func(m *macro.Macro) error

	// UndefMacro removes a macro by name.
	UndefMacro func(name string) bool

	// ListMacros returns a human-readable description of all defined macros.
	ListMacros func() []string

	// Setvar sets a global variable.
	Setvar func(name, value string)

	// Getvar reads a global variable.
	Getvar func(name string) (string, bool)

	// ListVars returns all variable name→value pairs.
	ListVars func() map[string]string

	// StartLog begins logging world output to a file.
	StartLog func(path string) error

	// StopLog stops the current log.
	StopLog func()

	// SearchHistory returns lines from history matching the pattern.
	SearchHistory func(pattern string) ([]string, error)

	// LoadFile parses and applies a .tf script file.
	LoadFile func(path string) error

	// SaveMacros serialises all currently defined macros to a JSON file.
	SaveMacros func(path string) error

	// AddWorld registers a new world without connecting.
	AddWorld func(name, url, char, pass string) error

	// ListWorlds returns all registered world names and their connection state.
	ListWorlds func() []WorldInfo

	// SaveWorlds persists world definitions to a TOML file.
	SaveWorlds func(path string) error

	// RemoveWorld removes a world from the manager.
	RemoveWorld func(name string) error

	// Restrict sets a restriction flag (SHELL, FILE, WORLD).
	Restrict func(what string)

	// IsRestricted returns true if the given capability is restricted.
	// what is one of "SHELL", "FILE", "WORLD".
	IsRestricted func(what string) bool

	// AddTimer registers a repeating or one-shot timer. Returns the timer ID.
	// duration is a Go duration string ("5s", "500ms"). body is executed each fire.
	// If repeat is true, fires every duration; otherwise fires once.
	AddTimer func(duration string, repeat bool, body string) (int, error)

	// CancelTimer removes a timer by ID.
	CancelTimer func(id int) bool

	// ListTimers returns a snapshot of all active timers.
	ListTimers func() []TimerInfo

	// LoadJS loads and executes a JavaScript file via the JS bridge.
	LoadJS func(path string) error

	// EvalJS evaluates a JS snippet and returns the string result.
	EvalJS func(src string) (string, error)

	// LoadPy loads a Python script via the Python bridge.
	LoadPy func(path string) error

	// Quit initiates a graceful shutdown.
	Quit func()
}

// WorldInfo is a summary of a world's state for /listworlds output.
type WorldInfo struct {
	Name      string
	URL       string
	Connected bool
}

// TimerInfo is one row in /ps output.
type TimerInfo struct {
	ID       int
	Interval string // human-readable duration
	Repeat   bool
	Next     string // time until next fire
}

// fg returns the effective foreground world: uses ForegroundWorld() if set,
// falling back to ctx.World.
func (c *Context) fg() string {
	if c.ForegroundWorld != nil {
		if w := c.ForegroundWorld(); w != "" {
			return w
		}
	}
	return c.World
}

// output writes to the output pane if the callback is set.
func (c *Context) output(text string) {
	if c.Output != nil {
		c.Output(text)
	}
}

// Dispatcher holds the builtin command table and dispatches /command lines.
type Dispatcher struct {
	builtins map[string]Handler
}

// New returns a Dispatcher pre-loaded with all builtin commands.
func New() *Dispatcher {
	d := &Dispatcher{builtins: make(map[string]Handler)}
	d.registerBuiltins()
	return d
}

// Register adds or overrides a command handler (used by plugins).
func (d *Dispatcher) Register(name string, h Handler) {
	d.builtins[strings.ToLower(name)] = h
}

// CommandNames returns a sorted list of all registered command names.
func (d *Dispatcher) CommandNames() []string {
	names := make([]string, 0, len(d.builtins))
	for name := range d.builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Dispatch parses and executes a /command line.
// line must start with '/'.
func (d *Dispatcher) Dispatch(ctx *Context, line string) error {
	if len(line) == 0 || line[0] != '/' {
		return fmt.Errorf("cmd: not a command: %q", line)
	}
	line = line[1:] // strip leading /
	name, args, _ := strings.Cut(line, " ")
	name = strings.ToLower(strings.TrimSpace(name))

	h, ok := d.builtins[name]
	if !ok {
		return fmt.Errorf("unknown command: /%s", name)
	}
	return h(ctx, strings.TrimSpace(args))
}

func (d *Dispatcher) registerBuiltins() {
	d.Register("connect", cmdConnect)
	d.Register("dc", cmdDisconnect)
	d.Register("sw", cmdSwitch)
	d.Register("send", cmdSend)
	d.Register("echo", cmdEcho)
	d.Register("def", cmdDef)
	d.Register("gag", cmdGag)
	d.Register("hilite", cmdHilite)
	d.Register("substitute", cmdSubstitute)
	d.Register("undef", cmdUndef)
	d.Register("list", cmdList)
	d.Register("set", cmdSet)
	d.Register("unset", cmdUnset)
	d.Register("listvar", cmdListVar)
	d.Register("log", cmdLog)
	d.Register("recall", cmdRecall)
	d.Register("load", cmdLoad)
	d.Register("save", cmdSave)
	d.Register("addworld", cmdAddWorld)
	d.Register("listworlds", cmdListWorlds)
	d.Register("saveworld", cmdSaveWorld)
	d.Register("unworld", cmdUnworld)
	d.Register("restrict", cmdRestrict)
	d.Register("repeat", cmdRepeat)
	d.Register("cancel", cmdCancel)
	d.Register("ps", cmdPS)
	d.Register("js", cmdJS)
	d.Register("py", cmdPy)
	d.Register("quit", cmdQuit)
	d.Register("help", cmdHelp)
}

// ---------------------------------------------------------------------------
// /connect [url] [name]
// /connect name          ← pre-registered world
// ---------------------------------------------------------------------------

func cmdConnect(ctx *Context, args string) error {
	if ctx.IsRestricted != nil && ctx.IsRestricted("WORLD") {
		return fmt.Errorf("/connect: restricted (WORLD)")
	}
	if ctx.Connect == nil {
		return fmt.Errorf("/connect: not available")
	}
	if args == "" {
		return fmt.Errorf("usage: /connect <url> [name]")
	}
	parts := strings.Fields(args)
	url := parts[0]
	name := ""
	if len(parts) >= 2 {
		name = parts[1]
	}
	if name == "" {
		name = worldNameFromURL(url)
	}
	if err := ctx.Connect(name, url); err != nil {
		return fmt.Errorf("/connect %s: %w", name, err)
	}
	ctx.output(fmt.Sprintf("Connecting to %s (%s)…", name, url))
	return nil
}

// worldNameFromURL derives a short name from a URL (scheme removed, port removed).
// mud://example.com:4000 → "example.com"
func worldNameFromURL(url string) string {
	s := url
	// strip scheme
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	// strip path
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	// strip port
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "world"
	}
	return s
}

// ---------------------------------------------------------------------------
// /dc [name]
// ---------------------------------------------------------------------------

func cmdDisconnect(ctx *Context, args string) error {
	if ctx.Disconnect == nil {
		return fmt.Errorf("/dc: not available")
	}
	name := strings.TrimSpace(args)
	if name == "" {
		name = ctx.fg()
	}
	if err := ctx.Disconnect(name); err != nil {
		return fmt.Errorf("/dc %s: %w", name, err)
	}
	ctx.output(fmt.Sprintf("Disconnected from %s.", name))
	return nil
}

// ---------------------------------------------------------------------------
// /sw <name>
// ---------------------------------------------------------------------------

func cmdSwitch(ctx *Context, args string) error {
	if ctx.Switch == nil {
		return fmt.Errorf("/sw: not available")
	}
	name := strings.TrimSpace(args)
	if name == "" {
		return fmt.Errorf("usage: /sw <world>")
	}
	if err := ctx.Switch(name); err != nil {
		return fmt.Errorf("/sw %s: %w", name, err)
	}
	ctx.output(fmt.Sprintf("Switched to %s.", name))
	return nil
}

// ---------------------------------------------------------------------------
// /send <text>
// /echo <text>
// ---------------------------------------------------------------------------

func cmdSend(ctx *Context, args string) error {
	if ctx.Send == nil {
		return fmt.Errorf("/send: not available")
	}
	return ctx.Send(ctx.fg(), args)
}

func cmdEcho(ctx *Context, args string) error {
	ctx.output(args)
	return nil
}

// ---------------------------------------------------------------------------
// /def [-t "pattern"] [-p priority] name=body
// ---------------------------------------------------------------------------

func cmdDef(ctx *Context, args string) error {
	if ctx.DefMacro == nil {
		return fmt.Errorf("/def: not available")
	}
	if args == "" {
		return fmt.Errorf("usage: /def name=body  or  /def -t \"pattern\" name=body")
	}
	m, err := parseDef(args)
	if err != nil {
		return fmt.Errorf("/def: %w", err)
	}
	if err := ctx.DefMacro(m); err != nil {
		return fmt.Errorf("/def %s: %w", m.Name, err)
	}
	ctx.output(fmt.Sprintf("Defined %s.", m.Name))
	return nil
}

// parseDef supports all TinyFugue /def flags:
//
//	-t <pattern>   trigger pattern (regex/glob/substr based on -m)
//	-p N           priority
//	-c N           probability 0-100
//	-n N           fire N times then auto-undef
//	-F             fallthrough
//	-i/-I          invisible
//	-E             define disabled
//	-w <world>     world scope
//	-m <mode>      match mode: regexp, glob, substr
//	-h <hook>      hook name (makes it a hook macro)
//	name=body
func parseDef(args string) (*macro.Macro, error) {
	m := &macro.Macro{Type: macro.TypeTrigger, Enabled: true}
	rest := strings.TrimSpace(args)

	for strings.HasPrefix(rest, "-") {
		rest = rest[1:] // strip '-'
		if len(rest) == 0 {
			break
		}
		flag := rest[0]
		rest = rest[1:]
		switch flag {
		case 't':
			rest = strings.TrimLeft(rest, " \t")
			pat, remaining, err := consumeQuoted(rest)
			if err != nil {
				return nil, fmt.Errorf("-t: %w", err)
			}
			m.Pattern = pat
			rest = remaining
		case 'p':
			rest = strings.TrimLeft(rest, " \t")
			tok, remaining, _ := consumeWord(rest)
			n, err := fmt.Sscanf(tok, "%d", &m.Priority)
			if err != nil || n == 0 {
				return nil, fmt.Errorf("-p: not an integer: %q", tok)
			}
			rest = remaining
		case 'c':
			rest = strings.TrimLeft(rest, " \t")
			tok, remaining, _ := consumeWord(rest)
			fmt.Sscanf(tok, "%d", &m.Prob) //nolint:errcheck
			rest = remaining
		case 'n':
			rest = strings.TrimLeft(rest, " \t")
			tok, remaining, _ := consumeWord(rest)
			var n int
			if _, err := fmt.Sscanf(tok, "%d", &n); err == nil {
				if n < 0 {
					n = 1
				}
				m.Shots = n
			}
			rest = remaining
		case 'F':
			m.Fallthru = true
		case 'i', 'I':
			m.Invisible = true
		case 'E':
			m.Enabled = false
		case 'w':
			rest = strings.TrimLeft(rest, " \t")
			w, remaining, _ := consumeQuoted(rest)
			m.World = w
			rest = remaining
		case 'm':
			rest = strings.TrimLeft(rest, " \t")
			modeStr, remaining, _ := consumeWord(rest)
			switch strings.ToLower(modeStr) {
			case "glob":
				m.MatchMode = macro.MatchGlob
			case "substr", "simple":
				m.MatchMode = macro.MatchSubstr
			default:
				m.MatchMode = macro.MatchRegexp
			}
			rest = remaining
		case 'h':
			rest = strings.TrimLeft(rest, " \t")
			hookName, remaining, _ := consumeQuoted(rest)
			m.Type = macro.TypeHook
			m.Pattern = strings.ToUpper(hookName)
			rest = remaining
		default:
			// Unknown flag — skip its value if followed by a quoted/bare word.
			rest = strings.TrimLeft(rest, " \t")
			if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
				_, remaining, _ := consumeQuoted(rest)
				rest = remaining
			}
		}
		rest = strings.TrimLeft(rest, " \t")
	}

	eq := strings.Index(rest, "=")
	if eq < 0 {
		return nil, fmt.Errorf("missing '=' in /def %q", args)
	}
	m.Name = strings.TrimSpace(rest[:eq])
	m.Body = strings.TrimSpace(rest[eq+1:])
	if m.Name == "" {
		return nil, fmt.Errorf("empty macro name")
	}
	return m, nil
}

// consumeQuoted reads a quoted string or bare word (until whitespace).
func consumeQuoted(s string) (string, string, error) {
	if len(s) == 0 {
		return "", s, nil
	}
	if s[0] == '"' || s[0] == '\'' {
		q := s[0]
		s = s[1:]
		i := strings.IndexByte(s, q)
		if i < 0 {
			return s, "", fmt.Errorf("unterminated quote")
		}
		return s[:i], s[i+1:], nil
	}
	return consumeWord(s)
}

func consumeWord(s string) (string, string, error) {
	i := 0
	for i < len(s) && s[i] != ' ' && s[i] != '\t' {
		i++
	}
	return s[:i], strings.TrimLeft(s[i:], " \t"), nil
}

// ---------------------------------------------------------------------------
// /undef <name>
// ---------------------------------------------------------------------------

func cmdUndef(ctx *Context, args string) error {
	if ctx.UndefMacro == nil {
		return fmt.Errorf("/undef: not available")
	}
	name := strings.TrimSpace(args)
	if name == "" {
		return fmt.Errorf("usage: /undef <name>")
	}
	if !ctx.UndefMacro(name) {
		return fmt.Errorf("/undef: no macro named %q", name)
	}
	ctx.output(fmt.Sprintf("Removed %s.", name))
	return nil
}

// ---------------------------------------------------------------------------
// /list
// ---------------------------------------------------------------------------

func cmdList(ctx *Context, args string) error {
	if ctx.ListMacros == nil {
		ctx.output("(macro list not available)")
		return nil
	}
	lines := ctx.ListMacros()
	if len(lines) == 0 {
		ctx.output("No macros defined.")
		return nil
	}
	sort.Strings(lines)
	for _, l := range lines {
		ctx.output(l)
	}
	return nil
}

// ---------------------------------------------------------------------------
// /set [name=value]
// /unset <name>
// ---------------------------------------------------------------------------

func cmdSet(ctx *Context, args string) error {
	if args == "" {
		return cmdListVar(ctx, "")
	}
	name, val, found := strings.Cut(args, "=")
	if !found {
		return fmt.Errorf("usage: /set name=value")
	}
	if ctx.Setvar != nil {
		ctx.Setvar(strings.TrimSpace(name), strings.TrimSpace(val))
	}
	ctx.output(fmt.Sprintf("%s = %s", strings.TrimSpace(name), strings.TrimSpace(val)))
	return nil
}

func cmdUnset(ctx *Context, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		return fmt.Errorf("usage: /unset <name>")
	}
	if ctx.Setvar != nil {
		ctx.Setvar(name, "")
	}
	ctx.output(fmt.Sprintf("Unset %s.", name))
	return nil
}

// ---------------------------------------------------------------------------
// /log [file]  — start/stop logging
// ---------------------------------------------------------------------------

func cmdLog(ctx *Context, args string) error {
	if ctx.IsRestricted != nil && ctx.IsRestricted("FILE") && strings.TrimSpace(args) != "" {
		return fmt.Errorf("/log: restricted (FILE)")
	}
	path := strings.TrimSpace(args)
	if path == "" {
		// Toggle off.
		if ctx.StopLog != nil {
			ctx.StopLog()
			ctx.output("Logging stopped.")
		}
		return nil
	}
	if ctx.StartLog == nil {
		return fmt.Errorf("/log: not available")
	}
	if err := ctx.StartLog(path); err != nil {
		return fmt.Errorf("/log %s: %w", path, err)
	}
	ctx.output(fmt.Sprintf("Logging to %s.", path))
	return nil
}

// ---------------------------------------------------------------------------
// /recall [pattern]
// ---------------------------------------------------------------------------

func cmdRecall(ctx *Context, args string) error {
	if ctx.SearchHistory == nil {
		return fmt.Errorf("/recall: not available")
	}
	lines, err := ctx.SearchHistory(strings.TrimSpace(args))
	if err != nil {
		return fmt.Errorf("/recall: %w", err)
	}
	if len(lines) == 0 {
		ctx.output("(nothing found)")
		return nil
	}
	for _, l := range lines {
		ctx.output(l)
	}
	return nil
}

// ---------------------------------------------------------------------------
// /load <file>  — import .tf script
// ---------------------------------------------------------------------------

func cmdLoad(ctx *Context, args string) error {
	if ctx.IsRestricted != nil && ctx.IsRestricted("FILE") {
		return fmt.Errorf("/load: restricted (FILE)")
	}
	path := strings.TrimSpace(args)
	if path == "" {
		return fmt.Errorf("usage: /load <file>")
	}
	if err := validateScriptPath(path, ".tf", ".gf"); err != nil {
		return fmt.Errorf("/load: %w", err)
	}
	if ctx.LoadFile == nil {
		return fmt.Errorf("/load: not available")
	}
	if err := ctx.LoadFile(path); err != nil {
		return fmt.Errorf("/load %s: %w", path, err)
	}
	ctx.output(fmt.Sprintf("Loaded %s.", path))
	return nil
}

// ---------------------------------------------------------------------------
// /save [path]
// ---------------------------------------------------------------------------

func cmdSave(ctx *Context, args string) error {
	if ctx.IsRestricted != nil && ctx.IsRestricted("FILE") {
		return fmt.Errorf("/save: restricted (FILE)")
	}
	path := strings.TrimSpace(args)
	if path == "" {
		path = "macros.json"
	}
	if err := validateScriptPath(path, ".json"); err != nil {
		return fmt.Errorf("/save: %w", err)
	}
	if ctx.SaveMacros == nil {
		return fmt.Errorf("/save: not available")
	}
	if err := ctx.SaveMacros(path); err != nil {
		return err
	}
	ctx.output(fmt.Sprintf("Macros saved to %s.", path))
	return nil
}

// ---------------------------------------------------------------------------
// /quit
// ---------------------------------------------------------------------------

func cmdQuit(ctx *Context, _ string) error {
	if ctx.Quit != nil {
		ctx.Quit()
	}
	return nil
}

// ---------------------------------------------------------------------------
// /gag [-t pattern] name=
// /hilite [-t pattern] [-a attr] name=attr
// /substitute [-t pattern] name=replacement
// ---------------------------------------------------------------------------

func cmdGag(ctx *Context, args string) error {
	if ctx.DefMacro == nil {
		return fmt.Errorf("/gag: not available")
	}
	m, err := parseDef(args)
	if err != nil {
		return fmt.Errorf("/gag: %w", err)
	}
	m.Type = macro.TypeGag
	if err := ctx.DefMacro(m); err != nil {
		return fmt.Errorf("/gag %s: %w", m.Name, err)
	}
	ctx.output(fmt.Sprintf("Gag %s defined.", m.Name))
	return nil
}

func cmdHilite(ctx *Context, args string) error {
	if ctx.DefMacro == nil {
		return fmt.Errorf("/hilite: not available")
	}
	m, err := parseDef(args)
	if err != nil {
		return fmt.Errorf("/hilite: %w", err)
	}
	m.Type = macro.TypeHilite
	if err := ctx.DefMacro(m); err != nil {
		return fmt.Errorf("/hilite %s: %w", m.Name, err)
	}
	ctx.output(fmt.Sprintf("Hilite %s defined.", m.Name))
	return nil
}

func cmdSubstitute(ctx *Context, args string) error {
	if ctx.DefMacro == nil {
		return fmt.Errorf("/substitute: not available")
	}
	m, err := parseDef(args)
	if err != nil {
		return fmt.Errorf("/substitute: %w", err)
	}
	m.Type = macro.TypeSubstitute
	if err := ctx.DefMacro(m); err != nil {
		return fmt.Errorf("/substitute %s: %w", m.Name, err)
	}
	ctx.output(fmt.Sprintf("Substitute %s defined.", m.Name))
	return nil
}

// ---------------------------------------------------------------------------
// /set [name=value]  — fix listing stub
// /listvar [pattern]
// ---------------------------------------------------------------------------

func cmdListVar(ctx *Context, args string) error {
	if ctx.ListVars == nil {
		ctx.output("(variable listing not available)")
		return nil
	}
	vars := ctx.ListVars()
	if len(vars) == 0 {
		ctx.output("No variables set.")
		return nil
	}
	pat := strings.TrimSpace(args)
	keys := make([]string, 0, len(vars))
	for k := range vars {
		if pat == "" || strings.Contains(k, pat) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		ctx.output(fmt.Sprintf("  %s = %s", k, vars[k]))
	}
	return nil
}

// ---------------------------------------------------------------------------
// /addworld <name> <url> [char [pass]]
// /listworlds
// /saveworld [file]
// /unworld <name>
// ---------------------------------------------------------------------------

func cmdAddWorld(ctx *Context, args string) error {
	if ctx.IsRestricted != nil && ctx.IsRestricted("WORLD") {
		return fmt.Errorf("/addworld: restricted (WORLD)")
	}
	if ctx.AddWorld == nil {
		return fmt.Errorf("/addworld: not available")
	}
	parts := strings.Fields(args)
	if len(parts) < 2 {
		return fmt.Errorf("usage: /addworld <name> <url> [char [pass]]")
	}
	name, url := parts[0], parts[1]
	char, pass := "", ""
	if len(parts) >= 3 {
		char = parts[2]
	}
	if len(parts) >= 4 {
		pass = parts[3]
	}
	if err := ctx.AddWorld(name, url, char, pass); err != nil {
		return fmt.Errorf("/addworld %s: %w", name, err)
	}
	ctx.output(fmt.Sprintf("World %q added (%s). Use /connect %s to connect.", name, url, name))
	return nil
}

func cmdListWorlds(ctx *Context, _ string) error {
	if ctx.ListWorlds == nil {
		ctx.output("(world list not available)")
		return nil
	}
	worlds := ctx.ListWorlds()
	if len(worlds) == 0 {
		ctx.output("No worlds registered.")
		return nil
	}
	for _, w := range worlds {
		state := "──"
		if w.Connected {
			state = "CONNECTED"
		}
		ctx.output(fmt.Sprintf("  %-20s  %-12s  %s", w.Name, state, w.URL))
	}
	return nil
}

func cmdSaveWorld(ctx *Context, args string) error {
	path := strings.TrimSpace(args)
	if path == "" {
		path = "worlds.toml"
	}
	if err := validateScriptPath(path, ".toml"); err != nil {
		return fmt.Errorf("/saveworld: %w", err)
	}
	if ctx.SaveWorlds == nil {
		return fmt.Errorf("/saveworld: not available")
	}
	if err := ctx.SaveWorlds(path); err != nil {
		return fmt.Errorf("/saveworld: %w", err)
	}
	ctx.output(fmt.Sprintf("Worlds saved to %s.", path))
	return nil
}

func cmdUnworld(ctx *Context, args string) error {
	name := strings.TrimSpace(args)
	if name == "" {
		return fmt.Errorf("usage: /unworld <name>")
	}
	if ctx.RemoveWorld == nil {
		return fmt.Errorf("/unworld: not available")
	}
	if err := ctx.RemoveWorld(name); err != nil {
		return fmt.Errorf("/unworld %s: %w", name, err)
	}
	ctx.output(fmt.Sprintf("World %q removed.", name))
	return nil
}

// ---------------------------------------------------------------------------
// /restrict SHELL|FILE|WORLD
// ---------------------------------------------------------------------------

func cmdRestrict(ctx *Context, args string) error {
	what := strings.ToUpper(strings.TrimSpace(args))
	if what == "" {
		return fmt.Errorf("usage: /restrict SHELL|FILE|WORLD")
	}
	if ctx.Restrict != nil {
		ctx.Restrict(what)
	}
	ctx.output(fmt.Sprintf("Restricted: %s", what))
	return nil
}

// ---------------------------------------------------------------------------
// /repeat <duration> <body>   — fire body every duration ("5s", "500ms", "1m")
// /cancel <id>                — cancel a timer by ID
// /ps                         — list active timers
// ---------------------------------------------------------------------------

func cmdRepeat(ctx *Context, args string) error {
	if ctx.AddTimer == nil {
		return fmt.Errorf("/repeat: not available")
	}
	args = strings.TrimSpace(args)
	// Parse: first token is duration, rest is body.
	dur, body, ok := strings.Cut(args, " ")
	if !ok || strings.TrimSpace(body) == "" {
		return fmt.Errorf("usage: /repeat <duration> <body>  e.g. /repeat 5s /echo tick")
	}
	id, err := ctx.AddTimer(strings.TrimSpace(dur), true, strings.TrimSpace(body))
	if err != nil {
		return fmt.Errorf("/repeat: %w", err)
	}
	ctx.output(fmt.Sprintf("Timer %d started (every %s).", id, dur))
	return nil
}

func cmdCancel(ctx *Context, args string) error {
	if ctx.CancelTimer == nil {
		return fmt.Errorf("/cancel: not available")
	}
	var id int
	if _, err := fmt.Sscanf(strings.TrimSpace(args), "%d", &id); err != nil {
		return fmt.Errorf("usage: /cancel <timer-id>")
	}
	if !ctx.CancelTimer(id) {
		return fmt.Errorf("/cancel: no timer with id %d", id)
	}
	ctx.output(fmt.Sprintf("Timer %d cancelled.", id))
	return nil
}

func cmdPS(ctx *Context, _ string) error {
	if ctx.ListTimers == nil {
		ctx.output("(timer list not available)")
		return nil
	}
	timers := ctx.ListTimers()
	if len(timers) == 0 {
		ctx.output("No active timers.")
		return nil
	}
	ctx.output(fmt.Sprintf("  %-4s  %-10s  %-6s  %s", "ID", "Interval", "Repeat", "Next"))
	for _, t := range timers {
		repeat := "no"
		if t.Repeat {
			repeat = "yes"
		}
		ctx.output(fmt.Sprintf("  %-4d  %-10s  %-6s  %s", t.ID, t.Interval, repeat, t.Next))
	}
	return nil
}

// ---------------------------------------------------------------------------
// /js <file>         — load a JavaScript file
// /js -e <snippet>   — evaluate a JS expression
// /py <file>         — load a Python script
// ---------------------------------------------------------------------------

func cmdJS(ctx *Context, args string) error {
	if ctx.IsRestricted != nil && ctx.IsRestricted("SHELL") {
		return fmt.Errorf("/js: restricted (SHELL)")
	}
	args = strings.TrimSpace(args)
	if strings.HasPrefix(args, "-e ") || strings.HasPrefix(args, "-e\t") {
		// Inline eval.
		src := strings.TrimSpace(args[2:])
		if ctx.EvalJS == nil {
			return fmt.Errorf("/js: JS engine not available")
		}
		result, err := ctx.EvalJS(src)
		if err != nil {
			return fmt.Errorf("/js eval: %w", err)
		}
		if result != "" && result != "undefined" {
			ctx.output(result)
		}
		return nil
	}
	if args == "" {
		return fmt.Errorf("usage: /js <file>  or  /js -e <expr>")
	}
	if err := validateScriptPath(args, ".js", ".mjs"); err != nil {
		return fmt.Errorf("/js: %w", err)
	}
	if ctx.LoadJS == nil {
		return fmt.Errorf("/js: JS engine not available")
	}
	if err := ctx.LoadJS(args); err != nil {
		return fmt.Errorf("/js %s: %w", args, err)
	}
	ctx.output(fmt.Sprintf("JS loaded: %s", args))
	return nil
}

func cmdPy(ctx *Context, args string) error {
	if ctx.IsRestricted != nil && ctx.IsRestricted("SHELL") {
		return fmt.Errorf("/py: restricted (SHELL)")
	}
	args = strings.TrimSpace(args)
	if args == "" {
		return fmt.Errorf("usage: /py <file>")
	}
	if err := validateScriptPath(args, ".py"); err != nil {
		return fmt.Errorf("/py: %w", err)
	}
	if ctx.LoadPy == nil {
		return fmt.Errorf("/py: Python bridge not available")
	}
	if err := ctx.LoadPy(args); err != nil {
		return fmt.Errorf("/py %s: %w", args, err)
	}
	ctx.output(fmt.Sprintf("Python loaded: %s", args))
	return nil
}

// ---------------------------------------------------------------------------
// /help
// ---------------------------------------------------------------------------

func cmdHelp(ctx *Context, _ string) error {
	ctx.output("GoFugue — available commands:")
	ctx.output("  /connect <url> [name]     dial a world (mud://, ws://, wss://…)")
	ctx.output("  /dc [name]                disconnect a world")
	ctx.output("  /sw <name>                switch foreground world")
	ctx.output("  /send <text>              send text to current world")
	ctx.output("  /echo <text>              print to local output")
	ctx.output("  /def [-t pat] [-p N] [-c N] [-n N] [-F] [-i] [-m mode] name=body")
	ctx.output("  /gag [-t pattern] name=   suppress matching lines")
	ctx.output("  /hilite [-t pattern] name= highlight matching lines")
	ctx.output("  /substitute [-t pat] name=replacement  rewrite line text")
	ctx.output("  /undef <name>             remove a macro")
	ctx.output("  /list                     list macros")
	ctx.output("  /set name=value           set a variable")
	ctx.output("  /unset <name>             unset a variable")
	ctx.output("  /listvar [pattern]        list variables")
	ctx.output("  /log [file]               start/stop logging")
	ctx.output("  /recall [pattern]         search history")
	ctx.output("  /load <file>              import a .tf script")
	ctx.output("  /save [file]              save macros to JSON")
	ctx.output("  /addworld <n> <url>       register a world")
	ctx.output("  /listworlds               list registered worlds")
	ctx.output("  /saveworld [file]         save world definitions")
	ctx.output("  /unworld <name>           remove a world")
	ctx.output("  /repeat <dur> <body>      fire body every duration (5s, 500ms…)")
	ctx.output("  /cancel <id>              cancel a timer")
	ctx.output("  /ps                       list active timers")
	ctx.output("  /restrict SHELL|FILE|WORLD  restrict capabilities")
	ctx.output("  /js <file>                load a JavaScript script")
	ctx.output("  /js -e <expr>             evaluate a JS expression")
	ctx.output("  /py <file>                load a Python script")
	ctx.output("  /quit                     exit")
	return nil
}
