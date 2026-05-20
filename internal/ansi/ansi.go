// Package ansi parses ANSI SGR (Select Graphic Rendition) escape sequences
// from MUD server output and splits a raw line into styled spans.
//
// Only the subset commonly used by MUD servers is supported:
//   - Reset (0)
//   - Bold (1), Italic (3), Underline (4), Reverse (7)
//   - Attribute off: 22 (bold), 23 (italic), 24 (underline), 27 (reverse)
//   - Standard foreground (30-37), bright foreground (90-97)
//   - Standard background (40-47), bright background (100-107)
//   - 256-colour: 38;5;n (FG), 48;5;n (BG)
//   - Default FG (39), default BG (49)
//   - Full reset via ESC[m or ESC[0m
package ansi

import (
	"strings"

	"github.com/kumakun/gofugue/internal/bus"
)

// defaultAttrs returns the default terminal attributes.
func defaultAttrs() bus.LineAttrs {
	return bus.LineAttrs{FG: -1, BG: -1}
}

// Parse splits rawLine (which may contain ANSI escape sequences) into a slice
// of bus.Span values. Each span contains plain text and the attributes active
// for that run of text.
//
// If rawLine contains no escape sequences, a single span with the full text
// and default attributes is returned.
//
// plainText is rawLine with all escape sequences stripped (for trigger
// matching and history search).
func Parse(rawLine string) (spans []bus.Span, plainText string) {
	if !strings.ContainsRune(rawLine, '\x1b') {
		return []bus.Span{{Text: rawLine, Attrs: defaultAttrs()}}, rawLine
	}

	cur := defaultAttrs()
	var textBuf strings.Builder
	var plainBuf strings.Builder

	flushSpan := func() {
		t := textBuf.String()
		if t != "" {
			spans = append(spans, bus.Span{Text: t, Attrs: cur})
			textBuf.Reset()
		}
	}

	i := 0
	for i < len(rawLine) {
		if rawLine[i] != '\x1b' {
			textBuf.WriteByte(rawLine[i])
			plainBuf.WriteByte(rawLine[i])
			i++
			continue
		}

		// ESC found. Expect '[' next for CSI sequences.
		if i+1 >= len(rawLine) || rawLine[i+1] != '[' {
			// Not a CSI sequence — emit ESC literally? Skip it.
			i++
			continue
		}

		// Find end of sequence: first byte in range 0x40-0x7E after the '['.
		j := i + 2
		for j < len(rawLine) && (rawLine[j] < 0x40 || rawLine[j] > 0x7E) {
			j++
		}
		if j >= len(rawLine) {
			// Unterminated — skip to end.
			break
		}

		finalByte := rawLine[j]
		paramStr := rawLine[i+2 : j]
		i = j + 1

		// Only handle 'm' (SGR).
		if finalByte != 'm' {
			continue
		}

		flushSpan()
		applySGR(paramStr, &cur)
	}

	// Flush any remaining text.
	flushSpan()
	plainText = plainBuf.String()

	if len(spans) == 0 {
		spans = []bus.Span{{Text: "", Attrs: defaultAttrs()}}
	}
	return spans, plainText
}

// applySGR applies SGR parameters to attrs. paramStr is the raw parameter
// string (e.g. "1;32" for bold green foreground).
func applySGR(paramStr string, attrs *bus.LineAttrs) {
	if paramStr == "" || paramStr == "0" {
		*attrs = defaultAttrs()
		return
	}

	params := splitParams(paramStr)
	for k := 0; k < len(params); k++ {
		p := params[k]
		switch {
		case p == 0:
			*attrs = defaultAttrs()
		case p == 1:
			attrs.Bold = true
		case p == 3:
			attrs.Italic = true
		case p == 4:
			attrs.Underline = true
		case p == 7:
			attrs.Reverse = true
		case p == 22:
			attrs.Bold = false
		case p == 23:
			attrs.Italic = false
		case p == 24:
			attrs.Underline = false
		case p == 27:
			attrs.Reverse = false
		case p >= 30 && p <= 37:
			attrs.FG = p - 30
		case p == 38:
			// 38;5;n = 256-colour FG; 38;2;r;g;b = truecolor FG
			if k+2 < len(params) && params[k+1] == 5 {
				attrs.FG = params[k+2]
				k += 2
			} else if k+4 < len(params) && params[k+1] == 2 {
				attrs.FG = -2 // sentinel: use FGRGB
				attrs.FGRGB = [3]byte{byte(params[k+2]), byte(params[k+3]), byte(params[k+4])}
				k += 4
			}
		case p == 39:
			attrs.FG = -1
		case p >= 40 && p <= 47:
			attrs.BG = p - 40
		case p == 48:
			// 48;5;n = 256-colour BG; 48;2;r;g;b = truecolor BG
			if k+2 < len(params) && params[k+1] == 5 {
				attrs.BG = params[k+2]
				k += 2
			} else if k+4 < len(params) && params[k+1] == 2 {
				attrs.BG = -2 // sentinel: use BGRGB
				attrs.BGRGB = [3]byte{byte(params[k+2]), byte(params[k+3]), byte(params[k+4])}
				k += 4
			}
		case p == 49:
			attrs.BG = -1
		case p >= 90 && p <= 97:
			attrs.FG = p - 90 + 8 // bright colours → palette 8-15
		case p >= 100 && p <= 107:
			attrs.BG = p - 100 + 8
		}
	}
}

// splitParams parses a semicolon-separated SGR parameter string into ints.
// Non-numeric segments are treated as 0 (SGR default).
func splitParams(s string) []int {
	if s == "" {
		return []int{0}
	}
	parts := strings.Split(s, ";")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		out = append(out, parseInt(p))
	}
	return out
}

func parseInt(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}
