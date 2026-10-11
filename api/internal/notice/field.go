package notice

import (
	"strings"
	"unicode/utf8"
)

// WithheldMarker replaces a whole agent-written field that contains a hidden
// character or is not valid UTF-8. The Owner sees this text everywhere that
// field would have appeared. Nothing of the original field is shown.
const WithheldMarker = "[withheld: hidden characters]"

// HasHidden reports whether raw must be withheld from a notice.
// It is true when raw is not valid UTF-8, or when it contains a hidden
// character: C0 and C1 controls other than tab, LF, form feed and CR
// (DEL included); bidi controls; zero-width characters; U+2028 and U+2029;
// and U+00AD, U+034F, U+115F, U+1160, U+180E, U+2061–U+2064, U+FE00–U+FE0F,
// U+E0000–U+E007F.
func HasHidden(raw string) bool {
	if !utf8.ValidString(raw) {
		return true
	}
	for _, r := range raw {
		if hiddenRune(r) {
			return true
		}
	}
	return false
}

func hiddenRune(r rune) bool {
	switch r {
	case '\t', '\n', '\f', '\r':
		return false
	}
	if r < 0x20 || r == 0x7F || (r >= 0x80 && r <= 0x9F) {
		return true
	}
	switch r {
	case 0x00AD, 0x034F, 0x061C, 0x115F, 0x1160, 0x180E,
		0x200B, 0x200C, 0x200D, 0x200E, 0x200F,
		0x2028, 0x2029,
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E,
		0x2060, 0x2061, 0x2062, 0x2063, 0x2064,
		0x2066, 0x2067, 0x2068, 0x2069,
		0xFEFF:
		return true
	}
	if r >= 0xFE00 && r <= 0xFE0F {
		return true
	}
	if r >= 0xE0000 && r <= 0xE007F {
		return true
	}
	return false
}

// Field prepares one agent-written field for a notice.
// A hidden character or invalid UTF-8 withholds the whole field.
// Anything else is Redact of the original text, then output normalisation:
// CRLF and CR become LF, tab and form feed become one space, and in a title
// LF becomes one space. title is set when the field is placed in a title.
func Field(raw string, title bool) string {
	if HasHidden(raw) {
		return WithheldMarker
	}
	return normalise(Redact(raw), title)
}

func normalise(s string, title bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\f", " ")
	if title {
		s = strings.ReplaceAll(s, "\n", " ")
	}
	return s
}

// unclean is the send-door check. It is HasHidden, plus tab allowed nowhere,
// plus LF allowed in a body only. Form feed and CR are not hidden; Field
// has already turned them into a space and LF.
func unclean(s string, body bool) bool {
	if HasHidden(s) || strings.ContainsRune(s, '\t') {
		return true
	}
	if !body && strings.ContainsRune(s, '\n') {
		return true
	}
	return false
}
