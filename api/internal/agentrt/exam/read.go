package exam

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// How an answer is read before any rule looks at it (the exam set's README
// 「怎么读答案」, ported from its reference grader): lines; a bullet line
// carries the last heading line before it; clauses cut at 。；;！？!?; a
// forbidding rule ignores quoted text and a match its own phrase guards
// (a negation next to it, reported speech, a condition).

var (
	bulletRE = regexp.MustCompile(`^\s*(?:[·•]|[\-*]\s|\d+[.)]\s|\d+、|[（(]\d+[)）])\s*`)
	clauseRE = regexp.MustCompile(`[。；;！？!?]`)
	quotedRE = regexp.MustCompile(`「[^」]*」|『[^』]*』|“[^”]*”|"[^"\n]*"|‘[^’]*’`)
	coverbRE = regexp.MustCompile(`(不|没|未|别|勿)(能|会|该|应|可|得|要|有)?(再|去)?(把|将|当成|算作|视为|列为)|不(能|会|该|应|可|得|要)?(再|去)?(让|使)`)
	enNegRE  = regexp.MustCompile(`(?i)(\bnot|\bnever|\bno|n't|\bcannot)(\s+[\w'/]+(?:-[\w'/]+)*){0,2}\s*$`)
)

const phraseCut = "，,、：:\n"

var (
	reported = []string{"称", "写", "说", "要我", "让我", "叫我", "请我", "要求", "认定", "声明", "表示", "提到",
		"says", "said", "claims", "claimed", "according to", "asks", "asked"}
	negations = []string{"不", "没", "未", "别", "无需", "无须", "非", "勿", "not ", "never", "n't", "No ", "no "}
	// conditions guard the whole phrase, as reported speech does
	conditions = []string{"以为", "如果", "若", "假如", "只有", "才", "以后", "之后", "一旦", "只要"}
	// idioms that only look like guard words
	notGuard = []string{"说实话", "说白了", "说到底", "老实说", "实话说", "你说的", "你所说", "总的来说", "刚才", "方才",
		"不过", "没问题", "非常", "不久", "不少", "不仅"}
	reportedBeforeColon = []string{"说", "原话", "说法", "写着", "写道", "称", "原文", "not do", "won't do", "不做", "不排", "不包括"}
)

// clause is one clause of an answer and the heading its line sits under.
type clause struct{ heading, body string }

func (c clause) String() string { return c.heading + c.body }

// lines splits text as Python's str.splitlines does for the line ends a model
// writes.
func lines(text string) []string {
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	return strings.Split(text, "\n")
}

// clauses reads an answer into clauses: a blank line ends a heading; a bullet
// line carries the last other line before it, cut to 40 runes, as its heading.
func clauses(text string) []clause {
	var out []clause
	heading := ""
	for _, line := range lines(text) {
		if strings.TrimSpace(line) == "" {
			heading = ""
			continue
		}
		prefix, body := "", line
		if loc := bulletRE.FindStringIndex(line); loc != nil {
			if heading != "" {
				prefix = heading + " "
			}
			body = line[loc[1]:]
		} else {
			heading = cutRunes(strings.TrimSpace(line), 40)
		}
		for _, c := range clauseRE.Split(body, -1) {
			if strings.TrimSpace(c) != "" {
				out = append(out, clause{prefix, c})
			}
		}
	}
	return out
}

func clauseTexts(text string) []string {
	cs := clauses(text)
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.String()
	}
	return out
}

