package releasepolicy

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// Additive DDL (ADR §5, 2026-10-08 revision) may ship on the signed policy
// when ClassifyDDL accepts the change: every statement that was already in
// the file remains, in the same order, and every added statement is one
// allow-listed shape (a new table, one added column, a concurrent index, or
// a comment). Dropping, renaming, changing a type or a constraint, rewriting
// rows, or changing grants and owners stays tier D.
//
// The check is an allow-list of statements, not a scan for forbidden words.
// A statement from before is still present only when it is textually the
// same after each comment is replaced by one space and each run of
// whitespace outside quotes is collapsed to one space. A comment is a
// separator, the way PostgreSQL treats it: removing the comment must not
// join the tokens on either side. Whitespace that was present between two
// tokens stays present; whitespace that was absent stays absent. Nothing is
// case-folded. Whitespace inside quotes is kept as written.
//
// The lexer refuses input that PostgreSQL sessions do not all read the same
// way: a U& unicode string or identifier (a lone U or u, an ampersand and a
// quote, even when whitespace or comments sit between those pieces), a
// backslash in an ordinary non-E string, a backslash-quote in an escape
// string, an escape that decodes to a zero byte, any non-ASCII byte outside
// a quoted string, a quoted identifier or a comment, and a NUL byte anywhere.
// Keyword matching for the allow-list is ASCII-only.
//
// Added columns accept only a type the grammar binds to a built-in without
// a catalog lookup, or pg_catalog.<name> for a built-in that would otherwise
// be resolved through the search path. Only a path ending in ".sql" is
// classified, and a migrations/ directory or a db_init* name is never
// classified. The policy's DDL paths also match *.py, db_init*, YAML jobs
// and migrations/**; those always wait for the Owner.
//
// ClassifyDDL compares a DDL file before and after a release. ok is true when
// the change is allow-listed. A file that did not exist before is compared
// against nothing. An unterminated quote or comment, a psql meta-command, or
// a line that is not SQL makes the file unclassifiable.

// ddlPathReason reports why path cannot be classified. Empty means the file
// is a .sql path this check may read. migrations/ and db_init* wait for the
// Owner even when the name ends in .sql; any other non-.sql hit does too.
func ddlPathReason(path string) string {
	parts := strings.Split(path, "/")
	for i, seg := range parts {
		if strings.EqualFold(seg, "migrations") && i < len(parts)-1 {
			return path + " is under migrations/ (ask the Owner)"
		}
		if strings.HasPrefix(strings.ToLower(seg), "db_init") {
			return path + " matches db_init* (ask the Owner)"
		}
	}
	if !strings.HasSuffix(path, ".sql") {
		return path + " is not a .sql file (ask the Owner)"
	}
	return ""
}

// ClassifyDDL compares a DDL file before and after a release. ok is true when
// every previous statement remains in order and every added statement is an
// allow-listed shape. A file that did not exist before is compared against nothing.
func ClassifyDDL(before, after string) (bool, string) {
	prev, err := splitSQL(before)
	if err != nil {
		return false, err.Error()
	}
	next, err := splitSQL(after)
	if err != nil {
		return false, err.Error()
	}
	i := 0
	var added []statement
	for _, st := range next {
		if i < len(prev) && stmtEqual(prev[i], st) {
			i++
			continue
		}
		added = append(added, st)
	}
	if i < len(prev) {
		return false, fmt.Sprintf("removes or rewrites %q", show(prev[i]))
	}
	for _, st := range added {
		if ok, why := allowStatement(st); !ok {
			return false, why
		}
	}
	return true, ""
}

type tokKind uint8

const (
	kindWord tokKind = iota
	kindQIdent
	kindString
	kindNumber
	kindOp
)

type token struct {
	kind   tokKind
	text   string
	prefix string // string introducer as written: "", "E"/"e", or "$tag$"
	start  int
	end    int
}

type statement struct {
	raw  string
	norm string
	toks []token
	semi int // index of ';' in the source, or -1
}

// stmtEqual is textual. Each comment is already one space in norm,
// whitespace outside quotes is already collapsed, and case is not folded.
func stmtEqual(a, b statement) bool {
	return a.norm == b.norm
}

func show(st statement) string {
	return clip(strings.Join(strings.Fields(st.raw), " "))
}

func notAllowed(st statement) (bool, string) {
	return false, fmt.Sprintf("not an allowed statement: %q", show(st))
}

