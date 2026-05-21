package config_test

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/kumakun/gofugue/internal/config"
)

// TestWorldConfig_ResolvePassword_PrefersPasswordCmd ensures users have a
// path off plaintext-in-TOML: a PasswordCmd shells out to a secret
// manager (pass, op, gopass, security-find-generic-password, …) and the
// stdout is used as the credential.
func TestWorldConfig_ResolvePassword_PrefersPasswordCmd(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh -c")
	}
	wc := config.WorldConfig{
		Password:    "plaintext-fallback",
		PasswordCmd: "printf hunter2",
	}
	got, err := wc.ResolvePassword()
	if err != nil {
		t.Fatalf("ResolvePassword: %v", err)
	}
	if got != "hunter2" {
		t.Fatalf("got %q, want %q", got, "hunter2")
	}
}

func TestWorldConfig_ResolvePassword_FallsBackToPlaintext(t *testing.T) {
	wc := config.WorldConfig{Password: "plain"}
	got, _ := wc.ResolvePassword()
	if got != "plain" {
		t.Fatalf("got %q", got)
	}
}

// TestLoad_LoosePermsWithPasswords_Warns — defensive UX: when the user
// has stored plaintext passwords AND the config file is group/world
// readable, surface a warning the host can display.
func TestLoad_LoosePermsWithPasswords_Warns(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix perms only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := `
[world.demo]
url = "mud://localhost:4000"
password = "secret"
`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, warnings, err := config.LoadWithWarnings(path)
	if err != nil {
		t.Fatalf("LoadWithWarnings: %v", err)
	}
	joined := strings.Join(warnings, "\n")
	if !strings.Contains(joined, "demo") || !strings.Contains(strings.ToLower(joined), "password") {
		t.Fatalf("expected warning naming world and password; got: %v", warnings)
	}
}
