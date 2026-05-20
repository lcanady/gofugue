// Package compat provides a best-effort importer for TinyFugue .tf script files.
// It handles the most common directives (%def, %alias, %set, %key, %hook) and
// warns on unsupported constructs rather than failing hard.
package compat

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/kumakun/gofugue/internal/macro"
)

// Result holds parsed macros and warnings from a .tf file import.
type Result struct {
	Macros   []*macro.Macro
	Warnings []string
	// KeyBindings contains key→body pairs from /key directives.
	KeyBindings []KeyBinding
	// Settings contains name→value pairs from /set directives.
	Settings map[string]string
}

// KeyBinding maps a key name to a body.
type KeyBinding struct {
	Key  string
	Body string
}

// ImportReader parses a TinyFugue .tf script from r and returns the result.
func ImportReader(r io.Reader) (*Result, error) {
	res := &Result{Settings: make(map[string]string)}
	scanner := bufio.NewScanner(r)
	lineno := 0

	for scanner.Scan() {
		lineno++
		line := strings.TrimSpace(scanner.Text())

		// skip blank lines and TF comments (;)
		if line == "" || strings.HasPrefix(line, ";") {
			continue
		}

		directive, rest, _ := strings.Cut(line, " ")
		directive = strings.ToLower(directive)

		switch directive {
		case "%def", "/def":
			m, err := parseDef(rest)
			if err != nil {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("line %d: /def parse error: %v", lineno, err))
				continue
			}
			res.Macros = append(res.Macros, m)

		case "%alias", "/alias":
			m, err := parseAlias(rest)
			if err != nil {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("line %d: /alias parse error: %v", lineno, err))
				continue
			}
			res.Macros = append(res.Macros, m)

		case "%gag", "/gag":
			m, err := parseDef(rest)
			if err != nil {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("line %d: /gag parse error: %v", lineno, err))
				continue
			}
			m.Type = macro.TypeGag
			res.Macros = append(res.Macros, m)

		case "%hilite", "/hilite":
			m, err := parseDef(rest)
			if err != nil {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("line %d: /hilite parse error: %v", lineno, err))
				continue
			}
			m.Type = macro.TypeHilite
			res.Macros = append(res.Macros, m)

		case "%set", "/set":
			name, val, found := strings.Cut(rest, "=")
			if !found {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("line %d: /set %q: expected name=value", lineno, rest))
				continue
			}
			res.Settings[strings.TrimSpace(name)] = strings.TrimSpace(val)

		case "%key", "/key":
			// /key <keyname>=<body>
			eq := strings.Index(rest, "=")
			if eq < 0 {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("line %d: /key %q: expected key=body", lineno, rest))
				continue
			}
			res.KeyBindings = append(res.KeyBindings, KeyBinding{
				Key:  strings.TrimSpace(rest[:eq]),
				Body: rest[eq+1:],
			})

		default:
			if strings.HasPrefix(line, "%") || strings.HasPrefix(line, "/") {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("line %d: unsupported directive %q", lineno, directive))
			}
		}
	}
	return res, scanner.Err()
}

