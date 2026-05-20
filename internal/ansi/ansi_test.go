package ansi_test

import (
	"testing"

	"github.com/kumakun/gofugue/internal/ansi"
	"github.com/kumakun/gofugue/internal/bus"
)

func attrs(fg, bg int, bold, italic, underline, reverse bool) bus.LineAttrs {
	return bus.LineAttrs{FG: fg, BG: bg, Bold: bold, Italic: italic, Underline: underline, Reverse: reverse}
}

func defaultA() bus.LineAttrs { return bus.LineAttrs{FG: -1, BG: -1} }

func TestParse_NoEscape_SingleSpan(t *testing.T) {
	spans, plain := ansi.Parse("hello world")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if spans[0].Text != "hello world" {
		t.Errorf("Text = %q, want %q", spans[0].Text, "hello world")
	}
	if plain != "hello world" {
		t.Errorf("plain = %q, want %q", plain, "hello world")
	}
	if spans[0].Attrs != defaultA() {
		t.Errorf("Attrs = %+v, want default", spans[0].Attrs)
	}
}

func TestParse_Empty_SingleSpan(t *testing.T) {
	spans, plain := ansi.Parse("")
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if plain != "" {
		t.Errorf("plain = %q, want empty", plain)
	}
}

func TestParse_Reset_SGR0(t *testing.T) {
	// ESC[0m resets; no visible text change since attrs were default.
	spans, plain := ansi.Parse("\x1b[0mhello")
	if plain != "hello" {
		t.Errorf("plain = %q, want %q", plain, "hello")
	}
	if len(spans) != 1 || spans[0].Text != "hello" {
		t.Errorf("unexpected spans: %+v", spans)
	}
}

func TestParse_BoldGreen(t *testing.T) {
	// \x1b[1;32m = bold + green FG
	spans, plain := ansi.Parse("\x1b[1;32mGoblin\x1b[0m attacks!")
	if plain != "Goblin attacks!" {
		t.Errorf("plain = %q", plain)
	}
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d: %+v", len(spans), spans)
	}
	if spans[0].Text != "Goblin" {
		t.Errorf("span[0].Text = %q", spans[0].Text)
	}
	if spans[0].Attrs.FG != 2 || !spans[0].Attrs.Bold {
		t.Errorf("span[0].Attrs = %+v, want FG=2 bold", spans[0].Attrs)
	}
	if spans[1].Text != " attacks!" {
		t.Errorf("span[1].Text = %q", spans[1].Text)
	}
	if spans[1].Attrs != defaultA() {
		t.Errorf("span[1].Attrs = %+v, want default", spans[1].Attrs)
	}
}

func TestParse_256ColourFG(t *testing.T) {
	spans, plain := ansi.Parse("\x1b[38;5;196mred\x1b[0m")
	if plain != "red" {
		t.Errorf("plain = %q", plain)
	}
	if len(spans) != 1 || spans[0].Attrs.FG != 196 {
		t.Errorf("expected FG=196, got: %+v", spans)
	}
}

func TestParse_256ColourBG(t *testing.T) {
	spans, plain := ansi.Parse("\x1b[48;5;22mbg\x1b[0m")
	if plain != "bg" {
		t.Errorf("plain = %q", plain)
	}
	if len(spans) != 1 || spans[0].Attrs.BG != 22 {
		t.Errorf("expected BG=22, got: %+v", spans)
	}
}

func TestParse_BrightFG(t *testing.T) {
	// 90 = bright black = palette 8
	spans, _ := ansi.Parse("\x1b[90mtext\x1b[0m")
	if len(spans) != 1 || spans[0].Attrs.FG != 8 {
		t.Errorf("expected FG=8 (bright black), got: %+v", spans)
	}
}

func TestParse_BrightBG(t *testing.T) {
	// 101 = bright red BG = palette 9
	spans, _ := ansi.Parse("\x1b[101mtext\x1b[0m")
	if len(spans) != 1 || spans[0].Attrs.BG != 9 {
		t.Errorf("expected BG=9 (bright red), got: %+v", spans)
	}
}