func clip(s string) string {
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

var (
	errUntermString   = errors.New("unterminated string")
	errUntermIdent    = errors.New("unterminated quoted identifier")
	errUntermComment  = errors.New("unterminated block comment")
	errUntermDollar   = errors.New("unterminated dollar quote")
	errBadDollar      = errors.New("invalid dollar quote")
	errUntermStmt     = errors.New("unterminated statement")
	errUnicode        = errors.New("unicode escape string or identifier")
	errBackslash      = errors.New("backslash in an ordinary string")
	errBackslashQuote = errors.New("backslash quote in an escape string")
	errEscapedNUL     = errors.New("escape string decodes to a zero byte")
	errUnicodeEscape  = errors.New("invalid Unicode escape")
	errSessionEscape  = errors.New("escape string is not session-independent")
	errNonASCII       = errors.New("non-ASCII outside a quoted string, quoted identifier, or comment")
	errNUL            = errors.New("NUL byte")
	errBadUTF8        = errors.New("invalid UTF-8")
)

// normText collapses whitespace outside quotes and remembers whether any
// whitespace sat between tokens. Token bytes are copied unchanged.
type normText struct {
	b         strings.Builder
	needSpace bool
	saw       bool
}

func (n *normText) reset() {
	n.b.Reset()
	n.needSpace = false
	n.saw = false
}

func (n *normText) space() {
	if n.saw {
		n.needSpace = true
	}
}

func (n *normText) write(s string) {
	if n.needSpace {
		n.b.WriteByte(' ')
		n.needSpace = false
	}
	n.b.WriteString(s)
	n.saw = true
}

func splitSQL(text string) ([]statement, error) {
	s := text
	var stmts []statement
	var toks []token
	var norm normText
	start := -1
	lineBare := true
	emit := func(kind tokKind, text, prefix string, tokStart, tokEnd int) {
		if start < 0 {
			start = tokStart
		}
		norm.write(s[tokStart:tokEnd])
		toks = append(toks, token{kind: kind, text: text, prefix: prefix, start: tokStart, end: tokEnd})
		lineBare = false
	}
	finish := func(semi int) {
		if start >= 0 && len(toks) > 0 {
			stmts = append(stmts, statement{
				raw:  strings.TrimSpace(s[start : semi+1]),
				norm: norm.b.String(),
				toks: toks,
				semi: semi,
			})
		}
		toks = nil
		start = -1
		norm.reset()
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0 {
			return nil, errNUL
		}
		if c == ' ' || c == '\t' || c == '\f' || c == '\v' {
			norm.space()
			i++
			continue
		}
		if c == '\n' || c == '\r' {
			norm.space()
			if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			lineBare = true
			continue
		}
		if strings.HasPrefix(s[i:], "--") {
			j, err := scanLineComment(s, i)
			if err != nil {
				return nil, err
			}
			// A comment is whitespace. Dropping it must not join tokens.
			norm.space()
			i = j
			continue
		}
		if strings.HasPrefix(s[i:], "/*") {
			j, err := scanBlock(s, i+2)
			if err != nil {
				return nil, err
			}
			norm.space()
			i = j
			continue
		}
		if c >= 0x80 {
			return nil, errNonASCII
		}
		if lineBare && c == '#' {
			return nil, fmt.Errorf("not SQL %q", clip(strings.TrimSpace(restOfLine(s, i))))
		}
		if lineBare && c == '/' && i+1 < len(s) && s[i+1] == '/' {
			return nil, fmt.Errorf("not SQL %q", clip(strings.TrimSpace(restOfLine(s, i))))
		}
		if c == '\\' {
			return nil, fmt.Errorf("psql meta-command %q", clip(strings.TrimSpace(restOfLine(s, i))))
		}
		switch c {
		case '\'':
			body, j, err := scanQuoted(s, i, '\'', errUntermString, true)
			if err != nil {
				return nil, err
			}
			emit(kindString, body, "", i, j)
			i = j
		case '"':
			body, j, err := scanQuoted(s, i, '"', errUntermIdent, false)
			if err != nil {
				return nil, err
			}
			emit(kindQIdent, body, "", i, j)
			i = j
		case '$':
			prefix, body, j, err := scanDollar(s, i)
			if err != nil {
				return nil, err
			}
			emit(kindString, body, prefix, i, j)
			i = j
		case '(', ')', '[', ']', ',', '.', ';':
			if c == '.' && i+1 < len(s) && isDigit(s[i+1]) {
				j := readNumber(s, i)
				emit(kindNumber, s[i:j], "", i, j)
				i = j
				continue
			}
			if c == ';' {
				if norm.saw {
					norm.write(";")
				}
				finish(i)
				i++
				continue
			}
			emit(kindOp, s[i:i+1], "", i, i+1)
			i++
		default:
			if isDigit(c) {
				j := readNumber(s, i)
				emit(kindNumber, s[i:j], "", i, j)
				i = j
				continue
			}
			if isASCIIIdentStart(c) {
				j := readASCIIIdent(s, i)
				word := s[i:j]
				if isSingleU(word) {
					yes, err := unicodeIntro(s, j)
					if err != nil {
						return nil, err
					}
					if yes {
						return nil, errUnicode
					}
				}
				if (word == "E" || word == "e") && j < len(s) && s[j] == '\'' {
					body, n, err := scanEString(s, j)
					if err != nil {
						return nil, err
					}
					emit(kindString, body, word, i, n)
					i = n
					continue
				}
				emit(kindWord, word, "", i, j)
				i = j
				continue
			}
			if isOpByte(c) {
				j := i + 1
				for j < len(s) && isOpByte(s[j]) {
					j++
				}
				emit(kindOp, s[i:j], "", i, j)
				i = j
				continue
			}
			return nil, fmt.Errorf("not SQL %q", clip(restOfLine(s, i)))
		}
	}
	if start >= 0 || len(toks) > 0 {
		return nil, errUntermStmt
	}
	return stmts, nil
}

func restOfLine(s string, i int) string {
	j := i
	for j < len(s) && s[j] != '\n' && s[j] != '\r' {
		j++
	}
	return s[i:j]
}

func scanLineComment(s string, i int) (int, error) {
	body := i + 2
	i = body
	for i < len(s) && s[i] != '\n' && s[i] != '\r' {
		if s[i] == 0 {
			return i, errNUL
		}
		i++
	}
	if !utf8.ValidString(s[body:i]) {
		return i, errBadUTF8
	}
	return i, nil
}

func scanBlock(s string, i int) (int, error) {
	body := i
	depth := 1
	for i < len(s) {
		if s[i] == 0 {
			return i, errNUL
		}
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			depth++
			i += 2
			continue
		}
		if i+1 < len(s) && s[i] == '*' && s[i+1] == '/' {
			depth--
			i += 2
			if depth == 0 {
				if !utf8.ValidString(s[body : i-2]) {
					return i, errBadUTF8
				}
				return i, nil
			}
			continue
		}
		i++
	}
	return i, errUntermComment
}

