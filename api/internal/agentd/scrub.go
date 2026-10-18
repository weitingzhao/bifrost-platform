package agentd

import (
	"regexp"
	"sort"
	"strings"
)

// What must not leave the host in a target (reviews 265 A8, 266 A1, 267 A1,
// 268 A1): a local path outside the checkout, and anything that looks like a
// credential. A target is read as a shell command line. A credential's value
// is a whole shell word, walked as the shell reads it: quotes, $'…', $(…) and
// `…` (nested, with their own quotes), ${…}, escapes and concatenations; a
// construct that never closes runs to the end, as does a private key with no
// END line; a value inside quotes ends where they close. The search pattern
// argument of a search command (rg, grep, git grep, Claude's Grep and Glob)
// is the only text not read for names and values, so that rg 'token:.*' still
// says what was searched (reviews 266 C8, 267 C5, 268 C3); a token of a known
// shape is masked there too.

// secretKey is a name that holds a credential: it ends in one of these words
// (GITHUB_TOKEN, PGPASSWORD, X-Api-Key, "api_key"); a name that only contains
// one (author, TOKEN_FILE) is not one (review 266 C8).
const secretKey = `[a-z0-9_-]*(?:token|secret|passw(?:or)?d|pwd|pass|api[_-]?key|private[_-]?key|access[_-]?key|secret[_-]?key|credentials?|auth|authorization|cookie|session[_-]?id)s?`

// maxScrub is the most of a target that is read; a longer one is cut at its
// last blank before that, so a credential is never cut in two (the part of it
// that is left would not be recognized).
const maxScrub = 64 << 10

// valueMask masks the value after each name re finds (the match is the name
// and its separator); value says where the value starting at i ends.
type valueMask struct {
	re       *regexp.Regexp
	value    func(s string, i int) int
	with     func(name string) string
	programs map[string]bool // only in a command of one of these programs
	// colon: a "name: value" inside quotes (a header, a YAML line) runs to
	// where they close
	colon bool
}

