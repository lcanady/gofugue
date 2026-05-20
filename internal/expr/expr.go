// Package expr provides variable scoping and a simple expression evaluator.
// Expressions are enclosed in {}: e.g. {1 + strlen($name)}.
// Variables are referenced as $name or ${name}.
package expr

import (
	"strings"
	"sync"
)

// Scope holds a layered set of variables. Local scopes shadow global ones.
type Scope struct {
	mu     sync.RWMutex
	global map[string]string
	local  []map[string]string // stack; last = innermost
}

// NewScope returns an empty global scope.
func NewScope() *Scope {
	return &Scope{global: make(map[string]string)}
}

// Set sets a global variable.
func (s *Scope) Set(name, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.global[name] = value
}

// Get returns a variable value, searching local scopes first then global.
func (s *Scope) Get(name string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for i := len(s.local) - 1; i >= 0; i-- {
		if v, ok := s.local[i][name]; ok {
			return v, true
		}
	}
	v, ok := s.global[name]
	return v, ok
}

// Push opens a new local scope (e.g. for a macro call).
func (s *Scope) Push(vars map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.local = append(s.local, vars)
}

// All returns a copy of all global variables as a map.
func (s *Scope) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.global))
	for k, v := range s.global {
		out[k] = v
	}
	return out
}

// Pop removes the innermost local scope.
func (s *Scope) Pop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.local) > 0 {
		s.local = s.local[:len(s.local)-1]
	}
}

// Expand performs $variable substitution on text.
// Supports $name and ${name} forms; unknown variables expand to empty string.
// A bare '$' with no valid identifier following is passed through unchanged.
// Does not evaluate {expressions} — call ExpandFull for that.
func (s *Scope) Expand(text string) string {
	if !strings.ContainsRune(text, '$') {
		return text // fast path: nothing to expand
	}
	var b strings.Builder
	b.Grow(len(text))
	i := 0
	for i < len(text) {
		if text[i] != '$' {
			b.WriteByte(text[i])
			i++
			continue
		}
		i++ // skip '$'
		if i >= len(text) {
			b.WriteByte('$')
			break
		}
		var name string
		if text[i] == '{' {
			// ${name} form
			i++ // skip '{'
			j := i
			for j < len(text) && text[j] != '}' {
				j++
			}
			name = text[i:j]
			if j < len(text) {
				i = j + 1 // skip '}'
			} else {
				i = j
			}
		} else if isIdentStart(text[i]) {
			// $name form
			j := i
			for j < len(text) && isIdentChar(text[j]) {
				j++
			}
			name = text[i:j]
			i = j
		} else {
			// '$' not followed by identifier — pass through
			b.WriteByte('$')
			continue
		}
		if v, ok := s.Get(name); ok {
			b.WriteString(v)
		}
		// Unknown variable → empty string (TinyFugue behaviour)
	}
	return b.String()
}

func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

func isIdentChar(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