func scanQuoted(s string, i int, quote byte, unterm error, ordinary bool) (string, int, error) {
	var b strings.Builder
	i++
	for i < len(s) {
		if s[i] == 0 {
			return "", i, errNUL
		}
		if ordinary && s[i] == '\\' {
			return "", i, errBackslash
		}
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				b.WriteByte(quote)
				b.WriteByte(quote)
				i += 2
				continue
			}
			body := b.String()
			if !utf8.ValidString(body) {
				return "", i, errBadUTF8
			}
			return body, i + 1, nil
		}
		b.WriteByte(s[i])
		i++
	}
	return "", i, unterm
}

// scanEString reads an E'...' literal. The body kept for the token is the
// source text inside the quotes; escapes are decoded only far enough to
// refuse the ones PostgreSQL sessions do not all accept.
//
// \' depends on backslash_quote. Octal (1–3 digits) and hex (1–2 digits)
// are byte values; PostgreSQL stores the low 8 bits, so \400 is a zero
// byte as well as \0, \00, \000, \x0 and \x00. \uXXXX and \UXXXXXXXX that
// decode to U+0000 are the same refusal. A short \u or \U sequence is an
// invalid Unicode escape. A decoded byte or code point above ASCII is
// refused too: it depends on the server encoding.
func scanEString(s string, i int) (string, int, error) {
	var b strings.Builder
	i++
	for i < len(s) {
		if s[i] == 0 {
			return "", i, errNUL
		}
		if s[i] == '\\' {
			n, err := scanEEscape(s, i, &b)
			if err != nil {
				return "", n, err
			}
			i = n
			continue
		}
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteString("''")
				i += 2
				continue
			}
			body := b.String()
			if !utf8.ValidString(body) {
				return "", i, errBadUTF8
			}
			return body, i + 1, nil
		}
		b.WriteByte(s[i])
		i++
	}
	return "", i, errUntermString
}

func scanEEscape(s string, i int, b *strings.Builder) (int, error) {
	if i+1 >= len(s) {
		return i, errUntermString
	}
	if s[i+1] == 0 {
		return i + 1, errNUL
	}
	c := s[i+1]
	switch {
	case c >= '0' && c <= '7':
		return scanOctalEscape(s, i, b)
	case c == 'x':
		return scanHexEscape(s, i, b)
	case c == 'u' || c == 'U':
		return scanUnicodeEscape(s, i, b)
	case c == '\'':
		return i + 1, errBackslashQuote
	default:
		b.WriteByte('\\')
		b.WriteByte(c)
		return i + 2, nil
	}
}

func scanOctalEscape(s string, i int, b *strings.Builder) (int, error) {
	j := i + 1
	val := 0
	for n := 0; n < 3 && j < len(s) && s[j] >= '0' && s[j] <= '7'; n++ {
		val = val*8 + int(s[j]-'0')
		j++
	}
	if err := refuseEscapeByte(val); err != nil {
		return j, err
	}
	b.WriteString(s[i:j])
	return j, nil
}

func scanHexEscape(s string, i int, b *strings.Builder) (int, error) {
	if i+2 >= len(s) || !isHex(s[i+2]) {
		b.WriteByte('\\')
		b.WriteByte('x')
		return i + 2, nil
	}
	j := i + 2
	val := hexVal(s[j])
	j++
	if j < len(s) && isHex(s[j]) {
		val = val*16 + hexVal(s[j])
		j++
	}
	if err := refuseEscapeByte(val); err != nil {
		return j, err
	}
	b.WriteString(s[i:j])
	return j, nil
}

