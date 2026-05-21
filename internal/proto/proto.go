// Package proto implements the Telnet FSM and layered MUD protocols (MCCP2,
// GMCP, MXP, charset negotiation) as an io.Reader/Writer wrapper around a
// transport connection.
//
// Architecture:
//
//	Transport conn (io.ReadWriteCloser)
//	    │
//	    ▼
//	zlib reader (activated on MCCP2 negotiation, wraps raw reader)
//	    │
//	    ▼
//	Telnet FSM (strips IAC sequences, drives side-effects)
//	    │  side-effects: → bus.GMCPEvent, → bus.HookEvent (MXP etc.)
//	    ▼
//	plain text bytes  (consumed by world.Manager readLoop via bufio.Scanner)
package proto

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"sync"

	"github.com/kumakun/gofugue/internal/bus"
)

const (
	readBufSize = 4096 // raw read chunk
	outBufSize  = 4096 // text output buffer
)

// Session wraps a transport connection with Telnet FSM processing.
// It implements io.Reader — callers (e.g. bufio.Scanner) receive only the
// plain text bytes; all IAC sequences are consumed internally.
// It is NOT safe for concurrent reads; a single goroutine should call Read.
// Writes pass through to the underlying connection unmodified.
type Session struct {
	world string
	raw   io.ReadWriteCloser // underlying transport
	conn  io.ReadWriteCloser // always the original transport (for writes)
	bus   *bus.Bus

	mu     sync.Mutex // protects reader
	reader io.Reader  // raw or zlib-wrapped reader

	// FSM state
	state fsmState
	sbOpt byte   // option byte for current subnegotiation
	sbBuf []byte // accumulated subneg payload

	// Output buffer: text bytes not yet returned to caller.
	outBuf []byte

	// mccp2Pending is set inside process() when COMPRESS2 subneg is seen.
	// Read() checks this after each process() call and activates zlib with
	// any remaining bytes from the same read chunk prepended.
	mccp2Pending bool

	// Negotiation state flags (for tests and introspection).
	MCCPActive bool
	GMCPActive bool
	nawsActive bool // true after server DO NAWS negotiated

	// EchoEnabled tracks whether the server is echoing (IAC WILL ECHO received).
	EchoEnabled bool

	// telnetEnabled is false for raw WebSocket sessions where IAC processing
	// is skipped. Used by SendNOP to avoid injecting Telnet bytes into raw streams.
	telnetEnabled bool

	// Window dimensions for NAWS negotiation.
	winW, winH uint16
}

// NewSession creates a Telnet session for the named world.
// telnetEnabled=false skips IAC processing (raw WebSocket servers).
func NewSession(world string, conn io.ReadWriteCloser, b *bus.Bus, telnetEnabled bool) *Session {
	s := &Session{
		world:         world,
		raw:           conn,
		conn:          conn,
		bus:           b,
		reader:        conn,
		outBuf:        make([]byte, 0, outBufSize),
		telnetEnabled: telnetEnabled,
	}
	if !telnetEnabled {
		s.state = stateData // always stay in data state — pass through
	}
	return s
}

// Read implements io.Reader. It processes raw bytes through the Telnet FSM
// and returns only the plain text bytes. Blocks until at least one text byte
// is available or an error occurs.
func (s *Session) Read(p []byte) (int, error) {
	for {
		// If we have buffered output, return it.
		if len(s.outBuf) > 0 {
			n := copy(p, s.outBuf)
			s.outBuf = s.outBuf[n:]
			return n, nil
		}

		// Read a chunk from the underlying reader.
		tmp := make([]byte, readBufSize)
		s.mu.Lock()
		r := s.reader
		s.mu.Unlock()

		n, err := r.Read(tmp)
		if n > 0 {
			// process() returns the number of bytes consumed before a
			// potential MCCP2 activation. Remaining bytes are compressed.
			consumed := s.process(tmp[:n])

			if s.mccp2Pending {
				// Bytes after consumed are the start of the zlib stream.
				// Prepend them to the raw reader so the zlib decoder sees
				// a complete stream from the very first byte.
				remaining := make([]byte, n-consumed)
				copy(remaining, tmp[consumed:n])

				newReader := io.MultiReader(bytes.NewReader(remaining), s.raw)
				zr, zerr := zlib.NewReader(newReader)
				if zerr != nil {
					slog.Error("proto: MCCP2 zlib init failed", "err", zerr)
				} else {
					s.mu.Lock()
					s.reader = zr
					s.mu.Unlock()
					s.MCCPActive = true
					slog.Debug("proto: MCCP2 active", "world", s.world)
				}
				s.mccp2Pending = false
			}
		}
		if err != nil {
			if len(s.outBuf) > 0 {
				continue
			}
			return 0, err
		}
	}
}

// Write sends bytes to the remote end (plain passthrough — callers are
// responsible for escaping IAC bytes if needed).
func (s *Session) Write(p []byte) (int, error) { return s.conn.Write(p) }

