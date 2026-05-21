package expr

import (
	"crypto/rand"
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
)

var reCache *lru.Cache[string, *regexp.Regexp]

func init() {
	var err error
	reCache, err = lru.New[string, *regexp.Regexp](128)
	if err != nil {
		panic(fmt.Sprintf("failed to initialize regex lru cache: %v", err))
	}
}

// Eval evaluates a single expression string (the content inside {}).
// Values are represented as strings internally; numeric operations parse
// on demand. Returns the result as a string, or an error.
func Eval(expression string, s *Scope) (string, error) {
	p := &parser{src: strings.TrimSpace(expression), scope: s}
	val, err := p.parseExpr()
	if err != nil {
		return "", fmt.Errorf("expr %q: %w", expression, err)
	}
	return val, nil
}

// ExpandFull performs full expansion: $vars then {exprs} in one pass.
func ExpandFull(text string, s *Scope) (string, error) {
	// First expand $variables, then evaluate {expressions}.
	expanded := s.Expand(text)
	if !strings.ContainsRune(expanded, '{') {
		return expanded, nil
	}

	var b strings.Builder
	i := 0
	for i < len(expanded) {
		if expanded[i] != '{' {
			b.WriteByte(expanded[i])
			i++
			continue
		}
		// Find matching '}'.
		depth := 1
		j := i + 1
		for j < len(expanded) && depth > 0 {
			if expanded[j] == '{' {
				depth++
			} else if expanded[j] == '}' {
				depth--
			}
			j++
		}
		if depth != 0 {
			// Unterminated — pass through literally.
			b.WriteByte('{')
			i++
			continue
		}
		inner := expanded[i+1 : j-1]
		result, err := Eval(inner, s)
		if err != nil {
			return "", err
		}
		b.WriteString(result)
		i = j
	}
	return b.String(), nil
}

// ---------------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------------

type parser struct {
	src   string
	pos   int
	scope *Scope

	// captureGroups stores the last regmatch() captures (%0..%9).
	captureGroups []string
}

func (p *parser) peek() byte {
	p.skipWS()
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) skipWS() {
	for p.pos < len(p.src) && (p.src[p.pos] == ' ' || p.src[p.pos] == '\t') {
		p.pos++
	}
}

func (p *parser) consume() byte {
	if p.pos >= len(p.src) {
		return 0
	}
	c := p.src[p.pos]
	p.pos++
	return c
}

func (p *parser) expect(c byte) error {
	p.skipWS()
	if p.pos >= len(p.src) || p.src[p.pos] != c {
		return fmt.Errorf("expected %q at pos %d (got %q)", c, p.pos, p.src[p.pos:])
	}
	p.pos++
	return nil
}

// parseExpr handles || and && at the top level.
func (p *parser) parseExpr() (string, error) {
	return p.parseOr()
}

func (p *parser) parseOr() (string, error) {
	left, err := p.parseAnd()
	if err != nil {
		return "", err
	}
	for {
		p.skipWS()
		if p.pos+1 < len(p.src) && p.src[p.pos] == '|' && p.src[p.pos+1] == '|' {
			p.pos += 2
			right, err := p.parseAnd()
			if err != nil {
				return "", err
			}
			if toBool(left) || toBool(right) {
				left = "1"
			} else {
				left = "0"
			}
		} else {
			break
		}
	}
	return left, nil
}

func (p *parser) parseAnd() (string, error) {
	left, err := p.parseCmp()
	if err != nil {
		return "", err
	}
	for {
		p.skipWS()
		if p.pos+1 < len(p.src) && p.src[p.pos] == '&' && p.src[p.pos+1] == '&' {
			p.pos += 2
			right, err := p.parseCmp()
			if err != nil {
				return "", err
			}
			if toBool(left) && toBool(right) {
				left = "1"
			} else {
				left = "0"
			}
		} else {
			break
		}
	}
	return left, nil
}

func (p *parser) parseCmp() (string, error) {
	left, err := p.parseAdd()
	if err != nil {
		return "", err
	}
	for {
		p.skipWS()
		if p.pos >= len(p.src) {
			break
		}
		var op string
		switch {
		case p.matchStr("=="):
			op = "=="
		case p.matchStr("!="):
			op = "!="
		case p.matchStr("<="):
			op = "<="
		case p.matchStr(">="):
			op = ">="
		case p.matchStr("<"):
			op = "<"
		case p.matchStr(">"):
			op = ">"
		default:
			return left, nil
		}
		right, err := p.parseAdd()
		if err != nil {
			return "", err
		}
		left = boolStr(cmpValues(left, right, op))
	}
	return left, nil
}