func scanUnicodeEscape(s string, i int, b *strings.Builder) (int, error) {
	need := 4
	if s[i+1] == 'U' {
		need = 8
	}
	j := i + 2
	if j+need > len(s) {
		return i, errUnicodeEscape
	}
	val := 0
	for k := 0; k < need; k++ {
		if !isHex(s[j+k]) {
			return i, errUnicodeEscape
		}
		val = val*16 + hexVal(s[j+k])
	}
	j += need
	if val == 0 {
		return j, errEscapedNUL
	}
	if val > 0x7F {
		return j, errSessionEscape
	}
	b.WriteString(s[i:j])
	return j, nil
}

// refuseEscapeByte applies PostgreSQL's unsigned-char truncation. \400 is
// 256 and becomes a zero byte. Bytes at or above 0x80 are not the same
// character in every server encoding.
func refuseEscapeByte(val int) error {
	b := val & 0xFF
	if b == 0 {
		return errEscapedNUL
	}
	if b >= 0x80 {
		return errSessionEscape
	}
	return nil
}

func isHex(b byte) bool {
	return (b >= '0' && b <= '9') || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
}

func hexVal(b byte) int {
	switch {
	case b >= '0' && b <= '9':
		return int(b - '0')
	case b >= 'a' && b <= 'f':
		return int(b-'a') + 10
	default:
		return int(b-'A') + 10
	}
}

func scanDollar(s string, i int) (prefix, body string, next int, err error) {
	if i >= len(s) || s[i] != '$' {
		return "", "", i, errBadDollar
	}
	j := i + 1
	if j < len(s) && s[j] != '$' {
		if s[j] >= 0x80 {
			return "", "", j, errNonASCII
		}
		if !isASCIIIdentStart(s[j]) {
			return "", "", i, errBadDollar
		}
		j++
		for j < len(s) && isTagCont(s[j]) {
			j++
		}
		if j < len(s) && s[j] >= 0x80 {
			return "", "", j, errNonASCII
		}
		if j >= len(s) || s[j] != '$' {
			return "", "", i, errBadDollar
		}
	}
	if j >= len(s) || s[j] != '$' {
		return "", "", i, errBadDollar
	}
	j++
	prefix = s[i:j]
	k := strings.Index(s[j:], prefix)
	if k < 0 {
		return "", "", i, errUntermDollar
	}
	body = s[j : j+k]
	if strings.IndexByte(body, 0) >= 0 {
		return "", "", i, errNUL
	}
	if !utf8.ValidString(body) {
		return "", "", i, errBadUTF8
	}
	return prefix, body, j + k + len(prefix), nil
}

// unicodeIntro reports whether a lone U that ended at i introduces a U&'…'
// or U&"…" form. Whitespace and comments may sit between U, & and the quote.
func unicodeIntro(s string, i int) (bool, error) {
	j, err := skipSep(s, i)
	if err != nil || j >= len(s) || s[j] != '&' {
		return false, err
	}
	j, err = skipSep(s, j+1)
	if err != nil || j >= len(s) {
		return false, err
	}
	return s[j] == '\'' || s[j] == '"', nil
}

func skipSep(s string, i int) (int, error) {
	for i < len(s) {
		if s[i] == 0 {
			return i, errNUL
		}
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\f' || s[i] == '\v' || s[i] == '\n' || s[i] == '\r' {
			if s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i += 2
				continue
			}
			i++
			continue
		}
		if strings.HasPrefix(s[i:], "--") {
			j, err := scanLineComment(s, i)
			if err != nil {
				return i, err
			}
			i = j
			continue
		}
		if strings.HasPrefix(s[i:], "/*") {
			j, err := scanBlock(s, i+2)
			if err != nil {
				return i, err
			}
			i = j
			continue
		}
		break
	}
	return i, nil
}

func isSingleU(word string) bool { return word == "U" || word == "u" }

func statementTexts(sql string) ([]string, error) {
	stmts, err := splitSQL(sql)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(stmts))
	for _, st := range stmts {
		out = append(out, st.raw)
	}
	return out, nil
}

// mutationPoints lists indexes where two tokens touch, including a semicolon
// that immediately follows the last token, and indexes of ASCII letters that
// belong to tokens. Comment text is not included.
func mutationPoints(sql string) (gaps, letters []int, err error) {
	stmts, err := splitSQL(sql)
	if err != nil {
		return nil, nil, err
	}
	for _, st := range stmts {
		for i := 0; i+1 < len(st.toks); i++ {
			if st.toks[i].end == st.toks[i+1].start {
				gaps = append(gaps, st.toks[i].end)
			}
		}
		if n := len(st.toks); n > 0 && st.semi == st.toks[n-1].end {
			gaps = append(gaps, st.semi)
		}
		for _, tok := range st.toks {
			for i := tok.start; i < tok.end && i < len(sql); i++ {
				c := sql[i]
				if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') {
					letters = append(letters, i)
				}
			}
		}
	}
	return gaps, letters, nil
}

