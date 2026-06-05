// Package js provides the JavaScript scripting bridge using goja (pure Go V8-compatible).
// Scripts access GoFugue via the global `tf` object.
//
// API surface exposed to scripts:
//
//	tf.send(world, text)           — send text to a world
//	tf.echo(text)                  — print to local output
//	tf.world()                     — returns foreground world name
//	tf.getvar(name)                — read a GoFugue scope variable
//	tf.setvar(name, value)         — write a GoFugue scope variable
//	tf.on(hookName, fn)            — subscribe to a lifecycle hook (CONNECT, etc.)
//	tf.def(name, pattern, fn)      — register a trigger (fn(world, line, caps))
//	tf.undef(name)                 — remove a trigger registered via tf.def
//	tf.log(text)                   — alias for tf.echo
//
// The bridge subscribes to bus.EvWorldLineRendered directly; triggers run after
// gag/hilite/substitute are applied. All goja calls are serialised through the
// event-loop goroutine.
package js

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/config"
	"github.com/kumakun/gofugue/internal/script"
	"github.com/kumakun/gofugue/internal/timers"
)

// jsTrigger is a trigger registered via tf.def.
type jsTrigger struct {
	name     string
	re       *regexp.Regexp
	callable goja.Callable
}

// Bridge is the JS scripting engine for a session.
type Bridge struct {
	cb  script.Callbacks
	bus *bus.Bus

	runtime *goja.Runtime
	taskCh  chan func()
	stopCh  chan struct{}
	once    sync.Once
	ctx     context.Context

	// triggers registered by JS scripts (protected by the event loop — only
	// accessed from taskCh goroutine so no separate lock needed).
	triggers []*jsTrigger

	// timers backing tf.setTimeout / tf.setInterval
	timerPool *timers.Pool
}

// New creates a JS Bridge backed by the given event bus and callbacks.
func New(b *bus.Bus, cb script.Callbacks) *Bridge {
	return &Bridge{
		cb:     cb,
		bus:    b,
		taskCh: make(chan func(), 256),
		stopCh: make(chan struct{}),
	}
}

// Start launches the JS event loop goroutine and the bus subscriber.
// Must be called before Load.
func (br *Bridge) Start(ctx context.Context) {
	br.once.Do(func() {
		br.ctx = ctx
		br.timerPool = timers.New()
		br.runtime = goja.New()
		br.exposeAPI()
		go br.loop(ctx)
		go br.watchLines(ctx)
		go br.watchHooks(ctx)
		go br.watchGMCP(ctx)
	})
}

// Stop shuts down the event loop.
func (br *Bridge) Stop() {
	close(br.stopCh)
}

// scriptDeadline bounds any single RunString invocation. Without this
// guard a malicious or buggy script (`while(true){}` or a catastrophic
// backtracking regex via goja's regexp2-backed RegExp) wedges the
// single JS event-loop goroutine and stalls every trigger.
const scriptDeadline = 2 * time.Second

// runWithDeadline runs src on the goja runtime, interrupting it if it
// exceeds scriptDeadline. Must be called from the event-loop goroutine.
func (br *Bridge) runWithDeadline(src string) (goja.Value, error) {
	t := time.AfterFunc(scriptDeadline, func() {
		br.runtime.Interrupt("script deadline exceeded")
	})
	defer t.Stop()
	v, err := br.runtime.RunString(src)
	br.runtime.ClearInterrupt()
	return v, err
}

// isSubDir reports whether realPath is inside base (after symlink resolution).
func isSubDir(base, realPath string) bool {
	if base == "" {
		return false
	}
	realBase, err := filepath.EvalSymlinks(base)
	if err != nil {
		realBase = base
	}
	safeBase := realBase
	if !strings.HasSuffix(safeBase, string(filepath.Separator)) {
		safeBase += string(filepath.Separator)
	}
	return realPath == realBase || strings.HasPrefix(realPath, safeBase)
}

// Load reads and executes a JS file in the event loop.
// The file must reside under an allowed root: the gofugue config directory or
// the process working directory. Symlinks are resolved before the check.
func (br *Bridge) Load(path string) error {
	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("js: invalid path: %w", err)
	}
	realPath, err := filepath.EvalSymlinks(cleanPath)
	if err != nil {
		return fmt.Errorf("js: invalid path (symlink eval): %w", err)
	}

	configDir, _ := filepath.Abs(config.ConfigDir())
	cwd, _ := os.Getwd()
	if cwd != "" {
		cwd, _ = filepath.Abs(cwd)
	}

	if !isSubDir(configDir, realPath) && !isSubDir(cwd, realPath) {
		return fmt.Errorf("js: script path %q is outside of allowed directories", path)
	}

	src, err := os.ReadFile(realPath)
	if err != nil {
		return fmt.Errorf("js: %w", err)
	}
	errCh := make(chan error, 1)
	br.taskCh <- func() {
		_, e := br.runWithDeadline(string(src))
		errCh <- e
	}
	return <-errCh
}

