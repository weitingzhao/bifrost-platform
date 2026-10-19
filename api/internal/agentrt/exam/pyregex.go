package exam

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A pattern of the set means what Python's re with re.ASCII makes of it: the
// reference grader runs it so (reviews 371 B2, 372 B2). compilePy writes the
// pattern for RE2 with that meaning:
//
//   - \s and \S are ASCII whitespace with \v, as in Python (RE2's lack \v);
//   - case folding is ASCII only: RE2's (?i) folds by Unicode (ſ to s, K to
//     k, ä to Ä), Python's re.ASCII folds a-z and A-Z alone. So the
//     translation takes the (?i) away and writes each ASCII letter in its
//     scope as both its cases, in a class as both ranges; every other rune
//     stays as it is and matches only itself;
//   - the flags taken are a leading (?i) and a (?i:…) group; a pattern with
//     any other flag, \Q…\E, or \S inside a class is refused, as is one that
//     does not compile.
//
// The answer is never rewritten: what a rule reads is what the seat wrote.

func compilePy(p string) (*regexp.Regexp, error) {
	out, err := translate(p)
	if err != nil {
		return nil, err
	}
	return regexp.Compile(out)
}

const asciiSpace = `\t\n\v\f\r `

func isLetter(r rune) bool { return r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' }

func swapCase(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 'a' + 'A'
	}
	return r - 'A' + 'a'
}

func hasAt(rs []rune, i int, s string) bool { return strings.HasPrefix(string(rs[i:]), s) }

// escapedRune reads the rune an escape at rs[i] (a backslash) names when it
// names one (\xHH, \x{H…}, or a punctuation or letter that is itself), and
// how many runes the escape takes; ok is false for a class escape (\d, \w,
// \b, \p{…} and the like).
func escapedRune(rs []rune, i int) (r rune, n int, ok bool, err error) {
	if i+1 >= len(rs) {
		return 0, 0, false, fmt.Errorf("a pattern ends in a backslash")
	}
	switch d := rs[i+1]; {
	case d == 'x' && i+2 < len(rs) && rs[i+2] == '{':
		end := strings.IndexRune(string(rs[i+3:]), '}')
		if end < 0 {
			return 0, 0, false, fmt.Errorf(`an unclosed \x{`)
		}
		hex := string(rs[i+3 : i+3+len([]rune(string(rs[i+3:])[:end]))])
		v, perr := strconv.ParseUint(hex, 16, 32)
		if perr != nil {
			return 0, 0, false, fmt.Errorf(`\x{%s}: %v`, hex, perr)
		}
		return rune(v), 4 + len([]rune(hex)), true, nil
	case d == 'x':
		if i+3 >= len(rs) {
			return 0, 0, false, fmt.Errorf(`a short \x escape`)
		}
		v, perr := strconv.ParseUint(string(rs[i+2:i+4]), 16, 8)
		if perr != nil {
			return 0, 0, false, fmt.Errorf(`\x%s: %v`, string(rs[i+2:i+4]), perr)
		}
		return rune(v), 4, true, nil
	case d == 'p' || d == 'P':
		if i+2 < len(rs) && rs[i+2] == '{' {
			end := strings.IndexRune(string(rs[i+2:]), '}')
			if end < 0 {
				return 0, 0, false, fmt.Errorf(`an unclosed \p{`)
			}
			return 0, 2 + len([]rune(string(rs[i+2:])[:end+1])), false, nil
		}
		return 0, 3, false, nil
	case d == 'Q' || d == 'E':
		return 0, 0, false, fmt.Errorf(`\Q…\E is not taken`)
	case strings.ContainsRune("dDwWbBAzsS", d):
		return 0, 2, false, nil
	case strings.ContainsRune("tnvfr", d):
		return map[rune]rune{'t': '\t', 'n': '\n', 'v': '\v', 'f': '\f', 'r': '\r'}[d], 2, true, nil
	case isLetter(d) || d >= '0' && d <= '9':
		return 0, 0, false, fmt.Errorf(`the escape \%c is not taken`, d)
	default:
		return d, 2, true, nil // punctuation stands for itself
	}
}

// classLiteral writes rune r as a member of a class.
func classLiteral(r rune) string {
	if strings.ContainsRune("\\]-^[", r) {
		return "\\" + string(r)
	}
	return string(r)
}

// literal writes rune r as a pattern atom, folded when fold is on.
func literal(b *strings.Builder, r rune, fold bool) {
	if fold && isLetter(r) {
		fmt.Fprintf(b, "[%c%c]", r, swapCase(r))
		return
	}
	b.WriteString(regexp.QuoteMeta(string(r)))
}