// separatorSpans lists source ranges between two tokens, and between the
// last token and the semicolon, that are only whitespace and comments.
// Replacing one of these ranges with a comment does not change the statement.
func separatorSpans(sql string) ([][2]int, error) {
	stmts, err := splitSQL(sql)
	if err != nil {
		return nil, err
	}
	var spans [][2]int
	for _, st := range stmts {
		for i := 0; i+1 < len(st.toks); i++ {
			a, b := st.toks[i].end, st.toks[i+1].start
			if a < b {
				spans = append(spans, [2]int{a, b})
			}
		}
		if n := len(st.toks); n > 0 && st.toks[n-1].end < st.semi {
			spans = append(spans, [2]int{st.toks[n-1].end, st.semi})
		}
	}
	return spans, nil
}

func readNumber(s string, i int) int {
	if s[i] == '.' {
		i++
		for i < len(s) && isDigit(s[i]) {
			i++
		}
	} else {
		for i < len(s) && isDigit(s[i]) {
			i++
		}
		if i < len(s) && s[i] == '.' && i+1 < len(s) && isDigit(s[i+1]) {
			i++
			for i < len(s) && isDigit(s[i]) {
				i++
			}
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if j < len(s) && isDigit(s[j]) {
			j++
			for j < len(s) && isDigit(s[j]) {
				j++
			}
			i = j
		}
	}
	return i
}

func readASCIIIdent(s string, i int) int {
	i++
	for i < len(s) && isTagCont(s[i]) {
		i++
	}
	return i
}

func isDigit(b byte) bool { return b >= '0' && b <= '9' }
func isASCIIIdentStart(b byte) bool {
	return b == '_' || (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}
func isTagCont(b byte) bool { return isASCIIIdentStart(b) || isDigit(b) }
func isOpByte(b byte) bool {
	switch b {
	case '+', '-', '*', '/', '%', '^', '<', '>', '=', '!', '~', '@', '#', '&', '|', '?', ':':
		return true
	default:
		return false
	}
}

func wordIs(t token, w string) bool {
	return t.kind == kindWord && asciiFoldEq(t.text, w)
}

// asciiFoldEq compares ASCII letters without Unicode case folding.
// A non-ASCII byte never matches.
func asciiFoldEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if ca >= 'A' && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if cb >= 'A' && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb || ca > 127 {
			return false
		}
	}
	return true
}

func hasWord(st statement, w string) bool {
	for _, t := range st.toks {
		if wordIs(t, w) {
			return true
		}
	}
	return false
}

type parser struct {
	toks []token
	i    int
}

func (p *parser) peek() (token, bool) {
	if p.i >= len(p.toks) {
		return token{}, false
	}
	return p.toks[p.i], true
}
func (p *parser) done() bool { return p.i >= len(p.toks) }
func (p *parser) peekWord(w string) bool {
	t, ok := p.peek()
	return ok && wordIs(t, w)
}
func (p *parser) eatWord(w string) bool {
	if !p.peekWord(w) {
		return false
	}
	p.i++
	return true
}
func (p *parser) peekOp(op string) bool {
	t, ok := p.peek()
	return ok && t.kind == kindOp && t.text == op
}
func (p *parser) eatOp(op string) bool {
	if !p.peekOp(op) {
		return false
	}
	p.i++
	return true
}
func (p *parser) eatIdent() bool {
	t, ok := p.peek()
	if !ok {
		return false
	}
	if t.kind == kindQIdent {
		if t.text == "" {
			return false
		}
		p.i++
		return true
	}
	if t.kind == kindWord {
		p.i++
		return true
	}
	return false
}
func (p *parser) eatQual(min, max int) bool {
	if !p.eatIdent() {
		return false
	}
	n := 1
	for n < max && p.peekOp(".") {
		p.i++
		if !p.eatIdent() {
			return false
		}
		n++
	}
	return n >= min
}

func allowStatement(st statement) (bool, string) {
	if len(st.toks) == 0 || st.toks[0].kind != kindWord {
		return notAllowed(st)
	}
	switch {
	case wordIs(st.toks[0], "CREATE"):
		if len(st.toks) >= 2 && wordIs(st.toks[1], "TABLE") {
			return allowCreateTable(st)
		}
		if isCreateIndex(st) {
			return allowCreateIndex(st)
		}
		return notAllowed(st)
	case wordIs(st.toks[0], "ALTER"):
		return allowAlterTable(st)
	case wordIs(st.toks[0], "COMMENT"):
		return allowComment(st)
	default:
		return notAllowed(st)
	}
}

func isCreateIndex(st statement) bool {
	if len(st.toks) < 2 || !wordIs(st.toks[0], "CREATE") {
		return false
	}
	if wordIs(st.toks[1], "INDEX") {
		return true
	}
	return len(st.toks) >= 3 && wordIs(st.toks[1], "UNIQUE") && wordIs(st.toks[2], "INDEX")
}

var (
	createForbiddenWords = []string{"AS", "INHERITS", "LIKE", "TABLESPACE", "REFERENCES"}
	createForbiddenPairs = [][2]string{{"PARTITION", "OF"}, {"FOREIGN", "KEY"}}
	alterForbiddenWords  = []string{"REFERENCES", "CHECK", "UNIQUE", "GENERATED", "CONSTRAINT"}
	alterForbiddenPairs  = [][2]string{{"PRIMARY", "KEY"}}
)