func (p *parser) parseAdd() (string, error) {
	left, err := p.parseMul()
	if err != nil {
		return "", err
	}
	for {
		p.skipWS()
		if p.pos >= len(p.src) {
			break
		}
		c := p.src[p.pos]
		if c == '+' || c == '-' || c == '.' {
			p.pos++
			right, err := p.parseMul()
			if err != nil {
				return "", err
			}
			switch c {
			case '+':
				left = numStr(toNum(left) + toNum(right))
			case '-':
				left = numStr(toNum(left) - toNum(right))
			case '.':
				left = left + right // string concat
			}
		} else {
			break
		}
	}
	return left, nil
}

func (p *parser) parseMul() (string, error) {
	left, err := p.parseUnary()
	if err != nil {
		return "", err
	}
	for {
		p.skipWS()
		if p.pos >= len(p.src) {
			break
		}
		c := p.src[p.pos]
		if c == '*' || c == '/' || c == '%' {
			p.pos++
			right, err := p.parseUnary()
			if err != nil {
				return "", err
			}
			switch c {
			case '*':
				left = numStr(toNum(left) * toNum(right))
			case '/':
				r := toNum(right)
				if r == 0 {
					return "", fmt.Errorf("division by zero")
				}
				left = numStr(toNum(left) / r)
			case '%':
				ri := int(toNum(right))
				if ri == 0 {
					return "", fmt.Errorf("modulo by zero")
				}
				left = fmt.Sprintf("%d", int(toNum(left))%ri)
			}
		} else {
			break
		}
	}
	return left, nil
}

func (p *parser) parseUnary() (string, error) {
	p.skipWS()
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("unexpected end of expression")
	}
	switch p.src[p.pos] {
	case '!':
		p.pos++
		val, err := p.parseUnary()
		if err != nil {
			return "", err
		}
		return boolStr(!toBool(val)), nil
	case '-':
		p.pos++
		val, err := p.parseUnary()
		if err != nil {
			return "", err
		}
		return numStr(-toNum(val)), nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (string, error) {
	p.skipWS()
	if p.pos >= len(p.src) {
		return "", fmt.Errorf("unexpected end of expression")
	}
	c := p.src[p.pos]

	// Parenthesised sub-expression.
	if c == '(' {
		p.pos++
		val, err := p.parseExpr()
		if err != nil {
			return "", err
		}
		if err := p.expect(')'); err != nil {
			return "", err
		}
		return val, nil
	}

	// String literals.
	if c == '"' || c == '\'' {
		return p.parseString(c)
	}

	// Numeric literals.
	if c >= '0' && c <= '9' {
		return p.parseNumber()
	}

	// Variable reference: $name or ${name}.
	if c == '$' {
		p.pos++
		name, err := p.parseVarName()
		if err != nil {
			return "", err
		}
		if v, ok := p.scope.Get(name); ok {
			return v, nil
		}
		return "", nil
	}

	// Identifier → function call or bare word.
	if isIdentStart(c) {
		ident := p.parseIdent()
		p.skipWS()
		if p.pos < len(p.src) && p.src[p.pos] == '(' {
			return p.callFunc(ident)
		}
		// Bare identifier: treat as variable name (TF convention).
		if v, ok := p.scope.Get(ident); ok {
			return v, nil
		}
		return ident, nil // pass through as string
	}

	return "", fmt.Errorf("unexpected character %q at pos %d", c, p.pos)
}

func (p *parser) parseString(quote byte) (string, error) {
	p.pos++ // skip opening quote
	var b strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == quote {
			p.pos++
			return b.String(), nil
		}
		if c == '\\' && p.pos+1 < len(p.src) {
			p.pos++
			switch p.src[p.pos] {
			case 'n':
				b.WriteByte('\n')
			case 't':
				b.WriteByte('\t')
			case '"', '\'', '\\':
				b.WriteByte(p.src[p.pos])
			default:
				// Preserve backslash for regex metacharacters like \w, \d, \s.
				b.WriteByte('\\')
				b.WriteByte(p.src[p.pos])
			}
		} else {
			b.WriteByte(c)
		}
		p.pos++
	}
	return "", fmt.Errorf("unterminated string")
}

func (p *parser) parseNumber() (string, error) {
	start := p.pos
	for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9') {
		p.pos++
	}
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.pos++
		for p.pos < len(p.src) && (p.src[p.pos] >= '0' && p.src[p.pos] <= '9') {
			p.pos++
		}
	}
	return p.src[start:p.pos], nil
}

