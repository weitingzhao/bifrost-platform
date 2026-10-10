package releasepolicy

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Additive DDL (ADR §5, 2026-10-08 revision) may ship on the signed policy
// when ClassifyDDL accepts the change: every statement that was already in
// the file remains, in the same order, and every added statement is one
// allow-listed shape (a new table, one added column, a concurrent index, or
// a comment). Dropping, renaming, changing a type or a constraint, rewriting
// rows, or changing grants and owners stays tier D.
//
// The check is an allow-list of statements, not a textual scan of lines.
// A lexer splits the file (line comments, nested block comments, quotes and
// dollar quotes); whitespace and comments do not by themselves make a change.
// Only a path ending in ".sql" is classified, and a migrations/ directory or
// a db_init* name is never classified. The policy's DDL paths also match
// *.py, db_init*, YAML jobs and migrations/**; those always wait for the Owner.
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
}

func (a token) equal(b token) bool {
	if a.kind != b.kind || a.prefix != b.prefix {
		return false
	}
	if a.kind == kindWord {
		ak, bk := isKeyword(a.text), isKeyword(b.text)
		if ak || bk {
			return ak && bk && strings.EqualFold(a.text, b.text)
		}
	}
	return a.text == b.text
}

type statement struct {
	raw  string
	toks []token
}

func stmtEqual(a, b statement) bool {
	if len(a.toks) != len(b.toks) {
		return false
	}
	for i := range a.toks {
		if !a.toks[i].equal(b.toks[i]) {
			return false
		}
	}
	return true
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
	errUntermString  = errors.New("unterminated string")
	errUntermIdent   = errors.New("unterminated quoted identifier")
	errUntermComment = errors.New("unterminated block comment")
	errUntermDollar  = errors.New("unterminated dollar quote")
	errBadDollar     = errors.New("invalid dollar quote")
	errUntermStmt    = errors.New("unterminated statement")
)