var (
	privateKey = regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?(?:-----END [A-Z ]*PRIVATE KEY-----|$)`)
	bearerVal  = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{8,}`)

	keep       = func(n string) string { return n + "<secret>" }
	valueMasks = []valueMask{
		// a JSON key, raw (in single quotes, a file) and inside a
		// double-quoted shell argument
		{re: regexp.MustCompile(`(?i)"` + secretKey + `"\s*:\s*`), value: jsonValueEnd, with: func(n string) string { return n + `"<secret>"` }},
		{re: regexp.MustCompile(`(?i)\\"` + secretKey + `\\"\s*:\s*`), value: escapedJSONValueEnd, with: func(n string) string { return n + `\"<secret>\"` }},
		// NAME=value, name: value (a header keeps its form)
		{re: regexp.MustCompile(`(?i)\b` + secretKey + `\s*[=:]\s*`), value: wordEnd, with: func(n string) string {
			if strings.Contains(n, ":") {
				return n + "<secret>"
			}
			return strings.TrimRight(n, " \t\r\n=") + "=<secret>"
		}, colon: true},
		// --password value, --token=value
		{re: regexp.MustCompile(`(?i)--?(?:(?:http-|proxy-|ftp-)?passw(?:or)?d|pass|token|auth-token|access-token|secret|client-secret|api-key|key)(?:=|\s+)`), value: wordEnd, with: keep},
		// curl -u user:password, sshpass -p password, mysql -ppassword
		{re: regexp.MustCompile(`(?:^|\s)(?:-u\s*|--(?:proxy-)?user(?:=|\s+))`), value: wordEnd, with: keep, programs: map[string]bool{"curl": true, "wget": true}},
		{re: regexp.MustCompile(`(?:^|\s)-p\s*`), value: wordEnd, with: keep, programs: map[string]bool{"sshpass": true}},
		{re: regexp.MustCompile(`(?:^|\s)-p`), value: wordEnd, with: keep, programs: map[string]bool{"mysql": true, "mysqldump": true, "mysqladmin": true}},
		// Bearer …, token …, Basic …
		{re: regexp.MustCompile(`(?i)\b(?:bearer|token|basic)\s+`), value: func(s string, i int) int { return i + len(bearerVal.FindString(s[i:])) }, with: keep},
	}

	plainMasks = []struct {
		re   *regexp.Regexp
		with string
	}{
		{regexp.MustCompile(`\b(github_pat_|ghp_|gho_|ghs_|ghr_|glpat-|xox[abprs]-|sk-|sk_live_|sk_test_|rk_live_|npm_|pypi-|hf_|AIza|AKIA)[A-Za-z0-9_\-]{8,}`), "${1}<secret>"},
		{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{4,}`), "<secret>"},
		{regexp.MustCompile(`(://)[^/\s:@]+:[^/\s@]+@`), "${1}<secret>@"},
		{regexp.MustCompile(`(?:/home|/Users|/root|/var/folders|/private/var|/private/tmp|/tmp)/[^\s"'<>|;&)]*`), "<local path>"},
		{regexp.MustCompile(`\b[A-Za-z]:\\[^\s"'<>|;&)]*`), "<local path>"},
	}
)

func scrub(s string) string {
	s = privateKey.ReplaceAllString(s, "<secret>")
	for _, m := range valueMasks {
		s = m.apply(s)
	}
	for _, m := range plainMasks {
		s = m.re.ReplaceAllString(s, m.with)
	}
	return s
}

// apply masks each value m finds in s, left to right, and looks again from
// where that value ends.
func (m valueMask) apply(s string) string {
	line := scanShell(s)
	var b strings.Builder
	done := 0
	for p := 0; p < len(s); {
		loc := m.re.FindStringIndex(s[p:])
		if loc == nil {
			break
		}
		at, vs := p+loc[0], p+loc[1]
		if at < vs && s[at] <= ' ' { // a leading blank is not the name's
			at++
		}
		if line.inPattern(at) || (m.programs != nil && !m.programs[line.at(at).program]) {
			p = max(vs, at+1)
			continue
		}
		var ve int
		if c := line.closeAt[at]; c >= 0 { // the name is inside quotes: so is its value
			ve = m.value(s[:max(c, vs)], vs)
			if m.colon && strings.Contains(s[at:vs], ":") {
				ve = c
			}
		} else {
			ve = m.value(s, vs)
		}
		if ve <= vs {
			p = max(vs, at+1)
			continue
		}
		b.WriteString(s[done:at])
		b.WriteString(m.with(s[at:vs]))
		done, p = ve, ve
	}
	b.WriteString(s[done:])
	return b.String()
}

// wordEnd is where the shell word that starts at i ends: quotes, $'…', $(…)
// and `…`, ${…} and escapes are inside it; an unquoted blank, ;, &, |, <, >
// or parenthesis ends it. A construct that does not close runs to the end.
func wordEnd(s string, i int) int {
	for i < len(s) {
		switch c := s[i]; {
		case c == '\\':
			i += 2
		case c == '\'':
			i = closing(s, i+1, '\'', false) + 1
		case c == '"':
			i = closing(s, i+1, '"', true) + 1
		case c == '`':
			i = closing(s, i+1, '`', true) + 1
		case c == '$' && i+1 < len(s) && s[i+1] == '\'':
			i = closing(s, i+2, '\'', true) + 1
		case c == '$' && i+1 < len(s) && s[i+1] == '(':
			i = parenEnd(s, i+2) + 1
		case c == '$' && i+1 < len(s) && s[i+1] == '{':
			i = closing(s, i+2, '}', true) + 1
		case strings.IndexByte(" \t\r\n;&|<>()", c) >= 0:
			return i
		default:
			i++
		}
	}
	return len(s)
}

// parenEnd is where the ) that closes a $( opened before i is, or len(s).
func parenEnd(s string, i int) int {
	for depth := 1; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\\':
			i++
		case c == '\'':
			i = closing(s, i+1, '\'', false)
		case c == '"':
			i = closing(s, i+1, '"', true)
		case c == '`':
			i = closing(s, i+1, '`', true)
		case c == '(':
			depth++
		case c == ')':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return len(s)
}

// jsonValueEnd is where a JSON value that starts at i ends: a string with
// its escapes, or a bare value up to , } ] " or a blank.
func jsonValueEnd(s string, i int) int {
	if i < len(s) && s[i] == '"' {
		for i++; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case '"':
				return i + 1
			}
		}
		return len(s)
	}
	return bareEnd(s, i, " \t\r\n,}]\"")
}

