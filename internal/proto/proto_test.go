package proto_test

import (
	"bytes"
	"compress/zlib"
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/kumakun/gofugue/internal/bus"
	"github.com/kumakun/gofugue/internal/proto"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// fakeConn is an in-memory ReadWriteCloser used as the transport stub.
type fakeConn struct {
	r      io.Reader
	writes [][]byte
	closed bool
}

func (f *fakeConn) Read(p []byte) (int, error)  { return f.r.Read(p) }
func (f *fakeConn) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	f.writes = append(f.writes, cp)
	return len(p), nil
}
func (f *fakeConn) Close() error { f.closed = true; return nil }

// written returns all bytes written so far as a flat slice.
func (f *fakeConn) written() []byte {
	var out []byte
	for _, w := range f.writes {
		out = append(out, w...)
	}
	return out
}

func newConn(data []byte) *fakeConn {
	return &fakeConn{r: bytes.NewReader(data)}
}

// readAll drains a proto.Session into a string.
func readAll(t *testing.T, s *proto.Session) string {
	t.Helper()
	buf := &bytes.Buffer{}
	if _, err := io.Copy(buf, s); err != nil && err != io.EOF {
		t.Fatalf("readAll: %v", err)
	}
	return buf.String()
}

// iac builds a Telnet IAC sequence.
func iac(b ...byte) []byte { return append([]byte{255}, b...) }

// ---------------------------------------------------------------------------
// Data pass-through
// ---------------------------------------------------------------------------

func TestSession_PlainText_PassThrough(t *testing.T) {
	conn := newConn([]byte("hello world\r\n"))
	b := bus.New()
	s := proto.NewSession("w", conn, b, true)

	got := readAll(t, s)
	if !strings.Contains(got, "hello world") {
		t.Errorf("plain text not passed through: %q", got)
	}
}

func TestSession_TelnetDisabled_PassThrough_WithIACBytes(t *testing.T) {
	// When telnet=false, IAC bytes pass through as-is.
	payload := []byte("line\r\n")
	payload = append(payload, 255, 251, 201) // would be WILL GMCP in telnet mode
	payload = append(payload, []byte("more\r\n")...)

	conn := newConn(payload)
	b := bus.New()
	s := proto.NewSession("w", conn, b, false)

	got := readAll(t, s)
	// All bytes including IAC should be present.
	if !strings.Contains(got, "line") || !strings.Contains(got, "more") {
		t.Errorf("telnet-disabled: unexpected output: %q", got)
	}
}

func TestSession_EscapedIAC_DecodedToLiteralByte(t *testing.T) {
	// IAC IAC = literal 0xFF in data stream.
	payload := []byte("data")
	payload = append(payload, 255, 255) // IAC IAC
	payload = append(payload, []byte("end")...)

	conn := newConn(payload)
	b := bus.New()
	s := proto.NewSession("w", conn, b, true)

	got := readAll(t, s)
	if !strings.Contains(got, "data") || !strings.Contains(got, "end") {
		t.Errorf("escaped IAC: %q", got)
	}
	// The literal 0xFF should appear between "data" and "end".
	if !bytes.Contains([]byte(got), []byte{255}) {
		t.Error("escaped IAC (0xFF) should appear in output")
	}
}

func TestSession_MultipleTextBlocks_AllPassThrough(t *testing.T) {
	conn := newConn([]byte("line1\r\nline2\r\nline3\r\n"))
	s := proto.NewSession("w", conn, bus.New(), true)
	got := readAll(t, s)
	for _, want := range []string{"line1", "line2", "line3"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %q", want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// Negotiation responses
// ---------------------------------------------------------------------------

func TestSession_WILL_GMCP_Responds_DO(t *testing.T) {
	payload := iac(251, 201) // IAC WILL GMCP
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s)

	w := conn.written()
	// Expect IAC DO GMCP (255 253 201) somewhere in the response.
	if !bytes.Contains(w, []byte{255, 253, 201}) {
		t.Errorf("expected IAC DO GMCP in response, got: %v", w)
	}
}

func TestSession_WILL_COMPRESS2_Responds_DO(t *testing.T) {
	payload := iac(251, 86) // IAC WILL COMPRESS2
	// Add plain text after negotiation so Read() has something to return.
	payload = append(payload, []byte("text\r\n")...)
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s)

	if !bytes.Contains(conn.written(), []byte{255, 253, 86}) {
		t.Errorf("expected IAC DO COMPRESS2, got: %v", conn.written())
	}
}

