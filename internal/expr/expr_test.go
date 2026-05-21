package expr_test

import (
	"testing"

	"github.com/kumakun/gofugue/internal/expr"
)

// --- Scope ---

func TestNewScope(t *testing.T) {
	s := expr.NewScope()
	if s == nil {
		t.Fatal("NewScope returned nil")
	}

	all := s.All()
	if len(all) != 0 {
		t.Errorf("NewScope should have an empty global scope, got %d items", len(all))
	}
}

func TestScope_SetAndGet(t *testing.T) {
	s := expr.NewScope()
	s.Set("name", "gandalf")

	v, ok := s.Get("name")
	if !ok {
		t.Fatal("Get returned ok=false for set variable")
	}
	if v != "gandalf" {
		t.Errorf("Get(%q) = %q, want %q", "name", v, "gandalf")
	}
}

func TestScope_GetMissing_ReturnsFalse(t *testing.T) {
	s := expr.NewScope()
	_, ok := s.Get("nonexistent")
	if ok {
		t.Error("Get of missing variable should return ok=false")
	}
}

func TestScope_LocalShadowsGlobal(t *testing.T) {
	s := expr.NewScope()
	s.Set("hero", "frodo")

	s.Push(map[string]string{"hero": "sam"})
	v, _ := s.Get("hero")
	if v != "sam" {
		t.Errorf("local scope should shadow global: got %q, want %q", v, "sam")
	}

	s.Pop()
	v, _ = s.Get("hero")
	if v != "frodo" {
		t.Errorf("after Pop, global restored: got %q, want %q", v, "frodo")
	}
}

func TestScope_MultipleLocalScopes(t *testing.T) {
	s := expr.NewScope()
	s.Set("x", "global")
	s.Push(map[string]string{"x": "local1"})
	s.Push(map[string]string{"x": "local2"})

	v, _ := s.Get("x")
	if v != "local2" {
		t.Errorf("innermost local should win: got %q, want %q", v, "local2")
	}

	s.Pop()
	v, _ = s.Get("x")
	if v != "local1" {
		t.Errorf("after one Pop: got %q, want %q", v, "local1")
	}

	s.Pop()
	v, _ = s.Get("x")
	if v != "global" {
		t.Errorf("after two Pops: got %q, want %q", v, "global")
	}
}

func TestScope_Pop_EmptyStack_NoOp(t *testing.T) {
	s := expr.NewScope()
	s.Pop() // should not panic
	s.Pop()
}

func TestScope_LocalDoesNotAffectGlobal(t *testing.T) {
	s := expr.NewScope()
	s.Set("a", "global-a")
	s.Push(map[string]string{"b": "local-b"})

	// global var should still be accessible from within local scope
	v, ok := s.Get("a")
	if !ok || v != "global-a" {
		t.Errorf("global variable not accessible from local scope: got %q %v", v, ok)
	}

	s.Pop()
}

func TestScope_Overwrite(t *testing.T) {
	s := expr.NewScope()
	s.Set("x", "first")
	s.Set("x", "second")
	v, _ := s.Get("x")
	if v != "second" {
		t.Errorf("overwrite failed: got %q, want %q", v, "second")
	}
}

// --- Expand (variable substitution) ---

func TestScope_Expand_NoVars_Passthrough(t *testing.T) {
	s := expr.NewScope()
	got := s.Expand("no variables here")
	if got != "no variables here" {
		t.Errorf("Expand = %q, want unchanged", got)
	}
}

func TestScope_Expand_DollarName(t *testing.T) {
	s := expr.NewScope()
	s.Set("hero", "aragorn")
	got := s.Expand("greetings, $hero!")
	if got != "greetings, aragorn!" {
		t.Errorf("Expand = %q, want %q", got, "greetings, aragorn!")
	}
}

func TestScope_Expand_DollarBrace(t *testing.T) {
	s := expr.NewScope()
	s.Set("world", "Arda")
	got := s.Expand("welcome to ${world}!")
	if got != "welcome to Arda!" {
		t.Errorf("Expand = %q, want %q", got, "welcome to Arda!")
	}
}

func TestScope_Expand_MultipleVars(t *testing.T) {
	s := expr.NewScope()
	s.Set("a", "foo")
	s.Set("b", "bar")
	got := s.Expand("$a and $b")
	if got != "foo and bar" {
		t.Errorf("Expand = %q, want %q", got, "foo and bar")
	}
}

func TestScope_Expand_UnknownVar_EmptyString(t *testing.T) {
	s := expr.NewScope()
	got := s.Expand("hello $nobody")
	if got != "hello " {
		t.Errorf("Expand = %q, want %q", got, "hello ")
	}
}

func TestScope_Expand_LocalScopeVar(t *testing.T) {
	s := expr.NewScope()
	s.Push(map[string]string{"x": "local"})
	got := s.Expand("value=$x")
	if got != "value=local" {
		t.Errorf("Expand = %q, want %q", got, "value=local")
	}
	s.Pop()
}

func TestScope_Expand_BareDollar_PassThrough(t *testing.T) {
	s := expr.NewScope()
	got := s.Expand("price $")
	if got != "price $" {
		t.Errorf("Expand bare '$' = %q, want %q", got, "price $")
	}
}

func TestScope_Expand_DollarNotIdent_PassThrough(t *testing.T) {
	s := expr.NewScope()
	got := s.Expand("cost $1.50")
	// '$1' is not a valid ident start — '$' passes through
	if got != "cost $1.50" {
		t.Errorf("Expand = %q, want %q", got, "cost $1.50")
	}
}

func TestScope_Expand_AdjacentVars(t *testing.T) {
	s := expr.NewScope()
	s.Set("first", "hello")
	s.Set("second", "world")
	got := s.Expand("${first}${second}")
	if got != "helloworld" {
		t.Errorf("Expand = %q, want %q", got, "helloworld")
	}
}

func TestScope_Expand_EmptyInput(t *testing.T) {
	s := expr.NewScope()
	got := s.Expand("")
	if got != "" {
		t.Errorf("Expand('') = %q, want empty", got)
	}
}

func TestExpandFull_SubstitutesVars(t *testing.T) {
	s := expr.NewScope()
	s.Set("hero", "aragorn")
	out, err := expr.ExpandFull("greetings $hero", s)
	if err != nil {
		t.Fatalf("ExpandFull error: %v", err)
	}
	if out != "greetings aragorn" {
		t.Errorf("ExpandFull = %q, want %q", out, "greetings aragorn")
	}
}
