package questions

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode"

	"github.com/weitingzhao/bifrost-platform/api/internal/notice"
)

// clean removes controls, bidi marks, zero-width characters, and U+2028/U+2029,
// masks secret shapes, then trims. body and answer text keep newlines, which
// are normalized to \n. Every other field loses newlines with the other controls.
func clean(s string, keepNewlines bool) string {
	s = stripControls(s, keepNewlines)
	s = notice.Redact(s)
	if keepNewlines {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return strings.TrimSpace(s)
}

func stripControls(s string, keepNewlines bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			if keepNewlines {
				b.WriteRune(r)
			}
			continue
		}
		if r == '\u2028' || r == '\u2029' || isBidi(r) || isZeroWidth(r) {
			continue
		}
		if r < 0x20 || (r >= 0x7F && r <= 0x9F) || unicode.IsControl(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isBidi(r rune) bool {
	switch r {
	case 0x061C, 0x200E, 0x200F,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2066, 0x2067, 0x2068, 0x2069:
		return true
	default:
		return false
	}
}

func isZeroWidth(r rune) bool {
	switch r {
	case 0x180E, 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF:
		return true
	default:
		return false
	}
}

// encodedBytes is the JSON encoding of s with no HTML escape, quotes included.
func encodedBytes(s string) int {
	raw, err := marshalJSON(s)
	if err != nil {
		return len(s) + 2
	}
	return len(raw)
}

func marshalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// clipEncoded cleans s the way a single-line question field is cleaned, then
// keeps the longest prefix whose JSON encoding is at most n bytes (quotes included).
func clipEncoded(s string, n int) string {
	s = clean(s, false)
	if encodedBytes(s) <= n {
		return s
	}
	r := []rune(s)
	lo, hi := 0, len(r)
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if encodedBytes(string(r[:mid])) <= n {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return string(r[:lo])
}

// ClipTitle cleans and cuts s to what a question's title holds, by the same
// rule the store checks it with: for a caller that builds titles from text
// it does not control (the agent runtime's decision cards).
func ClipTitle(s string) string { return clipEncoded(s, titleLimit) }

func clipPrincipal(s string) string {
	return clipEncoded(s, principalLimit)
}

func clipRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