func (p *parser) parseVarName() (string, error) {
	if p.pos < len(p.src) && p.src[p.pos] == '{' {
		p.pos++
		start := p.pos
		for p.pos < len(p.src) && p.src[p.pos] != '}' {
			p.pos++
		}
		name := p.src[start:p.pos]
		if p.pos < len(p.src) {
			p.pos++ // skip '}'
		}
		return name, nil
	}
	return p.parseIdent(), nil
}

func (p *parser) parseIdent() string {
	start := p.pos
	for p.pos < len(p.src) && (isIdentChar(p.src[p.pos]) || rune(p.src[p.pos]) > 127) {
		p.pos++
	}
	return p.src[start:p.pos]
}

func (p *parser) matchStr(s string) bool {
	if p.pos+len(s) <= len(p.src) && p.src[p.pos:p.pos+len(s)] == s {
		p.pos += len(s)
		return true
	}
	return false
}

// parseArgs parses a comma-separated argument list until ')'.
func (p *parser) parseArgs() ([]string, error) {
	if err := p.expect('('); err != nil {
		return nil, err
	}
	var args []string
	p.skipWS()
	if p.pos < len(p.src) && p.src[p.pos] == ')' {
		p.pos++
		return args, nil
	}
	for {
		val, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		args = append(args, val)
		p.skipWS()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unclosed function call")
		}
		if p.src[p.pos] == ')' {
			p.pos++
			return args, nil
		}
		if p.src[p.pos] != ',' {
			return nil, fmt.Errorf("expected ',' or ')' in args")
		}
		p.pos++
	}
}

// callFunc dispatches to built-in functions.
func (p *parser) callFunc(name string) (string, error) {
	switch strings.ToLower(name) {
	case "strlen":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("strlen: expected 1 arg")
		}
		return fmt.Sprintf("%d", len([]rune(args[0]))), nil

	case "substr":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) < 2 {
			return "", fmt.Errorf("substr: expected 2-3 args")
		}
		runes := []rune(args[0])
		start := int(toNum(args[1]))
		if start < 0 {
			start = max(0, len(runes)+start)
		}
		if start >= len(runes) {
			return "", nil
		}
		if len(args) == 3 {
			n := int(toNum(args[2]))
			end := start + n
			if end > len(runes) {
				end = len(runes)
			}
			return string(runes[start:end]), nil
		}
		return string(runes[start:]), nil

	case "tolower":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("tolower: expected 1 arg")
		}
		return strings.ToLower(args[0]), nil

	case "toupper":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("toupper: expected 1 arg")
		}
		return strings.ToUpper(args[0]), nil

	case "trim":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("trim: expected 1 arg")
		}
		return strings.TrimSpace(args[0]), nil

	case "strcmp":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 2 {
			return "", fmt.Errorf("strcmp: expected 2 args")
		}
		return fmt.Sprintf("%d", strings.Compare(args[0], args[1])), nil

	case "strcat":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		return strings.Join(args, ""), nil

	case "replace":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 3 {
			return "", fmt.Errorf("replace: expected 3 args (str, old, new)")
		}
		return strings.ReplaceAll(args[0], args[1], args[2]), nil

	case "pad":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) < 2 {
			return "", fmt.Errorf("pad: expected 2-3 args")
		}
		n := int(toNum(args[1]))
		padChar := " "
		if len(args) == 3 && len(args[2]) > 0 {
			padChar = args[2]
		}
		s := args[0]
		runes := []rune(s)
		for len(runes) < n {
			runes = append(runes, []rune(padChar)...)
		}
		return string(runes[:n]), nil

	case "time":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		_ = args
		return fmt.Sprintf("%d", time.Now().Unix()), nil

	case "ftime":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		format := "2006-01-02 15:04:05"
		if len(args) >= 1 && args[0] != "" {
			format = tfTimeFormat(args[0])
		}
		return time.Now().Format(format), nil

	case "rand":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		n := 100
		if len(args) >= 1 {
			n = int(toNum(args[0]))
		}
		if n <= 0 {
			n = 1
		}
		val, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
		if err != nil {
			return "0", nil // fallback or log error, return 0 is safe
		}
		return fmt.Sprintf("%d", val.Int64()), nil

	case "abs":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("abs: expected 1 arg")
		}
		v := toNum(args[0])
		if v < 0 {
			v = -v
		}
		return numStr(v), nil

	case "sqrt":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("sqrt: expected 1 arg")
		}
		return numStr(math.Sqrt(toNum(args[0]))), nil

	case "floor", "trunc":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("%s: expected 1 arg", name)
		}
		return fmt.Sprintf("%d", int(math.Floor(toNum(args[0])))), nil

	case "ceil":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("ceil: expected 1 arg")
		}
		return fmt.Sprintf("%d", int(math.Ceil(toNum(args[0])))), nil

	case "min":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) == 0 {
			return "0", nil
		}
		m := toNum(args[0])
		for _, a := range args[1:] {
			if v := toNum(a); v < m {
				m = v
			}
		}
		return numStr(m), nil

	case "max":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) == 0 {
			return "0", nil
		}
		m := toNum(args[0])
		for _, a := range args[1:] {
			if v := toNum(a); v > m {
				m = v
			}
		}
		return numStr(m), nil

	case "if":
		// Lazy: parse condition, then only parse the taken branch.
		if err := p.expect('('); err != nil {
			return "", err
		}
		cond, err := p.parseExpr()
		if err != nil {
			return "", err
		}
		if err := p.expect(','); err != nil {
			return "", err
		}
		if toBool(cond) {
			result, err := p.parseExpr()
			if err != nil {
				return "", err
			}
			// Skip else branch if present.
			p.skipWS()
			if p.pos < len(p.src) && p.src[p.pos] == ',' {
				p.pos++
				p.skipBranch()
			}
			if err := p.expect(')'); err != nil {
				return "", err
			}
			return result, nil
		}
		// Skip then branch.
		p.skipBranch()
		p.skipWS()
		if p.pos < len(p.src) && p.src[p.pos] == ',' {
			p.pos++
			result, err := p.parseExpr()
			if err != nil {
				return "", err
			}
			if err := p.expect(')'); err != nil {
				return "", err
			}
			return result, nil
		}
		return "", p.expect(')')

	case "regmatch":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 2 {
			return "", fmt.Errorf("regmatch: expected 2 args (pattern, string)")
		}
		var re *regexp.Regexp
		if cachedRe, ok := reCache.Get(args[0]); ok {
			re = cachedRe
		} else {
			re, err = regexp.Compile(args[0])
			if err != nil {
				return "", fmt.Errorf("regmatch: %w", err)
			}
			reCache.Add(args[0], re)
		}
		subs := re.FindStringSubmatch(args[1])
		if subs == nil {
			p.captureGroups = nil
			return "0", nil
		}
		p.captureGroups = subs
		// Store in scope as %0..%9.
		for i, s := range subs {
			if i > 9 {
				break
			}
			p.scope.Set(fmt.Sprintf("%d", i), s)
		}
		return "1", nil

	case "ascii":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 || len(args[0]) == 0 {
			return "0", nil
		}
		return fmt.Sprintf("%d", args[0][0]), nil

	case "char":
		args, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		if len(args) != 1 {
			return "", fmt.Errorf("char: expected 1 arg")
		}
		return string(rune(int(toNum(args[0])))), nil

	default:
		// Unknown function — consume args and return empty.
		_, err := p.parseArgs()
		if err != nil {
			return "", err
		}
		return "", fmt.Errorf("unknown function: %s", name)
	}
}

