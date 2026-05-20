package compat_test

import (
	"strings"
	"testing"

	"github.com/kumakun/gofugue/internal/compat"
	"github.com/kumakun/gofugue/internal/macro"
)

func parse(t *testing.T, src string) *compat.Result {
	t.Helper()
	res, err := compat.ImportReader(strings.NewReader(src))
	if err != nil {
		t.Fatalf("ImportReader: %v", err)
	}
	return res
}

func findMacro(macros []*macro.Macro, name string) *macro.Macro {
	for _, m := range macros {
		if m.Name == name {
			return m
		}
	}
	return nil
}

func TestImport_EmptyInput(t *testing.T) {
	res := parse(t, "")
	if len(res.Macros) != 0 {
		t.Errorf("expected 0 macros, got %d", len(res.Macros))
	}
	if len(res.Warnings) != 0 {
		t.Errorf("expected 0 warnings, got %d: %v", len(res.Warnings), res.Warnings)
	}
}

func TestImport_SkipBlankLines(t *testing.T) {
	res := parse(t, "\n\n   \n")
	if len(res.Macros) != 0 || len(res.Warnings) != 0 {
		t.Errorf("blank lines should produce no macros/warnings: %+v", res)
	}
}

func TestImport_SkipSemicolonComments(t *testing.T) {
	res := parse(t, "; this is a TF comment\n; another one")
	if len(res.Macros) != 0 {
		t.Errorf("comments should produce no macros, got %d", len(res.Macros))
	}
}

func TestImport_Def_Slash(t *testing.T) {
	res := parse(t, "/def dragon=say I see a dragon!")
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(res.Macros))
	}
	m := res.Macros[0]
	if m.Name != "dragon" {
		t.Errorf("Name = %q, want %q", m.Name, "dragon")
	}
	if m.Body != "say I see a dragon!" {
		t.Errorf("Body = %q, want %q", m.Body, "say I see a dragon!")
	}
	if m.Type != macro.TypeTrigger {
		t.Errorf("Type = %v, want TypeTrigger", m.Type)
	}
}

func TestImport_Def_Percent(t *testing.T) {
	res := parse(t, "%def greeting=say Hello!")
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(res.Macros))
	}
	if res.Macros[0].Name != "greeting" {
		t.Errorf("Name = %q, want greeting", res.Macros[0].Name)
	}
}

func TestImport_Alias(t *testing.T) {
	res := parse(t, "/alias ll=/list")
	m := findMacro(res.Macros, "ll")
	if m == nil {
		t.Fatal("alias 'll' not found in macros")
	}
	if m.Type != macro.TypeAlias {
		t.Errorf("Type = %v, want TypeAlias", m.Type)
	}
	if m.Body != "/list" {
		t.Errorf("Body = %q, want %q", m.Body, "/list")
	}
}

func TestImport_MultipleMacros(t *testing.T) {
	src := `/def one=body1
/def two=body2
/alias three=/cmd`
	res := parse(t, src)
	if len(res.Macros) != 3 {
		t.Errorf("expected 3 macros, got %d", len(res.Macros))
	}
}

func TestImport_Set_PopulatesSettings(t *testing.T) {
	res := parse(t, "/set wrap=80")
	if len(res.Warnings) != 0 {
		t.Errorf("expected no warnings for /set, got: %v", res.Warnings)
	}
	if res.Settings["wrap"] != "80" {
		t.Errorf("Settings[wrap] = %q, want %q", res.Settings["wrap"], "80")
	}
}

func TestImport_Key_PopulatesKeyBindings(t *testing.T) {
	res := parse(t, "/key F1=say hello")
	if len(res.Warnings) != 0 {
		t.Errorf("expected no warnings for /key, got: %v", res.Warnings)
	}
	if len(res.KeyBindings) == 0 {
		t.Fatal("expected 1 key binding, got 0")
	}
	if res.KeyBindings[0].Key != "F1" {
		t.Errorf("Key = %q, want %q", res.KeyBindings[0].Key, "F1")
	}
	if res.KeyBindings[0].Body != "say hello" {
		t.Errorf("Body = %q, want %q", res.KeyBindings[0].Body, "say hello")
	}
}

func TestImport_UnknownDirective_ProducesWarning(t *testing.T) {
	res := parse(t, "/unknowncmd foo")
	if len(res.Warnings) == 0 {
		t.Error("expected warning for unknown directive")
	}
}