func TestParse_Underline_Italic(t *testing.T) {
	spans, plain := ansi.Parse("\x1b[3;4mstyle\x1b[0m")
	if plain != "style" {
		t.Errorf("plain = %q", plain)
	}
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	if !spans[0].Attrs.Italic || !spans[0].Attrs.Underline {
		t.Errorf("Attrs = %+v, want italic+underline", spans[0].Attrs)
	}
}

func TestParse_Reverse(t *testing.T) {
	spans, _ := ansi.Parse("\x1b[7mreverse\x1b[27m normal")
	if len(spans) != 2 {
		t.Fatalf("expected 2 spans, got %d", len(spans))
	}
	if !spans[0].Attrs.Reverse {
		t.Errorf("span[0] should have Reverse=true, got %+v", spans[0].Attrs)
	}
	if spans[1].Attrs.Reverse {
		t.Errorf("span[1] should have Reverse=false, got %+v", spans[1].Attrs)
	}
}

func TestParse_MultipleColourChanges(t *testing.T) {
	// Red "fire", reset, blue " ice"
	line := "\x1b[31mfire\x1b[0m \x1b[34mice\x1b[0m"
	spans, plain := ansi.Parse(line)
	if plain != "fire ice" {
		t.Errorf("plain = %q", plain)
	}
	// Should produce 4 spans: "fire" (red), " " (default), "ice" (blue), "" (default after reset)
	// Or 3 if empty spans are omitted. We only care that fire is red and ice is blue.
	redFound, blueFound := false, false
	for _, sp := range spans {
		if sp.Text == "fire" && sp.Attrs.FG == 1 {
			redFound = true
		}
		if sp.Text == "ice" && sp.Attrs.FG == 4 {
			blueFound = true
		}
	}
	if !redFound {
		t.Errorf("red 'fire' span not found in %+v", spans)
	}
	if !blueFound {
		t.Errorf("blue 'ice' span not found in %+v", spans)
	}
}

func TestParse_NonSGR_Sequence_Ignored(t *testing.T) {
	// ESC[2J (erase display) — should be ignored, text unaffected.
	spans, plain := ansi.Parse("\x1b[2Jhello")
	if plain != "hello" {
		t.Errorf("plain = %q, want %q", plain, "hello")
	}
	_ = spans
}

func TestParse_UnterminatedEscape_NoHang(t *testing.T) {
	// Unterminated ESC[1 at end of string.
	spans, plain := ansi.Parse("text\x1b[1")
	if plain != "text" {
		t.Errorf("plain = %q", plain)
	}
	_ = spans
}

func TestParse_DefaultFGBG_Reset(t *testing.T) {
	// ESC[39m = default FG, ESC[49m = default BG
	spans, _ := ansi.Parse("\x1b[31mred\x1b[39m normal")
	redFound := false
	for _, sp := range spans {
		if sp.Text == "red" && sp.Attrs.FG == 1 {
			redFound = true
		}
		if sp.Text == " normal" && sp.Attrs.FG != -1 {
			t.Errorf("after ESC[39m FG should be -1, got %d", sp.Attrs.FG)
		}
	}
	if !redFound {
		t.Errorf("red span not found: %+v", spans)
	}
}

func TestParse_PlainTextPreserved(t *testing.T) {
	// Emoji and Unicode should pass through correctly.
	spans, plain := ansi.Parse("\x1b[32m龙\x1b[0m") // "dragon" in Chinese
	if plain != "龙" {
		t.Errorf("plain = %q", plain)
	}
	if len(spans) < 1 || spans[0].Text != "龙" {
		t.Errorf("spans = %+v", spans)
	}
}

// ---------------------------------------------------------------------------
// Truecolor (24-bit) RGB
// ---------------------------------------------------------------------------

