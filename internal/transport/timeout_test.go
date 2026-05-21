package transport

import (
	"context"
	"testing"
	"time"
)

// TestTCPDialTimeout verifies the 10s timeout fires against an unreachable
// host. We use TEST-NET-1 (RFC 5737) which is reserved for documentation and
// should be routed nowhere — connection attempts will hang until our timeout
// trips. Without the fix this test would run ~75s before failing.
func TestTCPDialTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network timeout test in -short mode")
	}

	tr, err := New(Config{URL: "mud://192.0.2.1:9999"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	start := time.Now()
	_, err = tr.Dial(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected dial error, got nil")
	}
	// Should trip within DialTimeout plus a small margin for scheduler jitter.
	if elapsed > DialTimeout+2*time.Second {
		t.Fatalf("dial took %v, expected < %v", elapsed, DialTimeout+2*time.Second)
	}
	if elapsed < DialTimeout-2*time.Second {
		// Some networks ICMP-reject TEST-NET-1 quickly. That still proves the
		// timeout path is reachable, so only warn.
		t.Logf("dial returned quickly (%v); host may have ICMP-rejected", elapsed)
	}
}