func translate(p string) (string, error) {
	rs := []rune(p)
	var b strings.Builder
	fold := false
	i := 0
	if hasAt(rs, 0, "(?i)") {
		fold, i = true, 4
	}
	var groups []bool // the fold of the enclosing scopes
	for ; i < len(rs); i++ {
		c := rs[i]
		switch {
		case c == '\\':
			r, n, isRune, err := escapedRune(rs, i)
			if err != nil {
				return "", err
			}
			switch {
			case rs[i+1] == 's':
				b.WriteString("[" + asciiSpace + "]")
			case rs[i+1] == 'S':
				b.WriteString("[^" + asciiSpace + "]")
			case isRune:
				literal(&b, r, fold)
			default:
				b.WriteString(string(rs[i : i+n]))
			}
			i += n - 1
		case c == '[':
			n, err := class(&b, rs, i, fold)
			if err != nil {
				return "", err
			}
			i += n - 1
		case c == '(':
			groups = append(groups, fold)
			switch {
			case hasAt(rs, i, "(?i:"):
				b.WriteString("(?:")
				fold = true
				i += 3
			case hasAt(rs, i, "(?:"):
				b.WriteString("(?:")
				i += 2
			case hasAt(rs, i, "(?P<"):
				end := strings.IndexRune(string(rs[i:]), '>')
				if end < 0 {
					return "", fmt.Errorf("an unclosed group name")
				}
				name := string(rs[i : i+len([]rune(string(rs[i:])[:end+1]))])
				b.WriteString(name)
				i += len([]rune(name)) - 1
			case hasAt(rs, i, "(?"):
				return "", fmt.Errorf("a flag other than a leading (?i) or a (?i:…) group")
			default:
				b.WriteRune('(')
			}
		case c == ')':
			if len(groups) == 0 {
				return "", fmt.Errorf("an unbalanced )")
			}
			fold, groups = groups[len(groups)-1], groups[:len(groups)-1]
			b.WriteRune(')')
		case fold && isLetter(c):
			literal(&b, c, true)
		default:
			b.WriteRune(c)
		}
	}
	return b.String(), nil
}

// class writes the class starting at rs[i] (a [) and returns how many runes
// it took. Under fold, each ASCII letter or letter range in it gets its other
// case too, inside the class, so a negated class leaves both cases out.
func class(b *strings.Builder, rs []rune, i int, fold bool) (int, error) {
	start := i
	b.WriteRune('[')
	i++
	if i < len(rs) && rs[i] == '^' {
		b.WriteRune('^')
		i++
	}
	var extra strings.Builder
	addFold := func(lo, hi rune) {
		if !fold {
			return
		}
		for _, span := range [][2]rune{{'a', 'z'}, {'A', 'Z'}} {
			l, h := max(lo, span[0]), min(hi, span[1])
			if l <= h {
				fmt.Fprintf(&extra, "%s-%s", classLiteral(swapCase(l)), classLiteral(swapCase(h)))
			}
		}
	}
	first := true
	var prev rune
	havePrev := false
	for ; i < len(rs); i++ {
		c := rs[i]
		if c == ']' && !first {
			b.WriteString(extra.String())
			b.WriteRune(']')
			return i - start + 1, nil
		}
		first = false
		var r rune
		isRune := true
		switch {
		case c == '[' && hasAt(rs, i, "[:"):
			end := strings.Index(string(rs[i:]), ":]")
			if end < 0 {
				return 0, fmt.Errorf("an unclosed [:class:]")
			}
			name := string(rs[i:])[:end+2]
			b.WriteString(name)
			i += len([]rune(name)) - 1
			havePrev = false
			continue
		case c == '\\':
			er, n, ok, err := escapedRune(rs, i)
			if err != nil {
				return 0, err
			}
			switch {
			case rs[i+1] == 's':
				b.WriteString(asciiSpace)
				isRune = false
			case rs[i+1] == 'S':
				return 0, fmt.Errorf(`\S inside a class is not taken`)
			case ok:
				r = er
				b.WriteString(classLiteral(r))
			default:
				b.WriteString(string(rs[i : i+n]))
				isRune = false
			}
			i += n - 1
		case c == '-' && havePrev && i+1 < len(rs) && rs[i+1] != ']':
			// a range: prev-hi
			i++
			hi := rs[i]
			if hi == '\\' {
				er, n, ok, err := escapedRune(rs, i)
				if err != nil || !ok {
					return 0, fmt.Errorf("a range ends in a class escape")
				}
				hi = er
				i += n - 1
			}
			b.WriteString("-" + classLiteral(hi))
			addFold(prev, hi)
			havePrev = false
			continue
		default:
			r = c
			b.WriteString(classLiteral(c))
		}
		if isRune {
			addFold(r, r)
		}
		prev, havePrev = r, isRune
	}
	return 0, fmt.Errorf("an unclosed class")
}
