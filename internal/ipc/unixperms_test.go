package ipc_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/ipc"
)

// TestUnixSocket_Perms0600 — security: socket must not be world/group-accessible.
// Without an explicit Chmod, the listener inherits the process umask
// (typically 0022 → 0755), letting any local user connect and issue
// arbitrary /commands over the unauthenticated IPC channel.
func TestUnixSocket_Perms0600(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets")
	}
	dir := t.TempDir()
	sock := filepath.Join(dir, "gf.sock")

	srv := ipc.New(ipc.Config{SocketPath: sock}, bus.New())
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go srv.Run(ctx) //nolint:errcheck
	time.Sleep(80 * time.Millisecond)

	st, err := os.Stat(sock)
	if err != nil {
		t.Fatalf("stat socket: %v", err)
	}
	mode := st.Mode().Perm()
	if mode&0o077 != 0 {
		t.Fatalf("socket mode = %#o, must not be group/world accessible (want 0600-style)", mode)
	}
}
