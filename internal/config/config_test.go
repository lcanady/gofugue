package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kumakun/gofugue/internal/config"
)

func TestDefaults_SensibleValues(t *testing.T) {
	cfg := config.Defaults()

	if cfg.ScrollbackLines <= 0 {
		t.Errorf("ScrollbackLines = %d, want > 0", cfg.ScrollbackLines)
	}
	if cfg.IPC.TCPPort <= 0 {
		t.Errorf("IPC.TCPPort = %d, want > 0", cfg.IPC.TCPPort)
	}
	if cfg.IPC.WSPort <= 0 {
		t.Errorf("IPC.WSPort = %d, want > 0", cfg.IPC.WSPort)
	}
	if cfg.Worlds == nil {
		t.Error("Worlds map should be initialised, not nil")
	}
}

func TestDefaults_TCPAndWSPortsDistinct(t *testing.T) {
	cfg := config.Defaults()
	if cfg.IPC.TCPPort == cfg.IPC.WSPort {
		t.Errorf("TCP port %d and WS port %d must differ", cfg.IPC.TCPPort, cfg.IPC.WSPort)
	}
}

func TestDefaultPath_NotEmpty(t *testing.T) {
	p := config.DefaultPath()
	if p == "" {
		t.Error("DefaultPath() returned empty string")
	}
}

func TestDefaultSocketPath_NotEmpty(t *testing.T) {
	p := config.DefaultSocketPath()
	if p == "" {
		t.Error("DefaultSocketPath() returned empty string")
	}
}

func TestDefaultPath_XDGOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	p := config.DefaultPath()
	if p == "" {
		t.Error("DefaultPath() returned empty under XDG_CONFIG_HOME")
	}
	// Should start with our temp dir.
	if len(p) < len(dir) || p[:len(dir)] != dir {
		t.Errorf("DefaultPath() = %q, expected prefix %q", p, dir)
	}
}

func TestDefaultSocketPath_XDGOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	p := config.DefaultSocketPath()
	if p == "" {
		t.Error("DefaultSocketPath() returned empty under XDG_CONFIG_HOME")
	}
	if len(p) < len(dir) || p[:len(dir)] != dir {
		t.Errorf("DefaultSocketPath() = %q, expected prefix %q", p, dir)
	}
}

func TestDefaultPath_NoXDG_UsesHomeDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "")
	home, _ := os.UserHomeDir()

	p := config.DefaultPath()
	if len(p) < len(home) {
		t.Errorf("DefaultPath() = %q, expected to contain home %q", p, home)
	}
}

// --- Load ---

func TestLoad_MissingFile_ReturnsDefaults(t *testing.T) {
	cfg, err := config.Load("/nonexistent/path/config.toml")
	if err != nil {
		t.Fatalf("Load of missing file should not error: %v", err)
	}
	// Should be populated with defaults.
	if cfg.ScrollbackLines <= 0 {
		t.Errorf("ScrollbackLines = %d, want > 0", cfg.ScrollbackLines)
	}
}

func writeToml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("writeToml: %v", err)
	}
	return path
}

func TestLoad_BasicFields(t *testing.T) {
	path := writeToml(t, `
scrollback_lines = 2000
default_world    = "myworld"
startup_script   = "/tmp/start.tf"
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ScrollbackLines != 2000 {
		t.Errorf("ScrollbackLines = %d, want 2000", cfg.ScrollbackLines)
	}
	if cfg.DefaultWorld != "myworld" {
		t.Errorf("DefaultWorld = %q, want %q", cfg.DefaultWorld, "myworld")
	}
	if cfg.StartupScript != "/tmp/start.tf" {
		t.Errorf("StartupScript = %q", cfg.StartupScript)
	}
}

func TestLoad_IPCConfig(t *testing.T) {
	path := writeToml(t, `
[ipc]
tcp_port = 9000
ws_port  = 9001
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.IPC.TCPPort != 9000 {
		t.Errorf("IPC.TCPPort = %d, want 9000", cfg.IPC.TCPPort)
	}
	if cfg.IPC.WSPort != 9001 {
		t.Errorf("IPC.WSPort = %d, want 9001", cfg.IPC.WSPort)
	}
}

func TestLoad_WorldProfiles(t *testing.T) {
	path := writeToml(t, `
[world.avalon]
url   = "mud://avalon.mud:23"
login = "player\npassword"

[world.test]
url            = "muds://secure.mud:4000"
tls_skip_verify = true
`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	avalon, ok := cfg.Worlds["avalon"]
	if !ok {
		t.Fatal("world 'avalon' not found")
	}
	if avalon.URL != "mud://avalon.mud:23" {
		t.Errorf("avalon.URL = %q", avalon.URL)
	}
	// Name should be filled from map key.
	if avalon.Name != "avalon" {
		t.Errorf("avalon.Name = %q, want %q", avalon.Name, "avalon")
	}
	testW, ok := cfg.Worlds["test"]
	if !ok {
		t.Fatal("world 'test' not found")
	}
	if !testW.TLSSkipVerify {
		t.Error("test.TLSSkipVerify should be true")
	}
}

func TestLoad_InvalidTOML_ReturnsError(t *testing.T) {
	path := writeToml(t, `this is not toml ][[[`)
	_, err := config.Load(path)
	if err == nil {
		t.Error("Load of invalid TOML should return error")
	}
}

func TestLoad_MergesOverDefaults(t *testing.T) {
	// Only set scrollback_lines; everything else should keep default values.
	path := writeToml(t, `scrollback_lines = 999`)
	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ScrollbackLines != 999 {
		t.Errorf("ScrollbackLines = %d, want 999", cfg.ScrollbackLines)
	}
	// Default IPC ports should still be present.
	if cfg.IPC.TCPPort == 0 {
		t.Error("IPC.TCPPort should have default value after partial load")
	}
}

func TestDefaults_Exhaustive(t *testing.T) {
	cfg := config.Defaults()

	if cfg.ScrollbackLines != 5000 {
		t.Errorf("ScrollbackLines = %d, want 5000", cfg.ScrollbackLines)
	}
	if cfg.WrapWidth != 0 {
		t.Errorf("WrapWidth = %d, want 0", cfg.WrapWidth)
	}
	if cfg.IPC.TCPPort != 7878 {
		t.Errorf("IPC.TCPPort = %d, want 7878", cfg.IPC.TCPPort)
	}
	if cfg.IPC.WSPort != 7879 {
		t.Errorf("IPC.WSPort = %d, want 7879", cfg.IPC.WSPort)
	}
	if cfg.DefaultWorld != "" {
		t.Errorf("DefaultWorld = %q, want empty", cfg.DefaultWorld)
	}
	if cfg.StartupScript != "" {
		t.Errorf("StartupScript = %q, want empty", cfg.StartupScript)
	}

	// Theme default checks
	if cfg.Theme.StatusFG != "" || cfg.Theme.StatusBG != "" || cfg.Theme.StatusBold || cfg.Theme.StatusReverse {
		t.Errorf("Theme Status defaults should be empty/false")
	}
	if cfg.Theme.InputFG != "" || cfg.Theme.InputBG != "" || cfg.Theme.InputReverse {
		t.Errorf("Theme Input defaults should be empty/false")
	}

	if cfg.Worlds == nil {
		t.Error("Worlds map should be initialised, not nil")
	} else if len(cfg.Worlds) != 0 {
		t.Errorf("Worlds map length = %d, want 0", len(cfg.Worlds))
	}
}
