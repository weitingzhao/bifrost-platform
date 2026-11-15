package releasepolicy

import (
	"fmt"
	"strings"
)

// A migration that cannot destroy rows (Owner 2026-11-15): every top-level
// statement of the file is one of the kinds below. It is an allow-list by
// statement kind, where the additive check in ddl.go is a deny-list on lines
// and passes what it does not know: here anything not recognised is out of
// the class, and so is a text that cannot be split into statements with
// confidence. A function body is opaque: it is defined, not run, by the
// statement that carries it. The only statements that may take a table away
// are the two helper calls, which look at the rows first and raise.
//
// The class says nothing about whether the file is new; the engine checks
// that where it has both commits (reversibleDDL).

// The helpers a migration may call (migration 0022). They are the class's
// only way to drop a table, so a migration may define them once and never
// replace or drop them.
const (
	helperSchema       = "AGENTRT"
	helperDropIfEmpty  = "DROP_TABLE_IF_EMPTY"
	helperAssertNoRows = "ASSERT_NO_ROWS"
)

type sqlTokenKind int

const (
	tokWord   sqlTokenKind = iota // a bare word, upper-cased: a keyword or an unquoted identifier
	tokQIdent                     // a double-quoted identifier: never a keyword
	tokString                     // a single-quoted string, quotes included
	tokDollar                     // a dollar-quoted body, opaque
	tokPunct                      // one character of anything else
)

type sqlToken struct {
	kind sqlTokenKind
	text string
}

func (t sqlToken) is(word string) bool { return t.kind == tokWord && t.text == word }
func (t sqlToken) punct(c string) bool { return t.kind == tokPunct && t.text == c }
func (t sqlToken) name() bool          { return t.kind == tokWord || t.kind == tokQIdent }

func wordStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func wordPart(c byte) bool { return wordStart(c) || c == '$' || (c >= '0' && c <= '9') }

