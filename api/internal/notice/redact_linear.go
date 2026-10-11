package notice

import (
	"strings"
	"unicode/utf8"
)

// replaceLinear finds the leftmost match at or after the cursor. A match whose
// edges fail resumes one byte later, so a token that starts inside the rejected
// span is still found. next must be linear in the distance it walks, and it
// must not report a start before from.
func replaceLinear(s string, next func(from int) (start, end int, repl string, ok bool)) string {
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		start, end, repl, ok := next(i)
		if !ok || start < i || end <= start {
			b.WriteString(s[i:])
			break
		}
		if leftEdge(s, start) && rightEdge(s, end) {
			b.WriteString(s[i:start])
			b.WriteString(repl)
			i = end
			continue
		}
		nextI := start + 1
		if nextI <= i {
			nextI = i + 1
		}
		if nextI > len(s) {
			b.WriteString(s[i:])
			break
		}
		b.WriteString(s[i:nextI])
		i = nextI
	}
	return b.String()
}

func replaceBearer(s string, steps *int) string {
	runs := buildBearerRuns(s, steps)
	return replaceLinear(s, func(from int) (int, int, string, bool) {
		for j := from; j < len(s); j++ {
			count(steps, 1)
			schemeEnd, ok := matchScheme(s, j)
			if !ok {
				continue
			}
			ws := asciiWSLen(s, schemeEnd)
			count(steps, ws)
			if ws < 1 {
				continue
			}
			val := schemeEnd + ws
			if val > len(s) || runs[val].runes < 8 {
				continue
			}
			return j, runs[val].end, s[j:schemeEnd] + " [redacted]", true
		}
		return 0, 0, "", false
	})
}

func replaceJWT(s string, steps *int) string {
	run := buildClassRun(s, steps, jwtByte)
	return replaceLinear(s, func(from int) (int, int, string, bool) {
		for j := from; j < len(s); j++ {
			count(steps, 1)
			if end, ok := jwtAt(s, j, run); ok {
				return j, end, "[redacted jwt]", true
			}
		}
		return 0, 0, "", false
	})
}

func replaceAWS(s string, steps *int) string {
	return replaceLinear(s, func(from int) (int, int, string, bool) {
		for j := from; j < len(s); j++ {
			count(steps, 1)
			if end, ok := awsAt(s, j, steps); ok {
				return j, end, "[redacted aws key]", true
			}
		}
		return 0, 0, "", false
	})
}

func replaceToken(s string, steps *int) string {
	runs := buildTokenRuns(s, steps)
	return replaceLinear(s, func(from int) (int, int, string, bool) {
		for j := from; j < len(s); j++ {
			count(steps, 1)
			if end, ok := tokenAt(s, j, runs); ok {
				return j, end, "[redacted token]", true
			}
		}
		return 0, 0, "", false
	})
}

func count(steps *int, n int) {
	if steps != nil && n > 0 {
		*steps += n
	}
}

type bearerRun struct {
	runes int
	end   int
}

func buildBearerRuns(s string, steps *int) []bearerRun {
	n := len(s)
	r := make([]bearerRun, n+1)
	r[n] = bearerRun{end: n}
	for i := n - 1; i >= 0; i-- {
		count(steps, 1)
		ch, size := utf8.DecodeRuneInString(s[i:])
		if size < 1 {
			size = 1
		}
		if (ch == utf8.RuneError && size == 1) || !bearerSecret(ch) {
			r[i] = bearerRun{end: i}
			continue
		}
		next := i + size
		if next < n && r[next].runes > 0 {
			r[i] = bearerRun{runes: 1 + r[next].runes, end: r[next].end}
			continue
		}
		r[i] = bearerRun{runes: 1, end: next}
	}
	return r
}

func bearerSecret(r rune) bool {
	switch r {
	case '.', '_', '~', '+', '/', '=', '-':
		return true
	}
	if r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' {
		return true
	}
	return r == '\u017f' || r == '\u212a'
}

func matchScheme(s string, i int) (int, bool) {
	if i+6 <= len(s) &&
		foldByte(s[i], 'b') && foldByte(s[i+1], 'e') && foldByte(s[i+2], 'a') &&
		foldByte(s[i+3], 'r') && foldByte(s[i+4], 'e') && foldByte(s[i+5], 'r') {
		return i + 6, true
	}
	return matchBasic(s, i)
}

func matchBasic(s string, i int) (int, bool) {
	if i >= len(s) || !foldByte(s[i], 'b') || i+1 >= len(s) || !foldByte(s[i+1], 'a') {
		return 0, false
	}
	j := i + 2
	if j >= len(s) {
		return 0, false
	}
	if s[j] == 's' || s[j] == 'S' {
		j++
	} else if strings.HasPrefix(s[j:], "\u017f") {
		j += len("\u017f")
	} else {
		return 0, false
	}
	if j+2 <= len(s) && foldByte(s[j], 'i') && foldByte(s[j+1], 'c') {
		return j + 2, true
	}
	return 0, false
}

