package notice

import (
	"log/slog"
	"unicode/utf8"
)

const (
	// WithheldTitle is posted when the title or the body has a hidden
	// character and the caller did not pass a fallback that itself is clean.
	WithheldTitle = "Notification withheld"
	// WithheldBody is the only body posted in place of composed text.
	WithheldBody = "Text withheld: it contained hidden characters. Open it in the Console to read it."
)

// HiddenSample is one value of a hidden class. Text is a single character,
// or one invalid UTF-8 byte when Name is "utf8".
type HiddenSample struct {
	Name string
	Text string
}

// HiddenSamples is one value of every class HasHidden removes from a push:
// a C0 control, a C1 control, each bidi range, each zero-width range, the
// line and paragraph separators, each extra invisible character, and one
// invalid UTF-8 byte.
func HiddenSamples() []HiddenSample {
	return []HiddenSample{
		{Name: "c0", Text: "\u0000"},
		{Name: "del", Text: "\u007f"},
		{Name: "c1", Text: "\u0080"},
		{Name: "bidi-202a", Text: "\u202a"},
		{Name: "bidi-202e", Text: "\u202e"},
		{Name: "bidi-2066", Text: "\u2066"},
		{Name: "bidi-2069", Text: "\u2069"},
		{Name: "bidi-061c", Text: "\u061c"},
		{Name: "bidi-200e", Text: "\u200e"},
		{Name: "bidi-200f", Text: "\u200f"},
		{Name: "zw-200b", Text: "\u200b"},
		{Name: "zw-200d", Text: "\u200d"},
		{Name: "zw-2060", Text: "\u2060"},
		{Name: "zw-feff", Text: "\ufeff"},
		{Name: "ls", Text: "\u2028"},
		{Name: "ps", Text: "\u2029"},
		{Name: "shy", Text: "\u00ad"},
		{Name: "cgj", Text: "\u034f"},
		{Name: "hangul-115f", Text: "\u115f"},
		{Name: "hangul-1160", Text: "\u1160"},
		{Name: "mvs", Text: "\u180e"},
		{Name: "inv-2061", Text: "\u2061"},
		{Name: "inv-2064", Text: "\u2064"},
		{Name: "vs-fe00", Text: "\ufe00"},
		{Name: "vs-fe0f", Text: "\ufe0f"},
		{Name: "tag-start", Text: string(rune(0xE0000))},
		{Name: "tag-end", Text: string(rune(0xE007F))},
		{Name: "utf8", Text: "\xff"},
	}
}

// HasHidden reports whether s must not be posted. It is true when s is not
// valid UTF-8, or when it contains a C0 or C1 control other than tab, LF and
// CR, a bidirectional control, a zero-width character, U+2028, U+2029, or one
// of the other invisible characters (soft hyphen, grapheme joiner, Hangul
// fillers, Mongolian vowel separator, invisible operators, variation
// selectors, tags).
func HasHidden(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if hiddenRune(r) {
			return true
		}
	}
	return false
}

func hiddenRune(r rune) bool {
	if r < 0x20 {
		return r != '\t' && r != '\n' && r != '\r'
	}
	if r == 0x7F || (r >= 0x80 && r <= 0x9F) {
		return true
	}
	switch r {
	case 0x00AD, 0x034F, 0x061C, 0x115F, 0x1160, 0x180E,
		0x200E, 0x200F, 0x2028, 0x2029, 0x2060, 0xFEFF:
		return true
	}
	if (r >= 0x200B && r <= 0x200D) || (r >= 0x202A && r <= 0x202E) ||
		(r >= 0x2061 && r <= 0x2064) || (r >= 0x2066 && r <= 0x2069) ||
		(r >= 0xFE00 && r <= 0xFE0F) || (r >= 0xE0000 && r <= 0xE007F) {
		return true
	}
	return false
}

// Posted is the message Send hands to the relay. When the title or the body
// has a hidden character, both are replaced: the title becomes Fallback, or
// WithheldTitle when Fallback is empty or itself hidden, and the body becomes
// WithheldBody. The click URL and the priority are left as the caller set
// them. Nothing of the composed title or body remains.
func Posted(msg Message) Message {
	if !HasHidden(msg.Title) && !HasHidden(msg.Message) {
		return msg
	}
	title := msg.Fallback
	if title == "" || HasHidden(title) || HasHidden(WithheldBody) {
		title = WithheldTitle
	}
	kind, id := msg.Kind, msg.ID
	if HasHidden(kind) {
		kind = ""
	}
	if HasHidden(id) {
		id = ""
	}
	slog.Warn("notice withheld", "kind", kind, "id", id)
	msg.Title = title
	msg.Message = WithheldBody
	return msg
}
