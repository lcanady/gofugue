package tui

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/kumakun/gofugue/internal/bus"
)

// BenchmarkOutputPane_Draw measures the steady-state cost of drawing a full
// 5000-line scrollback to an 80x24 pane. With the wrap cache, this should be
// O(visible rows) rather than O(N x lineLen).
func BenchmarkOutputPane_Draw(b *testing.B) {
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		b.Fatalf("init sim screen: %v", err)
	}
	defer screen.Fini()
	screen.SetSize(80, 24)

	p := NewOutputPane(80, 24)
	sample := strings.Repeat("the quick brown fox jumps over the lazy dog ", 3) // ~132 chars
	for i := 0; i < 5000; i++ {
		p.Append(MakeLogicalLine(sample, bus.LineAttrs{FG: -1, BG: -1}))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Draw(screen, 0)
	}
}
