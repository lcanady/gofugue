package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kumakun/gofugue/internal/config"
)

// TestLoadWithWarnings_PasswordCmd_GroupWritable proves that a
// password_cmd in a group/world-writable config file produces a loud
// RCE warning. A writable config means any attacker who can drop a
// line into it gets shell execution on every gofugue start.
func TestLoadWithWarnings_PasswordCmd_GroupWritable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix file modes")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[world.demo]
url = "mud://example.org:4000"
password_cmd = "pass show mud/demo"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chmod(path, 0o620); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	_, warnings, err := config.LoadWithWarnings(path)
	if err != nil {
		t.Fatalf("LoadWithWarnings: %v", err)
	}
	hit := false
	for _, w := range warnings {
		if strings.Contains(w, "password_cmd") && strings.Contains(w, "writable") {
			hit = true
		}
	}
	if !hit {
		t.Fatalf("expected password_cmd writable warning, got %v", warnings)
	}
}
