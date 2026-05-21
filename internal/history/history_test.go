package history_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/history"
)

func line(text string) history.Line {
	return history.Line{Text: text, Timestamp: time.Now()}
}

func TestNew(t *testing.T) {
	b := history.New(5)
	if b == nil {
		t.Fatal("New() returned nil")
	}

	// It should be empty initially
	lines := b.Tail(10)
	if len(lines) != 0 {
		t.Errorf("New buffer should be empty, got %d lines", len(lines))
	}

	// Verify capacity is set correctly by triggering an overflow
	for i := 0; i < 7; i++ {
		b.Append(line("text"))
	}

	lines = b.Tail(10)
	if len(lines) != 5 {
		t.Errorf("Buffer retained %d lines, expected capacity 5", len(lines))
	}
}

func TestBuffer_AppendAndTail(t *testing.T) {
	b := history.New(100)
	b.Append(line("first"))
	b.Append(line("second"))
	b.Append(line("third"))

	lines := b.Tail(3)
	if len(lines) != 3 {
		t.Fatalf("Tail(3) returned %d lines, want 3", len(lines))
	}
	if lines[0].Text != "first" || lines[1].Text != "second" || lines[2].Text != "third" {
		t.Errorf("unexpected order: %v", lines)
	}
}

func TestBuffer_Tail_ClipsToCount(t *testing.T) {
	b := history.New(100)
	b.Append(line("a"))
	b.Append(line("b"))

	lines := b.Tail(50) // ask for more than stored
	if len(lines) != 2 {
		t.Errorf("Tail(50) returned %d lines, want 2", len(lines))
	}
}

func TestBuffer_RingOverflow(t *testing.T) {
	cap := 3
	b := history.New(cap)
	for i, text := range []string{"a", "b", "c", "d", "e"} {
		_ = i
		b.Append(line(text))
	}

	lines := b.Tail(cap)
	if len(lines) != cap {
		t.Fatalf("Tail(%d) = %d lines, want %d", cap, len(lines), cap)
	}
	// oldest should be "c" (a, b evicted)
	if lines[0].Text != "c" {
		t.Errorf("lines[0].Text = %q, want %q", lines[0].Text, "c")
	}
	if lines[cap-1].Text != "e" {
		t.Errorf("lines[%d].Text = %q, want %q", cap-1, lines[cap-1].Text, "e")
	}
}

func TestBuffer_Search_FindsMatch(t *testing.T) {
	b := history.New(100)
	b.Append(line("A dragon appears"))
	b.Append(line("A goblin appears"))
	b.Append(line("Another dragon roars"))

	results := b.Search("dragon")
	if len(results) != 2 {
		t.Errorf("Search('dragon') = %d results, want 2", len(results))
	}
}

func TestBuffer_Search_EmptyPattern_ReturnsAll(t *testing.T) {
	b := history.New(100)
	b.Append(line("x"))
	b.Append(line("y"))

	results := b.Search("")
	if len(results) != 2 {
		t.Errorf("Search('') = %d, want 2", len(results))
	}
}

func TestBuffer_Search_NoMatch(t *testing.T) {
	b := history.New(100)
	b.Append(line("hello world"))

	results := b.Search("zzz")
	if len(results) != 0 {
		t.Errorf("Search('zzz') = %d results, want 0", len(results))
	}
}

func TestBuffer_Log_WritesToFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.log")

	b := history.New(100)
	if err := b.StartLog(path); err != nil {
		t.Fatalf("StartLog: %v", err)
	}

	b.Append(line("logged line 1"))
	b.Append(line("logged line 2"))
	b.StopLog()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	content := string(data)
	if !strings.Contains(content, "logged line 1") {
		t.Error("log missing 'logged line 1'")
	}
	if !strings.Contains(content, "logged line 2") {
		t.Error("log missing 'logged line 2'")
	}
}

func TestBuffer_Log_GaggedLines_NotLogged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.log")

	b := history.New(100)
	_ = b.StartLog(path)
	b.Append(history.Line{Text: "visible", Timestamp: time.Now(), Gagged: false})
	b.Append(history.Line{Text: "secret", Timestamp: time.Now(), Gagged: true})
	b.StopLog()

	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "secret") {
		t.Error("gagged line should not appear in log file")
	}
}