func TestSession_WILL_ECHO_Responds_DO_And_SetsFlag(t *testing.T) {
	payload := iac(251, 1) // IAC WILL ECHO
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s)

	if !bytes.Contains(conn.written(), []byte{255, 253, 1}) {
		t.Errorf("expected IAC DO ECHO in response")
	}
	if !s.EchoEnabled {
		t.Error("EchoEnabled should be true after IAC WILL ECHO")
	}
}

func TestSession_WONT_ECHO_ClearsFlag(t *testing.T) {
	// First set echo via WILL ECHO, then clear via WONT ECHO.
	payload := append(iac(251, 1), iac(252, 1)...) // WILL ECHO, WONT ECHO
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s)

	if s.EchoEnabled {
		t.Error("EchoEnabled should be false after IAC WONT ECHO")
	}
}

func TestSession_DO_TermType_Responds_WILL(t *testing.T) {
	payload := iac(253, 24) // IAC DO TERM_TYPE
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s)

	if !bytes.Contains(conn.written(), []byte{255, 251, 24}) {
		t.Errorf("expected IAC WILL TERM_TYPE, got: %v", conn.written())
	}
}

func TestSession_DO_NAWS_Responds_WILL_And_SendsSize(t *testing.T) {
	payload := iac(253, 31) // IAC DO NAWS
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s)

	w := conn.written()
	// WILL NAWS
	if !bytes.Contains(w, []byte{255, 251, 31}) {
		t.Errorf("expected IAC WILL NAWS")
	}
	// SB NAWS ... SE
	if !bytes.Contains(w, []byte{255, 250, 31}) {
		t.Errorf("expected IAC SB NAWS")
	}
}

func TestSession_DO_Unknown_Responds_WONT(t *testing.T) {
	payload := iac(253, 99) // IAC DO <unknown option 99>
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s)

	if !bytes.Contains(conn.written(), []byte{255, 252, 99}) {
		t.Errorf("expected IAC WONT 99 for unknown option, got: %v", conn.written())
	}
}

func TestSession_WILL_Unknown_Responds_DONT(t *testing.T) {
	payload := iac(251, 99) // IAC WILL <unknown>
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s)

	if !bytes.Contains(conn.written(), []byte{255, 254, 99}) {
		t.Errorf("expected IAC DONT 99 for unknown WILL, got: %v", conn.written())
	}
}

func TestSession_NOP_GoAhead_Ignored(t *testing.T) {
	// NOP (241) and GA (249) should be silently consumed.
	payload := []byte("pre")
	payload = append(payload, iac(241)...) // NOP
	payload = append(payload, iac(249)...) // GA
	payload = append(payload, []byte("post")...)

	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)
	got := readAll(t, s)
	if !strings.Contains(got, "pre") || !strings.Contains(got, "post") {
		t.Errorf("NOP/GA should not affect text: %q", got)
	}
}

// ---------------------------------------------------------------------------
// GMCP subnegotiation
// ---------------------------------------------------------------------------

func buildGMCP(module string, data any) []byte {
	payload, _ := json.Marshal(data)
	msg := []byte(module + " " + string(payload))
	pkt := []byte{255, 250, 201} // IAC SB GMCP
	pkt = append(pkt, msg...)
	pkt = append(pkt, 255, 240) // IAC SE
	return pkt
}

func TestSession_GMCP_PublishesEvent(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(8, bus.EvGMCP)
	defer sub.Cancel()

	pkt := buildGMCP("Char.Vitals", map[string]any{"hp": 100, "maxhp": 200})
	conn := newConn(pkt)
	s := proto.NewSession("w", conn, b, true)
	readAll(t, s)

	select {
	case ev := <-sub.C:
		gmcp := ev.(bus.GMCPEvent)
		if gmcp.Module != "Char.Vitals" {
			t.Errorf("Module = %q, want Char.Vitals", gmcp.Module)
		}
		var obj map[string]any
		if err := json.Unmarshal(gmcp.Data, &obj); err != nil {
			t.Fatalf("GMCP data unmarshal: %v", err)
		}
		if obj["hp"] != float64(100) {
			t.Errorf("hp = %v, want 100", obj["hp"])
		}
	default:
		t.Fatal("no GMCPEvent published")
	}
}