// Close shuts down the session.
func (s *Session) Close() error { return s.conn.Close() }

// SendRaw writes raw bytes (used internally for Telnet responses).
func (s *Session) sendRaw(b ...byte) {
	s.conn.Write(b) //nolint:errcheck
}

// process runs bytes through the Telnet FSM, appending text bytes to outBuf.
// Returns the number of bytes consumed. Stops early if MCCP2 is activated
// (remaining bytes belong to the compressed stream, not the Telnet layer).
func (s *Session) process(in []byte) int {
	for i, b := range in {
		if s.mccp2Pending {
			return i // stop — remaining bytes are compressed
		}
		switch s.state {

		case stateData:
			if b == telnetIAC {
				s.state = stateIAC
			} else {
				s.outBuf = append(s.outBuf, b)
			}

		case stateIAC:
			switch b {
			case telnetIAC:
				// Escaped IAC — literal 0xFF in data stream.
				s.outBuf = append(s.outBuf, telnetIAC)
				s.state = stateData
			case telnetWILL:
				s.state = stateWill
			case telnetWONT:
				s.state = stateWont
			case telnetDO:
				s.state = stateDo
			case telnetDONT:
				s.state = stateDont
			case telnetSB:
				s.state = stateSB
				s.sbBuf = s.sbBuf[:0]
			case telnetGA, telnetNOP, telnetDM, telnetBRK:
				// Silently accept go-ahead / no-op / data-mark / break.
				s.state = stateData
			default:
				// Unknown command — ignore.
				s.state = stateData
			}

		case stateWill:
			s.handleWill(b)
			s.state = stateData

		case stateWont:
			s.handleWont(b)
			s.state = stateData

		case stateDo:
			s.handleDo(b)
			s.state = stateData

		case stateDont:
			s.handleDont(b)
			s.state = stateData

		case stateSB:
			s.sbOpt = b
			s.sbBuf = s.sbBuf[:0]
			s.state = stateSBData

		case stateSBData:
			if b == telnetIAC {
				s.state = stateSBIAC
			} else {
				s.sbBuf = append(s.sbBuf, b)
			}

		case stateSBIAC:
			if b == telnetSE {
				s.handleSubneg(s.sbOpt, s.sbBuf)
				s.state = stateData
			} else if b == telnetIAC {
				// IAC IAC inside subneg = literal 0xFF
				s.sbBuf = append(s.sbBuf, telnetIAC)
				s.state = stateSBData
			} else {
				// Malformed — discard subneg and continue.
				slog.Debug("proto: malformed subneg IAC byte", "b", b)
				s.state = stateData
			}
		}
	}
	return len(in)
}

// ---------------------------------------------------------------------------
// Negotiation handlers
// ---------------------------------------------------------------------------

func (s *Session) handleWill(opt byte) {
	switch opt {
	case optCompress2:
		// Server offers MCCP2 compression — accept.
		s.sendRaw(telnetIAC, telnetDO, optCompress2)
	case optGMCP:
		// Server offers GMCP — accept.
		s.sendRaw(telnetIAC, telnetDO, optGMCP)
		s.GMCPActive = true
	case optEcho:
		// Server will echo — accept (suppress local echo).
		s.sendRaw(telnetIAC, telnetDO, optEcho)
		s.EchoEnabled = true
	case optSGA:
		s.sendRaw(telnetIAC, telnetDO, optSGA)
	case optMXP:
		s.sendRaw(telnetIAC, telnetDO, optMXP)
	default:
		// Refuse unknown options.
		s.sendRaw(telnetIAC, telnetDONT, opt)
	}
}

func (s *Session) handleWont(opt byte) {
	switch opt {
	case optEcho:
		s.EchoEnabled = false
	}
	// No response required for WONT.
}

func (s *Session) handleDo(opt byte) {
	switch opt {
	case optTermType:
		s.sendRaw(telnetIAC, telnetWILL, optTermType)
	case optNAWS:
		s.sendRaw(telnetIAC, telnetWILL, optNAWS)
		s.mu.Lock()
		s.nawsActive = true
		s.mu.Unlock()
		w, h := s.windowSize()
		s.sendNAWS(w, h)
	case optSGA:
		s.sendRaw(telnetIAC, telnetWILL, optSGA)
	default:
		s.sendRaw(telnetIAC, telnetWONT, opt)
	}
}

// windowSize returns the current window dimensions (defaults to 80×24).
func (s *Session) windowSize() (w, h uint16) {
	s.mu.Lock()
	w, h = s.winW, s.winH
	s.mu.Unlock()
	if w == 0 {
		w = 80
	}
	if h == 0 {
		h = 24
	}
	return w, h
}

