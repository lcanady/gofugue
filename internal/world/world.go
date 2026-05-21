// Package world manages named world connections and their lifecycle.
package world

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/kumakun/gofugue/internal/ansi"
	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/config"
	"github.com/kumakun/gofugue/internal/proto"
	"github.com/kumakun/gofugue/internal/transport"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/transform"
)

// State represents the connection state of a world.
type State int

const (
	StateDisconnected State = iota
	StateConnecting
	StateConnected
)

// World represents a single named MUD connection with its own state.
type World struct {
	Name   string
	Cfg    config.WorldConfig
	State  State

	conn   io.ReadWriteCloser
	cancel context.CancelFunc
	mu     sync.Mutex
}

// Send writes a line of text to the world (appends \r\n).
func (w *World) Send(text string) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.conn == nil {
		return fmt.Errorf("world %q: not connected", w.Name)
	}
	// Use specialized Send if available (handles Telnet escaping and atomic CRLF).
	if s, ok := w.conn.(interface{ Send(string) error }); ok {
		return s.Send(text)
	}
	// Fallback for other transports: single Write for atomicity.
	_, err := io.WriteString(w.conn, text+"\r\n")
	return err
}

// Manager owns the set of worlds and routes events between them and the bus.
type Manager struct {
	mu         sync.RWMutex
	worlds     map[string]*World
	foreground string
	bus        *bus.Bus
}

// NewManager creates a Manager backed by the given event bus.
func NewManager(b *bus.Bus) *Manager {
	return &Manager{
		worlds: make(map[string]*World),
		bus:    b,
	}
}

// Add registers a world profile without connecting.
func (m *Manager) Add(cfg config.WorldConfig) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.worlds[cfg.Name] = &World{Name: cfg.Name, Cfg: cfg}
}

// Connect dials the named world. cfg is used when the world is not yet
// registered; pass nil to use the pre-registered profile.
func (m *Manager) Connect(ctx context.Context, name string, cfg *config.WorldConfig) error {
	m.mu.Lock()
	w, ok := m.worlds[name]
	if !ok {
		if cfg == nil {
			m.mu.Unlock()
			return &ErrUnknownWorld{Name: name}
		}
		w = &World{Name: name, Cfg: *cfg}
		m.worlds[name] = w
	}
	if m.foreground == "" {
		m.foreground = name
	}
	m.mu.Unlock()

	w.mu.Lock()
	if w.State == StateConnected || w.State == StateConnecting {
		w.mu.Unlock()
		return fmt.Errorf("world %q: already connected", name)
	}
	w.State = StateConnecting
	w.mu.Unlock()

	tr, err := transport.New(transport.Config{
		URL:           w.Cfg.URL,
		TLSSkipVerify: w.Cfg.TLSSkipVerify,
		TelnetEnabled: telnetEnabled(w.Cfg),
	})
	if err != nil {
		w.mu.Lock()
		w.State = StateDisconnected
		w.mu.Unlock()
		return fmt.Errorf("world %q: transport: %w", name, err)
	}

	rawConn, err := tr.Dial(ctx)
	if err != nil {
		w.mu.Lock()
		w.State = StateDisconnected
		w.mu.Unlock()
		return fmt.Errorf("world %q: dial: %w", name, err)
	}

	// Wrap with Telnet FSM (skipped for raw WS servers).
	session := proto.NewSession(name, rawConn, m.bus, telnetEnabled(w.Cfg))

	runCtx, cancel := context.WithCancel(ctx)
	w.mu.Lock()
	w.conn = session
	w.cancel = cancel
	w.State = StateConnected
	w.mu.Unlock()

	// Warn when TLS certificate verification is disabled so users who set
	// tls_skip_verify = true for a dev server don't forget and leave themselves
	// open to MITM on future connections.
	if w.Cfg.TLSSkipVerify {
		m.bus.Publish(bus.WorldRenderedEvent{WorldLineEvent: bus.WorldLineEvent{
			WorldName: "local",
			Text:      "[WARNING] TLS certificate verification is DISABLED for world " + name + " (tls_skip_verify = true)",
			Attrs:     bus.LineAttrs{FG: 3, BG: -1}, // yellow
		}})
	}

	// Publish CONNECT hook.
	m.bus.Publish(bus.HookEvent{WorldName: name, Name: "CONNECT"})
	m.bus.Publish(bus.StatusEvent{WorldName: name, Connected: true})

	// When runCtx is cancelled (parent ctx cancel or explicit Disconnect),
	// close the session so that any blocked scanner.Scan() in readLoop
	// returns immediately rather than hanging until the next network event.
	go func() {
		<-runCtx.Done()
		m.disconnectWorld(w) //nolint:errcheck
	}()

	// Start reader goroutine using the session (Telnet-filtered) reader.
	go m.readLoop(runCtx, w, session)

	return nil
}

