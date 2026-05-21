package history

import (
	"sync/atomic"
	"testing"
	"time"
)

// countingWriter counts Write calls. Each bufio Flush that drains buffered
// data triggers one underlying Write — so counting Writes is a faithful
// proxy for counting flushes.
type countingWriter struct {
	writes int64
	bytes  int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	atomic.AddInt64(&c.writes, 1)
	atomic.AddInt64(&c.bytes, int64(len(p)))
	return len(p), nil
}

func TestAppendThrottlesFlush(t *testing.T) {
	b := New(2048)
	cw := &countingWriter{}
	b.startLogWriter(cw)

	const n = 1000
	for i := 0; i < n; i++ {
		b.Append(Line{Text: "hello", Timestamp: time.Now()})
	}
	b.StopLog()

	writes := atomic.LoadInt64(&cw.writes)
	// Worst case: one flush per flushLineLimit lines, plus one on StopLog.
	// Allow generous slack for timer-driven flushes too.
	upper := int64(n/flushLineLimit + 50)
	if writes >= int64(n) {
		t.Fatalf("flush not throttled: %d writes for %d appends", writes, n)
	}
	if writes > upper {
		t.Fatalf("too many flushes: got %d, expected <= %d", writes, upper)
	}
	if writes == 0 {
		t.Fatalf("expected at least one flush from StopLog")
	}
}
