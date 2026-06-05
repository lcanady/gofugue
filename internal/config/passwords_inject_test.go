package config_test

// C-1: shell injection via password_cmd
// This test verifies that shell metacharacters in password_cmd are NOT
// interpreted by a shell — i.e. the command is run directly without /bin/sh.

import (
	"os"
	"runtime"
	"testing"

	"github.com/kumakun/gofugue/internal/config"
)

func TestWorldConfig_ResolvePassword_NoShellInjection(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix only")
	}

	sentinel := "/tmp/gofugue_pwn_test"
	// Clean up any leftover sentinel from a previous run.
	_ = os.Remove(sentinel)

	// A password_cmd with shell metacharacters: if /bin/sh -c is used, the
	// injected `touch /tmp/gofugue_pwn_test` will execute.
	wc := config.WorldConfig{
		PasswordCmd: "echo safe; touch " + sentinel,
	}
	_, _ = wc.ResolvePassword()

	if _, err := os.Stat(sentinel); err == nil {
		_ = os.Remove(sentinel)
		t.Fatal("SECURITY: shell injection succeeded — sentinel file was created; password_cmd must not be passed to /bin/sh -c")
	}
}