// Eval runs an arbitrary JS snippet and returns the string result.
func (br *Bridge) Eval(src string) (string, error) {
	type result struct {
		s   string
		err error
	}
	resCh := make(chan result, 1)
	br.taskCh <- func() {
		v, err := br.runWithDeadline(src)
		if err != nil {
			resCh <- result{err: err}
			return
		}
		resCh <- result{s: v.String()}
	}
	r := <-resCh
	return r.s, r.err
}

// loop is the single-threaded JS event loop goroutine.
func (br *Bridge) loop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-br.stopCh:
			return
		case task, ok := <-br.taskCh:
			if !ok {
				return
			}
			br.safeRun(task)
		}
	}
}

// safeRun executes a task and recovers any unexpected Go panics that escape
// the goja runtime's own recovery (goja already recovers JS-level throws).
func (br *Bridge) safeRun(task func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("js: unexpected panic in event loop", "recovered", r)
		}
	}()
	task()
}

// watchLines subscribes to rendered world lines and runs matching triggers.
func (br *Bridge) watchLines(ctx context.Context) {
	sub := br.bus.Subscribe(256, bus.EvWorldLineRendered)
	defer sub.Cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-br.stopCh:
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			wl := ev.(bus.WorldRenderedEvent)
			if wl.Gagged {
				continue
			}
			line := wl.Text
			world := wl.WorldName
			// Post matching work to the event loop so goja stays single-threaded.
			br.taskCh <- func() {
				for _, tr := range br.triggers {
					caps := tr.re.FindStringSubmatch(line)
					if caps == nil {
						continue
					}
					capsArr := br.runtime.NewArray(len(caps))
					for i, c := range caps {
						capsArr.Set(fmt.Sprintf("%d", i), c) //nolint:errcheck
					}
					if _, err := tr.callable(goja.Undefined(),
						br.runtime.ToValue(world),
						br.runtime.ToValue(line),
						capsArr,
					); err != nil {
						slog.Warn("js trigger", "name", tr.name, "err", err)
					}
				}
			}
		}
	}
}

// watchGMCP delivers GMCP events to JS tf.on("GMCP:Module.Name", fn) callbacks.
// The callback receives (world, dataObject) where dataObject is the parsed JSON.
func (br *Bridge) watchGMCP(ctx context.Context) {
	sub := br.bus.Subscribe(128, bus.EvGMCP)
	defer sub.Cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-br.stopCh:
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			ge := ev.(bus.GMCPEvent)
			world := ge.WorldName
			module := ge.Module
			data := ge.Data
			br.taskCh <- func() {
				// Parse JSON data into a JS value.
				var jsData goja.Value
				if len(data) > 0 {
					v, err := br.runtime.RunString("(" + string(data) + ")")
					if err != nil {
						jsData = br.runtime.ToValue(string(data))
					} else {
						jsData = v
					}
				} else {
					jsData = goja.Undefined()
				}
				// Fire callbacks for "GMCP:Module.Name" and the wildcard "GMCP".
				for _, key := range []string{"GMCP:" + module, "GMCP"} {
					arr := br.runtime.Get("_gf_hook_" + key)
					if arr == nil || goja.IsUndefined(arr) {
						continue
					}
					obj, ok := arr.(*goja.Object)
					if !ok {
						continue
					}
					n := int(obj.Get("length").ToInteger())
					for i := 0; i < n; i++ {
						v := obj.Get(fmt.Sprintf("%d", i))
						if callable, ok := goja.AssertFunction(v); ok {
							if _, err := callable(goja.Undefined(),
								br.runtime.ToValue(world),
								br.runtime.ToValue(module),
								jsData,
							); err != nil {
								slog.Warn("js GMCP callback", "module", module, "err", err)
							}
						}
					}
				}
			}
		}
	}
}

// watchHooks delivers hook events to JS tf.on callbacks.
func (br *Bridge) watchHooks(ctx context.Context) {
	sub := br.bus.Subscribe(64, bus.EvHook)
	defer sub.Cancel()
	for {
		select {
		case <-ctx.Done():
			return
		case <-br.stopCh:
			return
		case ev, ok := <-sub.C:
			if !ok {
				return
			}
			he := ev.(bus.HookEvent)
			hookName := he.Name
			world := he.WorldName
			br.taskCh <- func() {
				key := "_gf_hook_" + hookName
				arr := br.runtime.Get(key)
				if arr == nil || goja.IsUndefined(arr) {
					return
				}
				obj, ok := arr.(*goja.Object)
				if !ok {
					return
				}
				length := obj.Get("length")
				if length == nil {
					return
				}
				n := int(length.ToInteger())
				for i := 0; i < n; i++ {
					v := obj.Get(fmt.Sprintf("%d", i))
					if callable, ok := goja.AssertFunction(v); ok {
						if _, err := callable(goja.Undefined(), br.runtime.ToValue(world)); err != nil {
							slog.Warn("js hook", "hook", hookName, "err", err)
						}
					}
				}
			}
		}
	}
}