func forbiddenWords(st statement, words []string, pairs [][2]string) bool {
	for i, t := range st.toks {
		if t.kind != kindWord && t.kind != kindQIdent {
			continue
		}
		for _, w := range words {
			if asciiFoldEq(t.text, w) {
				return true
			}
		}
		if i+1 >= len(st.toks) {
			continue
		}
		n := st.toks[i+1]
		if n.kind != kindWord && n.kind != kindQIdent {
			continue
		}
		for _, pr := range pairs {
			if asciiFoldEq(t.text, pr[0]) && asciiFoldEq(n.text, pr[1]) {
				return true
			}
		}
	}
	return false
}

func allowCreateTable(st statement) (bool, string) {
	p := &parser{toks: st.toks}
	if !p.eatWord("CREATE") || !p.eatWord("TABLE") {
		return notAllowed(st)
	}
	if p.peekWord("IF") {
		if !p.eatWord("IF") || !p.eatWord("NOT") || !p.eatWord("EXISTS") {
			return notAllowed(st)
		}
	}
	if !p.eatQual(1, 2) || !p.eatOp("(") || !p.eatCreateItems() || !p.eatOp(")") || !p.done() {
		return notAllowed(st)
	}
	if forbiddenWords(st, createForbiddenWords, createForbiddenPairs) {
		return notAllowed(st)
	}
	return true, ""
}

func (p *parser) eatCreateItems() bool {
	if !p.eatCreateItem() {
		return false
	}
	for p.peekOp(",") {
		p.i++
		if !p.eatCreateItem() {
			return false
		}
	}
	return true
}

func (p *parser) eatCreateItem() bool {
	if p.peekWord("PRIMARY") || p.peekWord("UNIQUE") {
		return p.eatTableConstraint()
	}
	return p.eatColumnDef()
}

func (p *parser) eatTableConstraint() bool {
	switch {
	case p.peekWord("PRIMARY"):
		return p.eatWord("PRIMARY") && p.eatWord("KEY") && p.eatNameList()
	case p.peekWord("UNIQUE"):
		return p.eatWord("UNIQUE") && p.eatNameList()
	default:
		return false
	}
}

func (p *parser) eatNameList() bool {
	if !p.eatOp("(") || !p.eatIdent() {
		return false
	}
	for p.peekOp(",") {
		p.i++
		if !p.eatIdent() {
			return false
		}
	}
	return p.eatOp(")")
}

func (p *parser) eatColumnDef() bool {
	if !p.eatIdent() || !p.eatType() {
		return false
	}
	for {
		switch p.eatColConstraint() {
		case 1:
			continue
		case 0:
			return true
		default:
			return false
		}
	}
}

// eatColConstraint returns 1 if it consumed a constraint, 0 if the next
// token is not one, and -1 if a constraint started and is malformed.
func (p *parser) eatColConstraint() int {
	switch {
	case p.peekWord("NOT"):
		if !p.eatWord("NOT") || !p.eatWord("NULL") {
			return -1
		}
		return 1
	case p.peekWord("NULL"):
		p.i++
		return 1
	case p.peekWord("DEFAULT"):
		if !p.eatWord("DEFAULT") || !p.eatLiteral() {
			return -1
		}
		return 1
	case p.peekWord("PRIMARY"):
		if !p.eatWord("PRIMARY") || !p.eatWord("KEY") {
			return -1
		}
		return 1
	case p.peekWord("UNIQUE"):
		p.i++
		return 1
	default:
		return 0
	}
}

// Column types an ADD COLUMN or CREATE TABLE column may use.
//
// PostgreSQL's grammar rewrites the names in grammarPlainTypes, plus the
// multi-word and typmod forms eaten below, to a built-in type before any
// catalog lookup. A domain, the temp schema, or search_path order cannot
// shadow them, so an unqualified name is accepted only from that set:
//
//	smallint, int, integer, bigint, real
//	double precision
//	float, with optional (n)
//	numeric, decimal, dec, with optional (p) or (p,s)
//	boolean
//	character, char, with optional (n)
//	character varying, varchar, with optional (n)
//	timestamp, time, with optional (p) and then optional
//	WITH TIME ZONE or WITHOUT TIME ZONE
//	bit, bit varying, with optional (n)
//
// Every other built-in (text, uuid, date, json, jsonb, bytea, bool,
// timestamptz, timetz, int2, int4, int8, float4, float8, …) is a generic
// type name. PostgreSQL resolves an unqualified one through the search
// path, so a domain of the same name can hide the built-in and attach a
// default or a check this classifier never sees. Those names are accepted
// only as the unquoted words pg_catalog.<name>, which look up that schema
// and no other. Any other schema, a quoted qualifier, a bare name from
// catalogTypes, or a typmod on the pg_catalog form is refused. Precision
// belongs on the grammar spellings above (timestamp(p) with time zone,
// not pg_catalog.timestamptz(p)).
//
// One [] suffix is accepted on the same terms as the element type.
var grammarPlainTypes = []string{
	"smallint", "int", "integer", "bigint", "real", "boolean",
}

