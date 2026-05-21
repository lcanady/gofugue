package world

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/config"
)

var statFile = os.Stat

// TestSaveTOML_FileModeIsOwnerOnly proves the world config file (which can
// hold plaintext passwords or password_cmd lines that disclose a user's
// secret-manager workflow) is written with mode 0600. Anything broader
// leaks credentials to other local users on shared hosts.
func TestSaveTOML_FileModeIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix file mode semantics not enforced on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "worlds.toml")

	mgr := NewManager(bus.New())
	mgr.Add(config.WorldConfig{Name: "demo", URL: "mud://example.org:4000", Pass: "hunter2"})

	if err := mgr.SaveTOML(path); err != nil {
		t.Fatalf("SaveTOML: %v", err)
	}

	st, err := statFile(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("world config written with mode %#o, want 0600 (group/world-readable secrets)", perm)
	}
}