func TestSession_GMCP_NoJSON_UsesEmptyObject(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(8, bus.EvGMCP)
	defer sub.Cancel()

	// GMCP packet with no JSON — just module name.
	pkt := []byte{255, 250, 201}
	pkt = append(pkt, []byte("Core.Keepalive")...)
	pkt = append(pkt, 255, 240)

	conn := newConn(pkt)
	s := proto.NewSession("w", conn, b, true)
	readAll(t, s)

	select {
	case ev := <-sub.C:
		gmcp := ev.(bus.GMCPEvent)
		if gmcp.Module != "Core.Keepalive" {
			t.Errorf("Module = %q, want Core.Keepalive", gmcp.Module)
		}
		if string(gmcp.Data) != "{}" {
			t.Errorf("Data = %q, want {}", gmcp.Data)
		}
	default:
		t.Fatal("no GMCPEvent for keepalive")
	}
}

func TestSession_GMCP_MultiplePackets_AllPublished(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(16, bus.EvGMCP)
	defer sub.Cancel()

	var pkt []byte
	pkt = append(pkt, buildGMCP("Char.Vitals", map[string]any{"hp": 1})...)
	pkt = append(pkt, buildGMCP("Room.Info", map[string]any{"name": "void"})...)
	pkt = append(pkt, buildGMCP("Char.Status", map[string]any{"level": 5})...)

	conn := newConn(pkt)
	s := proto.NewSession("w", conn, b, true)
	readAll(t, s)

	modules := make(map[string]bool)
	for i := 0; i < 3; i++ {
		select {
		case ev := <-sub.C:
			modules[ev.(bus.GMCPEvent).Module] = true
		default:
			t.Fatalf("only %d GMCP events, expected 3", i)
		}
	}
	for _, m := range []string{"Char.Vitals", "Room.Info", "Char.Status"} {
		if !modules[m] {
			t.Errorf("missing GMCP module %q", m)
		}
	}
}

func TestSession_GMCP_InvalidJSON_DoesNotPanic(t *testing.T) {
	b := bus.New()
	pkt := []byte{255, 250, 201}
	pkt = append(pkt, []byte("Char.Vitals {not valid json}")...)
	pkt = append(pkt, 255, 240)

	conn := newConn(pkt)
	s := proto.NewSession("w", conn, b, true)
	// Must not panic, must not return error for the text stream.
	readAll(t, s)
}

func TestSession_GMCP_WorldName_Propagated(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(8, bus.EvGMCP)
	defer sub.Cancel()

	conn := newConn(buildGMCP("Test.Pkg", map[string]any{}))
	s := proto.NewSession("myworld", conn, b, true)
	readAll(t, s)

	select {
	case ev := <-sub.C:
		if ev.(bus.GMCPEvent).WorldName != "myworld" {
			t.Errorf("WorldName = %q, want myworld", ev.(bus.GMCPEvent).WorldName)
		}
	default:
		t.Fatal("no GMCPEvent")
	}
}

func TestSession_GMCP_MixedWithText(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(8, bus.EvGMCP)
	defer sub.Cancel()

	var data []byte
	data = append(data, []byte("before\r\n")...)
	data = append(data, buildGMCP("Char.Vitals", map[string]any{"hp": 50})...)
	data = append(data, []byte("after\r\n")...)

	conn := newConn(data)
	s := proto.NewSession("w", conn, b, true)
	got := readAll(t, s)

	if !strings.Contains(got, "before") || !strings.Contains(got, "after") {
		t.Errorf("text around GMCP lost: %q", got)
	}
	select {
	case ev := <-sub.C:
		if ev.(bus.GMCPEvent).Module != "Char.Vitals" {
			t.Errorf("Module = %q", ev.(bus.GMCPEvent).Module)
		}
	default:
		t.Fatal("no GMCP event")
	}
}

// ---------------------------------------------------------------------------
// MCCP2
// ---------------------------------------------------------------------------

func buildMCCP2Stream(plainText string) []byte {
	// Start with the COMPRESS2 negotiation sequence.
	var buf []byte
	buf = append(buf, 255, 251, 86)      // IAC WILL COMPRESS2
	buf = append(buf, 255, 250, 86)      // IAC SB COMPRESS2
	buf = append(buf, 255, 240)          // IAC SE  — compression starts here

	// Compress the plain text with zlib.
	var zBuf bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&zBuf, zlib.DefaultCompression)
	zw.Write([]byte(plainText)) //nolint:errcheck
	zw.Close()

	buf = append(buf, zBuf.Bytes()...)
	return buf
}

