package notice

import (
	"fmt"
	"strings"
	"unicode"
)

// EscapeVisible makes a field safe to show on one line. Backslash, newlines,
// tabs, other controls, and Unicode bidirectional controls are written as
// visible ASCII escapes. U+200B and other non-controls are left as they are.
func EscapeVisible(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r == '\u2028' || r == '\u2029' || isBidi(r) || unicode.IsControl(r) {
				fmt.Fprintf(&b, `\u%04x`, r)
				continue
			}
			b.WriteRune(r)
		}
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
