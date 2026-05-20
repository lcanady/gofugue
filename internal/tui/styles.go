package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/kumakun/gofugue/internal/bus"
)

// AttrToStyle converts a bus.LineAttrs into a tcell.Style (exported for tests).
func AttrToStyle(a bus.LineAttrs) tcell.Style { return attrToStyle(a) }

// attrToStyle converts a bus.LineAttrs into a tcell.Style.
// Supports default (-1), palette 0-255, and truecolor (-2 with FGRGB/BGRGB).
func attrToStyle(a bus.LineAttrs) tcell.Style {
	s := tcell.StyleDefault

	switch {
	case a.FG == -2:
		s = s.Foreground(tcell.NewRGBColor(int32(a.FGRGB[0]), int32(a.FGRGB[1]), int32(a.FGRGB[2])))
	case a.FG >= 0 && a.FG <= 255:
		s = s.Foreground(tcell.PaletteColor(a.FG))
	}
	switch {
	case a.BG == -2:
		s = s.Background(tcell.NewRGBColor(int32(a.BGRGB[0]), int32(a.BGRGB[1]), int32(a.BGRGB[2])))
	case a.BG >= 0 && a.BG <= 255:
		s = s.Background(tcell.PaletteColor(a.BG))
	}
	if a.Bold {
		s = s.Bold(true)
	}
	if a.Underline {
		s = s.Underline(true)
	}
	if a.Reverse {
		s = s.Reverse(true)
	}
	if a.Italic {
		s = s.Italic(true)
	}
	return s
}

// Span is a run of text sharing a single style. A LogicalLine is made of
// one or more spans to support mid-line colour changes from the MUD server.
// It is an alias for bus.Span so the two packages share the same type.
type Span = bus.Span

// LogicalLine is a single line of MUD output, potentially multi-span.
type LogicalLine struct {
	Spans  []Span
	Gagged bool
}

// plain returns the concatenated text of all spans (for search / recall).
func (l LogicalLine) plain() string {
	var s string
	for _, sp := range l.Spans {
		s += sp.Text
	}
	return s
}

// fromWorldLine builds a LogicalLine from a bus event.
// When ev.Spans is populated by the ANSI parser it is used directly;
// otherwise the single Text+Attrs fields produce a plain single-span line.
func fromWorldLine(ev bus.WorldLineEvent) LogicalLine {
	if len(ev.Spans) > 0 {
		return LogicalLine{Spans: ev.Spans, Gagged: ev.Gagged}
	}
	return LogicalLine{
		Spans:  []Span{{Text: ev.Text, Attrs: ev.Attrs}},
		Gagged: ev.Gagged,
	}
}
