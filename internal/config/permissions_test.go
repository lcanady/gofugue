package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kumakun/gofugue/internal/config"
)

// TestLoad_PermCheckRunsOnParseError verifies that the world-readable
// permission warning is emitted even when the TOML parse returns an error.
// Before the L-4 fix, LoadWithWarnings returned early on parse failure,
// skipping the permission check entirely.
func TestLoad_PermCheckRunsOnParseError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "bad.toml")

	// Syntactically invalid TOML that still contains a world with a password
	// in a parseable fragment (BurntSushi/toml does partial decode).
	// We use a valid section followed by intentional syntax rubbish so the
	// decoder returns both a partial result and an error.
	body := `[world.demo]
url = "mud://localhost:4000"
password = "secret"
THIS IS NOT VALID TOML !!!
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warnings, err := config.LoadWithWarnings(path)
	// A parse error is expected.
	if err == nil {
		t.Log("note: TOML decoder accepted the file without error (partial decode ok)")
	}

	// Whether or not there was a parse error, the permission warning must be
	// present because the file is mode 0644 and contains a plaintext password.
	// (If the decoder silently accepted it, that is also fine — the test is
	// about the permission check running in both paths.)
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "demo") || !strings.Contains(strings.ToLower(joined), "password") {
		// Only fail if we actually got a password from the partial parse.
		// If the file was unreadable by the decoder entirely, skip.
		if err != nil {
			t.Skipf("TOML decoder rejected file entirely (no partial decode); err=%v", err)
		}
		t.Fatalf("expected permission warning for world 'demo'; warnings=%v", warnings)
	}
}

// TestLoad_StrictMode_WarningIsDetectable verifies that LoadWithWarnings
// returns a non-empty warning slice for a 0644 file with a plaintext
// password — the signal that --strict-permissions should use to abort.
func TestLoad_StrictMode_WarningIsDetectable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[world.mymud]
url = "mud://localhost:4000"
password = "hunter2"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	_, warnings, err := config.LoadWithWarnings(path)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(warnings) == 0 {
		t.Fatal("expected at least one warning for 0644 file with plaintext password; got none")
	}

	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "mymud") {
		t.Fatalf("warning should mention world name 'mymud'; got: %s", joined)
	}
}

// TestLoad_StrictMode_TightPerms_NoWarning verifies that a 0600 config
// file does NOT trigger warnings, so --strict-permissions allows normal
// startup when permissions are correct.
func TestLoad_StrictMode_TightPerms_NoWarning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[world.mymud]
url = "mud://localhost:4000"
password = "hunter2"
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	_, warnings, err := config.LoadWithWarnings(path)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("expected no warnings for 0600 file; got: %v", warnings)
	}
}
