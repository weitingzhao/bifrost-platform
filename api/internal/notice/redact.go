package notice

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// The classes are written out so the questions MCP tool can compile the same
// set. Go and JavaScript do not agree on \s, \w, \b, or case-folding flags.
// Whitespace is space, tab, LF, CR, and FF. A word edge is the start or end
// of the text, or one character outside [A-Za-z0-9_]. Letters are listed in
// both cases. Where a pattern matches a letter without regard to case, U+017F
// (ſ) counts as s and U+212A (K) counts as k: in keywords, in the bearer and
// basic schemes, and in the character classes of the secret. Characters
// outside the redacted span are copied unchanged. The hyphen stays at the end
// of a class so it is literal.
const (
	asciiWS        = "[ \t\n\r\f]"
	notSecretValue = "[^ \t\n\r\f\"',;]"
	notURLUser     = "[^/ \t\n\r\f:@]"
	notURLPass     = "[^@ \t\n\r\f/]"
	foldAZ         = `A-Za-z\x{017F}\x{212A}`
	secretWords    = `[Pp][Aa][Ss\x{017F}][Ss\x{017F}][Ww][Oo][Rr][Dd]|[Pp][Aa][Ss\x{017F}][Ss\x{017F}][Ww][Dd]|[Pp][Ww][Dd]|[Ss\x{017F}][Ee][Cc][Rr][Ee][Tt]|[Tt][Oo][Kk\x{212A}][Ee][Nn]|[Aa][Pp][Ii][_-]?[Kk\x{212A}][Ee][Yy]|[Aa][Cc][Cc][Ee][Ss\x{017F}][Ss\x{017F}][_-]?[Kk\x{212A}][Ee][Yy]|[Pp][Rr][Ii][Vv][Aa][Tt][Ee][_-]?[Kk\x{212A}][Ee][Yy]|[Cc][Rr][Ee][Dd][Ee][Nn][Tt][Ii][Aa][Ll]`
)

// A bounded rule matches the secret only. The characters on either side are
// checked afterwards and are not part of the match, so the next secret can
// use the same separator as its left edge. Github and Slack tokens fold a
// trailing identifier into the match: their body stops before '_', and a
// right-edge check would otherwise leave the token in place.
var redactions = []struct {
	re    *regexp.Regexp
	repl  string
	bound bool
}{
	{regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z ]*PRIVATE KEY-----|$)`), "[redacted private key]", false},
	{regexp.MustCompile(`([Bb][Ee][Aa][Rr][Ee][Rr]|[Bb][Aa][Ss\x{017F}][Ii][Cc])` + asciiWS + `+[` + foldAZ + `0-9._~+/=-]{8,}`), "$1 [redacted]", true},
	{regexp.MustCompile(`([` + foldAZ + `0-9_.-]*(?:` + secretWords + `)[` + foldAZ + `0-9_.-]*)(["']?` + asciiWS + `*[:=]` + asciiWS + `*["']?)` + notSecretValue + `+`), "$1$2[redacted]", true},
	{regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), "[redacted jwt]", true},
	{regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`), "[redacted aws key]", true},
	{regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9]{20,}[A-Za-z0-9_]*|glpat-[A-Za-z0-9_-]{20,}|xox[abpr]-[A-Za-z0-9-]{10,}[A-Za-z0-9_]*)`), "[redacted token]", true},
	{regexp.MustCompile(`://(` + notURLUser + `+):(` + notURLPass + `+)@`), "://$1:[redacted]@", false},
}

// Redact masks common secret shapes. Call it before clipping or storing:
// a tail or a clip that cuts the marker off would otherwise keep the secret.
// Controls, bidi marks and newlines are not rewritten.
func Redact(s string) string {
	for _, r := range redactions {
		if r.bound {
			s = replaceBounded(s, r.re, r.repl)
			continue
		}
		s = r.re.ReplaceAllString(s, r.repl)
	}
	return s
}

// replaceBounded accepts a candidate only when the character before it and the
// character after it are an edge, then resumes at the end of the match.
func replaceBounded(s string, re *regexp.Regexp, repl string) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		loc := re.FindStringSubmatchIndex(s[i:])
		if loc == nil {
			b.WriteString(s[i:])
			break
		}
		start := i + loc[0]
		end := i + loc[1]
		if start < end && leftEdge(s, start) && rightEdge(s, end) {
			b.WriteString(s[i:start])
			b.Write(re.ExpandString(nil, repl, s[i:], loc))
			i = end
			continue
		}
		next := start + 1
		if next <= i {
			next = i + 1
		}
		if next > len(s) {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i:next])
		i = next
	}
	return b.String()
}

func leftEdge(s string, i int) bool {
	if i <= 0 {
		return true
	}
	r, _ := utf8.DecodeLastRuneInString(s[:i])
	return !asciiWord(r)
}

func rightEdge(s string, i int) bool {
	if i >= len(s) {
		return true
	}
	r, _ := utf8.DecodeRuneInString(s[i:])
	return !asciiWord(r)
}

func asciiWord(r rune) bool {
	return r == '_' || (r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')
}
