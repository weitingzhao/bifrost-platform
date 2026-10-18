package agentd

import (
	"regexp"
	"strings"
)

// What must not leave the host in a target (reviews 265 A8, 266 A1, 267 A1):
// a local path outside the checkout, and anything that looks like a
// credential. A target is read as a shell command line: a value is a whole
// shell word, with its quotes, escapes, $'…' and concatenations; one whose
// quote never closes is masked to the end, as is a private key with no END
// line; a value inside quotes ends where those quotes do. Only in a search
// command (rg, grep, …) is a value that starts as a pattern kept, so that
// rg 'token:.*' still says what was searched (reviews 266 C8, 267 C5).

// secretKey is a name that holds a credential: it ends in one of these words
// (GITHUB_TOKEN, PGPASSWORD, "api_key"); a name that only contains one
// (author, TOKEN_FILE) is not one (review 266 C8).
const secretKey = `[a-z0-9_]*(?:token|secret|passw(?:or)?d|pwd|api_?key|private_?key|access_?key|secret_?key|credentials?|auth|authorization)s?`

// shellWord is one shell word; an unquoted ;, &, |, <, >, parenthesis or
// blank ends it.
const shellWord = `(?s:(?:"(?:[^"\\]|\\.)*(?:"|$)|\$'(?:[^'\\]|\\.)*(?:'|$)|'[^']*(?:'|$)|\\.|[^\s"'\;&|<>()])+)`

// maxScrub is the most of a target that is read; a longer one is cut at its
// last blank before that, so a credential is never cut in two (the part of it
// that is left would not be recognized).
const maxScrub = 64 << 10

type plainMask struct {
	re   *regexp.Regexp
	with string
}

// valueMask masks the value (group 2) after a name (group 1).
type valueMask struct {
	re      *regexp.Regexp
	with    func(name string) string
	pattern bool // in a search command, a value that starts as a pattern is kept
	quoted  bool // a "name: value" inside quotes runs to where they close
}