// SetWindowSize updates the advertised terminal size and sends a NAWS update
// if the server has already negotiated NAWS.
func (s *Session) SetWindowSize(w, h uint16) {
	s.mu.Lock()
	s.winW = w
	s.winH = h
	nawsActive := s.nawsActive
	s.mu.Unlock()
	if nawsActive {
		s.sendNAWS(w, h)
	}
}

func (s *Session) handleDont(opt byte) {
	// Acknowledge all DONT with WONT.
	s.sendRaw(telnetIAC, telnetWONT, opt)
}

// sendNAWS sends IAC SB NAWS <width-hi> <width-lo> <height-hi> <height-lo> IAC SE.
func (s *Session) sendNAWS(w, h uint16) {
	s.sendRaw(
		telnetIAC, telnetSB, optNAWS,
		byte(w>>8), byte(w),
		byte(h>>8), byte(h),
		telnetIAC, telnetSE,
	)
}

// UpdateWindowSize sends a NAWS subnegotiation with the current terminal size.
func (s *Session) UpdateWindowSize(w, h uint16) {
	s.sendNAWS(w, h)
}

// ---------------------------------------------------------------------------
// Subnegotiation handler
// ---------------------------------------------------------------------------

func (s *Session) handleSubneg(opt byte, data []byte) {
	switch opt {
	case optCompress2:
		s.activateMCCP2()

	case optGMCP:
		s.handleGMCP(data)

	case optTermType:
		if len(data) > 0 && data[0] == 1 { // SEND request
			// Respond: IAC SB TERM_TYPE IS "GoFugue" IAC SE
			resp := append(
				[]byte{telnetIAC, telnetSB, optTermType, 0}, // 0 = IS
				[]byte("GoFugue")...,
			)
			resp = append(resp, telnetIAC, telnetSE)
			s.conn.Write(resp) //nolint:errcheck
		}

	case optMXP:
		// MXP mode negotiation — accept silently.
	}
}

// activateMCCP2 signals that MCCP2 compression should begin.
// Actual zlib reader creation happens in Read() so the remaining bytes
// from the current read buffer can be prepended to the compressed stream.
func (s *Session) activateMCCP2() {
	if !s.MCCPActive {
		s.mccp2Pending = true
	}
}

// ---------------------------------------------------------------------------
// GMCP
// ---------------------------------------------------------------------------

// handleGMCP parses a GMCP subneg payload: "<Module.Name> <json>"
// and publishes a bus.GMCPEvent.
func (s *Session) handleGMCP(data []byte) {
	// Find the space separating module from JSON.
	idx := bytes.IndexByte(data, ' ')
	var module string
	var payload []byte

	if idx < 0 {
		// No JSON — e.g. "Core.Keepalive"
		module = string(bytes.TrimSpace(data))
		payload = []byte("{}")
	} else {
		module = string(bytes.TrimSpace(data[:idx]))
		payload = bytes.TrimSpace(data[idx+1:])
	}

	// Validate JSON.
	if !json.Valid(payload) {
		slog.Debug("proto: invalid GMCP JSON", "module", module, "raw", string(payload))
		payload = []byte("{}")
	}

	s.bus.Publish(bus.GMCPEvent{
		WorldName: s.world,
		Module:    module,
		Data:      payload,
	})
}

// SendGMCP sends a GMCP subneg: IAC SB GMCP "<module> <json>" IAC SE.
func (s *Session) SendGMCP(module string, data any) error {
	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("GMCP marshal: %w", err)
	}
	msg := module + " " + string(payload)
	pkt := []byte{telnetIAC, telnetSB, optGMCP}
	pkt = append(pkt, []byte(msg)...)
	pkt = append(pkt, telnetIAC, telnetSE)
	_, err = s.conn.Write(pkt)
	return err
}

// ---------------------------------------------------------------------------
// Convenience: Send plain text (with CRLF).
// ---------------------------------------------------------------------------

// SendNOP sends a Telnet No-Operation (IAC NOP) to the server.
// This is used as a keepalive probe to detect whether the server has silently
// closed the connection without sending a TCP FIN. It is a no-op for raw
// (non-Telnet) sessions such as WebSocket connections.
func (s *Session) SendNOP() {
	if s.telnetEnabled {
		s.sendRaw(telnetIAC, telnetNOP)
	}
}

// Send writes text + CRLF to the server, escaping any IAC bytes (if Telnet
// is enabled for this session).
func (s *Session) Send(text string) error {
	var payload []byte
	if s.telnetEnabled {
		payload = escapeIAC([]byte(text))
	} else {
		payload = []byte(text)
	}
	payload = append(payload, '\r', '\n')
	_, err := s.conn.Write(payload)
	return err
}

// escapeIAC doubles any 0xFF bytes in the payload.
func escapeIAC(b []byte) []byte {
	out := make([]byte, 0, len(b)+4)
	for _, c := range b {
		if c == telnetIAC {
			out = append(out, telnetIAC, telnetIAC)
		} else {
			out = append(out, c)
		}
	}
	return out
}
