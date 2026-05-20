package tui

import (
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
	"github.com/kumakun/gofugue/internal/keyboard"
)

// InputBar renders the single-line input field at the bottom of the screen.
type InputBar struct {
	editor *keyboard.LineEditor
	width  int
	prompt string
	style  tcell.Style
}

func newInputBar(width int) *InputBar {
	return &InputBar{
		editor: &keyboard.LineEditor{},
		width:  width,
		prompt: "> ",
		style:  tcell.StyleDefault,
	}
}

// SetStyle replaces the style used to render the input bar.
func (b *InputBar) SetStyle(style tcell.Style) { b.style = style }

// NewInputBar is the exported constructor used by tests.
func NewInputBar(width int) *InputBar { return newInputBar(width) }

// Resize updates the bar width on terminal resize.
func (b *InputBar) Resize(width int) { b.width = width }

// Editor returns the underlying line editor.
func (b *InputBar) Editor() *keyboard.LineEditor { return b.editor }

// SetPrompt changes the prompt prefix (e.g. "[world] > ").
func (b *InputBar) SetPrompt(p string) { b.prompt = p }

// Draw renders the input bar on the given screen row.
func (b *InputBar) Draw(screen tcell.Screen, row int) {
	style := b.style
	text := b.prompt + b.editor.Text()

	col := 0
	gr := uniseg.NewGraphemes(text)
	for gr.Next() && col < b.width {
		runes := gr.Runes()
		if len(runes) == 0 {
			continue
		}
		screen.SetContent(col, row, runes[0], runes[1:], style)
		col += gr.Width()
	}
	// Pad the rest of the bar.
	for ; col < b.width; col++ {
		screen.SetContent(col, row, ' ', nil, style)
	}

	// Place cursor after prompt + text up to editor cursor position.
	cursorCol := visWidth(b.prompt) + visWidth(b.editor.Text()[:runeByteOffset(b.editor.Text(), b.editor.Cursor())])
	if cursorCol > b.width-1 {
		cursorCol = b.width - 1
	}
	screen.ShowCursor(cursorCol, row)
}

// visWidth returns the display width of a string (counting wide chars as 2).
func visWidth(s string) int {
	w := 0
	gr := uniseg.NewGraphemes(s)
	for gr.Next() {
		w += gr.Width()
	}
	return w
}

// runeByteOffset converts a rune index into a byte offset in s.
func runeByteOffset(s string, runeIdx int) int {
	b := 0
	for i := range s {
		if runeIdx == 0 {
			return i
		}
		runeIdx--
		b = i
		_ = b
	}
	return len(s)
}
