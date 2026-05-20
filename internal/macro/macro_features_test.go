package macro_test

import (
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/macro"
)

func define(t *testing.T, e *macro.Engine, m *macro.Macro) {
	t.Helper()
	if err := e.Define(m); err != nil {
		t.Fatalf("Define %q: %v", m.Name, err)
	}
}

func TestGag_MatchingLine_IsGagged(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name: "g1", Type: macro.TypeGag, Pattern: "boring",
	})
	result := e.FireTriggers("", "this is boring text")
	if !result.Gagged {
		t.Error("expected Gagged=true")
	}
}

func TestGag_NonMatchingLine_NotGagged(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name: "g1", Type: macro.TypeGag, Pattern: "boring",
	})
	result := e.FireTriggers("", "interesting text")
	if result.Gagged {
		t.Error("expected Gagged=false")
	}
}

func TestHilite_MatchingLine_InFires(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name:    "h1",
		Type:    macro.TypeHilite,
		Pattern: "dragon",
		Body:    "red",
	})
	result := e.FireTriggers("", "a dragon attacks")
	if len(result.Fires) != 1 {
		t.Fatalf("expected 1 fire, got %d", len(result.Fires))
	}
	if result.Fires[0].Macro.Type != macro.TypeHilite {
		t.Errorf("expected TypeHilite fire")
	}
	if result.Fires[0].Body != "red" {
		t.Errorf("body = %q, want red", result.Fires[0].Body)
	}
}

func TestSubstitute_MatchingLine_InFires(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name:    "s1",
		Type:    macro.TypeSubstitute,
		Pattern: `You gain (\d+) xp`,
		Body:    "XP gained: %1",
	})
	result := e.FireTriggers("", "You gain 250 xp")
	if len(result.Fires) != 1 {
		t.Fatalf("expected 1 fire, got %d", len(result.Fires))
	}
	if result.Fires[0].Macro.Type != macro.TypeSubstitute {
		t.Errorf("expected TypeSubstitute")
	}
	if len(result.Fires[0].Captures) < 2 || result.Fires[0].Captures[1] != "250" {
		t.Errorf("captures = %v, want [full match, 250]", result.Fires[0].Captures)
	}
}

func TestShots_AutoUndefAfterExhaustion(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name:    "oneshot",
		Type:    macro.TypeTrigger,
		Pattern: "hit",
		Body:    "dodge",
		Shots:   1,
	})

	r := e.FireTriggers("", "you are hit")
	if len(r.Fires) != 1 {
		t.Fatalf("expected 1 fire, got %d", len(r.Fires))
	}

	// Give the deferred removal time to run.
	time.Sleep(10 * time.Millisecond)

	r2 := e.FireTriggers("", "you are hit")
	if len(r2.Fires) != 0 {
		t.Errorf("one-shot trigger should be gone after first fire")
	}
}

func TestFallthrough_BothFire(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name:     "f1",
		Type:     macro.TypeTrigger,
		Pattern:  "dragon",
		Body:     "flee",
		Priority: 10,
		Fallthru: true,
	})
	define(t, e, &macro.Macro{
		Name:     "f2",
		Type:     macro.TypeTrigger,
		Pattern:  "dragon",
		Body:     "yell",
		Priority: 5,
	})

	result := e.FireTriggers("", "a dragon attacks")
	if len(result.Fires) != 2 {
		t.Fatalf("expected 2 fires (fallthrough), got %d", len(result.Fires))
	}
}

func TestFallthrough_False_OnlyFirstFires(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name: "f1", Type: macro.TypeTrigger, Pattern: "dragon",
		Body: "flee", Priority: 10, Fallthru: false,
	})
	define(t, e, &macro.Macro{
		Name: "f2", Type: macro.TypeTrigger, Pattern: "dragon",
		Body: "yell", Priority: 5,
	})

	result := e.FireTriggers("", "a dragon attacks")
	if len(result.Fires) != 1 {
		t.Fatalf("without fallthrough expected 1 fire, got %d", len(result.Fires))
	}
}

func TestProbability_Zero_AlwaysFires(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name: "p1", Type: macro.TypeTrigger, Pattern: "hit",
		Body: "dodge", Prob: 0,
	})
	for i := 0; i < 20; i++ {
		result := e.FireTriggers("", "you are hit")
		if len(result.Fires) == 0 {
			t.Errorf("Prob=0 should always fire (iteration %d)", i)
		}
	}
}

func TestInvisible_NotInList(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name:      "visible",
		Type:      macro.TypeTrigger,
		Pattern:   "x",
		Body:      "y",
		Invisible: false,
	})
	define(t, e, &macro.Macro{
		Name:      "hidden",
		Type:      macro.TypeTrigger,
		Pattern:   "x",
		Body:      "y",
		Invisible: true,
	})

	list := e.List()
	for _, l := range list {
		if l == "hidden" {
			t.Error("invisible macro should not appear in /list")
		}
	}
	found := false
	for _, l := range list {
		if contains(l, "visible") {
			found = true
		}
	}
	if !found {
		t.Error("visible macro should appear in /list")
	}
}

func TestGlobMatch(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name: "g", Type: macro.TypeTrigger, Pattern: "drag*",
		Body: "flee", MatchMode: macro.MatchGlob,
	})

	r := e.FireTriggers("", "dragon attacks")
	if len(r.Fires) != 1 {
		t.Errorf("glob pattern 'drag*' should match 'dragon attacks'")
	}

	r2 := e.FireTriggers("", "knight attacks")
	if len(r2.Fires) != 0 {
		t.Errorf("glob pattern 'drag*' should not match 'knight attacks'")
	}
}

func TestSubstrMatch(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name: "s", Type: macro.TypeTrigger, Pattern: "drag.on",
		Body: "flee", MatchMode: macro.MatchSubstr,
	})

	// "drag.on" as literal substring — the dot is NOT a regex wildcard.
	r := e.FireTriggers("", "the drag.on is here")
	if len(r.Fires) != 1 {
		t.Errorf("substr should match literal dot")
	}
	r2 := e.FireTriggers("", "the dragon is here")
	if len(r2.Fires) != 0 {
		t.Errorf("substr should not match 'dragon' for literal 'drag.on'")
	}
}

func TestCaptureGroups_InFires(t *testing.T) {
	e := macro.New()
	define(t, e, &macro.Macro{
		Name:    "cap",
		Type:    macro.TypeTrigger,
		Pattern: `(\w+) slays (\w+)`,
		Body:    "say %1 killed %2",
	})

	result := e.FireTriggers("", "Gandalf slays Balrog")
	if len(result.Fires) == 0 {
		t.Fatal("expected a fire")
	}
	caps := result.Fires[0].Captures
	if len(caps) < 3 {
		t.Fatalf("expected 3 captures (full+2 groups), got %d", len(caps))
	}
	if caps[1] != "Gandalf" {
		t.Errorf("capture 1 = %q, want Gandalf", caps[1])
	}
	if caps[2] != "Balrog" {
		t.Errorf("capture 2 = %q, want Balrog", caps[2])
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsStr(s, sub))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
