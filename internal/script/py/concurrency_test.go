package py

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
)

// TestBridge_WriteJSON_Concurrency executes concurrent writeJSON calls
// to verify thread safety and ensure the Go race detector passes.
func TestBridge_WriteJSON_Concurrency(t *testing.T) {
	var buf bytes.Buffer
	br := &Bridge{
		enc: json.NewEncoder(&buf),
	}

	const numGoroutines = 10
	const numWritesPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numWritesPerGoroutine; j++ {
				br.writeJSON(map[string]any{
					"goroutine": id,
					"write":     j,
				})
			}
		}(i)
	}

	wg.Wait()

	// Parse the output to ensure we wrote exactly the expected number of valid JSON lines.
	decoder := json.NewDecoder(&buf)
	count := 0
	for decoder.More() {
		var val map[string]any
		if err := decoder.Decode(&val); err != nil {
			t.Fatalf("Failed to decode JSON: %v", err)
		}
		count++
	}

	expectedCount := numGoroutines * numWritesPerGoroutine
	if count != expectedCount {
		t.Errorf("Expected %d JSON objects, got %d", expectedCount, count)
	}
}

// TestBridge_WriteJSON_NilEncoder verifies that writeJSON is safe when enc is nil.
func TestBridge_WriteJSON_NilEncoder(t *testing.T) {
	br := &Bridge{
		enc: nil,
	}
	// Should return early and not panic
	br.writeJSON(map[string]any{"test": 123})
}
