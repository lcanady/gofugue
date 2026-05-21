package tui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/kumakun/gofugue/internal/bus"
)

func TestAttrToStyle(t *testing.T) {
	tests := []struct {
		name string
		in   bus.LineAttrs
		want tcell.Style
	}{
		{
			name: "default",
			in:   bus.LineAttrs{FG: -1, BG: -1},
			want: tcell.StyleDefault,
		},
		{
			name: "palette colors",
			in:   bus.LineAttrs{FG: 1, BG: 2},
			want: tcell.StyleDefault.Foreground(tcell.PaletteColor(1)).Background(tcell.PaletteColor(2)),
		},
		{
			name: "true colors",
			in: bus.LineAttrs{
				FG:    -2,
				FGRGB: [3]byte{255, 128, 64},
				BG:    -2,
				BGRGB: [3]byte{10, 20, 30},
			},
			want: tcell.StyleDefault.
				Foreground(tcell.NewRGBColor(255, 128, 64)).
				Background(tcell.NewRGBColor(10, 20, 30)),
		},
		{
			name: "attributes",
			in: bus.LineAttrs{
				FG:        -1,
				BG:        -1,
				Bold:      true,
				Underline: true,
				Reverse:   true,
				Italic:    true,
			},
			want: tcell.StyleDefault.
				Bold(true).
				Underline(true).
				Reverse(true).
				Italic(true),
		},
		{
			name: "mix colors and attributes",
			in: bus.LineAttrs{
				FG:   4,
				BG:   5,
				Bold: true,
			},
			want: tcell.StyleDefault.
				Foreground(tcell.PaletteColor(4)).
				Background(tcell.PaletteColor(5)).
				Bold(true),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := AttrToStyle(tc.in)
			if got != tc.want {
				t.Errorf("AttrToStyle() = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestLogicalLine_plain(t *testing.T) {
	ll := LogicalLine{
		Spans: []Span{
			{Text: "first part ", Attrs: bus.LineAttrs{}},
			{Text: "second part", Attrs: bus.LineAttrs{}},
		},
	}
	if got := ll.plain(); got != "first part second part" {
		t.Errorf("expected %q, got %q", "first part second part", got)
	}
}