var (
	privateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)
	bearer     = regexp.MustCompile(`(?i)\b(bearer|token|basic)\s+[a-z0-9._~+/=-]{8,}`)

	valueMasks = []valueMask{
		// a JSON key, and one inside a double-quoted shell argument
		{re: regexp.MustCompile(`(?i)("` + secretKey + `"\s*:\s*)("(?:[^"\\]|\\.)*(?:"|$)|[^\s,}\]"]+)`),
			with: func(n string) string { return n + `"<secret>"` }, pattern: true},
		{re: regexp.MustCompile(`(?i)(\\"` + secretKey + `\\"\s*:\s*)((?s:\\".*?(?:\\"|$))|[^\s,}\]\\]+)`),
			with: func(n string) string { return n + `\"<secret>\"` }, pattern: true},
		// an Authorization header keeps its form
		{re: regexp.MustCompile(`(?i)(authorization:\s*)((?:[a-z]+\s+)?` + shellWord + `)`),
			with: func(n string) string { return n + "<secret>" }, pattern: true, quoted: true},
		// NAME=value, name: value
		{re: regexp.MustCompile(`(?i)\b(` + secretKey + `\s*[=:]\s*)(` + shellWord + `)`),
			with: func(n string) string { return strings.TrimRight(n, " \t\r\n=:") + "=<secret>" }, pattern: true, quoted: true},
		// --password value, --token=value
		{re: regexp.MustCompile(`(?i)(--?(?:password|passwd|token|secret|api-key|key)(?:=|\s+))(` + shellWord + `)`),
			with: func(n string) string { return n + "<secret>" }},
	}

	plainMasks = []plainMask{
		{regexp.MustCompile(`\b(github_pat_|ghp_|gho_|ghs_|ghr_|glpat-|xox[abprs]-|sk-|AKIA)[A-Za-z0-9_\-]{8,}`), "${1}<secret>"},
		{regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`), "${1}<secret>@"},
		{regexp.MustCompile(`(?:/home|/Users|/root|/var/folders|/private/var|/private/tmp|/tmp)/[^\s"'<>|;&)]*`), "<local path>"},
		{regexp.MustCompile(`\b[A-Za-z]:\\[^\s"'<>|;&)]*`), "<local path>"},
	}
)

func scrub(s string) string {
	s = privateKey.ReplaceAllString(s, "<secret>")
	s = bearer.ReplaceAllString(s, "${1} <secret>")
	for _, m := range valueMasks {
		s = m.apply(s)
	}
	for _, m := range plainMasks {
		s = m.re.ReplaceAllString(s, m.with)
	}
	return s
}

// apply masks each value m finds in s, left to right; after a value it looks
// again from where that value ends, which is where its quotes close when the
// name is inside quotes.
func (m valueMask) apply(s string) string {
	line := scanShell(s)
	var b strings.Builder
	done := 0
	for p := 0; p < len(s); {
		loc := m.re.FindStringSubmatchIndex(s[p:])
		if loc == nil {
			break
		}
		at, nameEnd, valEnd := p+loc[2], p+loc[3], p+loc[5]
		if c := line.closeAt[at]; c >= 0 {
			if m.quoted && strings.Contains(s[at:nameEnd], ":") {
				valEnd = c
			}
			valEnd = max(min(valEnd, c), nameEnd)
		}
		if valEnd == nameEnd { // nothing of the value inside the name's quotes
			p = nameEnd
			continue
		}
		if m.pattern && line.search(at) && startsAsPattern(s[nameEnd:valEnd]) {
			p = valEnd
			continue
		}
		b.WriteString(s[done:at])
		b.WriteString(m.with(s[at:nameEnd]))
		done, p = valEnd, valEnd
	}
	b.WriteString(s[done:])
	return b.String()
}

// startsAsPattern: a value (past an opening quote) that starts as a regular
// expression or a glob.
func startsAsPattern(v string) bool {
	switch {
	case strings.HasPrefix(v, `\"`):
		v = v[2:]
	case strings.HasPrefix(v, `"`), strings.HasPrefix(v, `'`):
		v = v[1:]
	}
	return v != "" && strings.ContainsAny(v[:1], `.*+?\([^|{`)
}

// shellLine is what masking needs of a command line: where each command
// starts (an unquoted ;, &, |, parenthesis or new line ends one), whether it
// is a search, and for each byte inside quotes where they close (-1 outside).
type shellLine struct {
	starts   []int
	searches []bool
	closeAt  []int
}

func scanShell(s string) shellLine {
	l := shellLine{starts: []int{0}, closeAt: make([]int, len(s))}
	for i := range l.closeAt {
		l.closeAt[i] = -1
	}
	for i := 0; i < len(s); i++ {
		end := -1
		switch c := s[i]; {
		case c == '\\':
			i++
		case c == '\'':
			end = closing(s, i+1, '\'', false)
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			end = closing(s, i+2, '\'', true)
		case c == '"':
			end = closing(s, i+1, '"', true)
		case strings.IndexByte(";&|()\n", c) >= 0:
			l.starts = append(l.starts, i+1)
		}
		if end >= 0 {
			for j := i + 1; j < end; j++ {
				l.closeAt[j] = end
			}
			i = end
		}
	}
	for k, start := range l.starts {
		end := len(s)
		if k+1 < len(l.starts) {
			end = l.starts[k+1]
		}
		l.searches = append(l.searches, isSearch(s[start:end]))
	}
	return l
}

// closing is where the quote q that closes a region from i is, or len(s).
func closing(s string, i int, q byte, escapes bool) int {
	for ; i < len(s); i++ {
		switch {
		case escapes && s[i] == '\\':
			i++
		case s[i] == q:
			return i
		}
	}
	return len(s)
}

// search says whether the command holding byte i is a search.
func (l shellLine) search(i int) bool {
	k := 0
	for k+1 < len(l.starts) && l.starts[k+1] <= i {
		k++
	}
	return l.searches[k]
}

var (
	words     = regexp.MustCompile(shellWord)
	assigned  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	searchers = map[string]bool{"rg": true, "grep": true, "egrep": true, "fgrep": true, "zgrep": true, "ag": true, "ack": true, "glob": true}
	wrappers  = map[string]bool{"sudo": true, "env": true, "time": true, "command": true, "nice": true, "xargs": true}
)

// isSearch: the command's program, past assignments and wrappers, is a
// search tool (git grep too; Grep and Glob are Claude's tools).
func isSearch(cmd string) bool {
	ws := words.FindAllString(cmd, 8)
	for i, w := range ws {
		if assigned.MatchString(w) || wrappers[w] {
			continue
		}
		name := strings.ToLower(w[strings.LastIndexByte(w, '/')+1:])
		return searchers[name] || (name == "git" && i+1 < len(ws) && ws[i+1] == "grep")
	}
	return false
}

// clean is a target as it is queued: relative to the checkout, at most
// maxScrub of it read, masked, cut to the platform's limit and marked.
func (t *actionTail) clean(target string) (string, bool) {
	target = t.local(target)
	cut := false
	if len(target) > maxScrub {
		target, cut = target[:strings.LastIndexAny(target[:maxScrub], " \t\r\n")+1], true
	}
	target = strings.TrimSpace(scrub(target))
	if r := []rune(target); len(r) > maxTargetRunes {
		target, cut = string(r[:maxTargetRunes]), true
	}
	return target, cut
}