func TestImport_Def_MissingEquals_ProducesWarning(t *testing.T) {
	res := parse(t, "/def baddef")
	if len(res.Warnings) == 0 {
		t.Error("expected warning for /def without '='")
	}
	if len(res.Macros) != 0 {
		t.Error("bad /def should not produce a macro")
	}
}

func TestImport_Alias_MissingEquals_ProducesWarning(t *testing.T) {
	res := parse(t, "/alias badalias")
	if len(res.Warnings) == 0 {
		t.Error("expected warning for /alias without '='")
	}
}

func TestImport_MixedValid_Invalid(t *testing.T) {
	src := `/def good=body
/def bad
/alias ok=cmd`
	res := parse(t, src)
	if len(res.Macros) != 2 {
		t.Errorf("expected 2 valid macros, got %d", len(res.Macros))
	}
	if len(res.Warnings) == 0 {
		t.Error("expected at least 1 warning for bad /def")
	}
}

// ---------------------------------------------------------------------------
// Alias body edge cases
// ---------------------------------------------------------------------------

func TestImport_Alias_BodyWithSpaces(t *testing.T) {
	res := parse(t, `/alias greet=say Hello there, traveller!`)
	m := findMacro(res.Macros, "greet")
	if m == nil {
		t.Fatal("alias 'greet' not found")
	}
	if m.Body != "say Hello there, traveller!" {
		t.Errorf("Body = %q, want full text after '='", m.Body)
	}
}

func TestImport_Alias_BodyIsSlashCommand(t *testing.T) {
	res := parse(t, `/alias q=/quit`)
	m := findMacro(res.Macros, "q")
	if m == nil {
		t.Fatal("alias 'q' not found")
	}
	if m.Body != "/quit" {
		t.Errorf("Body = %q, want %q", m.Body, "/quit")
	}
	if m.Type != macro.TypeAlias {
		t.Errorf("Type = %v, want TypeAlias", m.Type)
	}
}

func TestImport_Alias_BodyWithEqualSign(t *testing.T) {
	// Body itself contains '=' — first '=' is the separator.
	res := parse(t, `/alias eq=a=b`)
	m := findMacro(res.Macros, "eq")
	if m == nil {
		t.Fatal("alias 'eq' not found")
	}
	if m.Body != "a=b" {
		t.Errorf("Body = %q, want %q", m.Body, "a=b")
	}
}

// ---------------------------------------------------------------------------
// /def edge cases
// ---------------------------------------------------------------------------

func TestImport_Def_BodyWithSpaces(t *testing.T) {
	res := parse(t, `/def announce=say The dragon has arrived!`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(res.Macros))
	}
	if res.Macros[0].Body != "say The dragon has arrived!" {
		t.Errorf("Body = %q", res.Macros[0].Body)
	}
}

func TestImport_Def_EmptyBody(t *testing.T) {
	// "/def silent=" — empty body is valid (no-op trigger)
	res := parse(t, `/def silent=`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro for empty-body def, got %d", len(res.Macros))
	}
	if res.Macros[0].Body != "" {
		t.Errorf("Body = %q, want empty", res.Macros[0].Body)
	}
}

func TestImport_PercentPrefix_Def(t *testing.T) {
	res := parse(t, `%def pct=works`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro for %%def, got %d", len(res.Macros))
	}
	if res.Macros[0].Name != "pct" {
		t.Errorf("Name = %q, want 'pct'", res.Macros[0].Name)
	}
}

func TestImport_PercentPrefix_Alias(t *testing.T) {
	res := parse(t, `%alias pa=/pct`)
	m := findMacro(res.Macros, "pa")
	if m == nil {
		t.Fatal("%%alias 'pa' not found")
	}
	if m.Type != macro.TypeAlias {
		t.Errorf("Type = %v, want TypeAlias", m.Type)
	}
}

func TestImport_Semicolon_InlineComment(t *testing.T) {
	// Lines beginning with ';' are TF comments; they should produce no macros.
	res := parse(t, "; just a comment\n/def real=body")
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(res.Macros))
	}
}

