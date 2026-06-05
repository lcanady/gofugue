// Package macro implements the trigger/alias/keybind engine.
// Macros are stored in a priority-sorted list; on each incoming world line the
// engine walks the list and fires all matching triggers in priority order.
package macro

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// Type classifies how a macro is activated.
type Type int

const (
	TypeTrigger   Type = iota // fires on matching world output
	TypeAlias                 // fires on matching user input (command prefix)
	TypeKeybind               // fires on a key sequence
	TypeHook                  // fires on a named lifecycle event
	TypeGag                   // suppresses matching lines (no body executed)
	TypeHilite                // highlights matching lines (body = attribute spec)
	TypeSubstitute            // replaces matching lines (body = replacement text)
)

// MatchMode controls how a trigger pattern is matched.
type MatchMode int

const (
	MatchRegexp MatchMode = iota // full regexp (default)
	MatchGlob                   // shell glob: * → .*, ? → .
	MatchSubstr                 // literal substring
	MatchSimple                 // alias for MatchSubstr
)

// Macro is a single named trigger/alias/keybind/hook definition.
type Macro struct {
	Name      string
	Type      Type
	MatchMode MatchMode
	Pattern   string // regex pattern (triggers/aliases) or hook name (hooks)
	Body      string // GoFugue script body to execute
	Priority  int    // higher = checked first
	World     string // empty = all worlds
	Enabled   bool
	Shots     int    // 0 = infinite; >0 = fires this many times then auto-undefines
	Prob      int    // 0-100 probability; 0 = always (same as 100)
	Fallthru  bool   // continue checking lower-priority triggers after match
	Invisible bool   // omit from /list output
	Condition string // TF -E expression; evaluated at fire time (empty = always fire)

	re *regexp.Regexp // compiled pattern (nil for hooks/keybinds/substr)
}

// TriggerFire holds one match result from FireTriggers.
type TriggerFire struct {
	Macro    *Macro
	Body     string
	Captures []string // index 0 = full match, 1+ = subgroups
}

// TriggerResult is the aggregated output of evaluating triggers against a line.
type TriggerResult struct {
	Fires  []TriggerFire
	Gagged bool // a TypeGag matched
}

// Engine stores and evaluates macros.
type Engine struct {
	mu       sync.RWMutex
	byName   map[string]*Macro
	triggers []*Macro // sorted by descending Priority
	aliases  []*Macro
	hooks    map[string][]*Macro // hook name → macros
	keybinds map[string]*Macro   // key sequence → macro

	// evalFunc is called to evaluate -E condition expressions.
	// Returns (true, nil) when the macro should fire. Set via SetEvalFunc.
	evalFunc func(expr string) (bool, error)
}

// New returns an empty macro Engine.
func New() *Engine {
	return &Engine{
		byName:   make(map[string]*Macro),
		hooks:    make(map[string][]*Macro),
		keybinds: make(map[string]*Macro),
	}
}

// SetEvalFunc sets the expression evaluator used for -E condition gates.
// fn should return (true, nil) when the macro condition is satisfied.
func (e *Engine) SetEvalFunc(fn func(expr string) (bool, error)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.evalFunc = fn
}

// Define adds or replaces a macro. Returns an error if the pattern is invalid.
func (e *Engine) Define(m *Macro) error {
	if (m.Type == TypeTrigger || m.Type == TypeGag || m.Type == TypeHilite ||
		m.Type == TypeSubstitute || m.Type == TypeAlias) && m.Pattern != "" {
		re, err := compilePattern(m.Pattern, m.MatchMode)
		if err != nil {
			return err
		}
		m.re = re
	}
	m.Enabled = true

	e.mu.Lock()
	defer e.mu.Unlock()

	if old, ok := e.byName[m.Name]; ok {
		e.remove(old)
	}
	e.byName[m.Name] = m
	e.insert(m)
	return nil
}

// compilePattern compiles a pattern according to the given match mode.
// Go's regexp package uses RE2 semantics, which guarantees linear-time matching
// and is therefore safe against ReDoS (catastrophic backtracking) by design.
func compilePattern(pattern string, mode MatchMode) (*regexp.Regexp, error) {
	switch mode {
	case MatchGlob:
		return regexp.Compile(globToRegexp(pattern))
	case MatchSubstr, MatchSimple:
		return regexp.Compile(regexp.QuoteMeta(pattern))
	default: // MatchRegexp
		return regexp.Compile(pattern)
	}
}

// globToRegexp converts a shell-glob pattern to a regexp string.
// * → .*, ? → ., [ → [, ] → ], everything else is quoted.
func globToRegexp(glob string) string {
	var b strings.Builder
	b.WriteString("(?i)") // globs are case-insensitive by TF convention
	i := 0
	for i < len(glob) {
		switch glob[i] {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		case '[':
			// pass character classes through literally
			j := i
			for j < len(glob) && glob[j] != ']' {
				j++
			}
			if j < len(glob) {
				b.WriteString(glob[i : j+1])
				i = j + 1
				continue
			}
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		default:
			b.WriteString(regexp.QuoteMeta(string(glob[i])))
		}
		i++
	}
	return b.String()
}

// Undefine removes a macro by name.
func (e *Engine) Undefine(name string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	m, ok := e.byName[name]
	if !ok {
		return false
	}
	delete(e.byName, name)
	e.remove(m)
	return true
}

