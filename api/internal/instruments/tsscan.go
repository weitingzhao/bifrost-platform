package instruments

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// The platform MCP server registers its tools in TypeScript:
//
//	reg('name', 'text' + 'more', {...}, handler)
//	reg('name', `text ${NOTE}`, ...), reg('name', IMPORTED_DESCRIPTION, ...)
//	server.registerTool('name', {description: '...', ...}, handler)
//
// so the source is tokenized (a pattern over the text would take a comment or
// a string for a registration) and the description evaluated, following the
// constants it names, across the files of the directory.

type tokenKind int

const (
	tokIdent tokenKind = iota
	tokString
	tokTemplate
	tokNumber
	tokPunct
)

type token struct {
	kind  tokenKind
	text  string
	parts []templatePart // tokTemplate
}

// templatePart is literal text followed by an optional ${expression}.
type templatePart struct {
	text string
	expr []token
}

type lexer struct {
	src  string
	at   int
	toks []token
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
func isDigit(c byte) bool     { return c >= '0' && c <= '9' }
func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) }

// tokenize reads src up to its end, or up to an unmatched closing brace when
// inTemplate (the end of a ${...}).
func (l *lexer) tokenize(inTemplate bool) error {
	depth := 0
	for l.at < len(l.src) {
		c := l.src[l.at]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			l.at++
		case strings.HasPrefix(l.src[l.at:], "//"):
			if nl := strings.IndexByte(l.src[l.at:], '\n'); nl >= 0 {
				l.at += nl
			} else {
				l.at = len(l.src)
			}
		case strings.HasPrefix(l.src[l.at:], "/*"):
			end := strings.Index(l.src[l.at+2:], "*/")
			if end < 0 {
				return errors.New("unterminated comment")
			}
			l.at += end + 4
		case c == '/' && l.regexAllowed():
			if err := l.skipRegex(); err != nil {
				return err
			}
		case c == '\'' || c == '"':
			s, err := l.quoted(c)
			if err != nil {
				return err
			}
			l.toks = append(l.toks, token{kind: tokString, text: s})
		case c == '`':
			t, err := l.template()
			if err != nil {
				return err
			}
			l.toks = append(l.toks, t)
		case isIdentStart(c):
			start := l.at
			for l.at < len(l.src) && isIdentPart(l.src[l.at]) {
				l.at++
			}
			l.toks = append(l.toks, token{kind: tokIdent, text: l.src[start:l.at]})
		case isDigit(c):
			start := l.at
			for l.at < len(l.src) && (isIdentPart(l.src[l.at]) || l.src[l.at] == '.' && l.at+1 < len(l.src) && isDigit(l.src[l.at+1])) {
				l.at++
			}
			l.toks = append(l.toks, token{kind: tokNumber, text: l.src[start:l.at]})
		default:
			if inTemplate {
				if c == '{' {
					depth++
				} else if c == '}' {
					if depth == 0 {
						l.at++
						return nil
					}
					depth--
				}
			}
			_, size := utf8.DecodeRuneInString(l.src[l.at:])
			l.toks = append(l.toks, token{kind: tokPunct, text: l.src[l.at : l.at+size]})
			l.at += size
		}
	}
	if inTemplate {
		return errors.New("unterminated template expression")
	}
	return nil
}

// regexAllowed says a "/" here starts a regular expression, not a division:
// what comes before it is not a value.
func (l *lexer) regexAllowed() bool {
	if len(l.toks) == 0 {
		return true
	}
	last := l.toks[len(l.toks)-1]
	switch last.kind {
	case tokString, tokTemplate, tokNumber:
		return false
	case tokIdent:
		return last.text == "return" || last.text == "typeof" || last.text == "case"
	}
	return last.text != ")" && last.text != "]" && last.text != "}"
}

func (l *lexer) skipRegex() error {
	inClass := false
	for i := l.at + 1; i < len(l.src); i++ {
		switch c := l.src[i]; {
		case c == '\n':
			return errors.New("unterminated regular expression")
		case c == '\\':
			i++
		case c == '[':
			inClass = true
		case c == ']':
			inClass = false
		case c == '/' && !inClass:
			l.at = i + 1
			return nil
		}
	}
	return errors.New("unterminated regular expression")
}

