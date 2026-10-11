package notice

import (
	"strings"
	"unicode"
)

// StripControls removes C0 and C1 controls, bidirectional marks, zero-width
// characters, and the line separators U+2028 and U+2029. LF and CR are kept
// only when keepNewlines is set; every other character is copied through.
// A push title passes false: a phone title has no room for a visible escape.
func StripControls(s string, keepNewlines bool) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\n' || r == '\r' {
			if keepNewlines {
				b.WriteRune(r)
			}
			continue
		}
		if removedRune(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// removedRune is deleted even when newlines are kept: C0 other than LF/CR,
// C1, bidi marks, zero-width characters, and the line separators U+2028 and
// U+2029. A keyword value stops on these too. If one of them were the whole
// value, the first redaction would consume the assignment and the secret
// after it would survive the strip.
func removedRune(r rune) bool {
	if r == '\n' || r == '\r' {
		return false
	}
	if r == '\u2028' || r == '\u2029' || isBidi(r) || isZeroWidth(r) {
		return true
	}
	return r < 0x20 || (r >= 0x7F && r <= 0x9F) || unicode.IsControl(r)
}

func isZeroWidth(r rune) bool {
	switch r {
	case 0x180E, 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF:
		return true
	default:
		return false
	}
}