func foldByte(c byte, lower byte) bool {
	return c == lower || c == lower-32
}

func asciiWSLen(s string, i int) int {
	j := i
	for j < len(s) {
		switch s[j] {
		case ' ', '\t', '\n', '\r', '\f':
			j++
		default:
			return j - i
		}
	}
	return j - i
}

func buildClassRun(s string, steps *int, ok func(byte) bool) []int {
	n := len(s)
	run := make([]int, n+1)
	for i := n - 1; i >= 0; i-- {
		count(steps, 1)
		if ok(s[i]) {
			run[i] = run[i+1] + 1
		}
	}
	return run
}

func jwtByte(c byte) bool {
	return isAZ09(c) || c == '_' || c == '-'
}

func jwtAt(s string, i int, run []int) (int, bool) {
	if i+3 > len(s) || s[i] != 'e' || s[i+1] != 'y' || s[i+2] != 'J' {
		return 0, false
	}
	p := i + 3
	if p > len(s) || run[p] < 8 {
		return 0, false
	}
	p += run[p]
	if p >= len(s) || s[p] != '.' {
		return 0, false
	}
	p++
	if p > len(s) || run[p] < 8 {
		return 0, false
	}
	p += run[p]
	if p >= len(s) || s[p] != '.' {
		return 0, false
	}
	p++
	if p > len(s) || run[p] < 8 {
		return 0, false
	}
	return p + run[p], true
}

func awsAt(s string, i int, steps *int) (int, bool) {
	if i+20 > len(s) {
		return 0, false
	}
	prefix := s[i : i+4]
	if prefix != "AKIA" && prefix != "ASIA" {
		return 0, false
	}
	for k := i + 4; k < i+20; k++ {
		count(steps, 1)
		c := s[k]
		if c < '0' || c > 'Z' || (c > '9' && c < 'A') {
			return 0, false
		}
	}
	return i + 20, true
}

type tokenRuns struct {
	alnum, alnumU, alnumH, alnumUH []int
}

func buildTokenRuns(s string, steps *int) tokenRuns {
	n := len(s)
	r := tokenRuns{
		alnum:   make([]int, n+1),
		alnumU:  make([]int, n+1),
		alnumH:  make([]int, n+1),
		alnumUH: make([]int, n+1),
	}
	for i := n - 1; i >= 0; i-- {
		count(steps, 1)
		c := s[i]
		az := isAZ09(c)
		if az {
			r.alnum[i] = r.alnum[i+1] + 1
		}
		if az || c == '_' {
			r.alnumU[i] = r.alnumU[i+1] + 1
		}
		if az || c == '-' {
			r.alnumH[i] = r.alnumH[i+1] + 1
		}
		if az || c == '_' || c == '-' {
			r.alnumUH[i] = r.alnumUH[i+1] + 1
		}
	}
	return r
}

func isAZ09(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z'
}

func tokenAt(s string, i int, r tokenRuns) (int, bool) {
	if end, ok := ghAt(s, i, r); ok {
		return end, true
	}
	if end, ok := glpatAt(s, i, r); ok {
		return end, true
	}
	return xoxAt(s, i, r)
}

func ghAt(s string, i int, r tokenRuns) (int, bool) {
	if i+4 > len(s) || s[i] != 'g' || s[i+1] != 'h' || s[i+3] != '_' {
		return 0, false
	}
	switch s[i+2] {
	case 'p', 'o', 'u', 's', 'r':
	default:
		return 0, false
	}
	n := r.alnum[i+4]
	if n < 20 {
		return 0, false
	}
	e := i + 4 + n
	if e < len(s) {
		e += r.alnumU[e]
	}
	return e, true
}

func glpatAt(s string, i int, r tokenRuns) (int, bool) {
	if i+6 > len(s) || s[i:i+6] != "glpat-" {
		return 0, false
	}
	n := r.alnumUH[i+6]
	if n < 20 {
		return 0, false
	}
	return i + 6 + n, true
}

func xoxAt(s string, i int, r tokenRuns) (int, bool) {
	if i+5 > len(s) || s[i] != 'x' || s[i+1] != 'o' || s[i+2] != 'x' || s[i+4] != '-' {
		return 0, false
	}
	switch s[i+3] {
	case 'a', 'b', 'p', 'r':
	default:
		return 0, false
	}
	n := r.alnumH[i+5]
	if n < 10 {
		return 0, false
	}
	e := i + 5 + n
	if e < len(s) {
		e += r.alnumU[e]
	}
	return e, true
}