// escape decodes the escape at l.at (the character after the backslash).
func (l *lexer) escape(out *strings.Builder) error {
	if l.at >= len(l.src) {
		return errors.New("unterminated escape")
	}
	c := l.src[l.at]
	l.at++
	hex := func(n int) error {
		if l.at+n > len(l.src) {
			return errors.New("unterminated escape")
		}
		v, err := strconv.ParseUint(l.src[l.at:l.at+n], 16, 32)
		if err != nil {
			return errors.New("invalid escape")
		}
		l.at += n
		out.WriteRune(rune(v))
		return nil
	}
	switch c {
	case 'n':
		out.WriteByte('\n')
	case 't':
		out.WriteByte('\t')
	case 'r':
		out.WriteByte('\r')
	case '\n': // a line continuation
	case 'x':
		return hex(2)
	case 'u':
		if l.at < len(l.src) && l.src[l.at] == '{' {
			end := strings.IndexByte(l.src[l.at:], '}')
			if end < 0 {
				return errors.New("unterminated escape")
			}
			l.at++
			if err := hex(end - 1); err != nil {
				return err
			}
			l.at++
			return nil
		}
		return hex(4)
	default:
		out.WriteByte(c)
	}
	return nil
}

func (l *lexer) quoted(quote byte) (string, error) {
	var out strings.Builder
	for l.at++; l.at < len(l.src); {
		c := l.src[l.at]
		l.at++
		switch c {
		case quote:
			return out.String(), nil
		case '\n':
			return "", errors.New("unterminated string")
		case '\\':
			if err := l.escape(&out); err != nil {
				return "", err
			}
		default:
			out.WriteByte(c)
		}
	}
	return "", errors.New("unterminated string")
}

func (l *lexer) template() (token, error) {
	t := token{kind: tokTemplate}
	var text strings.Builder
	for l.at++; l.at < len(l.src); {
		c := l.src[l.at]
		switch {
		case c == '`':
			l.at++
			t.parts = append(t.parts, templatePart{text: text.String()})
			return t, nil
		case c == '\\':
			l.at++
			if err := l.escape(&text); err != nil {
				return t, err
			}
		case strings.HasPrefix(l.src[l.at:], "${"):
			inner := lexer{src: l.src, at: l.at + 2}
			if err := inner.tokenize(true); err != nil {
				return t, err
			}
			l.at = inner.at
			t.parts = append(t.parts, templatePart{text: text.String(), expr: inner.toks})
			text.Reset()
		default:
			text.WriteByte(c)
			l.at++
		}
	}
	return t, errors.New("unterminated template")
}

// constRef is a `const NAME = ...`: where its value starts.
type constRef struct {
	file     string
	at       int
	exported bool
}

type toolScan struct {
	tokens map[string][]token
	consts map[string][]constRef
}

func (s *toolScan) collectConsts(file string) {
	toks := s.tokens[file]
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].kind != tokIdent || toks[i].text != "const" || toks[i+1].kind != tokIdent {
			continue
		}
		at := i + 2
		if isPunct(toks, at, ":") && at+2 < len(toks) && toks[at+1].kind == tokIdent {
			at += 2
		}
		if !isPunct(toks, at, "=") || isPunct(toks, at+1, "=") || isPunct(toks, at+1, ">") {
			continue
		}
		exported := i > 0 && toks[i-1].kind == tokIdent && toks[i-1].text == "export"
		s.consts[toks[i+1].text] = append(s.consts[toks[i+1].text], constRef{file: file, at: at + 1, exported: exported})
	}
}

func isPunct(toks []token, i int, text string) bool {
	return i >= 0 && i < len(toks) && toks[i].kind == tokPunct && toks[i].text == text
}

func isIdent(toks []token, i int, text string) bool {
	return i >= 0 && i < len(toks) && toks[i].kind == tokIdent && toks[i].text == text
}

// constant is the value of NAME as seen from file: its own const, else the one
// exported by another file of the directory.
func (s *toolScan) constant(file, name string, active map[string]bool) (string, error) {
	var local, exported []constRef
	for _, c := range s.consts[name] {
		if c.file == file {
			local = append(local, c)
		} else if c.exported {
			exported = append(exported, c)
		}
	}
	refs := local
	if len(refs) == 0 {
		refs = exported
	}
	if len(refs) != 1 {
		return "", fmt.Errorf("%s has %d definitions in reach", name, len(refs))
	}
	key := refs[0].file + "\x00" + name
	if active[key] {
		return "", fmt.Errorf("%s refers to itself", name)
	}
	active[key] = true
	defer delete(active, key)
	value, _, err := s.expression(refs[0].file, s.tokens[refs[0].file], refs[0].at, active)
	return value, err
}

// expression evaluates term (+ term)* at i; anything more than that is not a
// text this scanner can vouch for.
func (s *toolScan) expression(file string, toks []token, i int, active map[string]bool) (string, int, error) {
	var out strings.Builder
	for {
		if i >= len(toks) {
			return "", i, errors.New("the expression ends early")
		}
		t := toks[i]
		switch t.kind {
		case tokString, tokNumber:
			out.WriteString(t.text)
		case tokTemplate:
			for _, part := range t.parts {
				out.WriteString(part.text)
				if part.expr == nil {
					continue
				}
				value, end, err := s.expression(file, part.expr, 0, active)
				if err != nil {
					return "", i, err
				}
				if end != len(part.expr) {
					return "", i, errors.New("a template expression is not a plain constant")
				}
				out.WriteString(value)
			}
		case tokIdent:
			value, err := s.constant(file, t.text, active)
			if err != nil {
				return "", i, err
			}
			out.WriteString(value)
		default:
			return "", i, fmt.Errorf("unexpected %q in the expression", t.text)
		}
		i++
		if isPunct(toks, i, "+") {
			i++
			continue
		}
		if i < len(toks) && toks[i].kind == tokPunct && !strings.Contains(",)};]", toks[i].text) {
			return "", i, fmt.Errorf("unexpected %q after the expression", toks[i].text)
		}
		return out.String(), i, nil
	}
}