var catalogTypes = []string{
	"text", "uuid", "date", "json", "jsonb", "bytea", "bool",
	"timestamptz", "timetz", "int2", "int4", "int8", "float4", "float8",
}

func (p *parser) eatType() bool {
	if p.peekCatalogQual() {
		if !p.eatCatalogType() {
			return false
		}
		return p.eatOneArray()
	}
	if !p.eatGrammarType() {
		return false
	}
	return p.eatOneArray()
}

func (p *parser) peekCatalogQual() bool {
	if !p.peekWord("pg_catalog") || p.i+1 >= len(p.toks) {
		return false
	}
	dot := p.toks[p.i+1]
	return dot.kind == kindOp && dot.text == "."
}

func (p *parser) eatCatalogType() bool {
	if !p.eatWord("pg_catalog") || !p.eatOp(".") {
		return false
	}
	t, ok := p.peek()
	if !ok || t.kind != kindWord || !listedType(t.text, catalogTypes) {
		return false
	}
	p.i++
	if p.peekOp("(") || p.peekOp(".") {
		return false
	}
	return true
}

func (p *parser) eatGrammarType() bool {
	switch {
	case p.peekWord("double"):
		return p.eatWord("double") && p.eatWord("precision") && !p.peekOp("(")
	case p.peekWord("character"):
		if !p.eatWord("character") {
			return false
		}
		if p.peekWord("varying") {
			p.i++
		}
		if p.peekOp("(") && !p.eatParenUint() {
			return false
		}
		return true
	case p.peekWord("varchar"), p.peekWord("char"):
		p.i++
		if p.peekOp("(") && !p.eatParenUint() {
			return false
		}
		return true
	case p.peekWord("numeric"), p.peekWord("decimal"), p.peekWord("dec"):
		p.i++
		if p.peekOp("(") && !p.eatNumericMod() {
			return false
		}
		return true
	case p.peekWord("float"):
		p.i++
		if p.peekOp("(") && !p.eatParenUint() {
			return false
		}
		return true
	case p.peekWord("bit"):
		p.i++
		if p.peekWord("varying") {
			p.i++
		}
		if p.peekOp("(") && !p.eatParenUint() {
			return false
		}
		return true
	case p.peekWord("timestamp"), p.peekWord("time"):
		word := "timestamp"
		if p.peekWord("time") {
			word = "time"
		}
		if !p.eatWord(word) {
			return false
		}
		if p.peekOp("(") && !p.eatParenUint() {
			return false
		}
		if p.peekWord("with") || p.peekWord("without") {
			which := "with"
			if p.peekWord("without") {
				which = "without"
			}
			if !p.eatWord(which) || !p.eatWord("time") || !p.eatWord("zone") {
				return false
			}
		}
		return true
	default:
		if !listedTypeWord(p, grammarPlainTypes) {
			return false
		}
		p.i++
		return !p.peekOp("(")
	}
}

func listedTypeWord(p *parser, names []string) bool {
	t, ok := p.peek()
	return ok && t.kind == kindWord && listedType(t.text, names)
}

func listedType(got string, names []string) bool {
	for _, w := range names {
		if asciiFoldEq(got, w) {
			return true
		}
	}
	return false
}

func (p *parser) eatOneArray() bool {
	if !p.peekOp("[") {
		return true
	}
	if !p.eatOp("[") || !p.eatOp("]") || p.peekOp("[") {
		return false
	}
	return true
}

func (p *parser) eatParenUint() bool {
	return p.eatOp("(") && p.eatUint() && p.eatOp(")")
}

func (p *parser) eatNumericMod() bool {
	if !p.eatOp("(") || !p.eatUint() {
		return false
	}
	if p.eatOp(",") && !p.eatUint() {
		return false
	}
	return p.eatOp(")")
}

func (p *parser) eatUint() bool {
	t, ok := p.peek()
	if !ok || t.kind != kindNumber || t.text == "" {
		return false
	}
	for i := 0; i < len(t.text); i++ {
		if t.text[i] < '0' || t.text[i] > '9' {
			return false
		}
	}
	p.i++
	return true
}

func (p *parser) eatLiteral() bool {
	t, ok := p.peek()
	if !ok {
		return false
	}
	switch t.kind {
	case kindNumber:
		p.i++
		return true
	case kindString:
		if t.prefix == "" || asciiFoldEq(t.prefix, "E") {
			p.i++
			return true
		}
		return false
	case kindWord:
		if wordIs(t, "true") || wordIs(t, "false") || wordIs(t, "null") {
			p.i++
			return true
		}
		return false
	default:
		return false
	}
}