// splitSQL cuts a migration into its top-level statements, as tokens with
// the comments gone. It reads quotes the way PostgreSQL does by default
// (standard_conforming_strings on): ” and "" are the only escapes. It gives
// up, with why, on what it cannot read that way: an unterminated quote,
// comment or dollar body, or an E” string, whose backslashes quote.
func splitSQL(text string) (statements [][]sqlToken, why string) {
	var cur []sqlToken
	flush := func() {
		if len(cur) > 0 {
			statements = append(statements, cur)
		}
		cur = nil
	}
	n := len(text)
	lastWordEnd := -1 // where the last word token ended, to see a prefix glued to a quote
	for i := 0; i < n; {
		c := text[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < n && text[i+1] == '-':
			for i < n && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < n && text[i+1] == '*':
			depth, j := 1, i+2
			for j < n && depth > 0 {
				switch {
				case text[j] == '/' && j+1 < n && text[j+1] == '*':
					depth, j = depth+1, j+2
				case text[j] == '*' && j+1 < n && text[j+1] == '/':
					depth, j = depth-1, j+2
				default:
					j++
				}
			}
			if depth > 0 {
				return nil, "a /* comment is not closed"
			}
			i = j
		case c == '\'':
			if lastWordEnd == i && len(cur) > 0 && cur[len(cur)-1].is("E") {
				return nil, "an E'' string, whose backslashes quote, is not read"
			}
			j := i + 1
			for {
				if j >= n {
					return nil, "a single-quoted string is not closed"
				}
				if text[j] == '\'' {
					if j+1 < n && text[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			cur = append(cur, sqlToken{tokString, text[i : j+1]})
			i = j + 1
		case c == '"':
			j := i + 1
			for {
				if j >= n {
					return nil, "a double-quoted identifier is not closed"
				}
				if text[j] == '"' {
					if j+1 < n && text[j+1] == '"' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			cur = append(cur, sqlToken{tokQIdent, text[i : j+1]})
			i = j + 1
		case c == '$':
			// $$ or $tag$ opens a body that runs to the same marker; $1 is
			// a parameter and not a quote.
			j := i + 1
			for j < n && wordPart(text[j]) && text[j] != '$' {
				j++
			}
			if j < n && text[j] == '$' && (j == i+1 || wordStart(text[i+1])) {
				tag := text[i : j+1]
				end := strings.Index(text[j+1:], tag)
				if end < 0 {
					return nil, "a dollar-quoted body (" + tag + ") is not closed"
				}
				cur = append(cur, sqlToken{tokDollar, tag})
				i = j + 1 + end + len(tag)
				continue
			}
			cur = append(cur, sqlToken{tokPunct, "$"})
			i++
		case wordStart(c):
			j := i + 1
			for j < n && wordPart(text[j]) {
				j++
			}
			cur = append(cur, sqlToken{tokWord, strings.ToUpper(text[i:j])})
			i, lastWordEnd = j, j
		case c == ';':
			flush()
			i++
		default:
			cur = append(cur, sqlToken{tokPunct, string(c)})
			i++
		}
	}
	flush()
	return statements, ""
}

// ClassifyMigration reports whether a migration file is in the class "cannot
// destroy rows": every top-level statement is one of
//
//	CREATE TABLE (not AS), CREATE SCHEMA, CREATE [UNIQUE] INDEX, CREATE TYPE,
//	CREATE [OR REPLACE] VIEW, CREATE [OR REPLACE] FUNCTION, CREATE TRIGGER;
//	DROP FUNCTION | TRIGGER | VIEW | INDEX;
//	ALTER TABLE with only ADD COLUMN (nullable or with a DEFAULT),
//	ADD CONSTRAINT and DROP CONSTRAINT actions;
//	COMMENT ON;
//	SELECT agentrt.drop_table_if_empty('<table>') and
//	SELECT agentrt.assert_no_rows('<table>', '<column>', '<pattern>');
//
// none with CASCADE. Otherwise why names the first statement that is not,
// by its number in the file and its kind. An empty file is not in the class.
func ClassifyMigration(sql string) (ok bool, why string) {
	statements, bad := splitSQL(sql)
	if bad != "" {
		return false, "cannot be split into statements: " + bad
	}
	if len(statements) == 0 {
		return false, "holds no statement"
	}
	for i, st := range statements {
		if bad := statementOutOfClass(st); bad != "" {
			return false, fmt.Sprintf("statement %d: %s", i+1, bad)
		}
	}
	return true, ""
}

// statementOutOfClass is "" for a statement of the class, else why not.
func statementOutOfClass(st []sqlToken) string {
	for _, t := range st {
		if t.is("CASCADE") {
			return statementKind(st) + " with CASCADE"
		}
	}
	lead := func(words ...string) bool {
		if len(st) < len(words) {
			return false
		}
		for i, w := range words {
			if !st[i].is(w) {
				return false
			}
		}
		return true
	}
	switch {
	case lead("CREATE", "TABLE"):
		if depthZeroWord(st, "AS") {
			return "CREATE TABLE AS"
		}
		return ""
	case lead("CREATE", "SCHEMA"):
		// The name alone: a schema statement can carry other statements
		// (CREATE SCHEMA s GRANT ...) and an owner.
		rest := st[2:]
		if len(rest) == 4 && rest[0].is("IF") && rest[1].is("NOT") && rest[2].is("EXISTS") {
			rest = rest[3:]
		}
		if len(rest) != 1 || !rest[0].name() {
			return "CREATE SCHEMA with more than a name"
		}
		return ""
	case lead("CREATE", "INDEX"), lead("CREATE", "UNIQUE", "INDEX"),
		lead("CREATE", "TYPE"), lead("CREATE", "VIEW"), lead("CREATE", "OR", "REPLACE", "VIEW"),
		lead("CREATE", "TRIGGER"), lead("COMMENT", "ON"),
		lead("DROP", "TRIGGER"), lead("DROP", "VIEW"), lead("DROP", "INDEX"):
		return ""
	case lead("CREATE", "FUNCTION"):
		return ""
	case lead("CREATE", "OR", "REPLACE", "FUNCTION"), lead("DROP", "FUNCTION"):
		if namesHelper(st) {
			return statementKind(st) + " of a helper the class relies on (" + strings.ToLower(helperSchema) + "." +
				strings.ToLower(helperDropIfEmpty) + ", " + strings.ToLower(helperAssertNoRows) + ")"
		}
		return ""
	case lead("ALTER", "TABLE"):
		return alterTableOutOfClass(st)
	case lead("SELECT"):
		if helperCall(st) {
			return ""
		}
		return "SELECT of anything but " + strings.ToLower(helperSchema) + "." + strings.ToLower(helperDropIfEmpty) +
			"('<table>') or " + strings.ToLower(helperSchema) + "." + strings.ToLower(helperAssertNoRows) + "('<table>', '<column>', '<pattern>')"
	}
	return statementKind(st) + " is not a statement of the class"
}

// statementKind names a statement by its leading keywords: one word, or for
// CREATE, DROP and ALTER the words up to the kind of object.
func statementKind(st []sqlToken) string {
	var words []string
	for _, t := range st {
		if t.kind != tokWord || len(words) == 4 {
			break
		}
		words = append(words, t.text)
		switch len(words) {
		case 1:
			if t.text != "CREATE" && t.text != "DROP" && t.text != "ALTER" {
				return t.text
			}
		case 2:
			if t.text != "OR" && t.text != "UNIQUE" {
				return strings.Join(words, " ")
			}
		case 3:
			if t.text != "REPLACE" {
				return strings.Join(words, " ")
			}
		}
	}
	if len(words) == 0 {
		return "a statement that starts with no keyword"
	}
	return strings.Join(words, " ")
}

// depthZeroWord reports whether the bare word occurs outside every parenthesis.
func depthZeroWord(st []sqlToken, word string) bool {
	depth := 0
	for _, t := range st {
		switch {
		case t.punct("("):
			depth++
		case t.punct(")"):
			depth--
		case depth == 0 && t.is(word):
			return true
		}
	}
	return false
}

// namesHelper reports whether the statement names one of the helper
// functions, in any schema: the unqualified name is what a caller could be
// made to resolve to.
func namesHelper(st []sqlToken) bool {
	for _, t := range st {
		if t.is(helperDropIfEmpty) || t.is(helperAssertNoRows) {
			return true
		}
		if t.kind == tokQIdent {
			if q := strings.ToUpper(strings.Trim(t.text, `"`)); q == helperDropIfEmpty || q == helperAssertNoRows {
				return true
			}
		}
	}
	return false
}

// helperCall is exactly SELECT agentrt.<helper>(<string literals>), with as
// many literals as the helper takes and nothing else.
func helperCall(st []sqlToken) bool {
	if len(st) < 7 || !st[1].is(helperSchema) || !st[2].punct(".") || !st[4].punct("(") || !st[len(st)-1].punct(")") {
		return false
	}
	want := map[string]int{helperDropIfEmpty: 1, helperAssertNoRows: 3}[st[3].text]
	if st[3].kind != tokWord || want == 0 {
		return false
	}
	args := st[5 : len(st)-1]
	if len(args) != 2*want-1 {
		return false
	}
	for i, t := range args {
		if i%2 == 0 && t.kind != tokString || i%2 == 1 && !t.punct(",") {
			return false
		}
	}
	return true
}

// alterTableOutOfClass checks each comma-separated action of one ALTER TABLE.
func alterTableOutOfClass(st []sqlToken) string {
	rest := st[2:]
	if len(rest) >= 2 && rest[0].is("IF") && rest[1].is("EXISTS") {
		rest = rest[2:]
	}
	if len(rest) >= 1 && rest[0].is("ONLY") {
		rest = rest[1:]
	}
	// The table's name: name [. name], then an optional *.
	if len(rest) == 0 || !rest[0].name() {
		return "ALTER TABLE that names no table"
	}
	rest = rest[1:]
	for len(rest) >= 2 && rest[0].punct(".") && rest[1].name() {
		rest = rest[2:]
	}
	if len(rest) >= 1 && rest[0].punct("*") {
		rest = rest[1:]
	}
	var actions [][]sqlToken
	depth, start := 0, 0
	for i, t := range rest {
		switch {
		case t.punct("("):
			depth++
		case t.punct(")"):
			depth--
		case depth == 0 && t.punct(","):
			actions = append(actions, rest[start:i])
			start = i + 1
		}
	}
	actions = append(actions, rest[start:])
	for _, a := range actions {
		switch {
		case len(a) >= 2 && a[0].is("ADD") && a[1].is("COLUMN"):
			if depthZeroPair(a, "NOT", "NULL") && !depthZeroWord(a, "DEFAULT") {
				return "ALTER TABLE ADD COLUMN that is NOT NULL without a DEFAULT"
			}
		case len(a) >= 2 && a[0].is("ADD") && a[1].is("CONSTRAINT"):
		case len(a) >= 2 && a[0].is("DROP") && a[1].is("CONSTRAINT"):
		default:
			kind := make([]string, 0, 2)
			for _, t := range a {
				if t.kind != tokWord || len(kind) == 2 {
					break
				}
				kind = append(kind, t.text)
			}
			if len(kind) == 0 {
				return "ALTER TABLE with an empty action"
			}
			return "ALTER TABLE " + strings.Join(kind, " ")
		}
	}
	return ""
}

// depthZeroPair reports whether the two bare words occur one after the
// other outside every parenthesis.
func depthZeroPair(st []sqlToken, first, second string) bool {
	depth := 0
	for i, t := range st {
		switch {
		case t.punct("("):
			depth++
		case t.punct(")"):
			depth--
		case depth == 0 && t.is(first) && i+1 < len(st) && st[i+1].is(second):
			return true
		}
	}
	return false
}
