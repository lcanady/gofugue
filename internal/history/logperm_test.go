package history

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestStartLog_FileModeIsOwnerOnly proves world logs (which contain
// channel chatter, tells, login sequences) are written 0600. Anything
// broader leaks game-session content to other local users.
func TestStartLog_FileModeIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("posix file mode semantics not enforced on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "world.log")

	b := New(100)
	if err := b.StartLog(path); err != nil {
		t.Fatalf("StartLog: %v", err)
	}
	defer b.StopLog()

	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o600 {
		t.Fatalf("log written with mode %#o, want 0600", perm)
	}
}
