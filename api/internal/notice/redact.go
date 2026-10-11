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

// How a rule is applied. The token-prefix rules (bearer, jwt, aws, github and
// slack) keep overlapping start positions: a failed edge check resumes one
// byte after the match start, because a later token can begin inside it.
// Each of those rules walks the text a constant number of times. A regexp
// Find that restarts one byte later re-scans a long greedy tail on every
// candidate, which is quadratic on "Zxoxb-" repeated.
const (
	scanRegex = iota
	scanKeyword
	scanBearer
	scanJWT
	scanAWS
	scanToken
)

var (
	pemRE    = regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----(?s:.*?)(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)
	bearerRE = regexp.MustCompile(`([Bb][Ee][Aa][Rr][Ee][Rr]|[Bb][Aa][Ss\x{017F}][Ii][Cc])` + asciiWS + `+[` + foldAZ + `0-9._~+/=-]{8,}`)
	jwtRE    = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	awsRE    = regexp.MustCompile(`(?:AKIA|ASIA)[0-9A-Z]{16}`)
	tokenRE  = regexp.MustCompile(`(?:gh[pousr]_[A-Za-z0-9]{20,}[A-Za-z0-9_]*|glpat-[A-Za-z0-9_-]{20,}|xox[abpr]-[A-Za-z0-9-]{10,}[A-Za-z0-9_]*)`)
	urlRE    = regexp.MustCompile(`://(` + notURLUser + `+):(` + notURLPass + `+)@`)
)

// Github and Slack tokens fold a trailing identifier into the match: their
// body stops before '_', and a right-edge check would otherwise leave the
// token in place. The characters on either side of a bounded match are not
// part of it, so the next secret can use the same separator as its left edge.
var redactions = []struct {
	re   *regexp.Regexp
	repl string
	scan int
}{
	{pemRE, "[redacted private key]", scanRegex},
	{bearerRE, "$1 [redacted]", scanBearer},
	// Keyword assignments are a single pass in replaceKeyword. The expression
	// used to be a regexp whose failure restarted one character later; on a
	// long run of one character that re-scan was quadratic.
	{nil, "$1$2[redacted]", scanKeyword},
	{jwtRE, "[redacted jwt]", scanJWT},
	{awsRE, "[redacted aws key]", scanAWS},
	{tokenRE, "[redacted token]", scanToken},
	{urlRE, "://$1:[redacted]@", scanRegex},
}

// keywordHit is the keyword alternation alone, with no leading star, so a
// miss on a repeated character is one left-to-right pass.
var keywordHit = regexp.MustCompile(secretWords)

// Redact masks common secret shapes. Call it before clipping or storing:
// a tail or a clip that cuts the marker off would otherwise keep the secret.
// Controls, bidi marks and newlines are not rewritten.
func Redact(s string) string {
	return applyRedactions(s, nil)
}

// redactScanSteps counts characters the scans examine, including the class-run
// walks inside the token-prefix rules. A counter that only ticks once per
// failed candidate is not a bound on that work.
func redactScanSteps(s string) int {
	var n int
	applyRedactions(s, &n)
	return n
}

func applyRedactions(s string, steps *int) string {
	for _, r := range redactions {
		switch r.scan {
		case scanKeyword:
			s = replaceKeyword(s, steps)
		case scanBearer:
			s = replaceBearer(s, steps)
		case scanJWT:
			s = replaceJWT(s, steps)
		case scanAWS:
			s = replaceAWS(s, steps)
		case scanToken:
			s = replaceToken(s, steps)
		default:
			s = r.re.ReplaceAllString(s, r.repl)
		}
	}
	return s
}

// replaceBounded is the regexp form of a bounded rule. The keyword equivalence
// test still uses it. The token-prefix rules do not: Find restarts one byte
// after a failed edge and re-scans a long greedy tail each time. A candidate
// that fails the edge check resumes one byte later, because a later token can
// begin inside this match, and the shared vectors depend on finding it.
func replaceBounded(s string, re *regexp.Regexp, repl string, steps *int) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		if steps != nil {
			*steps++
		}
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

// replaceKeyword masks keyword assignments in one pass. The separator (= or :)
// is required, so a run of one character with no separator is a single walk.
// A separator that is not an assignment is stepped past; the search does not
// restart one character after a failed candidate.
func replaceKeyword(s string, steps *int) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		sep := nextSep(s, i, steps)
		if sep < 0 {
			b.WriteString(s[i:])
			break
		}
		start, sepEnd, valEnd, ok := keywordAssignmentAt(s, i, sep, steps)
		if !ok || !leftEdge(s, start) || !rightEdge(s, valEnd) {
			next := sep + 1
			b.WriteString(s[i:next])
			i = next
			continue
		}
		b.WriteString(s[i:start])
		b.WriteString(s[start:sepEnd])
		b.WriteString("[redacted]")
		i = valEnd
	}
	return b.String()
}

func nextSep(s string, from int, steps *int) int {
	for i := from; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if steps != nil {
			*steps++
		}
		if r == '=' || r == ':' {
			return i
		}
		if size < 1 {
			size = 1
		}
		i += size
	}
	return -1
}

// keywordAssignmentAt parses the assignment whose [:=] is at sep, but only the
// part that starts at or after from. sepEnd is the first byte of the value.
func keywordAssignmentAt(s string, from, sep int, steps *int) (start, sepEnd, valEnd int, ok bool) {
	j := sep
	for j > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		if steps != nil {
			*steps++
		}
		if !isASCIIWS(r) {
			break
		}
		j -= size
	}
	if j > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:j])
		if r == '\'' || r == '"' {
			if steps != nil {
				*steps++
			}
			j -= size
		}
	}
	idEnd := j
	idStart := idEnd
	for idStart > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:idStart])
		if steps != nil {
			*steps++
		}
		if !isSecretClass(r) {
			break
		}
		idStart -= size
	}
	if idStart < from {
		idStart = from
	}
	if idStart >= idEnd || !keywordHit.MatchString(s[idStart:idEnd]) {
		return 0, 0, 0, false
	}
	p := sep + 1
	for p < len(s) {
		r, size := utf8.DecodeRuneInString(s[p:])
		if steps != nil {
			*steps++
		}
		if !isASCIIWS(r) {
			break
		}
		if size < 1 {
			size = 1
		}
		p += size
	}
	if p < len(s) {
		r, size := utf8.DecodeRuneInString(s[p:])
		if r == '\'' || r == '"' {
			if steps != nil {
				*steps++
			}
			if size < 1 {
				size = 1
			}
			p += size
		}
	}
	valStart := p
	valEnd = p
	for valEnd < len(s) {
		r, size := utf8.DecodeRuneInString(s[valEnd:])
		if steps != nil {
			*steps++
		}
		if isValueStop(r) {
			break
		}
		if size < 1 {
			size = 1
		}
		valEnd += size
	}
	if valEnd == valStart {
		return 0, 0, 0, false
	}
	return idStart, valStart, valEnd, true
}

func isASCIIWS(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\f'
}

func isValueStop(r rune) bool {
	return isASCIIWS(r) || r == '"' || r == '\'' || r == ',' || r == ';'
}

func isSecretClass(r rune) bool {
	return r == '_' || r == '.' || r == '-' ||
		(r >= '0' && r <= '9') ||
		(r >= 'A' && r <= 'Z') ||
		(r >= 'a' && r <= 'z') ||
		r == '\u017f' || r == '\u212a'
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
