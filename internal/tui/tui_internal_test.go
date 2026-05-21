package tui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/kumakun/gofugue/internal/bus"
)

func TestNew_Internal(t *testing.T) {
	b := bus.New()
	app := New(b)

	if app == nil {
		t.Fatal("expected New() to return a non-nil App, got nil")
	}
	if app.bus != b {
		t.Errorf("expected bus to be set, got %v", app.bus)
	}
	if app.themeStatus != tcell.StyleDefault {
		t.Errorf("expected themeStatus to be StyleDefault, got %v", app.themeStatus)
	}
	if app.themeInput != tcell.StyleDefault {
		t.Errorf("expected themeInput to be StyleDefault, got %v", app.themeInput)
	}
	if len(app.bindings) == 0 {
		t.Errorf("expected default bindings to be set")
	}
}
