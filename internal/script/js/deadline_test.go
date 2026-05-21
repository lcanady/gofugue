package js_test

import (
	"strings"
	"testing"
	"time"
)

// TestEval_InfiniteLoop_HitsDeadline — guards against a malicious or
// buggy script wedging the JS event-loop goroutine. Pre-fix this hung
// forever; post-fix it returns within a couple of seconds with an
// "interrupted" / "deadline" error.
func TestEval_InfiniteLoop_HitsDeadline(t *testing.T) {
	br := newBridge(t, nil)
	done := make(chan error, 1)
	go func() {
		_, err := br.Eval("while(true){}")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected interrupt error, got nil (script returned normally?)")
		}
		if !strings.Contains(strings.ToLower(err.Error()), "deadline") &&
			!strings.Contains(strings.ToLower(err.Error()), "interrupt") {
			t.Fatalf("error %q does not look like a deadline interrupt", err)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("script did not terminate within 6s — deadline not enforced")
	}
}