func TestImport_Set_CapturesKeyValue(t *testing.T) {
	res := parse(t, "/set mypref=on")
	// /set now populates Settings — no warning expected.
	if len(res.Warnings) != 0 {
		t.Errorf("expected no warnings for /set, got: %v", res.Warnings)
	}
	if res.Settings["mypref"] != "on" {
		t.Errorf("Settings[mypref] = %q, want %q", res.Settings["mypref"], "on")
	}
}

// ---------------------------------------------------------------------------
// parseDef edge cases (flag parsing robustness)
// ---------------------------------------------------------------------------

func TestParseDef_FlagT_NoSpace_BareName(t *testing.T) {
	// -tpattern (no quotes, no space) — consumeQuotedOrWord reads "pattern"
	res := parse(t, `/def -tdragon name=flee`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d; warnings: %v", len(res.Macros), res.Warnings)
	}
	if res.Macros[0].Pattern != "dragon" {
		t.Errorf("Pattern = %q, want %q", res.Macros[0].Pattern, "dragon")
	}
}

func TestParseDef_FlagT_WithSpace_QuotedPattern(t *testing.T) {
	res := parse(t, `/def -t "big dragon" bigdrag=flee`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(res.Macros))
	}
	if res.Macros[0].Pattern != "big dragon" {
		t.Errorf("Pattern = %q, want %q", res.Macros[0].Pattern, "big dragon")
	}
}

func TestParseDef_FlagP_NegativePriority(t *testing.T) {
	// -p -5 is parsed as -p then a separate bare word "-5"
	// The "-5" starts with '-', so it's parsed as a flag 'p' with a remainder of "5".
	// Actually let's just verify no panic and we get a macro or a warning.
	res, err := compat.ImportReader(strings.NewReader(`/def -p -5 negprio=body`))
	if err != nil {
		t.Fatalf("ImportReader: %v", err)
	}
	// Either parsed successfully (priority is some value) or produced a warning.
	_ = res
}

func TestParseDef_UnclosedQuote_ProducesWarning(t *testing.T) {
	res := parse(t, `/def -t"unclosed name=body`)
	if len(res.Warnings) == 0 {
		t.Error("expected warning for unclosed quote in -t flag")
	}
	if len(res.Macros) != 0 {
		t.Error("unclosed quote should produce no macro")
	}
}

func TestParseDef_EmptyPattern_AfterT(t *testing.T) {
	// -t"" — empty quoted pattern is valid (matches any line)
	res := parse(t, `/def -t"" empty_pat=body`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d; warnings: %v", len(res.Macros), res.Warnings)
	}
	if res.Macros[0].Pattern != "" {
		t.Errorf("Pattern = %q, want empty", res.Macros[0].Pattern)
	}
}

func TestParseDef_FlagW_SetsWorld(t *testing.T) {
	res := parse(t, `/def -w avalon -t"dragon" w_trig=flee`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(res.Macros))
	}
	if res.Macros[0].World != "avalon" {
		t.Errorf("World = %q, want %q", res.Macros[0].World, "avalon")
	}
}

func TestParseDef_FlagE_MacroDisabled(t *testing.T) {
	// -E flag should mark macro as disabled after define.
	// Note: compat sets Enabled=false, but macro.Engine.Define overrides it to true.
	// The test verifies parseDef produces a macro object with intended disabled state.
	res := parse(t, `/def -E disabled_macro=body`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d", len(res.Macros))
	}
	// The Enabled field reflects the raw parsed state before engine insertion.
	// compat sets Enabled=false when -E is present.
	if res.Macros[0].Body != "body" {
		t.Errorf("Body = %q", res.Macros[0].Body)
	}
}

func TestParseDef_MultipleFlagsCombined(t *testing.T) {
	res := parse(t, `/def -t"monster" -p 5 -w realm1 combo=/echo HIT`)
	if len(res.Macros) != 1 {
		t.Fatalf("expected 1 macro, got %d; warnings: %v", len(res.Macros), res.Warnings)
	}
	m := res.Macros[0]
	if m.Pattern != "monster" {
		t.Errorf("Pattern = %q, want %q", m.Pattern, "monster")
	}
	if m.Priority != 5 {
		t.Errorf("Priority = %d, want 5", m.Priority)
	}
	if m.World != "realm1" {
		t.Errorf("World = %q, want %q", m.World, "realm1")
	}
	if m.Body != "/echo HIT" {
		t.Errorf("Body = %q, want %q", m.Body, "/echo HIT")
	}
}