// skipBranch skips one expression without evaluating it, for the if() lazy form.
func (p *parser) skipBranch() {
	depth := 0
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '(' {
			depth++
		} else if c == ')' {
			if depth == 0 {
				return
			}
			depth--
		} else if c == ',' && depth == 0 {
			return
		}
		p.pos++
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func toNum(s string) float64 {
	s = strings.TrimSpace(s)
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

func numStr(f float64) string {
	if f == math.Trunc(f) && !math.IsInf(f, 0) {
		return fmt.Sprintf("%d", int64(f))
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

func toBool(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" || s == "0" {
		return false
	}
	return true
}

func boolStr(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func cmpValues(a, b, op string) bool {
	// Try numeric comparison first.
	an, aerr := strconv.ParseFloat(a, 64)
	bn, berr := strconv.ParseFloat(b, 64)
	if aerr == nil && berr == nil {
		switch op {
		case "==":
			return an == bn
		case "!=":
			return an != bn
		case "<":
			return an < bn
		case ">":
			return an > bn
		case "<=":
			return an <= bn
		case ">=":
			return an >= bn
		}
	}
	// Fall back to string comparison.
	cmp := strings.Compare(a, b)
	switch op {
	case "==":
		return cmp == 0
	case "!=":
		return cmp != 0
	case "<":
		return cmp < 0
	case ">":
		return cmp > 0
	case "<=":
		return cmp <= 0
	case ">=":
		return cmp >= 0
	}
	return false
}

// tfTimeFormat converts a strftime-like format string to Go time format.
func tfTimeFormat(tf string) string {
	replacer := strings.NewReplacer(
		"%Y", "2006",
		"%m", "01",
		"%d", "02",
		"%H", "15",
		"%M", "04",
		"%S", "05",
		"%p", "PM",
		"%A", "Monday",
		"%a", "Mon",
		"%B", "January",
		"%b", "Jan",
	)
	return replacer.Replace(tf)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