// FireTriggers evaluates all triggers against line, returning every match.
// Execution stops after the first non-fallthrough match, unless Fallthru is set.
// Shots-limited macros are decremented and auto-removed when exhausted.
// Returns a TriggerResult summarising all fires and whether the line is gagged.
func (e *Engine) FireTriggers(world, line string) TriggerResult {
	e.mu.Lock()

	var result TriggerResult
	var toRemove []string // names of shot-exhausted macros to remove after unlock

	for _, m := range e.triggers {
		if !m.Enabled {
			continue
		}
		if m.World != "" && m.World != world {
			continue
		}
		if m.re == nil {
			continue
		}

		// Condition gate: -E expression evaluated at fire time.
		if m.Condition != "" && e.evalFunc != nil {
			ok, err := e.evalFunc(m.Condition)
			if err != nil || !ok {
				continue
			}
		}

		// Probability check (0 = always fire).
		if m.Prob > 0 {
			v, err := rand.Int(rand.Reader, big.NewInt(100))
			if err == nil && int(v.Int64()) >= m.Prob {
				continue
			}
		}

		// Substring match optimization: check strings.Contains before running regex.
		if (m.MatchMode == MatchSubstr || m.MatchMode == MatchSimple) && !strings.Contains(line, m.Pattern) {
			continue
		}

		sub := m.re.FindStringSubmatch(line)
		if sub == nil {
			continue
		}

		// Shots countdown.
		if m.Shots > 0 {
			m.Shots--
			if m.Shots == 0 {
				toRemove = append(toRemove, m.Name)
			}
		}

		fire := TriggerFire{Macro: m, Body: m.Body, Captures: sub}

		switch m.Type {
		case TypeGag:
			result.Gagged = true
			result.Fires = append(result.Fires, fire)
		case TypeHilite, TypeSubstitute, TypeTrigger:
			result.Fires = append(result.Fires, fire)
		}

		if !m.Fallthru {
			break
		}
	}
	e.mu.Unlock()

	// Remove exhausted shot-limited macros outside the main lock.
	if len(toRemove) > 0 {
		e.mu.Lock()
		for _, name := range toRemove {
			if mm, ok := e.byName[name]; ok {
				delete(e.byName, name)
				e.remove(mm)
			}
		}
		e.mu.Unlock()
	}

	return result
}

// MatchTrigger returns the body and capture groups of the first matching trigger
// for the given world line. Kept for backwards compatibility with tests.
func (e *Engine) MatchTrigger(world, line string) (body string, captures []string) {
	result := e.FireTriggers(world, line)
	for _, f := range result.Fires {
		if f.Macro.Type == TypeTrigger {
			return f.Body, f.Captures
		}
	}
	return "", nil
}

// MatchAlias returns the expanded body if the input matches an alias prefix.
func (e *Engine) MatchAlias(world, input string) (body string, ok bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, m := range e.aliases {
		if !m.Enabled {
			continue
		}
		if m.World != "" && m.World != world {
			continue
		}
		if m.re != nil && m.re.MatchString(input) {
			return m.Body, true
		}
	}
	return "", false
}

// List returns a human-readable description of every non-invisible macro.
func (e *Engine) List() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	out := make([]string, 0, len(e.byName))
	for _, m := range e.byName {
		if m.Invisible {
			continue
		}
		kind := typeLabel(m.Type)
		line := fmt.Sprintf("[%s] %s", kind, m.Name)
		if m.Pattern != "" {
			line += fmt.Sprintf(" /%s/", m.Pattern)
		}
		line += " = " + m.Body
		out = append(out, line)
	}
	return out
}

func typeLabel(t Type) string {
	switch t {
	case TypeAlias:
		return "alias"
	case TypeHook:
		return "hook"
	case TypeKeybind:
		return "keybind"
	case TypeGag:
		return "gag"
	case TypeHilite:
		return "hilite"
	case TypeSubstitute:
		return "substitute"
	default:
		return "trigger"
	}
}

// FireHook returns bodies of all macros registered for the named hook.
func (e *Engine) FireHook(world, hookName string) []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	var bodies []string
	for _, m := range e.hooks[hookName] {
		if !m.Enabled {
			continue
		}
		if m.World != "" && m.World != world {
			continue
		}
		bodies = append(bodies, m.Body)
	}
	return bodies
}

// insert adds m to the appropriate index (caller holds write lock).
func (e *Engine) insert(m *Macro) {
	switch m.Type {
	case TypeTrigger, TypeGag, TypeHilite, TypeSubstitute:
		e.triggers = append(e.triggers, m)
		sort.Slice(e.triggers, func(i, j int) bool {
			return e.triggers[i].Priority > e.triggers[j].Priority
		})
	case TypeAlias:
		e.aliases = append(e.aliases, m)
	case TypeHook:
		e.hooks[m.Pattern] = append(e.hooks[m.Pattern], m)
	case TypeKeybind:
		e.keybinds[m.Pattern] = m
	}
}

// remove deletes m from the index (caller holds write lock).
func (e *Engine) remove(m *Macro) {
	removeFn := func(sl []*Macro, target *Macro) []*Macro {
		for i, x := range sl {
			if x == target {
				return append(sl[:i], sl[i+1:]...)
			}
		}
		return sl
	}
	switch m.Type {
	case TypeTrigger, TypeGag, TypeHilite, TypeSubstitute:
		e.triggers = removeFn(e.triggers, m)
	case TypeAlias:
		e.aliases = removeFn(e.aliases, m)
	case TypeHook:
		e.hooks[m.Pattern] = removeFn(e.hooks[m.Pattern], m)
	case TypeKeybind:
		delete(e.keybinds, m.Pattern)
	}
}