func TestBuffer_StopLog_Idempotent(t *testing.T) {
	b := history.New(10)
	b.StopLog() // no log started — should not panic
	b.StopLog()
}

func TestBuffer_LineAttrs_Preserved(t *testing.T) {
	b := history.New(10)
	l := history.Line{
		Text:      "colored",
		Attrs:     bus.LineAttrs{FG: 31, BG: -1, Bold: true},
		Timestamp: time.Now(),
	}
	b.Append(l)
	lines := b.Tail(1)
	if lines[0].Attrs.FG != 31 {
		t.Errorf("Attrs.FG = %d, want 31", lines[0].Attrs.FG)
	}
	if !lines[0].Attrs.Bold {
		t.Error("Attrs.Bold should be true")
	}
}

func TestBuffer_Concurrent_AppendSearchTail(t *testing.T) {
	b := history.New(500)
	const writers = 4
	const readsPerGoroutine = 50
	const writesPerGoroutine = 100

	var wg sync.WaitGroup

	// Writers
	for g := 0; g < writers; g++ {
		g := g
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < writesPerGoroutine; i++ {
				b.Append(history.Line{
					Text:      strings.Repeat("x", g*10+i%5),
					Timestamp: time.Now(),
				})
			}
		}()
	}

	// Concurrent readers: Tail
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < readsPerGoroutine; i++ {
				_ = b.Tail(10)
			}
		}()
	}

	// Concurrent readers: Search
	for g := 0; g < 2; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < readsPerGoroutine; i++ {
				_ = b.Search("xx")
			}
		}()
	}

	wg.Wait()

	// Sanity: some lines were written
	if lines := b.Tail(1); len(lines) == 0 {
		t.Error("Tail(1) returned no lines after concurrent writes")
	}
}

func TestBuffer_StartLog_PermissionDenied(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	dir := t.TempDir()
	// Make directory read-only so file creation fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) }) //nolint:errcheck

	b := history.New(10)
	path := filepath.Join(dir, "nope.log")
	err := b.StartLog(path)
	if err == nil {
		b.StopLog()
		t.Fatal("StartLog should fail on unwritable directory")
	}
}

func TestBuffer_StartLog_ReplacesExistingLog(t *testing.T) {
	dir := t.TempDir()
	path1 := filepath.Join(dir, "first.log")
	path2 := filepath.Join(dir, "second.log")

	b := history.New(100)
	if err := b.StartLog(path1); err != nil {
		t.Fatalf("StartLog(path1): %v", err)
	}
	b.Append(line("in first"))

	// Opening a second log should close the first.
	if err := b.StartLog(path2); err != nil {
		t.Fatalf("StartLog(path2): %v", err)
	}
	b.Append(line("in second"))
	b.StopLog()

	data2, _ := os.ReadFile(path2)
	if !strings.Contains(string(data2), "in second") {
		t.Error("second log missing 'in second'")
	}
	// First log file still exists (just closed), but "in second" not in it.
	data1, _ := os.ReadFile(path1)
	if strings.Contains(string(data1), "in second") {
		t.Error("first log should not contain 'in second'")
	}
}

func TestBuffer_StopLog_FlushesBuffered(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "flush.log")

	b := history.New(100)
	_ = b.StartLog(path)
	b.Append(line("before stop"))
	// Do NOT call StopLog yet — Append flushes after each write, so data
	// is already on disk. This verifies the normal flush path.
	b.StopLog()

	data, _ := os.ReadFile(path)
	if !strings.Contains(string(data), "before stop") {
		t.Error("log should contain 'before stop' after StopLog")
	}
}

func BenchmarkBuffer_Search(b *testing.B) {
	buf := history.New(1000)
	for i := 0; i < 1000; i++ {
		buf.Append(history.Line{
			Text:      "this is a sample line that might contain the word dragon or goblin or maybe something else entirely",
			Timestamp: time.Now(),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Search("dragon")
	}
}

func BenchmarkBuffer_Search_Regex(b *testing.B) {
	buf := history.New(1000)
	for i := 0; i < 1000; i++ {
		buf.Append(history.Line{
			Text:      "this is a sample line that might contain the word dragon or goblin or maybe something else entirely",
			Timestamp: time.Now(),
		})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Search("dragon|goblin")
	}
}
