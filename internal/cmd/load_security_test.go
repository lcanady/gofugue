package cmd_test

import (
	"context"
	"strings"
	"testing"

	"github.com/kumakun/gofugue/internal/cmd"
)

// TestLoad_RejectsArbitraryFileRead — end-to-end exploit guard.
// Without the extension check, an unauthenticated IPC caller (or any
// reachable channel that funnels into the dispatcher) can point /load
// at sensitive system files and observe the contents via parser
// diagnostics. The dispatcher must refuse the call before any
// filesystem access happens.
func TestLoad_RejectsArbitraryFileRead(t *testing.T) {
	called := false
	d := cmd.New()
	ctx := &cmd.Context{
		Ctx:    context.Background(),
		Output: func(string) {},
		LoadFile: func(string) error {
			called = true
			return nil
		},
	}

	exploits := []string{
		"/load /etc/passwd",
		"/load /root/.ssh/id_rsa",
		"/load ../../../../etc/shadow",
	}
	for _, line := range exploits {
		err := d.Dispatch(ctx, line)
		if err == nil {
			t.Errorf("%q: expected refusal, got nil error", line)
		}
		if called {
			t.Fatalf("%q: LoadFile was invoked — exploit succeeded", line)
		}
		if !strings.Contains(err.Error(), "extension") && !strings.Contains(err.Error(), "refusing") {
			t.Errorf("%q: error %q does not look like a path-guard refusal", line, err)
		}
	}
}

func TestLoad_AcceptsTfScript(t *testing.T) {
	d := cmd.New()
	called := false
	ctx := &cmd.Context{
		Ctx:    context.Background(),
		Output: func(string) {},
		LoadFile: func(p string) error {
			called = true
			if !strings.HasSuffix(p, ".tf") {
				t.Errorf("got %q", p)
			}
			return nil
		},
	}
	if err := d.Dispatch(ctx, "/load ~/scripts/mymud.tf"); err != nil {
		t.Fatalf("legitimate .tf load rejected: %v", err)
	}
	if !called {
		t.Fatal("LoadFile not invoked for valid path")
	}
}
