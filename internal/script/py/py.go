// Package py provides the Python scripting bridge via a subprocess sidecar.
//
// GoFugue launches `python3 -u <bridge.py> <script.py>` and communicates via
// newline-delimited JSON over stdin/stdout.
//
// Protocol — Go → Python:
//
//	{"t":"event","name":"TRIGGER","world":"mud","line":"...","caps":["..."]}
//	{"t":"event","name":"HOOK","hook":"CONNECT","world":"mud"}
//	{"t":"load","path":"script.py"}
//
// Protocol — Python → Go:
//
//	{"t":"cmd","method":"send","world":"mud","text":"..."}
//	{"t":"cmd","method":"echo","text":"..."}
//	{"t":"cmd","method":"setvar","name":"x","value":"1"}
//	{"t":"cmd","method":"getvar","name":"x"}          ← Go replies with {"t":"reply",...}
//	{"t":"cmd","method":"def","name":"h1","pattern":"regex"}
//	{"t":"cmd","method":"undef","name":"h1"}
//
// The bridge subscribes to bus.EvWorldLineRendered and bus.EvHook internally and
// forwards matching events to the Python process.
package py

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"regexp"
	"sync"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/script"
)

var reCache, _ = lru.New[string, *regexp.Regexp](1000)

// pyTrigger tracks a pattern registered by Python.
type pyTrigger struct {
	name string
	re   *regexp.Regexp
}

// Bridge manages the Python subprocess and its event subscriptions.
type Bridge struct {
	cb       script.Callbacks
	bus      *bus.Bus
	bridgePy string // path to bridge.py

	mu        sync.Mutex
	cmd       *exec.Cmd
	enc       *json.Encoder
	triggers  []*pyTrigger
	watchOnce bool // true after bus watchers are started (survive subprocess restart)
}

// New creates a Python Bridge with the given bus, callbacks, and bridge.py path.
func New(b *bus.Bus, cb script.Callbacks, bridgePy string) *Bridge {
	return &Bridge{cb: cb, bus: b, bridgePy: bridgePy}
}

// Load starts the Python subprocess. If already running, it is killed and
// restarted (hot-reload) so the new script takes effect immediately.
func (br *Bridge) Load(ctx context.Context, scriptPath string) error {
	br.mu.Lock()
	// Kill existing subprocess if running.
	if br.cmd != nil {
		br.cmd.Process.Kill() //nolint:errcheck
		br.cmd = nil
		br.enc = nil
	}
	br.mu.Unlock()

	cmd := exec.CommandContext(ctx, "python3", "-u", br.bridgePy, scriptPath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("py: stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("py: stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("py: start: %w", err)
	}

	br.mu.Lock()
	br.cmd = cmd
	br.enc = json.NewEncoder(stdin)
	firstLoad := br.watchOnce
	br.watchOnce = true
	br.mu.Unlock()

	go br.readLoop(ctx, bufio.NewScanner(stdout))
	// Start bus watchers only on the first Load; they survive subprocess restarts.
	if !firstLoad {
		go br.watchLines(ctx)
		go br.watchHooks(ctx)
		go br.watchGMCP(ctx)
	}

	return nil
}

// Stop kills the Python subprocess.
func (br *Bridge) Stop() error {
	br.mu.Lock()
	defer br.mu.Unlock()
	if br.cmd == nil {
		return nil
	}
	err := br.cmd.Process.Kill()
	br.cmd = nil
	br.enc = nil
	return err
}

// watchLines subscribes to rendered world lines and forwards one TRIGGER event
// per matched trigger, each with its own capture groups.
func (br *Bridge) watchLines(ctx context.Context) {
	sub := br.bus.Subscribe(256, bus.EvWorldLineRendered)
	defer sub.Cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			wl := ev.(bus.WorldRenderedEvent)
			if wl.Gagged {
				continue
			}
			br.mu.Lock()
			enc := br.enc
			type match struct {
				name string
				caps []string
			}
			var matches []match
			for _, tr := range br.triggers {
				if c := tr.re.FindStringSubmatch(wl.Text); c != nil {
					matches = append(matches, match{name: tr.name, caps: c})
				}
			}
			br.mu.Unlock()
			if enc == nil {
				continue
			}
			for _, m := range matches {
				enc.Encode(map[string]any{ //nolint:errcheck
					"t":      "event",
					"name":   "TRIGGER",
					"world":  wl.WorldName,
					"line":   wl.Text,
					"caps":   m.caps,
					"trigger": m.name,
				})
			}
		}
	}
}

