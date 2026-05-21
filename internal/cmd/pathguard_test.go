package cmd

import "testing"

// TestValidateScriptPath_RejectsExfilAttempts — security:
// /load, /js, /py must not be usable to read arbitrary files. The
// extension guard makes it impossible to point them at /etc/passwd,
// ~/.ssh/id_rsa, or the user's config.toml.
func TestValidateScriptPath_RejectsExfilAttempts(t *testing.T) {
	bad := []string{
		"/etc/passwd",
		"/root/.ssh/id_rsa",
		"../../../etc/shadow",
		"/proc/self/environ",
		"/Users/alice/.config/gofugue/config.toml",
		"oops\x00.tf", // NUL byte
	}
	for _, p := range bad {
		if err := validateScriptPath(p, ".tf", ".gf"); err == nil {
			t.Errorf("validateScriptPath(%q) = nil, want error", p)
		}
	}
}

func TestValidateScriptPath_AcceptsLegitimateScripts(t *testing.T) {
	good := []string{
		"~/.tf/mymud.tf",
		"./scripts/foo.gf",
		"/usr/local/share/gofugue/example.TF", // case-insensitive
	}
	for _, p := range good {
		if err := validateScriptPath(p, ".tf", ".gf"); err != nil {
			t.Errorf("validateScriptPath(%q) = %v, want nil", p, err)
		}
	}
}

func TestValidateScriptPath_NoExtList_StillBlocksNUL(t *testing.T) {
	if err := validateScriptPath("foo\x00.tf"); err == nil {
		t.Error("NUL byte must always be rejected")
	}
}