// firstSentence ends at the first ". " (or "。") outside brackets, skipping the
// abbreviations the descriptions use.
func firstSentence(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	depth := 0
	runes := []rune(text)
	for i, c := range runes {
		switch c {
		case '(', '[', '{', '（':
			depth++
		case ')', ']', '}', '）':
			if depth > 0 {
				depth--
			}
		case '。':
			if depth == 0 {
				return string(runes[:i+1])
			}
		case '.':
			if depth > 0 || i+1 >= len(runes) || runes[i+1] != ' ' {
				continue
			}
			head := strings.ToLower(string(runes[:i+1]))
			if strings.HasSuffix(head, "e.g.") || strings.HasSuffix(head, "i.e.") || strings.HasSuffix(head, " vs.") || strings.HasSuffix(head, " etc.") {
				continue
			}
			return string(runes[:i+1])
		}
	}
	return text
}

type toolProblem struct{ at, problem string }

// scanTools finds the registrations in sources (file path to text): tools
// sorted by name. A registration whose description cannot be evaluated is
// listed without one, and reported.
func scanTools(sources map[string]string) ([]MCPTool, []toolProblem) {
	s := &toolScan{tokens: map[string][]token{}, consts: map[string][]constRef{}}
	var problems []toolProblem
	files := make([]string, 0, len(sources))
	for f := range sources {
		files = append(files, f)
	}
	sort.Strings(files)
	for _, f := range files {
		l := lexer{src: sources[f]}
		if err := l.tokenize(false); err != nil {
			problems = append(problems, toolProblem{f, "the source could not be tokenized: " + err.Error()})
			continue
		}
		s.tokens[f] = l.toks
		s.collectConsts(f)
	}
	var tools []MCPTool
	seen := map[string]bool{}
	add := func(file, name, description string, err error) {
		at := file + "#" + name
		if seen[name] {
			problems = append(problems, toolProblem{at, "the tool is registered more than once; the first is shown"})
			return
		}
		seen[name] = true
		if err != nil {
			problems = append(problems, toolProblem{at, "the description could not be read: " + err.Error()})
		} else if strings.TrimSpace(description) == "" {
			problems = append(problems, toolProblem{at, "the tool has no description"})
		}
		tools = append(tools, MCPTool{Name: name, Description: firstSentence(description), Source: file})
	}
	for _, f := range files {
		toks := s.tokens[f]
		for i := range toks {
			var open int
			structured := false
			switch {
			case isIdent(toks, i, "reg") && !isPunct(toks, i-1, ".") && !isIdent(toks, i-1, "function") && isPunct(toks, i+1, "("):
				open = i + 1
			case isIdent(toks, i, "server") && isPunct(toks, i+1, ".") && isIdent(toks, i+2, "registerTool") && isPunct(toks, i+3, "("):
				open, structured = i+3, true
			default:
				continue
			}
			if open+2 >= len(toks) || toks[open+1].kind != tokString || !isPunct(toks, open+2, ",") {
				problems = append(problems, toolProblem{f, "a registration whose first argument is not a literal tool name"})
				continue
			}
			name := toks[open+1].text
			start := open + 3
			if structured {
				var err error
				start, err = descriptionField(toks, start)
				if err != nil {
					add(f, name, "", err)
					continue
				}
			}
			description, _, err := s.expression(f, toks, start, map[string]bool{})
			add(f, name, description, err)
		}
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools, problems
}

// descriptionField finds description: in the object literal at i.
func descriptionField(toks []token, i int) (int, error) {
	if !isPunct(toks, i, "{") {
		return 0, errors.New("the tool's configuration is not an object literal")
	}
	depth := 0
	for ; i < len(toks); i++ {
		if toks[i].kind != tokPunct {
			if depth == 1 && isIdent(toks, i, "description") && isPunct(toks, i+1, ":") {
				return i + 2, nil
			}
			continue
		}
		switch toks[i].text {
		case "{", "(", "[":
			depth++
		case "}", ")", "]":
			depth--
			if depth == 0 {
				return 0, errors.New("the tool's configuration has no description")
			}
		}
	}
	return 0, errors.New("the tool's configuration is not closed")
}