// parseDef parses the arguments of a /def directive.
//
// TinyFugue /def syntax:
//
//	/def [-t"pattern"] [-p N] [-c N] [-n N] [-F] [-i] [-E] [-w world] [-m mode] [-h hook] name=body
//
// Flags may appear in any order before name=body.
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
			pat, remaining, err := consumeQuotedOrWord(rest)
			if err != nil {
				return nil, fmt.Errorf("/def -t: %w", err)
			}
			m.Pattern = pat
			rest = remaining

		case 'p':
			rest = strings.TrimLeft(rest, " \t")
			numStr, remaining, _ := consumeQuotedOrWord(rest)
			n, err := strconv.Atoi(numStr)
			if err != nil {
				return nil, fmt.Errorf("/def -p: expected integer, got %q", numStr)
			}
			m.Priority = n
			rest = remaining

		case 'c':
			// -c N — probability 0-100
			rest = strings.TrimLeft(rest, " \t")
			numStr, remaining, _ := consumeQuotedOrWord(rest)
			n, err := strconv.Atoi(numStr)
			if err == nil {
				m.Prob = n
			}
			rest = remaining

		case 'n':
			// -n N — fire N times then auto-undef (TF: -1=one-shot encoded as 1)
			rest = strings.TrimLeft(rest, " \t")
			numStr, remaining, _ := consumeQuotedOrWord(rest)
			n, err := strconv.Atoi(numStr)
			if err == nil {
				if n < 0 {
					n = 1 // TF -1 means one-shot
				}
				m.Shots = n
			}
			rest = remaining

		case 'F':
			m.Fallthru = true

		case 'i', 'I':
			m.Invisible = true

		case 'E':
			// -E disables at define time (TF semantics: define but disable)
			m.Enabled = false

		case 'w':
			rest = strings.TrimLeft(rest, " \t")
			w, remaining, _ := consumeQuotedOrWord(rest)
			m.World = w
			rest = remaining

		case 'm':
			// -m <mode>: regexp, glob, substr, simple
			rest = strings.TrimLeft(rest, " \t")
			modeStr, remaining, _ := consumeQuotedOrWord(rest)
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
			// -h <hookname> — convert def into a hook macro
			rest = strings.TrimLeft(rest, " \t")
			hookName, remaining, _ := consumeQuotedOrWord(rest)
			m.Type = macro.TypeHook
			m.Pattern = strings.ToUpper(hookName)
			rest = remaining

		case 'a', 'f':
			// -a <attr> / -f <attr> — attribute/colour spec for hilite; stored in Body when TypeHilite
			rest = strings.TrimLeft(rest, " \t")
			_, remaining, _ := consumeQuotedOrWord(rest)
			rest = remaining

		default:
			// Unknown flag — skip its value if followed by a quoted string or word.
			rest = strings.TrimLeft(rest, " \t")
			if len(rest) > 0 && (rest[0] == '"' || rest[0] == '\'') {
				_, remaining, _ := consumeQuotedOrWord(rest)
				rest = remaining
			}
		}
		rest = strings.TrimLeft(rest, " \t")
	}

	// For hook type, name=body may or may not have a pattern.
	if m.Type == macro.TypeHook {
		// "name=body" remaining
		eq := strings.Index(rest, "=")
		if eq < 0 {
			return nil, fmt.Errorf("missing '=' in /def %q", args)
		}
		m.Name = strings.TrimSpace(rest[:eq])
		m.Body = strings.TrimSpace(rest[eq+1:])
		if m.Name == "" {
			return nil, fmt.Errorf("missing name in /def %q", args)
		}
		return m, nil
	}

	// Remaining is "name=body".
	eq := strings.Index(rest, "=")
	if eq < 0 {
		return nil, fmt.Errorf("missing '=' in /def %q", args)
	}
	m.Name = strings.TrimSpace(rest[:eq])
	m.Body = strings.TrimSpace(rest[eq+1:])
	if m.Name == "" {
		return nil, fmt.Errorf("missing name in /def %q", args)
	}
	return m, nil
}

// consumeQuotedOrWord reads a quoted string (single or double) or a bare word
// from src, returning (value, remainder, error).
func consumeQuotedOrWord(src string) (string, string, error) {
	if len(src) == 0 {
		return "", src, nil
	}
	if src[0] == '"' || src[0] == '\'' {
		q := src[0]
		src = src[1:]
		i := strings.IndexByte(src, q)
		if i < 0 {
			return src, "", fmt.Errorf("unclosed quote")
		}
		return src[:i], src[i+1:], nil
	}
	// Bare word: read until space or '='.
	i := 0
	for i < len(src) && src[i] != ' ' && src[i] != '\t' && src[i] != '=' {
		i++
	}
	return src[:i], src[i:], nil
}

func parseAlias(args string) (*macro.Macro, error) {
	eq := strings.Index(args, "=")
	if eq < 0 {
		return nil, fmt.Errorf("missing '=' in /alias %q", args)
	}
	m := &macro.Macro{
		Type:    macro.TypeAlias,
		Name:    strings.TrimSpace(args[:eq]),
		Pattern: strings.TrimSpace(args[:eq]), // alias name IS the match pattern
		Body:    strings.TrimSpace(args[eq+1:]),
	}
	return m, nil
}