// escapedJSONValueEnd is jsonValueEnd for JSON inside a double-quoted shell
// argument: the shell's escapes (\" \\ \$ \`) are read first, then JSON's,
// and an unescaped " (the argument's own end) stops it.
func escapedJSONValueEnd(s string, i int) int {
	if !strings.HasPrefix(s[i:], `\"`) {
		return bareEnd(s, i, " \t\r\n,}]\"\\")
	}
	esc := false
	for i += 2; i < len(s); {
		c, w := s[i], 1
		if c == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`\n", s[i+1]) >= 0 {
			c, w = s[i+1], 2
		} else if c == '"' {
			return i
		}
		switch {
		case esc:
			esc = false
		case c == '\\':
			esc = true
		case c == '"':
			return i + w
		}
		i += w
	}
	return len(s)
}

// bareEnd is where the first byte of stops at or after i is, or len(s).
func bareEnd(s string, i int, stops string) int {
	if j := strings.IndexAny(s[i:], stops); j >= 0 {
		return i + j
	}
	return len(s)
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

// shellLine is what masking needs of a command line: its commands (an
// unquoted ;, &, |, parenthesis or new line ends one), and for each byte
// inside quotes where they close (-1 outside).
type shellLine struct {
	cmds    []shellCmd
	closeAt []int
}

type shellCmd struct {
	end     int
	program string
	pattern [2]int // the search pattern argument; {0, 0} for none
}

func scanShell(s string) shellLine {
	l := shellLine{closeAt: make([]int, len(s))}
	for i := range l.closeAt {
		l.closeAt[i] = -1
	}
	start := 0
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
			l.cmds = append(l.cmds, readCmd(s, start, i))
			start = i + 1
		}
		if end >= 0 {
			for j := i + 1; j < end; j++ {
				l.closeAt[j] = end
			}
			i = end
		}
	}
	l.cmds = append(l.cmds, readCmd(s, start, len(s)))
	return l
}

// at is the command that holds byte i.
func (l shellLine) at(i int) shellCmd {
	k := sort.Search(len(l.cmds), func(k int) bool { return i < l.cmds[k].end })
	return l.cmds[min(k, len(l.cmds)-1)]
}

func (l shellLine) inPattern(i int) bool {
	c := l.at(i)
	return c.pattern[0] <= i && i < c.pattern[1]
}

var (
	assigned  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)
	searchers = map[string]bool{"rg": true, "grep": true, "egrep": true, "fgrep": true, "zgrep": true, "ag": true, "ack": true, "glob": true}
	wrappers  = map[string]bool{"sudo": true, "env": true, "time": true, "command": true, "nice": true, "xargs": true, "exec": true}
	// options of the search tools whose value is the next word
	valueOpts = map[string]bool{"-A": true, "-B": true, "-C": true, "-m": true, "-g": true, "-t": true, "-T": true, "-j": true, "-M": true,
		"-r": true, "-f": true, "-E": true, "-d": true, "-D": true, "-G": true, "-O": true, "--glob": true, "--iglob": true, "--type": true,
		"--type-not": true, "--type-add": true, "--threads": true, "--max-columns": true, "--max-count": true, "--max-depth": true,
		"--max-filesize": true, "--pre": true, "--pre-glob": true, "--replace": true, "--file": true, "--encoding": true, "--context": true,
		"--after-context": true, "--before-context": true, "--color": true, "--colors": true, "--sort": true, "--sortr": true,
		"--engine": true, "--include": true, "--exclude": true, "--exclude-dir": true, "--exclude-from": true, "--label": true,
		"--binary-files": true, "--devices": true, "--directories": true, "--ignore": true, "--file-search-regex": true}
	gitValueOpts = map[string]bool{"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true, "--exec-path": true}
)

// readCmd reads the command in s[start:end]: its program, past assignments
// and wrappers, and, for a search, its pattern argument (the value of -e or
// --regexp, or the first argument that is neither an option nor an option's
// value).
func readCmd(s string, start, end int) shellCmd {
	c := shellCmd{end: end}
	var ws [][2]int
	for i := start; i < end; {
		if strings.IndexByte(" \t\r", s[i]) >= 0 {
			i++
			continue
		}
		e := max(wordEnd(s[:end], i), i+1)
		ws = append(ws, [2]int{i, e})
		i = e
	}
	word := func(k int) string { return s[ws[k][0]:ws[k][1]] }
	k := 0
	for k < len(ws) && (assigned.MatchString(word(k)) || wrappers[word(k)]) {
		k++
	}
	if k == len(ws) {
		return c
	}
	w := word(k)
	c.program = strings.ToLower(strings.Trim(w[strings.LastIndexByte(w, '/')+1:], `"'`))
	k++
	if c.program == "git" {
		for k < len(ws) && strings.HasPrefix(word(k), "-") {
			if gitValueOpts[word(k)] {
				k++
			}
			k++
		}
		if k >= len(ws) || word(k) != "grep" {
			return c
		}
		k++
	} else if !searchers[c.program] {
		return c
	}
	for ; k < len(ws); k++ {
		w := word(k)
		switch {
		case w == "-e" || w == "--regexp" || w == "--":
			if k+1 < len(ws) {
				c.pattern = ws[k+1]
			}
			return c
		case strings.HasPrefix(w, "--regexp="), strings.HasPrefix(w, "-e") && len(w) > 2:
			c.pattern = ws[k]
			return c
		case valueOpts[w]:
			k++
		case strings.HasPrefix(w, "-"):
		default:
			c.pattern = ws[k]
			return c
		}
	}
	return c
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
