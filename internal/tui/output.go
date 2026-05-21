package tui

import (
	"sync"

	"github.com/gdamore/tcell/v2"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/rivo/uniseg"
)

// OutputPane is the scrollable world-output area.
type OutputPane struct {
	mu       sync.Mutex
	lines    []LogicalLine // all received logical lines
	wrapped  [][]physRow   // cached wrapped rows per logical line; nil = not yet wrapped
	scroll   int           // rows scrolled up from bottom (0 = live view)
	width    int           // current column width for wrapping
	height   int           // current row height of the pane
}

func newOutputPane(width, height int) *OutputPane {
	return &OutputPane{width: width, height: height}
}

// NewOutputPane is the exported constructor used by tests and external packages.
func NewOutputPane(width, height int) *OutputPane { return newOutputPane(width, height) }

// MakeLogicalLine builds a single-span LogicalLine (used by tests and cmd layer).
func MakeLogicalLine(text string, attrs bus.LineAttrs) LogicalLine {
	return LogicalLine{Spans: []Span{{Text: text, Attrs: attrs}}}
}

// Append adds a logical line to the buffer. Gagged lines are stored but
// not rendered (triggers may still reference them).
func (p *OutputPane) Append(l LogicalLine) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lines = append(p.lines, l)
	// Wrap eagerly only if we know our width; otherwise leave nil and let
	// Draw lazily compute it once geometry is known.
	if p.width > 0 && !l.Gagged {
		p.wrapped = append(p.wrapped, p.wrapLine(l))
	} else {
		p.wrapped = append(p.wrapped, nil)
	}
}

// Resize updates the pane dimensions. Called on terminal resize.
func (p *OutputPane) Resize(width, height int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if width != p.width {
		// Invalidate all cached wraps; Draw will recompute lazily.
		for i := range p.wrapped {
			p.wrapped[i] = nil
		}
	}
	p.width = width
	p.height = height
}

// ScrollUp scrolls up by n rows.
func (p *OutputPane) ScrollUp(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scroll += n
}

// ScrollDown scrolls down by n rows, clamped to 0 (live view).
func (p *OutputPane) ScrollDown(n int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.scroll -= n
	if p.scroll < 0 {
		p.scroll = 0
	}
}

// AtBottom returns true when live view is active (no scroll offset).
func (p *OutputPane) AtBottom() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scroll == 0
}

// Draw renders the output pane onto screen starting at row offsetY.
func (p *OutputPane) Draw(screen tcell.Screen, offsetY int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.width <= 0 || p.height <= 0 {
		return
	}

	// Wrap all logical lines into physical rows.
	rows := p.wrap()

	// Determine the window of rows to display.
	total := len(rows)
	end := total - p.scroll
	if end < 0 {
		end = 0
	}
	start := end - p.height
	if start < 0 {
		start = 0
	}

	// Clear the pane area first.
	for row := 0; row < p.height; row++ {
		for col := 0; col < p.width; col++ {
			screen.SetContent(col, offsetY+row, ' ', nil, tcell.StyleDefault)
		}
	}

	// Render rows top-to-bottom.
	screenRow := 0
	for ri := start; ri < end && screenRow < p.height; ri++ {
		col := 0
		for _, cell := range rows[ri] {
			if col >= p.width {
				break
			}
			screen.SetContent(col, offsetY+screenRow, cell.r, nil, cell.style)
			col += cell.width
		}
		screenRow++
	}
}

// ---------------------------------------------------------------------------
// internal wrapping helpers
// ---------------------------------------------------------------------------

type physCell struct {
	r     rune
	style tcell.Style
	width int // 1 for most chars, 2 for wide (CJK/emoji)
}

type physRow []physCell

// wrap converts all logical lines (with their spans) into physical rows
// of width p.width, respecting Unicode grapheme clusters and wide chars.
// Results are memoized per logical line in p.wrapped; only entries with a
// nil cache slot are recomputed.
func (p *OutputPane) wrap() []physRow {
	// Ensure cache slice tracks the lines slice.
	if len(p.wrapped) != len(p.lines) {
		nw := make([][]physRow, len(p.lines))
		copy(nw, p.wrapped)
		p.wrapped = nw
	}
	var rows []physRow
	for i, ll := range p.lines {
		if ll.Gagged {
			continue
		}
		if p.wrapped[i] == nil {
			p.wrapped[i] = p.wrapLine(ll)
		}
		rows = append(rows, p.wrapped[i]...)
	}
	return rows
}

func (p *OutputPane) wrapLine(ll LogicalLine) []physRow {
	// Build the full flat list of cells for the logical line first.
	type cell struct {
		r      rune
		style  tcell.Style
		width  int
		isSpace bool
	}
	var all []cell
	for _, span := range ll.Spans {
		style := attrToStyle(span.Attrs)
		gr := uniseg.NewGraphemes(span.Text)
		for gr.Next() {
			runes := gr.Runes()
			if len(runes) == 0 {
				continue
			}
			r := runes[0]
			if r == '\r' {
				continue
			}
			if r == '\n' {
				all = append(all, cell{r: '\n', style: style, width: 0})
				continue
			}
			w := gr.Width()
			if w == 0 {
				w = 1
			}
			all = append(all, cell{r: r, style: style, width: w, isSpace: r == ' ' || r == '\t'})
		}
	}

	// Walk cells, breaking at word boundaries when a line overflows.
	var rows []physRow
	var cur physRow
	col := 0
	lastBreak := -1  // index in cur where a space was last seen
	lastBreakCol := 0

	flushRow := func(row physRow) {
		rows = append(rows, row)
	}

	for _, c := range all {
		if c.r == '\n' {
			flushRow(cur)
			cur = nil
			col = 0
			lastBreak = -1
			continue
		}

		if c.isSpace {
			lastBreak = len(cur)
			lastBreakCol = col
		}

		if col+c.width > p.width {
			// Need to wrap. If there's a break point in current row, split there.
			if lastBreak > 0 {
				// Emit up to (not including) the space at lastBreak.
				flushRow(cur[:lastBreak])
				// Continue with cells after the break, dropping the space.
				carry := make(physRow, len(cur)-lastBreak)
				for i, pc := range cur[lastBreak:] {
					carry[i] = physCell{r: pc.r, style: pc.style, width: pc.width}
				}
				// Drop leading spaces from carry.
				for len(carry) > 0 && (carry[0].r == ' ' || carry[0].r == '\t') {
					carry = carry[1:]
				}
				cur = carry
				col = lastBreakCol - lastBreakCol // recalculate col from carry
				col = 0
				for _, pc := range cur {
					col += pc.width
				}
				lastBreak = -1
				lastBreakCol = 0
			} else {
				// No break point — hard wrap at column boundary.
				flushRow(cur)
				cur = nil
				col = 0
				lastBreak = -1
			}
		}

		cur = append(cur, physCell{r: c.r, style: c.style, width: c.width})
		col += c.width
	}

	if len(cur) > 0 || len(rows) == 0 {
		flushRow(cur)
	}
	return rows
}