func TestSession_MCCP2_Activates_And_DecompressesText(t *testing.T) {
	b := bus.New()
	conn := newConn(buildMCCP2Stream("compressed line\r\n"))
	s := proto.NewSession("w", conn, b, true)

	got := readAll(t, s)

	if !s.MCCPActive {
		t.Error("MCCPActive should be true after COMPRESS2 negotiation")
	}
	if !strings.Contains(got, "compressed line") {
		t.Errorf("decompressed text not found: %q", got)
	}
}

func TestSession_MCCP2_MultipleLines_AllDecompressed(t *testing.T) {
	lines := "first line\r\nsecond line\r\nthird line\r\n"
	conn := newConn(buildMCCP2Stream(lines))
	s := proto.NewSession("w", conn, bus.New(), true)
	got := readAll(t, s)

	for _, want := range []string{"first line", "second line", "third line"} {
		if !strings.Contains(got, want) {
			t.Errorf("MCCP2: missing %q in %q", want, got)
		}
	}
}

func TestSession_MCCP2_GMCP_After_Compression(t *testing.T) {
	b := bus.New()
	sub := b.Subscribe(8, bus.EvGMCP)
	defer sub.Cancel()

	// Build: WILL COMPRESS2, SB COMPRESS2 SE, then zlib(GMCP + text).
	var compressed []byte
	compressed = append(compressed, buildGMCP("Char.Vitals", map[string]any{"hp": 77})...)
	compressed = append(compressed, []byte("text after gmcp\r\n")...)

	var zBuf bytes.Buffer
	zw, _ := zlib.NewWriterLevel(&zBuf, zlib.DefaultCompression)
	zw.Write(compressed) //nolint:errcheck
	zw.Close()

	var data []byte
	data = append(data, 255, 251, 86) // IAC WILL COMPRESS2
	data = append(data, 255, 250, 86) // IAC SB COMPRESS2
	data = append(data, 255, 240)     // IAC SE
	data = append(data, zBuf.Bytes()...)

	conn := newConn(data)
	s := proto.NewSession("w", conn, b, true)
	got := readAll(t, s)

	if !strings.Contains(got, "text after gmcp") {
		t.Errorf("text after GMCP in MCCP2 stream lost: %q", got)
	}
	select {
	case ev := <-sub.C:
		if ev.(bus.GMCPEvent).Module != "Char.Vitals" {
			t.Errorf("GMCP module = %q", ev.(bus.GMCPEvent).Module)
		}
	default:
		t.Fatal("no GMCP event after MCCP2 activation")
	}
}

// ---------------------------------------------------------------------------
// SendGMCP
// ---------------------------------------------------------------------------

func TestSession_SendGMCP_WritesCorrectBytes(t *testing.T) {
	conn := newConn([]byte{}) // nothing to read
	s := proto.NewSession("w", conn, bus.New(), true)

	if err := s.SendGMCP("Core.Hello", map[string]string{"client": "gofugue"}); err != nil {
		t.Fatalf("SendGMCP: %v", err)
	}

	w := conn.written()
	// Must start with IAC SB GMCP.
	if !bytes.HasPrefix(w, []byte{255, 250, 201}) {
		t.Errorf("SendGMCP missing IAC SB GMCP header: %v", w)
	}
	// Must end with IAC SE.
	if !bytes.HasSuffix(w, []byte{255, 240}) {
		t.Errorf("SendGMCP missing IAC SE footer: %v", w)
	}
	// Must contain module name.
	if !bytes.Contains(w, []byte("Core.Hello")) {
		t.Errorf("SendGMCP missing module name")
	}
}

// ---------------------------------------------------------------------------
// Send (plain text + CRLF + IAC escaping)
// ---------------------------------------------------------------------------

func TestSession_Send_AppendsCRLF(t *testing.T) {
	conn := newConn([]byte{})
	s := proto.NewSession("w", conn, bus.New(), true)
	s.Send("go north") //nolint:errcheck

	w := conn.written()
	if !bytes.HasSuffix(w, []byte("\r\n")) {
		t.Errorf("Send should append CRLF, got: %q", w)
	}
	if !bytes.Contains(w, []byte("go north")) {
		t.Errorf("Send text missing from write: %q", w)
	}
}

func TestSession_Send_EscapesIAC(t *testing.T) {
	conn := newConn([]byte{})
	s := proto.NewSession("w", conn, bus.New(), true)
	s.Send("text\xff more") //nolint:errcheck // contains IAC byte

	w := conn.written()
	// 0xFF should be doubled.
	if !bytes.Contains(w, []byte{255, 255}) {
		t.Errorf("IAC byte not escaped in Send: %v", w)
	}
}