func splitSQL(text string) ([]statement, error) {
	s := text
	var stmts []statement
	var toks []token
	start := -1
	lineBare := true
	emit := func(kind tokKind, text, prefix string, tokStart int) {
		if start < 0 {
			start = tokStart
		}
		toks = append(toks, token{kind: kind, text: text, prefix: prefix})
		lineBare = false
	}
	finish := func(semi int) {
		if start >= 0 {
			stmts = append(stmts, statement{raw: strings.TrimSpace(s[start:semi]), toks: toks})
		}
		toks = nil
		start = -1
	}
	for i := 0; i < len(s); {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\f' || c == '\v' {
			i++
			continue
		}
		if c == '\n' || c == '\r' {
			if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i += 2
			} else {
				i++
			}
			lineBare = true
			continue
		}
		if strings.HasPrefix(s[i:], "--") {
			i += 2
			for i < len(s) && s[i] != '\n' && s[i] != '\r' {
				i++
			}
			continue
		}
		if strings.HasPrefix(s[i:], "/*") {
			j, err := scanBlock(s, i+2)
			if err != nil {
				return nil, err
			}
			i = j
			continue
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
			body, j, err := scanQuoted(s, i, '\'', errUntermString)
			if err != nil {
				return nil, err
			}
			emit(kindString, body, "", i)
			i = j
		case '"':
			body, j, err := scanQuoted(s, i, '"', errUntermIdent)
			if err != nil {
				return nil, err
			}
			emit(kindQIdent, body, "", i)
			i = j
		case '$':
			prefix, body, j, err := scanDollar(s, i)
			if err != nil {
				return nil, err
			}
			emit(kindString, body, prefix, i)
			i = j
		case '(', ')', '[', ']', ',', '.', ';':
			if c == '.' && i+1 < len(s) && isDigit(s[i+1]) {
				j := readNumber(s, i)
				emit(kindNumber, s[i:j], "", i)
				i = j
				continue
			}
			if c == ';' {
				finish(i + 1)
				i++
				continue
			}
			emit(kindOp, s[i:i+1], "", i)
			i++
		default:
			if isDigit(c) {
				j := readNumber(s, i)
				emit(kindNumber, s[i:j], "", i)
				i = j
				continue
			}
			if r, _ := utf8.DecodeRuneInString(s[i:]); identStart(r) {
				j := readIdent(s, i)
				if (s[i:j] == "E" || s[i:j] == "e") && j < len(s) && s[j] == '\'' {
					body, n, err := scanEString(s, j)
					if err != nil {
						return nil, err
					}
					emit(kindString, body, s[i:j], i)
					i = n
					continue
				}
				emit(kindWord, s[i:j], "", i)
				i = j
				continue
			}
			if isOpByte(c) {
				j := i + 1
				for j < len(s) && isOpByte(s[j]) {
					j++
				}
				emit(kindOp, s[i:j], "", i)
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

func scanBlock(s string, i int) (int, error) {
	depth := 1
	for i < len(s) {
		if i+1 < len(s) && s[i] == '/' && s[i+1] == '*' {
			depth++
			i += 2
			continue
		}
		if i+1 < len(s) && s[i] == '*' && s[i+1] == '/' {
			depth--
			i += 2
			if depth == 0 {
				return i, nil
			}
			continue
		}
		i++
	}
	return i, errUntermComment
}

func scanQuoted(s string, i int, quote byte, unterm error) (string, int, error) {
	var b strings.Builder
	i++
	for i < len(s) {
		if s[i] == quote {
			if i+1 < len(s) && s[i+1] == quote {
				b.WriteByte(quote)
				b.WriteByte(quote)
				i += 2
				continue
			}
			return b.String(), i + 1, nil
		}
		b.WriteByte(s[i])
		i++
	}
	return "", i, unterm
}

func scanEString(s string, i int) (string, int, error) {
	var b strings.Builder
	i++
	for i < len(s) {
		if s[i] == '\\' {
			if i+1 >= len(s) {
				return "", i, errUntermString
			}
			b.WriteByte('\\')
			b.WriteByte(s[i+1])
			i += 2
			continue
		}
		if s[i] == '\'' {
			if i+1 < len(s) && s[i+1] == '\'' {
				b.WriteString("''")
				i += 2
				continue
			}
			return b.String(), i + 1, nil
		}
		b.WriteByte(s[i])
		i++
	}
	return "", i, errUntermString
}

func scanDollar(s string, i int) (prefix, body string, next int, err error) {
	if i >= len(s) || s[i] != '$' {
		return "", "", i, errBadDollar
	}
	j := i + 1
	if j < len(s) && s[j] != '$' {
		if !isASCIIIdentStart(s[j]) {
			return "", "", i, errBadDollar
		}
		j++
		for j < len(s) && isTagCont(s[j]) {
			j++
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
	return prefix, s[j : j+k], j + k + len(prefix), nil
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

func readIdent(s string, i int) int {
	_, w := utf8.DecodeRuneInString(s[i:])
	i += w
	for i < len(s) {
		r, w := utf8.DecodeRuneInString(s[i:])
		if (r == utf8.RuneError && w == 1) || !identCont(r) {
			break
		}
		i += w
	}
	return i
}

func identStart(r rune) bool { return r == '_' || unicode.IsLetter(r) }
func identCont(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r)
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
	return t.kind == kindWord && strings.EqualFold(t.text, w)
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
	switch strings.ToUpper(st.toks[0].text) {
	case "CREATE":
		if len(st.toks) >= 2 && wordIs(st.toks[1], "TABLE") {
			return allowCreateTable(st)
		}
		if isCreateIndex(st) {
			return allowCreateIndex(st)
		}
		return notAllowed(st)
	case "ALTER":
		return allowAlterTable(st)
	case "COMMENT":
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
			if strings.EqualFold(t.text, w) {
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
			if strings.EqualFold(t.text, pr[0]) && strings.EqualFold(n.text, pr[1]) {
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

func (p *parser) eatType() bool {
	t, ok := p.peek()
	if !ok || !typeStart(t) {
		return false
	}
	if !p.eatQual(1, 2) {
		return false
	}
	if p.peekOp("(") && !p.eatTypmod() {
		return false
	}
	for p.peekOp("[") {
		p.i++
		if !p.eatOp("]") {
			return false
		}
	}
	return true
}

func typeStart(t token) bool {
	if t.kind == kindQIdent {
		return t.text != ""
	}
	if t.kind != kindWord {
		return false
	}
	switch strings.ToUpper(t.text) {
	case "NOT", "NULL", "DEFAULT", "PRIMARY", "UNIQUE", "CHECK", "CONSTRAINT",
		"REFERENCES", "FOREIGN", "GENERATED", "COLLATE", "LIKE", "TABLESPACE",
		"INHERITS", "AS", "PARTITION", "ARRAY":
		return false
	default:
		return true
	}
}

func (p *parser) eatTypmod() bool {
	if !p.eatOp("(") || !p.eatTypmodItem() {
		return false
	}
	for p.peekOp(",") {
		p.i++
		if !p.eatTypmodItem() {
			return false
		}
	}
	return p.eatOp(")")
}

func (p *parser) eatTypmodItem() bool {
	t, ok := p.peek()
	if !ok {
		return false
	}
	switch t.kind {
	case kindNumber, kindWord, kindQIdent:
		p.i++
		return true
	case kindString:
		if strings.HasPrefix(t.prefix, "$") {
			return false
		}
		p.i++
		return true
	default:
		return false
	}
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
		if t.prefix == "" || strings.EqualFold(t.prefix, "E") {
			p.i++
			return true
		}
		return false
	case kindWord:
		switch strings.ToUpper(t.text) {
		case "TRUE", "FALSE", "NULL":
			p.i++
			return true
		default:
			return false
		}
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

func isKeyword(s string) bool {
	_, ok := sqlKeywords[strings.ToLower(s)]
	return ok
}

var sqlKeywords = map[string]struct{}{}

func init() {
	for _, w := range strings.Fields(sqlKeywordList) {
		sqlKeywords[w] = struct{}{}
	}
}

const sqlKeywordList = "" +
	"abort absolute access action add admin after aggregate all also alter always analyse analyze and any array as asc " +
	"asymmetric at attach attribute authorization backward before begin between binary both by cache call called cascade " +
	"cascaded case cast catalog chain char character characteristics check checkpoint class close cluster coalesce collate " +
	"collation column comment comments commit committed concurrently configuration conflict connection constraint constraints " +
	"content continue conversion copy cost create cross csv current current_catalog current_date current_role current_schema " +
	"current_time current_timestamp current_user cursor cycle data database day deallocate dec decimal declare default defaults " +
	"deferrable deferred definer delete delimiter delimiters depends desc detach dictionary disable discard distinct do document " +
	"domain double drop each else enable encoding encrypted end enum escape event except exclude excluding exclusive execute " +
	"exists explain expression extension external extract false family fetch filter first float following for force foreign " +
	"forward freeze from full function functions generated global grant granted greatest group grouping handler having header " +
	"hold hour identity if ilike immediate immutable implicit import in including increment index indexes inherit inherits " +
	"initially inline inner inout input insensitive insert instead int integer intersect interval into invoker is isnull " +
	"isolation join key language large last lateral leading leakproof least left level like limit listen load local localtime " +
	"localtimestamp location lock locked logged mapping match materialized maxvalue method minvalue mode month move name names " +
	"national natural nchar new next no none not nothing notify notnull nowait null nulls numeric object of off offset oids old " +
	"on only operator option options or order ordinality others out outer over overlaps overlay overriding owned owner parallel " +
	"parser partial partition passing password placing plans policy position preceding precision prepare prepared preserve " +
	"primary prior privileges procedural procedure procedures program publication quote range read real reassign recursive ref " +
	"references referencing refresh reindex relative release rename repeatable replace replica reset restart restrict return " +
	"returning returns revoke right role rollback rollup routine routines row rows rule savepoint schema schemas scroll search " +
	"second security select sequence sequences serializable server session session_user set setof sets share show similar simple " +
	"skip snapshot some sql stable standalone start statement statistics stdin stdout storage stored strict strip subscription " +
	"substring support symmetric sysid system table tables tablesample tablespace temp template temporary text then ties time " +
	"timestamp to trailing transaction transform treat trigger trim true truncate trusted type types unbounded uncommitted " +
	"unencrypted union unique unknown unlisten unlogged until update user using vacuum valid validate validator value values " +
	"varchar variadic varying verbose version view views volatile when where whitespace window with within without work wrapper " +
	"write xml xmlattributes xmlconcat xmlelement xmlexists xmlforest xmlnamespaces xmlparse xmlpi xmlroot xmlserialize " +
	"xmltable year yes zone " +
	"bigint bigserial bool boolean bytea cidr float4 float8 inet jsonb macaddr money serial smallint smallserial timestamptz uuid json"

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
