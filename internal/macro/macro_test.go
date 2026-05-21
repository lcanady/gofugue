package macro_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/kumakun/gofugue/internal/macro"
)

func TestNew(t *testing.T) {
	e := macro.New()
	if e == nil {
		t.Fatal("expected New() to return a non-nil Engine")
	}

	// Verify maps are initialized by defining a dummy macro without panic.
	err := e.Define(&macro.Macro{
		Name:    "dummy",
		Type:    macro.TypeTrigger,
		Pattern: `dummy`,
		Body:    "say dummy",
	})
	if err != nil {
		t.Fatalf("unexpected error defining macro on new Engine: %v", err)
	}
}

func TestEngine_DefineTrigger_MatchesLine(t *testing.T) {
	e := macro.New()
	err := e.Define(&macro.Macro{
		Name:    "dragon",
		Type:    macro.TypeTrigger,
		Pattern: `(?i)dragon`,
		Body:    "say I see a dragon!",
	})
	if err != nil {
		t.Fatalf("Define: %v", err)
	}

	body, caps := e.MatchTrigger("myworld", "A red dragon appears.")
	if body == "" {
		t.Fatal("expected trigger to match, got no match")
	}
	if caps == nil {
		t.Fatal("expected captures slice")
	}
}

func TestEngine_DefineTrigger_NoMatchOnMismatch(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{
		Name:    "t1",
		Type:    macro.TypeTrigger,
		Pattern: `dragon`,
		Body:    "body",
	})

	body, _ := e.MatchTrigger("myworld", "A goblin appears.")
	if body != "" {
		t.Errorf("expected no match, got body %q", body)
	}
}

func TestEngine_Define_InvalidPattern_ReturnsError(t *testing.T) {
	e := macro.New()
	err := e.Define(&macro.Macro{
		Name:    "bad",
		Type:    macro.TypeTrigger,
		Pattern: `[invalid`,
		Body:    "body",
	})
	if err == nil {
		t.Fatal("expected error for invalid regex, got nil")
	}
}

func TestEngine_Undefine_RemovesTrigger(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "t1", Type: macro.TypeTrigger, Pattern: `hello`, Body: "hi"})

	if !e.Undefine("t1") {
		t.Fatal("Undefine returned false for existing macro")
	}

	body, _ := e.MatchTrigger("w", "hello world")
	if body != "" {
		t.Error("trigger still fires after Undefine")
	}
}

func TestEngine_Undefine_ReturnsFalseForUnknown(t *testing.T) {
	e := macro.New()
	if e.Undefine("nonexistent") {
		t.Error("Undefine returned true for nonexistent macro")
	}
}

func TestEngine_Priority_HigherFiringFirst(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "low", Type: macro.TypeTrigger, Pattern: `.*`, Body: "low-body", Priority: 1})
	_ = e.Define(&macro.Macro{Name: "high", Type: macro.TypeTrigger, Pattern: `.*`, Body: "high-body", Priority: 10})

	body, _ := e.MatchTrigger("w", "anything")
	if body != "high-body" {
		t.Errorf("expected high-priority body, got %q", body)
	}
}

func TestEngine_WorldScoping_MatchesCorrectWorld(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "scoped", Type: macro.TypeTrigger, Pattern: `test`, Body: "scoped-body", World: "world-a"})

	body, _ := e.MatchTrigger("world-a", "test line")
	if body == "" {
		t.Error("expected match for world-a")
	}

	body, _ = e.MatchTrigger("world-b", "test line")
	if body != "" {
		t.Errorf("expected no match for world-b, got %q", body)
	}
}

func TestEngine_WorldScoping_EmptyWorldMatchesAll(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "global", Type: macro.TypeTrigger, Pattern: `test`, Body: "global-body", World: ""})

	for _, world := range []string{"world-a", "world-b", "any"} {
		body, _ := e.MatchTrigger(world, "test line")
		if body == "" {
			t.Errorf("expected global trigger to match for world %q", world)
		}
	}
}

func TestEngine_Alias_MatchesInput(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{
		Name:    "ll",
		Type:    macro.TypeAlias,
		Pattern: `^ll$`,
		Body:    "/list",
	})

	body, ok := e.MatchAlias("w", "ll")
	if !ok {
		t.Fatal("expected alias match")
	}
	if body != "/list" {
		t.Errorf("got body %q, want %q", body, "/list")
	}
}

func TestEngine_Alias_NoMatchForNonAlias(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "ll", Type: macro.TypeAlias, Pattern: `^ll$`, Body: "/list"})

	_, ok := e.MatchAlias("w", "ls")
	if ok {
		t.Error("expected no alias match for 'ls'")
	}
}

func TestEngine_Hook_FiresRegisteredBodies(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "on-connect", Type: macro.TypeHook, Pattern: "CONNECT", Body: "say Connected!"})
	_ = e.Define(&macro.Macro{Name: "on-connect-2", Type: macro.TypeHook, Pattern: "CONNECT", Body: "say Hello!"})

	bodies := e.FireHook("w", "CONNECT")
	if len(bodies) != 2 {
		t.Fatalf("expected 2 hook bodies, got %d", len(bodies))
	}
}

func TestEngine_Hook_DoesNotFireForDifferentHook(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "on-dc", Type: macro.TypeHook, Pattern: "DISCONNECT", Body: "say Bye!"})

	bodies := e.FireHook("w", "CONNECT")
	if len(bodies) != 0 {
		t.Errorf("expected no hook bodies for CONNECT, got %d", len(bodies))
	}
}