// exposeAPI registers the `tf` global object inside goja.
func (br *Bridge) exposeAPI() {
	rt := br.runtime

	tf := rt.NewObject()

	// tf.send(world, text)
	tf.Set("send", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		world := call.Argument(0).String()
		text := call.Argument(1).String()
		if br.cb.Send != nil {
			if err := br.cb.Send(world, text); err != nil {
				panic(rt.NewGoError(err))
			}
		}
		return goja.Undefined()
	})

	// tf.echo(text) / tf.log(text)
	echoFn := func(call goja.FunctionCall) goja.Value {
		if br.cb.Echo != nil {
			br.cb.Echo(call.Argument(0).String())
		}
		return goja.Undefined()
	}
	tf.Set("echo", echoFn)  //nolint:errcheck
	tf.Set("log", echoFn)   //nolint:errcheck

	// tf.world()
	tf.Set("world", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		if br.cb.ForegroundWorld != nil {
			return rt.ToValue(br.cb.ForegroundWorld())
		}
		return rt.ToValue("")
	})

	// tf.getvar(name)
	tf.Set("getvar", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		if br.cb.Getvar != nil {
			if v, ok := br.cb.Getvar(call.Argument(0).String()); ok {
				return rt.ToValue(v)
			}
		}
		return goja.Undefined()
	})

	// tf.setvar(name, value)
	tf.Set("setvar", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		if br.cb.Setvar != nil {
			br.cb.Setvar(call.Argument(0).String(), call.Argument(1).String())
		}
		return goja.Undefined()
	})

	// tf.on(hookName, fn) — stores callbacks in _gf_hook_<NAME> arrays.
	tf.Set("on", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		hookName := call.Argument(0).String()
		fn := call.Argument(1)
		key := "_gf_hook_" + hookName
		existing := rt.Get(key)
		var arr *goja.Object
		if existing != nil && !goja.IsUndefined(existing) {
			arr = existing.(*goja.Object)
		} else {
			arr = rt.NewArray()
			rt.Set(key, arr) //nolint:errcheck
		}
		if push, ok := goja.AssertFunction(arr.Get("push")); ok {
			push(arr, fn) //nolint:errcheck
		}
		return goja.Undefined()
	})

	// tf.def(name, pattern, fn) — register a trigger pattern.
	tf.Set("def", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		name := call.Argument(0).String()
		pattern := call.Argument(1).String()
		fn := call.Argument(2)
		callable, ok := goja.AssertFunction(fn)
		if !ok {
			panic(rt.NewTypeError("tf.def: third argument must be a function"))
		}
		re, err := script.CompileRegexp(pattern)
		if err != nil {
			panic(rt.NewGoError(fmt.Errorf("tf.def %q: invalid pattern: %w", name, err)))
		}
		// Remove any existing trigger with the same name.
		br.removeTrigger(name)
		br.triggers = append(br.triggers, &jsTrigger{name: name, re: re, callable: callable})
		return goja.Undefined()
	})

	// tf.undef(name)
	tf.Set("undef", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		br.removeTrigger(call.Argument(0).String())
		return goja.Undefined()
	})

	// tf.setTimeout(ms, fn) / tf.clearTimeout(id)
	tf.Set("setTimeout", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		ms := call.Argument(0).ToInteger()
		fn := call.Argument(1)
		callable, ok := goja.AssertFunction(fn)
		if !ok {
			panic(rt.NewTypeError("tf.setTimeout: second argument must be a function"))
		}
		delay := time.Duration(ms) * time.Millisecond
		id := br.timerPool.Add(br.ctx, delay, false, func() {
			br.taskCh <- func() {
				if _, err := callable(goja.Undefined()); err != nil {
					slog.Warn("js setTimeout callback", "err", err)
				}
			}
		})
		return rt.ToValue(id)
	})

	// tf.setInterval(ms, fn) / tf.clearInterval(id)
	tf.Set("setInterval", func(call goja.FunctionCall) goja.Value { //nolint:errcheck
		ms := call.Argument(0).ToInteger()
		fn := call.Argument(1)
		callable, ok := goja.AssertFunction(fn)
		if !ok {
			panic(rt.NewTypeError("tf.setInterval: second argument must be a function"))
		}
		delay := time.Duration(ms) * time.Millisecond
		id := br.timerPool.Add(br.ctx, delay, true, func() {
			br.taskCh <- func() {
				if _, err := callable(goja.Undefined()); err != nil {
					slog.Warn("js setInterval callback", "err", err)
				}
			}
		})
		return rt.ToValue(id)
	})

	clearFn := func(call goja.FunctionCall) goja.Value {
		id := int(call.Argument(0).ToInteger())
		br.timerPool.Cancel(id)
		return goja.Undefined()
	}
	tf.Set("clearTimeout", clearFn)   //nolint:errcheck
	tf.Set("clearInterval", clearFn)  //nolint:errcheck

	rt.Set("tf", tf) //nolint:errcheck
}

// removeTrigger removes a named trigger from the bridge's list (must be called
// from the event loop goroutine).
func (br *Bridge) removeTrigger(name string) {
	for i, tr := range br.triggers {
		if tr.name == name {
			br.triggers = append(br.triggers[:i], br.triggers[i+1:]...)
			return
		}
	}
}