// watchGMCP forwards GMCP events to Python as
// {"t":"event","name":"GMCP","module":"Char.Vitals","data":{...}}.
func (br *Bridge) watchGMCP(ctx context.Context) {
	sub := br.bus.Subscribe(128, bus.EvGMCP)
	defer sub.Cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			ge := ev.(bus.GMCPEvent)
			br.mu.Lock()
			enc := br.enc
			br.mu.Unlock()
			if enc != nil {
				enc.Encode(map[string]any{ //nolint:errcheck
					"t":      "event",
					"name":   "GMCP",
					"world":  ge.WorldName,
					"module": ge.Module,
					"data":   json.RawMessage(ge.Data),
				})
			}
		}
	}
}

// watchHooks subscribes to hook events and forwards them to Python.
func (br *Bridge) watchHooks(ctx context.Context) {
	sub := br.bus.Subscribe(64, bus.EvHook)
	defer sub.Cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			he := ev.(bus.HookEvent)
			br.mu.Lock()
			enc := br.enc
			br.mu.Unlock()
			if enc != nil {
				enc.Encode(map[string]any{ //nolint:errcheck
					"t":     "event",
					"name":  "HOOK",
					"hook":  he.Name,
					"world": he.WorldName,
				})
			}
		}
	}
}

// readLoop reads JSON-RPC commands from the Python process stdout.
func (br *Bridge) readLoop(ctx context.Context, s *bufio.Scanner) {
	for s.Scan() {
		var msg map[string]json.RawMessage
		if err := json.Unmarshal(s.Bytes(), &msg); err != nil {
			slog.Warn("py: bad message", "err", err)
			continue
		}
		var method string
		if v, ok := msg["method"]; ok {
			json.Unmarshal(v, &method) //nolint:errcheck
		}
		br.dispatch(method, msg)
	}
	if err := s.Err(); err != nil && ctx.Err() == nil {
		slog.Warn("py: reader error", "err", err)
	}
}

// dispatch handles one incoming command from Python.
func (br *Bridge) dispatch(method string, msg map[string]json.RawMessage) {
	str := func(key string) string {
		v, ok := msg[key]
		if !ok {
			return ""
		}
		var s string
		json.Unmarshal(v, &s) //nolint:errcheck
		return s
	}

	switch method {
	case "send":
		if br.cb.Send != nil {
			if err := br.cb.Send(str("world"), str("text")); err != nil {
				slog.Warn("py: send", "err", err)
			}
		}
	case "echo":
		if br.cb.Echo != nil {
			br.cb.Echo(str("text"))
		}
	case "setvar":
		if br.cb.Setvar != nil {
			br.cb.Setvar(str("name"), str("value"))
		}
	case "getvar":
		if br.cb.Getvar != nil {
			name := str("name")
			v, _ := br.cb.Getvar(name)
			br.mu.Lock()
			enc := br.enc
			br.mu.Unlock()
			if enc != nil {
				enc.Encode(map[string]any{"t": "reply", "name": name, "value": v}) //nolint:errcheck
			}
		}
	case "def":
		name, pattern := str("name"), str("pattern")

		var re *regexp.Regexp
		if v, ok := reCache.Get(pattern); ok {
			re = v
		} else {
			var err error
			re, err = regexp.Compile(pattern)
			if err != nil {
				slog.Warn("py: def bad pattern", "name", name, "err", err)
				return
			}
			reCache.Add(pattern, re)
		}

		br.mu.Lock()
		br.removeTriggerLocked(name)
		br.triggers = append(br.triggers, &pyTrigger{name: name, re: re})
		br.mu.Unlock()
	case "undef":
		br.mu.Lock()
		br.removeTriggerLocked(str("name"))
		br.mu.Unlock()
	default:
		slog.Warn("py: unknown method", "method", method)
	}
}

// removeTriggerLocked removes a trigger by name. Caller must hold mu.
func (br *Bridge) removeTriggerLocked(name string) {
	for i, tr := range br.triggers {
		if tr.name == name {
			br.triggers = append(br.triggers[:i], br.triggers[i+1:]...)
			return
		}
	}
}
