package history_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/kumakun/gofugue/internal/history"
)

func TestNewManager(t *testing.T) {
	capacity := 42
	m := history.NewManager(capacity)
	if m == nil {
		t.Fatal("NewManager returned nil")
	}

	// Verify that the manager is properly initialized by adding a world
	// and checking if its buffer respects the configured capacity.
	b := m.World("test_world")
	if b == nil {
		t.Fatal("World() returned nil for a newly created Manager")
	}

	// Add more lines than the capacity to verify the capacity was set correctly
	for i := 0; i < capacity+10; i++ {
		b.Append(history.Line{Text: fmt.Sprintf("line %d", i), Timestamp: time.Now()})
	}

	lines := b.Tail(capacity + 20)
	if len(lines) != capacity {
		t.Errorf("buffer has capacity %d, but Tail returned %d lines", capacity, len(lines))
	}
}

func TestManager_World_CreatesBuffer(t *testing.T) {
	m := history.NewManager(100)
	b := m.World("mud1")
	if b == nil {
		t.Fatal("World() returned nil")
	}
}

func TestManager_World_SameNameReturnsSame(t *testing.T) {
	m := history.NewManager(100)
	b1 := m.World("mud1")
	b2 := m.World("mud1")
	if b1 != b2 {
		t.Error("same world name should return the same Buffer pointer")
	}
}

func TestManager_World_DifferentNamesDifferentBuffers(t *testing.T) {
	m := history.NewManager(100)
	b1 := m.World("alpha")
	b2 := m.World("beta")
	if b1 == b2 {
		t.Error("different world names must return different buffers")
	}
}

func TestManager_World_Lines_Isolated(t *testing.T) {
	m := history.NewManager(100)
	m.World("alpha").Append(history.Line{Text: "alpha line", Timestamp: time.Now()})
	m.World("beta").Append(history.Line{Text: "beta line", Timestamp: time.Now()})

	alphaLines := m.World("alpha").Tail(10)
	betaLines := m.World("beta").Tail(10)

	if len(alphaLines) != 1 || alphaLines[0].Text != "alpha line" {
		t.Errorf("alpha lines = %v, want [alpha line]", alphaLines)
	}
	if len(betaLines) != 1 || betaLines[0].Text != "beta line" {
		t.Errorf("beta lines = %v, want [beta line]", betaLines)
	}
}

func TestManager_SearchAll_FindsAcrossWorlds(t *testing.T) {
	m := history.NewManager(100)
	m.World("w1").Append(history.Line{Text: "dragon attacks", Timestamp: time.Now()})
	m.World("w2").Append(history.Line{Text: "a dragon breathes fire", Timestamp: time.Now()})
	m.World("w3").Append(history.Line{Text: "nothing relevant", Timestamp: time.Now()})

	results := m.SearchAll("dragon")
	if len(results) != 2 {
		t.Errorf("SearchAll('dragon') = %d results, want 2", len(results))
	}
}

func TestManager_TailAll_ReturnsLinesFromAllWorlds(t *testing.T) {
	m := history.NewManager(100)
	m.World("a").Append(history.Line{Text: "line-a", Timestamp: time.Now()})
	m.World("b").Append(history.Line{Text: "line-b", Timestamp: time.Now()})

	all := m.TailAll(10)
	if len(all) < 2 {
		t.Errorf("TailAll returned %d lines, want >= 2", len(all))
	}
}

func TestManager_Concurrent_Access(t *testing.T) {
	m := history.NewManager(500)
	var wg sync.WaitGroup
	worlds := []string{"alpha", "beta", "gamma"}

	for i := 0; i < 10; i++ {
		for _, w := range worlds {
			w := w
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				m.World(w).Append(history.Line{
					Text:      fmt.Sprintf("%s-line-%d", w, i),
					Timestamp: time.Now(),
				})
			}()
		}
	}
	// Concurrent readers.
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.SearchAll("alpha")
			_ = m.TailAll(5)
		}()
	}
	wg.Wait()

	// Each world should have 10 lines.
	for _, w := range worlds {
		lines := m.World(w).Tail(100)
		if len(lines) != 10 {
			t.Errorf("world %q has %d lines, want 10", w, len(lines))
		}
	}
}