// Disconnect closes the named world's connection.
func (m *Manager) Disconnect(name string) error {
	m.mu.RLock()
	w, ok := m.worlds[name]
	m.mu.RUnlock()
	if !ok {
		return &ErrUnknownWorld{Name: name}
	}
	return m.disconnectWorld(w)
}

func (m *Manager) disconnectWorld(w *World) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	if w.conn != nil {
		err := w.conn.Close()
		w.conn = nil
		w.State = StateDisconnected
		m.bus.Publish(bus.HookEvent{WorldName: w.Name, Name: "DISCONNECT"})
		m.bus.Publish(bus.StatusEvent{WorldName: w.Name, Connected: false})
		return err
	}
	w.State = StateDisconnected
	return nil
}

// readLoop reads lines from conn and publishes WorldLineEvents.
// It also:
//   - Publishes ACTIVITY hooks when text arrives from non-foreground worlds.
//   - Detects partial-line prompts (no newline after promptWait) and fires PROMPT hook.
func (m *Manager) readLoop(ctx context.Context, w *World, conn io.Reader) {
	const promptWait = 200 * time.Millisecond

	// Use a byte-level reader so we can detect partial lines (prompts).
	br := bufio.NewReaderSize(conn, 4096)
	promptTimer := time.NewTimer(promptWait)
	promptTimer.Stop()
	var partialBuf strings.Builder

	// Keepalive: send Telnet NOP every KeepaliveInterval seconds.
	// If no data arrives within 2× that interval, assume the server silently
	// dropped the connection (e.g. after a MUSH 'quit') and close our end.
	lastData := time.Now()
	var keepaliveTick <-chan time.Time
	if ka := w.Cfg.KeepaliveInterval; ka > 0 {
		t := time.NewTicker(time.Duration(ka) * time.Second)
		defer t.Stop()
		keepaliveTick = t.C
	}

	// decoder converts non-UTF-8 bytes to UTF-8 based on world charset config.
	decoder := charsetDecoder(w.Cfg.Charset)

	publishLine := func(raw string) {
		raw = strings.TrimRight(raw, "\r")
		if decoder != nil {
			if out, _, err := transform.String(decoder, raw); err == nil {
				raw = out
			}
			decoder.Reset()
		}
		spans, plain := ansi.Parse(raw)
		ev := bus.WorldLineEvent{
			WorldName: w.Name,
			Text:      plain,
			Attrs:     bus.LineAttrs{FG: -1, BG: -1},
		}
		if len(spans) > 1 || (len(spans) == 1 && spans[0].Attrs != (bus.LineAttrs{FG: -1, BG: -1})) {
			ev.Spans = spans
		}
		m.bus.Publish(ev)

		// ACTIVITY hook when not foreground.
		m.mu.RLock()
		fg := m.foreground
		m.mu.RUnlock()
		if fg != "" && fg != w.Name {
			m.bus.Publish(bus.HookEvent{WorldName: w.Name, Name: "ACTIVITY"})
		}
	}

	readCh := make(chan []byte, 32)
	errCh := make(chan error, 1)

	go func() {
		for {
			// Read up to 4096 bytes without blocking on newline.
			chunk := make([]byte, 4096)
			n, err := br.Read(chunk)
			if n > 0 {
				c := make([]byte, n)
				copy(c, chunk[:n])
				select {
				case readCh <- c:
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				errCh <- err
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			m.disconnectWorld(w) //nolint:errcheck
			return

		case err := <-errCh:
			if partialBuf.Len() > 0 {
				publishLine(partialBuf.String())
				partialBuf.Reset()
			}
			if err != nil && err != io.EOF {
				slog.Debug("world read error", "world", w.Name, "err", err)
			}
			m.disconnectWorld(w) //nolint:errcheck
			return

		case chunk := <-readCh:
			lastData = time.Now()
			promptTimer.Stop()
			// Process chunk byte by byte, splitting on '\n'.
			for _, b := range chunk {
				if b == '\n' {
					publishLine(partialBuf.String())
					partialBuf.Reset()
				} else {
					partialBuf.WriteByte(b)
				}
			}
			// If there are partial bytes (no trailing newline), start prompt timer.
			if partialBuf.Len() > 0 {
				promptTimer.Reset(promptWait)
			}

		case <-keepaliveTick:
			kaInterval := time.Duration(w.Cfg.KeepaliveInterval) * time.Second
			if time.Since(lastData) >= 2*kaInterval {
				// No data for 2× the keepalive interval — server appears gone.
				slog.Debug("keepalive timeout, disconnecting", "world", w.Name,
					"idle", time.Since(lastData).Truncate(time.Second))
				m.disconnectWorld(w) //nolint:errcheck
				return
			}
			// Probe the server with a Telnet NOP.
			if sess, ok := conn.(*proto.Session); ok {
				sess.SendNOP()
			}

		case <-promptTimer.C:
			// Partial line with no newline after promptWait → server prompt.
			if partialBuf.Len() > 0 {
				// Publish the partial line as-is.
				publishLine(partialBuf.String())
				partialBuf.Reset()
				m.bus.Publish(bus.HookEvent{WorldName: w.Name, Name: "PROMPT"})
			}
		}
	}
}

// Send sends text to the named world.
func (m *Manager) Send(name, text string) error {
	m.mu.RLock()
	w, ok := m.worlds[name]
	m.mu.RUnlock()
	if !ok {
		return &ErrUnknownWorld{Name: name}
	}
	return w.Send(text)
}

// Foreground returns the name of the currently active world.
func (m *Manager) Foreground() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.foreground
}

// Switch sets the foreground world by name.
func (m *Manager) Switch(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.worlds[name]; !ok {
		return &ErrUnknownWorld{Name: name}
	}
	m.foreground = name
	return nil
}

// State returns the connection state for the named world.
func (m *Manager) WorldState(name string) (State, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.worlds[name]
	if !ok {
		return StateDisconnected, &ErrUnknownWorld{Name: name}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.State, nil
}

// Remove removes a world by name. Returns an error if it is currently connected.
func (m *Manager) Remove(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.worlds[name]
	if !ok {
		return &ErrUnknownWorld{Name: name}
	}
	w.mu.Lock()
	st := w.State
	w.mu.Unlock()
	if st == StateConnected {
		return fmt.Errorf("world %q: disconnect before removing", name)
	}
	delete(m.worlds, name)
	if m.foreground == name {
		m.foreground = ""
	}
	return nil
}

// SetWindowSize broadcasts the new terminal dimensions to all connected sessions
// so they can send a NAWS update to the server.
func (m *Manager) SetWindowSize(w, h uint16) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, world := range m.worlds {
		world.mu.Lock()
		if world.State == StateConnected {
			if sess, ok := world.conn.(interface{ SetWindowSize(w, h uint16) }); ok {
				sess.SetWindowSize(w, h)
			}
		}
		world.mu.Unlock()
	}
}

// WorldConfig returns the saved config for a named world (if registered).
func (m *Manager) WorldConfig(name string) (config.WorldConfig, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	w, ok := m.worlds[name]
	if !ok {
		return config.WorldConfig{}, false
	}
	return w.Cfg, true
}

// WorldInfos returns a snapshot of all registered worlds for display.
func (m *Manager) WorldInfos() []WorldInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]WorldInfo, 0, len(m.worlds))
	for _, w := range m.worlds {
		w.mu.Lock()
		connected := w.State == StateConnected
		w.mu.Unlock()
		out = append(out, WorldInfo{Name: w.Name, URL: w.Cfg.URL, Connected: connected})
	}
	return out
}

