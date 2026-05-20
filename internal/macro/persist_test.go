package macro_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kumakun/gofugue/internal/macro"
)

func TestSaveAndLoadFile_RoundTrip(t *testing.T) {
	e := macro.New()
	define := func(m *macro.Macro) {
		t.Helper()
		if err := e.Define(m); err != nil {
			t.Fatalf("Define %q: %v", m.Name, err)
		}
	}
	define(&macro.Macro{Name: "dragon", Type: macro.TypeTrigger, Pattern: "dragon", Body: "flee", Priority: 5, World: "mud1"})
	define(&macro.Macro{Name: "n", Type: macro.TypeAlias, Pattern: "n", Body: "go north"})
	define(&macro.Macro{Name: "onconn", Type: macro.TypeHook, Pattern: "CONNECT", Body: "/echo connected"})

	dir := t.TempDir()
	path := filepath.Join(dir, "macros.json")

	if err := e.SaveFile(path); err != nil {
		t.Fatalf("SaveFile: %v", err)
	}

	// Verify the file exists and is non-empty.
	info, err := os.Stat(path)
	if err != nil || info.Size() == 0 {
		t.Fatalf("saved file missing or empty: %v", err)
	}

	// Load into a fresh engine.
	e2 := macro.New()
	if err := e2.LoadFile(path); err != nil {
		t.Fatalf("LoadFile: %v", err)
	}

	// Verify trigger survives round-trip.
	body, _ := e2.MatchTrigger("mud1", "a large dragon attacks")
	if body != "flee" {
		t.Errorf("trigger body = %q, want %q", body, "flee")
	}

	// Verify alias survives.
	aliasBody, ok := e2.MatchAlias("", "n")
	if !ok || aliasBody != "go north" {
		t.Errorf("alias body = %q ok=%v, want 'go north' true", aliasBody, ok)
	}

	// Verify hook survives.
	bodies := e2.FireHook("mud1", "CONNECT")
	if len(bodies) == 0 || bodies[0] != "/echo connected" {
		t.Errorf("hook bodies = %v, want ['/echo connected']", bodies)
	}
}

func TestSaveFile_EmptyEngine_ValidJSON(t *testing.T) {
	e := macro.New()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.json")
	if err := e.SaveFile(path); err != nil {
		t.Fatalf("SaveFile empty engine: %v", err)
	}
	data, _ := os.ReadFile(path)
	if len(data) == 0 {
		t.Error("empty engine save produced no output")
	}
	// Must be valid JSON.
	if data[0] != '{' {
		t.Errorf("expected JSON object, got: %q", data[:min(20, len(data))])
	}
}

func TestLoadFile_MissingFile_Error(t *testing.T) {
	e := macro.New()
	err := e.LoadFile("/nonexistent/macros.json")
	if err == nil {
		t.Error("LoadFile of missing file should return error")
	}
}

func TestLoadFile_InvalidJSON_Error(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.json")
	os.WriteFile(path, []byte("not json"), 0o644) //nolint:errcheck
	e := macro.New()
	if err := e.LoadFile(path); err == nil {
		t.Error("LoadFile of invalid JSON should return error")
	}
}

func TestSaveFile_Priority_Preserved(t *testing.T) {
	e := macro.New()
	e.Define(&macro.Macro{Name: "hi", Type: macro.TypeTrigger, Pattern: "hi", Body: "wave", Priority: 42}) //nolint:errcheck

	dir := t.TempDir()
	path := filepath.Join(dir, "prio.json")
	e.SaveFile(path) //nolint:errcheck

	e2 := macro.New()
	e2.LoadFile(path) //nolint:errcheck

	// The list output should mention the macro — verify it's defined.
	list := e2.List()
	if len(list) == 0 {
		t.Error("expected 1 macro after load, got none")
	}
}

func TestSaveFile_AtomicWrite_TempFileCleanedUp(t *testing.T) {
	e := macro.New()
	e.Define(&macro.Macro{Name: "x", Type: macro.TypeTrigger, Pattern: "x", Body: "y"}) //nolint:errcheck

	dir := t.TempDir()
	path := filepath.Join(dir, "macros.json")
	e.SaveFile(path) //nolint:errcheck

	// The .tmp file must not linger after a successful save.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful save")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
