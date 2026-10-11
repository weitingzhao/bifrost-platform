package notice

import "strings"

// withheldMarker replaces an agent-written field that still contains a hidden
// character after ASCII whitespace is normalised. Nothing of that field is
// shown. The Owner reads it on the request page, where the Console escapes
// those characters.
const withheldMarker = "[withheld: hidden characters]"

// PushField is the only way an agent-written field becomes push text.
// title is set when the field is placed in a title: a newline becomes one
// space. A body field keeps newlines.
//
// ASCII whitespace controls are normalised first: CRLF and CR become LF, and
// tab and form feed become one space. Those are the redactor's whitespace
// class, so a field with no other hidden character is Redact of the
// normalised text — the same bytes Redact produces on it. Any other character
// of the removed classes replaces the whole field. The classes are C0 and C1
// controls (DEL included), the bidi controls, the zero-width characters
// U+200B–U+200D, U+2060 and U+FEFF, and U+2028 and U+2029. An LF a body field
// kept is not one of them.
func PushField(s string, title bool) string {
	s = normalizePushSpace(s, title)
	if fieldHidden(s) {
		return withheldMarker
	}
	return Redact(s)
}

func normalizePushSpace(s string, title bool) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	s = strings.ReplaceAll(s, "\t", " ")
	s = strings.ReplaceAll(s, "\f", " ")
	if title {
		s = strings.ReplaceAll(s, "\n", " ")
	}
	return s
}

// fieldHidden reports a removed-class character other than LF. Call it after
// normalizePushSpace: tab, CR and form feed are already spaces or LF.
func fieldHidden(s string) bool {
	for _, r := range s {
		if r == '\n' {
			continue
		}
		if removedClass(r) {
			return true
		}
	}
	return false
}

// removedClass is a character Send refuses in a title. A body may contain LF;
// the caller exempts that one character. U+180E is not in this set.
func removedClass(r rune) bool {
	if r == '\u2028' || r == '\u2029' || isBidi(r) || isPushZeroWidth(r) {
		return true
	}
	return r < 0x20 || (r >= 0x7F && r <= 0x9F)
}

func isPushZeroWidth(r rune) bool {
	switch r {
	case 0x200B, 0x200C, 0x200D, 0x2060, 0xFEFF:
		return true
	default:
		return false
	}
}

// pushUnclean reports a removed-class character. allowNewline exempts LF,
// which a body may keep and a title may not.
func pushUnclean(s string, allowNewline bool) bool {
	for _, r := range s {
		if allowNewline && r == '\n' {
			continue
		}
		if removedClass(r) {
			return true
		}
	}
	return false
}