// ---------------------------------------------------------------------------
// Malformed / edge cases
// ---------------------------------------------------------------------------

func TestSession_TruncatedIAC_DoesNotPanic(t *testing.T) {
	// IAC at end of stream with no following byte.
	conn := newConn([]byte{255})
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s) // must not panic
}

func TestSession_TruncatedSubneg_DoesNotPanic(t *testing.T) {
	// IAC SB GMCP with data but no IAC SE.
	conn := newConn([]byte{255, 250, 201, 'C', 'h', 'a', 'r'})
	s := proto.NewSession("w", conn, bus.New(), true)
	readAll(t, s) // must not panic
}

func TestSession_EmptyInput_ReturnsEOF(t *testing.T) {
	conn := newConn([]byte{})
	s := proto.NewSession("w", conn, bus.New(), true)
	buf := make([]byte, 16)
	_, err := s.Read(buf)
	if err != io.EOF {
		t.Errorf("empty input: got err %v, want io.EOF", err)
	}
}

func TestSession_Close_ClosesUnderlying(t *testing.T) {
	conn := newConn([]byte{})
	s := proto.NewSession("w", conn, bus.New(), true)
	s.Close() //nolint:errcheck
	if !conn.closed {
		t.Error("Close should close the underlying connection")
	}
}

// ---------------------------------------------------------------------------
// NAWS — Negotiate About Window Size (RFC 1073)
// ---------------------------------------------------------------------------

// nawsPayload returns the IAC SB NAWS ... IAC SE bytes for a given size.
func nawsPayload(w, h uint16) []byte {
	return []byte{
		255, 250, 31, // IAC SB NAWS
		byte(w >> 8), byte(w),
		byte(h >> 8), byte(h),
		255, 240, // IAC SE
	}
}

func TestSession_NAWS_DoNAWS_SendsWillAndNAWS(t *testing.T) {
	// Server sends: IAC DO NAWS
	payload := append([]byte("hi\r\n"), 255, 253, 31) // IAC DO NAWS
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)

	readAll(t, s)

	w := conn.written()
	// Must contain IAC WILL NAWS (255 251 31).
	willNAWS := []byte{255, 251, 31}
	if !bytes.Contains(w, willNAWS) {
		t.Errorf("DO NAWS: missing IAC WILL NAWS in response: %v", w)
	}
	// Must also send IAC SB NAWS with default 80×24.
	expected := nawsPayload(80, 24)
	if !bytes.Contains(w, expected) {
		t.Errorf("DO NAWS: missing NAWS subneg (80x24): %v", w)
	}
}

func TestSession_NAWS_SetWindowSize_SendsUpdate(t *testing.T) {
	// Server sends DO NAWS; then caller updates size.
	payload := append([]byte{}, 255, 253, 31) // IAC DO NAWS (no text, just negotiation)
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)

	readAll(t, s) // consumes DO NAWS → sends WILL NAWS + 80×24

	// Clear writes so far; resize to 132×50.
	conn.writes = nil
	s.SetWindowSize(132, 50)

	w := conn.written()
	expected := nawsPayload(132, 50)
	if !bytes.Contains(w, expected) {
		t.Errorf("SetWindowSize: missing NAWS subneg (132x50): %v", w)
	}
}

func TestSession_NAWS_SetWindowSize_BeforeNegotiation_NoSend(t *testing.T) {
	// No DO NAWS from server — SetWindowSize should NOT send anything.
	conn := newConn([]byte{})
	s := proto.NewSession("w", conn, bus.New(), true)

	s.SetWindowSize(120, 40)

	w := conn.written()
	// Should be empty — no NAWS until server requests it.
	for _, b := range w {
		if b == 31 { // NAWS option byte
			t.Errorf("SetWindowSize sent NAWS bytes before negotiation: %v", w)
			break
		}
	}
}

func TestSession_NAWS_DefaultSize_Is80x24(t *testing.T) {
	// Server sends DO NAWS; check the default dimensions sent.
	payload := []byte{255, 253, 31} // IAC DO NAWS
	conn := newConn(payload)
	s := proto.NewSession("w", conn, bus.New(), true)

	readAll(t, s)

	w := conn.written()
	expected := nawsPayload(80, 24)
	if !bytes.Contains(w, expected) {
		t.Errorf("default NAWS size should be 80x24, got bytes: %v", w)
	}
}