func TestEngine_Redefine_ReplacesExisting(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "t1", Type: macro.TypeTrigger, Pattern: `foo`, Body: "old"})
	_ = e.Define(&macro.Macro{Name: "t1", Type: macro.TypeTrigger, Pattern: `foo`, Body: "new"})

	body, _ := e.MatchTrigger("w", "foo bar")
	if body != "new" {
		t.Errorf("expected redefined body 'new', got %q", body)
	}
}

func TestEngine_CaptureGroups_ReturnedCorrectly(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{
		Name:    "hp",
		Type:    macro.TypeTrigger,
		Pattern: `HP: (\d+)/(\d+)`,
		Body:    "echo hp=%1 max=%2",
	})

	_, caps := e.MatchTrigger("w", "HP: 42/100")
	if len(caps) < 3 {
		t.Fatalf("expected ≥3 captures (full match + 2 groups), got %d", len(caps))
	}
	if caps[1] != "42" {
		t.Errorf("caps[1] = %q, want %q", caps[1], "42")
	}
	if caps[2] != "100" {
		t.Errorf("caps[2] = %q, want %q", caps[2], "100")
	}
}

// ---------------------------------------------------------------------------
// Enabled flag
// ---------------------------------------------------------------------------

func TestEngine_DisabledTrigger_DoesNotFire(t *testing.T) {
	e := macro.New()
	m := &macro.Macro{Name: "t", Type: macro.TypeTrigger, Pattern: `hello`, Body: "hi"}
	_ = e.Define(m)
	m.Enabled = false

	body, _ := e.MatchTrigger("w", "hello world")
	if body != "" {
		t.Errorf("disabled trigger fired: body=%q", body)
	}
}

func TestEngine_DisabledAlias_DoesNotMatch(t *testing.T) {
	e := macro.New()
	m := &macro.Macro{Name: "ll", Type: macro.TypeAlias, Pattern: `^ll$`, Body: "/list"}
	_ = e.Define(m)
	m.Enabled = false

	_, ok := e.MatchAlias("w", "ll")
	if ok {
		t.Error("disabled alias matched")
	}
}

func TestEngine_DisabledHook_DoesNotFire(t *testing.T) {
	e := macro.New()
	m := &macro.Macro{Name: "h", Type: macro.TypeHook, Pattern: "CONNECT", Body: "say hi"}
	_ = e.Define(m)
	m.Enabled = false

	bodies := e.FireHook("w", "CONNECT")
	if len(bodies) != 0 {
		t.Errorf("disabled hook fired %d bodies", len(bodies))
	}
}

// ---------------------------------------------------------------------------
// Hook world scoping
// ---------------------------------------------------------------------------

func TestEngine_Hook_WorldScoped_OnlyMatchesCorrectWorld(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{
		Name: "scoped-hook", Type: macro.TypeHook,
		Pattern: "CONNECT", World: "world-a", Body: "echo A",
	})

	// Wrong world — should not fire.
	if bodies := e.FireHook("world-b", "CONNECT"); len(bodies) != 0 {
		t.Errorf("hook fired for wrong world: %v", bodies)
	}
	// Correct world — must fire.
	if bodies := e.FireHook("world-a", "CONNECT"); len(bodies) != 1 {
		t.Errorf("expected 1 hook body for world-a, got %d", len(bodies))
	}
}

// ---------------------------------------------------------------------------
// Alias world scoping
// ---------------------------------------------------------------------------

func TestEngine_Alias_WorldScoped_OnlyMatchesCorrectWorld(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{
		Name: "scoped-alias", Type: macro.TypeAlias,
		Pattern: `^ll$`, World: "world-a", Body: "/list",
	})

	_, ok := e.MatchAlias("world-b", "ll")
	if ok {
		t.Error("alias matched for wrong world")
	}

	_, ok = e.MatchAlias("world-a", "ll")
	if !ok {
		t.Error("alias did not match for correct world")
	}
}

// ---------------------------------------------------------------------------
// List output
// ---------------------------------------------------------------------------

func TestEngine_List_ContainsNameAndBody(t *testing.T) {
	e := macro.New()
	_ = e.Define(&macro.Macro{Name: "mymacro", Type: macro.TypeTrigger, Pattern: "foo", Body: "/echo foo"})

	list := e.List()
	if len(list) != 1 {
		t.Fatalf("expected 1 item, got %d", len(list))
	}
	if list[0] == "" {
		t.Error("list entry is empty")
	}
}

// ---------------------------------------------------------------------------
// Concurrent safety — run with go test -race
// ---------------------------------------------------------------------------

func TestEngine_Concurrent_DefineUndefine_NoRace(t *testing.T) {
	e := macro.New()
	var wg sync.WaitGroup

	// Writers: define and undefine concurrently.
	for i := 0; i < 20; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			name := fmt.Sprintf("trigger-%d", i)
			e.Define(&macro.Macro{ //nolint:errcheck
				Name: name, Type: macro.TypeTrigger,
				Pattern: fmt.Sprintf("line%d", i), Body: "body",
			})
			e.Undefine(name)
		}()
	}

	// Readers: MatchTrigger concurrently with writers.
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.MatchTrigger("w", "any line") //nolint:errcheck
		}()
	}

	wg.Wait()
}

func TestEngine_Concurrent_FireHook_NoRace(t *testing.T) {
	e := macro.New()
	for i := 0; i < 5; i++ {
		e.Define(&macro.Macro{ //nolint:errcheck
			Name: fmt.Sprintf("hook-%d", i), Type: macro.TypeHook,
			Pattern: "CONNECT", Body: fmt.Sprintf("body-%d", i),
		})
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.FireHook("w", "CONNECT")
		}()
	}
	wg.Wait()
}
