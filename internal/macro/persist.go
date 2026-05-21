package macro

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// SaveFile writes all macros in the engine to a JSON file at path.
// The file is written atomically (temp file + rename).
func (e *Engine) SaveFile(path string) error {
	e.mu.RLock()
	records := make([]macroRecord, 0, len(e.byName))
	for _, m := range e.byName {
		records = append(records, macroRecord{
			Name:      m.Name,
			Type:      int(m.Type),
			MatchMode: int(m.MatchMode),
			Pattern:   m.Pattern,
			Body:      m.Body,
			Priority:  m.Priority,
			World:     m.World,
			Enabled:   m.Enabled,
			Shots:     m.Shots,
			Prob:      m.Prob,
			Fallthru:  m.Fallthru,
			Invisible: m.Invisible,
		})
	}
	e.mu.RUnlock()

	// Stable output order.
	sort.Slice(records, func(i, j int) bool {
		return records[i].Name < records[j].Name
	})

	data, err := json.MarshalIndent(struct {
		Macros []macroRecord `json:"macros"`
	}{records}, "", "  ")
	if err != nil {
		return fmt.Errorf("macro save: marshal: %w", err)
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	base := filepath.Base(path)

	f, err := os.CreateTemp(dir, base+".*.tmp")
	if err != nil {
		return fmt.Errorf("macro save: create temp: %w", err)
	}
	tmp := f.Name()

	closed := false
	defer func() {
		if !closed {
			f.Close()
		}
		os.Remove(tmp) // Clean up the temp file if it wasn't successfully renamed
	}()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("macro save: write temp: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("macro save: close temp: %w", err)
	}
	closed = true

	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("macro save: rename: %w", err)
	}

	return nil
}

// LoadFile reads macros from a JSON file previously written by SaveFile and
// defines them in the engine. Existing macros with the same name are replaced.
func (e *Engine) LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("macro load: %w", err)
	}
	var payload struct {
		Macros []macroRecord `json:"macros"`
	}
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("macro load: parse: %w", err)
	}
	for _, r := range payload.Macros {
		m := &Macro{
			Name:      r.Name,
			Type:      Type(r.Type),
			MatchMode: MatchMode(r.MatchMode),
			Pattern:   r.Pattern,
			Body:      r.Body,
			Priority:  r.Priority,
			World:     r.World,
			Shots:     r.Shots,
			Prob:      r.Prob,
			Fallthru:  r.Fallthru,
			Invisible: r.Invisible,
		}
		if err := e.Define(m); err != nil {
			return fmt.Errorf("macro load: define %q: %w", m.Name, err)
		}
		if !r.Enabled {
			// Re-disable after Define (which sets Enabled=true).
			e.mu.Lock()
			if stored, ok := e.byName[m.Name]; ok {
				stored.Enabled = false
			}
			e.mu.Unlock()
		}
	}
	return nil
}

type macroRecord struct {
	Name      string `json:"name"`
	Type      int    `json:"type"`
	MatchMode int    `json:"match_mode,omitempty"`
	Pattern   string `json:"pattern,omitempty"`
	Body      string `json:"body"`
	Priority  int    `json:"priority,omitempty"`
	World     string `json:"world,omitempty"`
	Enabled   bool   `json:"enabled"`
	Shots     int    `json:"shots,omitempty"`
	Prob      int    `json:"prob,omitempty"`
	Fallthru  bool   `json:"fallthru,omitempty"`
	Invisible bool   `json:"invisible,omitempty"`
}
