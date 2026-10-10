package approvals

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// CanonicalApprovalLine is the one line the server shows for a request.
// Control characters, Unicode line separators (U+2028, U+2029) and
// bidirectional controls are escaped, and the line ends with the first 12
// hex characters of the params hash. Those 12 characters are a display
// digest (48 bits), not a uniqueness proof: two param sets can share a
// prefix. The server compares the full params hash. Callers compare the
// line with strict equality.
func CanonicalApprovalLine(a Approval) string {
	ref := a.ID
	if a.Number != 0 {
		ref = fmt.Sprintf("#%d", a.Number)
	}
	parts := []string{ref, "tier " + orMark(a.Tier), a.Action}
	if a.Env != "" {
		parts = append(parts, "env "+escapeVisible(a.Env))
	}
	if a.Summary != "" {
		parts = append(parts, escapeVisible(a.Summary))
	}
	if len(a.KeyParams) > 0 {
		keys := make([]string, 0, len(a.KeyParams))
		for k := range a.KeyParams {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		kv := make([]string, 0, len(keys))
		for _, k := range keys {
			kv = append(kv, escapeVisible(k)+"="+escapeVisible(a.KeyParams[k]))
		}
		parts = append(parts, strings.Join(kv, ", "))
	}
	line := strings.Join(parts, " · ")
	sum := a.ParamsHash
	if len(sum) > 12 {
		sum = sum[:12]
	}
	if sum == "" {
		sum = "000000000000"
	}
	return line + " · " + sum
}

func orMark(s string) string {
	if s == "" {
		return "?"
	}
	return s
}

// escapeVisible makes a field safe to show on one line. Backslash, newlines,
// tabs, other controls, and Unicode bidirectional controls are written as
// visible ASCII escapes.
func escapeVisible(s string) string {
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
