// Package history manages per-world scrollback ring buffers and session logging.
package history

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sync"
	"time"

	"github.com/kumakun/gofugue/internal/bus"
)

var (
	regexCacheMu sync.Mutex
	regexCache   [16]struct {
		pattern string
		re      *regexp.Regexp
		err     error
		valid   bool
	}
)

func compileSearchPattern(pattern string) (*regexp.Regexp, error) {
	regexCacheMu.Lock()
	for i := 0; i < len(regexCache); i++ {
		if regexCache[i].valid && regexCache[i].pattern == pattern {
			re, err := regexCache[i].re, regexCache[i].err
			// Move to front (LRU)
			if i > 0 {
				entry := regexCache[i]
				copy(regexCache[1:i+1], regexCache[0:i])
				regexCache[0] = entry
			}
			regexCacheMu.Unlock()
			return re, err
		}
	}
	regexCacheMu.Unlock()

	re, err := regexp.Compile(pattern)

	regexCacheMu.Lock()
	// move everything down by 1
	copy(regexCache[1:], regexCache[0:len(regexCache)-1])
	regexCache[0].pattern = pattern
	regexCache[0].re = re
	regexCache[0].err = err
	regexCache[0].valid = true
	regexCacheMu.Unlock()

	return re, err
}

// Line is a single entry in the scrollback buffer.
type Line struct {
	Text      string
	Attrs     bus.LineAttrs
	Timestamp time.Time
	Gagged    bool
}

// Buffer is a fixed-capacity ring buffer of Lines for one world.
type Buffer struct {
	mu       sync.RWMutex
	lines    []Line
	head     int
	count    int
	capacity int

	logFile *os.File
	logBuf  *bufio.Writer
}

// New returns a Buffer with the given capacity.
func New(capacity int) *Buffer {
	return &Buffer{
		lines:    make([]Line, capacity),
		capacity: capacity,
	}
}

// Append adds a line to the buffer and writes it to the log file if open.
func (b *Buffer) Append(l Line) {
	b.mu.Lock()
	defer b.mu.Unlock()

	idx := (b.head + b.count) % b.capacity
	b.lines[idx] = l
	if b.count < b.capacity {
		b.count++
	} else {
		b.head = (b.head + 1) % b.capacity
	}

	if b.logBuf != nil && !l.Gagged {
		fmt.Fprintf(b.logBuf, "[%s] %s\n", l.Timestamp.Format("15:04:05"), l.Text)
		b.logBuf.Flush()
	}
}

// Tail returns the last n lines (or all if n > count).
func (b *Buffer) Tail(n int) []Line {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if n > b.count {
		n = b.count
	}
	out := make([]Line, n)
	for i := 0; i < n; i++ {
		idx := (b.head + b.count - n + i) % b.capacity
		out[i] = b.lines[idx]
	}
	return out
}

// Search returns all lines whose Text matches pattern.
// Pattern is compiled as a regexp; if it fails to compile, a literal substring
// match is used as a fallback.
func (b *Buffer) Search(pattern string) []Line {
	b.mu.RLock()
	defer b.mu.RUnlock()
	re, reErr := compileSearchPattern(pattern)
	var out []Line
	for i := 0; i < b.count; i++ {
		l := b.lines[(b.head+i)%b.capacity]
		var match bool
		if reErr == nil {
			match = re.MatchString(l.Text)
		} else {
			match = containsStr(l.Text, pattern)
		}
		if match {
			out = append(out, l)
		}
	}
	return out
}

// StartLog opens path for appending and logs subsequent non-gagged lines.
func (b *Buffer) StartLog(path string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.logFile != nil {
		b.logFile.Close()
	}
	// 0o600 — world logs hold private channel chatter and login sequences.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	b.logFile = f
	b.logBuf = bufio.NewWriter(f)
	return nil
}

// StopLog closes the log file.
func (b *Buffer) StopLog() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.logBuf != nil {
		b.logBuf.Flush()
	}
	if b.logFile != nil {
		b.logFile.Close()
		b.logFile = nil
		b.logBuf = nil
	}
}

func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexStr(s, sub) >= 0)
}

func indexStr(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
