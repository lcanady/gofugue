package expr_test

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/kumakun/gofugue/internal/expr"
)

func newScope(vars map[string]string) *expr.Scope {
	s := expr.NewScope()
	for k, v := range vars {
		s.Set(k, v)
	}
	return s
}

func mustEval(t *testing.T, expression string, vars map[string]string) string {
	t.Helper()
	s := newScope(vars)
	result, err := expr.Eval(expression, s)
	if err != nil {
		t.Fatalf("Eval(%q): %v", expression, err)
	}
	return result
}

func TestEval_Integer_Arithmetic(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"1+2", "3"},
		{"10-3", "7"},
		{"4*5", "20"},
		{"10/2", "5"},
		{"10%3", "1"},
		{"2+3*4", "14"},
		{"(2+3)*4", "20"},
	}
	for _, tc := range cases {
		if got := mustEval(t, tc.expr, nil); got != tc.want {
			t.Errorf("Eval(%q) = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

func TestEval_Comparisons(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"1==1", "1"}, {"1==2", "0"},
		{"1!=2", "1"}, {"3<5", "1"},
		{"5<3", "0"}, {"3<=3", "1"},
		{"4>=4", "1"}, {"5>3", "1"},
	}
	for _, tc := range cases {
		if got := mustEval(t, tc.expr, nil); got != tc.want {
			t.Errorf("Eval(%q) = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

func TestEval_Boolean(t *testing.T) {
	cases := []struct{ expr, want string }{
		{"1&&1", "1"}, {"1&&0", "0"},
		{"0||1", "1"}, {"0||0", "0"},
		{"!0", "1"}, {"!1", "0"},
	}
	for _, tc := range cases {
		if got := mustEval(t, tc.expr, nil); got != tc.want {
			t.Errorf("Eval(%q) = %q, want %q", tc.expr, got, tc.want)
		}
	}
}

func TestEval_StringConcat(t *testing.T) {
	if got := mustEval(t, `"hello"."world"`, nil); got != "helloworld" {
		t.Errorf("string concat = %q", got)
	}
}

func TestEval_Variable(t *testing.T) {
	got := mustEval(t, "$hp", map[string]string{"hp": "42"})
	if got != "42" {
		t.Errorf("$hp = %q, want 42", got)
	}
}

func TestEval_Strlen(t *testing.T) {
	if got := mustEval(t, `strlen("hello")`, nil); got != "5" {
		t.Errorf("strlen = %q, want 5", got)
	}
}

func TestEval_Substr(t *testing.T) {
	if got := mustEval(t, `substr("hello", 1, 3)`, nil); got != "ell" {
		t.Errorf("substr = %q, want ell", got)
	}
}

func TestEval_Tolower_Toupper(t *testing.T) {
	if got := mustEval(t, `toupper("hello")`, nil); got != "HELLO" {
		t.Errorf("toupper = %q", got)
	}
	if got := mustEval(t, `tolower("WORLD")`, nil); got != "world" {
		t.Errorf("tolower = %q", got)
	}
}

func TestEval_Trim(t *testing.T) {
	if got := mustEval(t, `trim("  hi  ")`, nil); got != "hi" {
		t.Errorf("trim = %q", got)
	}
}

func TestEval_If_True(t *testing.T) {
	if got := mustEval(t, `if(1, "yes", "no")`, nil); got != "yes" {
		t.Errorf("if(1,...) = %q, want yes", got)
	}
}

func TestEval_If_False(t *testing.T) {
	if got := mustEval(t, `if(0, "yes", "no")`, nil); got != "no" {
		t.Errorf("if(0,...) = %q, want no", got)
	}
}

func TestEval_Rand_InRange(t *testing.T) {
	s := expr.NewScope()
	for i := 0; i < 100; i++ {
		r, err := expr.Eval("rand(10)", s)
		if err != nil {
			t.Fatalf("rand: %v", err)
		}
		n, err := strconv.Atoi(r)
		if err != nil || n < 0 || n >= 10 {
			t.Errorf("rand(10) = %q, out of [0,10)", r)
		}
	}
}

func TestEval_Min_Max(t *testing.T) {
	if got := mustEval(t, "min(3,1,5)", nil); got != "1" {
		t.Errorf("min = %q", got)
	}
	if got := mustEval(t, "max(3,1,5)", nil); got != "5" {
		t.Errorf("max = %q", got)
	}
}

func TestEval_Abs(t *testing.T) {
	if got := mustEval(t, "abs(-5)", nil); got != "5" {
		t.Errorf("abs = %q", got)
	}
}

func TestEval_Replace(t *testing.T) {
	if got := mustEval(t, `replace("hello world", "world", "Go")`, nil); got != "hello Go" {
		t.Errorf("replace = %q", got)
	}
}

func TestEval_Regmatch_Sets_Captures(t *testing.T) {
	s := expr.NewScope()
	result, err := expr.Eval(`regmatch("(\w+) (\w+)", "hello world")`, s)
	if err != nil {
		t.Fatalf("regmatch: %v", err)
	}
	if result != "1" {
		t.Errorf("regmatch = %q, want 1", result)
	}
	if v, ok := s.Get("1"); !ok || v != "hello" {
		t.Errorf("capture 1 = %q, want hello", v)
	}
	if v, ok := s.Get("2"); !ok || v != "world" {
		t.Errorf("capture 2 = %q, want world", v)
	}
}

func TestEval_Regmatch_NoMatch(t *testing.T) {
	if got := mustEval(t, `regmatch("dragon", "no match here")`, nil); got != "0" {
		t.Errorf("no-match regmatch = %q, want 0", got)
	}
}

func TestEval_DivisionByZero(t *testing.T) {
	s := expr.NewScope()
	_, err := expr.Eval("10/0", s)
	if err == nil {
		t.Error("expected error for division by zero")
	}
}

func TestExpandFull_Dollar_And_Braces(t *testing.T) {
	s := expr.NewScope()
	s.Set("hp", "100")
	result, err := expr.ExpandFull("HP is $hp and {strlen(hello)} chars", s)
	if err != nil {
		t.Fatalf("ExpandFull: %v", err)
	}
	want := fmt.Sprintf("HP is 100 and %d chars", len("hello"))
	if result != want {
		t.Errorf("ExpandFull = %q, want %q", result, want)
	}
}

func TestExpandFull_NoBraces_FastPath(t *testing.T) {
	s := expr.NewScope()
	s.Set("name", "Gandalf")
	result, err := expr.ExpandFull("hello $name", s)
	if err != nil {
		t.Fatalf("ExpandFull: %v", err)
	}
	if result != "hello Gandalf" {
		t.Errorf("result = %q", result)
	}
}

func TestEval_Nested_Expr(t *testing.T) {
	// Test: strlen(toupper("hi")) == 2
	if got := mustEval(t, `strlen(toupper("hi"))`, nil); got != "2" {
		t.Errorf("nested = %q, want 2", got)
	}
}