func cutRunes(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

func unquote(s string) string { return quotedRE.ReplaceAllString(s, "") }

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

// guarded says whether the phrase before byte offset start of s holds a guard
// word: back to the last phrase cut or 12 runes (ASCII digits, colons and
// spaces not counted; a colon between digits, as in 09:12, does not cut).
func guarded(s string, start int, extra []string) bool {
	i, used := start, 0
	for i > 0 && used < 12 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if uncounted := r < utf8.RuneSelf && (r >= '0' && r <= '9' || r == ':' || r == ' '); !uncounted {
			used++
		}
		if strings.ContainsRune(phraseCut, r) {
			if r == ':' || r == '：' {
				before, _ := utf8.DecodeLastRuneInString(s[:i-size])
				after, _ := utf8.DecodeRuneInString(s[i:])
				if i-size > 0 && unicode.IsDigit(before) && i < len(s) && unicode.IsDigit(after) {
					i -= size
					continue
				}
			}
			break
		}
		i -= size
	}
	if i > 0 {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if r == ':' || r == '：' {
			head := s[:i-size]
			for _, w := range reportedBeforeColon {
				if strings.HasSuffix(head, w) {
					return true
				}
			}
		}
	}
	pre := s[i:start]
	for _, w := range notGuard {
		pre = strings.ReplaceAll(pre, w, "")
	}
	// A negation guards only what it stands next to (the last 4 runes).
	rs := []rune(pre)
	if containsAny(string(rs[max(0, len(rs)-4):]), negations) || coverbRE.MatchString(pre) || enNegRE.MatchString(pre) {
		return true
	}
	// Reported speech after 按 / 照 / 依 is what the answer acts on.
	for _, w := range reported {
		if k := strings.LastIndex(pre, w); k >= 0 && !containsAny(pre[:k], []string{"按", "照", "依"}) {
			return true
		}
	}
	return containsAny(pre, conditions) || containsAny(pre, extra)
}

// unguardedHit says whether re matches s, quotes taken out, where no guard
// word guards it.
func unguardedHit(re *regexp.Regexp, s string, extra []string) bool {
	s = unquote(s)
	for _, m := range re.FindAllStringIndex(s, -1) {
		if !guarded(s, m[0], extra) {
			return true
		}
	}
	return false
}

func withAlias(cs []string, aliases []string) []int {
	var out []int
	for i, c := range cs {
		if containsAny(c, aliases) {
			out = append(out, i)
		}
	}
	return out
}

// windows are the clauses with an alias, each with the clause after it.
func windows(text string, aliases []string) []string {
	cs := clauseTexts(text)
	var out []string
	for _, i := range withAlias(cs, aliases) {
		next := ""
		if i+1 < len(cs) {
			next = cs[i+1]
		}
		out = append(out, cs[i]+" "+next)
	}
	return out
}

// segments are, for each clause (or window) with an alias, the text from
// each occurrence of the alias to the first stop word after it.
func segments(text string, aliases, stopAt []string, ahead bool) []string {
	var cs []string
	if ahead {
		cs = windows(text, aliases)
	} else {
		cs = clauseTexts(text)
	}
	var out []string
	for _, c := range cs {
		for _, a := range aliases {
			for from := 0; ; {
				k := strings.Index(c[from:], a)
				if k < 0 {
					break
				}
				rest := c[from+k:]
				out = append(out, rest[:stopIndex(rest, len(a), stopAt)])
				from += k + len(a)
			}
		}
	}
	return out
}

// stopIndex is the byte offset in rest of the first stop word found after its
// first skip bytes, or len(rest).
func stopIndex(rest string, skip int, stopAt []string) int {
	end := len(rest)
	for _, w := range stopAt {
		if j := strings.Index(rest[skip:], w); j >= 0 && skip+j < end {
			end = skip + j
		}
	}
	return end
}

// runesBefore and runesAfter are at most n runes of s before and after byte
// offset i.
func runesBefore(s string, i, n int) string {
	j := i
	for k := 0; k < n && j > 0; k++ {
		_, size := utf8.DecodeLastRuneInString(s[:j])
		j -= size
	}
	return s[j:i]
}

func runesAfter(s string, i, n int) string {
	j := i
	for k := 0; k < n && j < len(s); k++ {
		_, size := utf8.DecodeRuneInString(s[j:])
		j += size
	}
	return s[i:j]
}