func TestParse_TruecolorFG(t *testing.T) {
	// ESC[38;2;255;0;0m = bright red FG
	spans, plain := ansi.Parse("\x1b[38;2;255;0;0mred\x1b[0m")
	if plain != "red" {
		t.Errorf("plain = %q", plain)
	}
	if len(spans) == 0 {
		t.Fatal("no spans returned")
	}
	sp := spans[0]
	if sp.Text != "red" {
		t.Errorf("Text = %q, want red", sp.Text)
	}
	if sp.Attrs.FG != -2 {
		t.Errorf("FG = %d, want -2 (truecolor sentinel)", sp.Attrs.FG)
	}
	if sp.Attrs.FGRGB != [3]byte{255, 0, 0} {
		t.Errorf("FGRGB = %v, want {255,0,0}", sp.Attrs.FGRGB)
	}
}

func TestParse_TruecolorBG(t *testing.T) {
	// ESC[48;2;0;128;255m = blue BG
	spans, plain := ansi.Parse("\x1b[48;2;0;128;255mbg\x1b[0m")
	if plain != "bg" {
		t.Errorf("plain = %q", plain)
	}
	if len(spans) == 0 {
		t.Fatal("no spans returned")
	}
	sp := spans[0]
	if sp.Attrs.BG != -2 {
		t.Errorf("BG = %d, want -2 (truecolor sentinel)", sp.Attrs.BG)
	}
	if sp.Attrs.BGRGB != [3]byte{0, 128, 255} {
		t.Errorf("BGRGB = %v, want {0,128,255}", sp.Attrs.BGRGB)
	}
}

func TestParse_TruecolorFGAndBG(t *testing.T) {
	// Both FG and BG set to truecolor.
	spans, _ := ansi.Parse("\x1b[38;2;10;20;30;48;2;40;50;60mtext\x1b[0m")
	if len(spans) == 0 {
		t.Fatal("no spans")
	}
	sp := spans[0]
	if sp.Attrs.FG != -2 {
		t.Errorf("FG = %d, want -2", sp.Attrs.FG)
	}
	if sp.Attrs.FGRGB != [3]byte{10, 20, 30} {
		t.Errorf("FGRGB = %v, want {10,20,30}", sp.Attrs.FGRGB)
	}
	if sp.Attrs.BG != -2 {
		t.Errorf("BG = %d, want -2", sp.Attrs.BG)
	}
	if sp.Attrs.BGRGB != [3]byte{40, 50, 60} {
		t.Errorf("BGRGB = %v, want {40,50,60}", sp.Attrs.BGRGB)
	}
}

func TestParse_TruecolorReset_ClearsSentinel(t *testing.T) {
	// After ESC[0m, FG/BG must return to -1 (not -2).
	spans, _ := ansi.Parse("\x1b[38;2;255;0;0mred\x1b[0mnormal")
	found := false
	for _, sp := range spans {
		if sp.Text == "normal" {
			found = true
			if sp.Attrs.FG != -1 {
				t.Errorf("after reset, FG = %d, want -1", sp.Attrs.FG)
			}
			if sp.Attrs.BG != -1 {
				t.Errorf("after reset, BG = %d, want -1", sp.Attrs.BG)
			}
		}
	}
	if !found {
		t.Errorf("'normal' span not found in %+v", spans)
	}
}

func TestParse_TruecolorFG_AllZero(t *testing.T) {
	// ESC[38;2;0;0;0m = black FG (edge case: all zeros)
	spans, _ := ansi.Parse("\x1b[38;2;0;0;0mblack\x1b[0m")
	if len(spans) == 0 {
		t.Fatal("no spans")
	}
	if spans[0].Attrs.FG != -2 {
		t.Errorf("FG = %d, want -2", spans[0].Attrs.FG)
	}
	if spans[0].Attrs.FGRGB != [3]byte{0, 0, 0} {
		t.Errorf("FGRGB = %v, want {0,0,0}", spans[0].Attrs.FGRGB)
	}
}