func allowAlterTable(st statement) (bool, string) {
	p := &parser{toks: st.toks}
	if !p.eatWord("ALTER") || !p.eatWord("TABLE") {
		return notAllowed(st)
	}
	if p.peekWord("IF") {
		if !p.eatWord("IF") || !p.eatWord("EXISTS") {
			return notAllowed(st)
		}
	}
	if p.peekWord("ONLY") {
		p.i++
	}
	if !p.eatQual(1, 2) || !p.eatWord("ADD") {
		return notAllowed(st)
	}
	if p.peekWord("COLUMN") {
		p.i++
	}
	if p.peekWord("IF") {
		if !p.eatWord("IF") || !p.eatWord("NOT") || !p.eatWord("EXISTS") {
			return notAllowed(st)
		}
	}
	if !p.eatIdent() || !p.eatType() {
		return notAllowed(st)
	}
	var sawNull, sawNotNull, sawDefault bool
	for !p.done() {
		switch {
		case p.peekWord("NOT"):
			if sawNotNull || !p.eatWord("NOT") || !p.eatWord("NULL") {
				return notAllowed(st)
			}
			sawNotNull = true
		case p.peekWord("NULL"):
			if sawNull || !p.eatWord("NULL") {
				return notAllowed(st)
			}
			sawNull = true
		case p.peekWord("DEFAULT"):
			if sawDefault || !p.eatWord("DEFAULT") || !p.eatLiteral() {
				return notAllowed(st)
			}
			sawDefault = true
		default:
			return notAllowed(st)
		}
	}
	if sawNull && sawNotNull {
		return notAllowed(st)
	}
	if forbiddenWords(st, alterForbiddenWords, alterForbiddenPairs) {
		return notAllowed(st)
	}
	if sawNotNull && !sawDefault {
		return false, fmt.Sprintf("adds a NOT NULL column without a DEFAULT in %q", show(st))
	}
	return true, ""
}

func allowCreateIndex(st statement) (bool, string) {
	p := &parser{toks: st.toks}
	if !p.eatWord("CREATE") {
		return notAllowed(st)
	}
	if p.peekWord("UNIQUE") {
		p.i++
	}
	if !p.eatWord("INDEX") {
		return notAllowed(st)
	}
	if !p.eatWord("CONCURRENTLY") {
		if !hasWord(st, "CONCURRENTLY") {
			return false, fmt.Sprintf("builds an index without CONCURRENTLY in %q", show(st))
		}
		return notAllowed(st)
	}
	if p.peekWord("IF") {
		if !p.eatWord("IF") || !p.eatWord("NOT") || !p.eatWord("EXISTS") {
			return notAllowed(st)
		}
	}
	if !p.eatQual(1, 2) || !p.eatWord("ON") {
		return notAllowed(st)
	}
	if p.peekWord("ONLY") {
		p.i++
	}
	if !p.eatQual(1, 2) || !p.eatOp("(") || !p.eatIndexElems() || !p.eatOp(")") || !p.done() {
		return notAllowed(st)
	}
	return true, ""
}

func (p *parser) eatIndexElems() bool {
	if !p.eatIndexElem() {
		return false
	}
	for p.peekOp(",") {
		p.i++
		if !p.eatIndexElem() {
			return false
		}
	}
	return true
}

func (p *parser) eatIndexElem() bool {
	if !p.eatQual(1, 2) {
		return false
	}
	if p.peekWord("ASC") || p.peekWord("DESC") {
		p.i++
	}
	if p.peekWord("NULLS") {
		p.i++
		if !p.eatWord("FIRST") && !p.eatWord("LAST") {
			return false
		}
	}
	return true
}

func allowComment(st statement) (bool, string) {
	p := &parser{toks: st.toks}
	if !p.eatWord("COMMENT") || !p.eatWord("ON") {
		return notAllowed(st)
	}
	min, max := 1, 2
	switch {
	case p.peekWord("TABLE"), p.peekWord("INDEX"):
		p.i++
	case p.peekWord("COLUMN"):
		p.i++
		min, max = 2, 3
	default:
		return notAllowed(st)
	}
	if !p.eatQual(min, max) || !p.eatWord("IS") {
		return notAllowed(st)
	}
	t, ok := p.peek()
	if !ok || t.kind != kindString || t.prefix != "" {
		return notAllowed(st)
	}
	p.i++
	if !p.done() {
		return notAllowed(st)
	}
	return true, ""
}

// dbStep is the front matter of one db-steps.d file, read the way
// bifrost-trade-infra scripts/release/release_tool.py read_step reads it.
type dbStep struct {
	id, when   string
	envs, done []string
}

var frontMatter = regexp.MustCompile(`(?s)^---\n(.*?)\n---`)

func parseDBStep(text string) (dbStep, bool) {
	m := frontMatter.FindStringSubmatch(strings.ReplaceAll(text, "\r\n", "\n"))
	if m == nil {
		return dbStep{}, false
	}
	meta := map[string]string{}
	for _, line := range strings.Split(m[1], "\n") {
		line, _, _ = strings.Cut(line, "#")
		if k, v, found := strings.Cut(line, ":"); found {
			meta[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	s := dbStep{id: meta["id"], when: meta["when"], envs: strings.Fields(meta["envs"]), done: strings.Fields(meta["done"])}
	if s.id == "" || len(s.envs) == 0 || (s.when != "before" && s.when != "after") {
		return dbStep{}, false
	}
	return s, true
}

func (s dbStep) pendingBefore(env string) bool {
	return s.when == "before" && contains(s.envs, env) && !contains(s.done, env)
}