// WorldInfo summarises a world for listing.
type WorldInfo struct {
	Name      string
	URL       string
	Connected bool
}

// SaveTOML serialises all registered world configs to a TOML file.
func (m *Manager) SaveTOML(path string) error {
	m.mu.RLock()
	worlds := make(map[string]config.WorldConfig, len(m.worlds))
	for k, w := range m.worlds {
		worlds[k] = w.Cfg
	}
	m.mu.RUnlock()

	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	if err := enc.Encode(map[string]any{"worlds": worlds}); err != nil {
		return fmt.Errorf("saveworld: encode: %w", err)
	}
	return os.WriteFile(path, buf.Bytes(), 0o644)
}

// telnetEnabled returns whether Telnet FSM should be active for a world.
func telnetEnabled(cfg config.WorldConfig) bool {
	if cfg.TelnetEnabled != nil {
		return *cfg.TelnetEnabled
	}
	// Default: enable Telnet for mud:// and muds://, disable for ws:// / wss:// / wt://
	url := cfg.URL
	return len(url) >= 3 && url[:3] == "mud"
}

// NameFromURL derives a short world name from a connection URL.
// "mud://example.com:4000" → "example.com"
func NameFromURL(url string) string {
	s := url
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i]
	}
	if s == "" {
		return "world"
	}
	return s
}

// charsetDecoder returns a transform.Transformer for the given charset name,
// or nil for UTF-8 / unrecognised values (pass-through).
func charsetDecoder(charset string) transform.Transformer {
	switch strings.ToLower(strings.TrimSpace(charset)) {
	case "latin-1", "latin1", "iso-8859-1", "iso8859-1":
		return charmap.ISO8859_1.NewDecoder()
	case "latin-2", "latin2", "iso-8859-2":
		return charmap.ISO8859_2.NewDecoder()
	case "windows-1252", "cp1252":
		return charmap.Windows1252.NewDecoder()
	case "koi8-r", "koi8r":
		return charmap.KOI8R.NewDecoder()
	default:
		return nil // UTF-8 or unknown — no transcoding
	}
}

// ErrUnknownWorld is returned when an operation targets an unregistered world.
type ErrUnknownWorld struct{ Name string }

func (e *ErrUnknownWorld) Error() string { return "unknown world: " + e.Name }
