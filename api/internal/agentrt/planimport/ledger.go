package planimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Reading the ledger. The rules are the board's (~/agent-work/status/roadmap.py
// round_of and finish), so a plan imported from the ledger shows on the board
// exactly as the ledger did: an item is a line "- <key> <name>（<note>）", a
// step a line "- [x|~| ] <title>｜<when>" under it, numbered "<key>-<n>" by its
// place in the item; the title is what comes before the first ｜ with a
// leading ★ taken off, and a step is the Owner's when its title then starts
// with 「（你）」. Whitespace is what Python's str.isspace calls whitespace,
// as the board's strip() and \s read it. Two things are read more carefully
// than the board does, and refused rather than dropped or misread: a line
// that is neither an item nor a step, and a step before any item. The ★ is
// read from the whole run of marks a title starts with, so 「（补强）★ …」 is
// a ★ step, which the board misses; that is the one place the two differ.

var (
	roundHead = regexp.MustCompile(`^- \*\*第 ([0-9]+) 轮逐项（核对于 ([0-9]{4})-([0-9]{2})-([0-9]{2}) ([0-9]{2}):([0-9]{2})）\*\*`)
	stepLine  = regexp.MustCompile(`^- \[([x~ ])\] (.+)$`)
	itemName  = regexp.MustCompile(`^(.*?)（([^（）]*)）$`)
	workHead  = regexp.MustCompile(`^### (W-[0-9]+)\s*$`)
	markedAt  = regexp.MustCompile(`(?:([0-9]{4})-)?([0-9]{2})-([0-9]{2})(?: ([0-9]{2}):([0-9]{2}))?`)
	backticks = regexp.MustCompile("`([^`]+)`")
	hexToken  = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
)

// pySpace is Python's str.isspace for one character.
func pySpace(r rune) bool { return unicode.IsSpace(r) || r >= 0x1c && r <= 0x1f }

func trim(s string) string { return strings.TrimFunc(s, pySpace) }

// indent is the number of whitespace characters a line starts with, and the
// rest of the line.
func indent(line string) (int, string) {
	rest := strings.TrimLeftFunc(line, pySpace)
	return utf8.RuneCountInString(line[:len(line)-len(rest)]), rest
}

// LedgerOptions say what the ledger itself does not: the path to name it by
// and the commit it was read at.
type LedgerOptions struct {
	File   string // as it is named in the request, e.g. bifrost-trade-infra/agent-config/WORK.md
	Commit string // the commit of the ledger's repository, when known
}

// header reads a round's header line: its indentation, round, year and the
// 「核对于」 time as written.
type header struct {
	indent, round, year int
	checked             string
}

func readHeader(line string) (header, bool) {
	ind, rest := indent(line)
	m := roundHead.FindStringSubmatch(rest)
	if m == nil {
		return header{}, false
	}
	round, _ := strconv.Atoi(m[1])
	year, _ := strconv.Atoi(m[2])
	return header{indent: ind, round: round, year: year, checked: fmt.Sprintf("%s-%s-%s %s:%s", m[2], m[3], m[4], m[5], m[6])}, true
}

// ParseLedger reads round n of the ledger into an import request.
func ParseLedger(text string, n int, opt LedgerOptions) (Request, error) {
	lines := strings.Split(text, "\n")
	head := -1
	var h header
	for i, line := range lines {
		got, ok := readHeader(line)
		if !ok || got.round != n {
			continue
		}
		if head >= 0 {
			return Request{}, fmt.Errorf("line %d: a second 「第 %d 轮逐项」 header (the first is on line %d)", i+1, n, head+1)
		}
		head, h = i, got
	}
	if head < 0 {
		return Request{}, fmt.Errorf("the ledger has no 「第 %d 轮逐项（核对于 …）」 header", n)
	}
	work := ""
	for i := head; i >= 0; i-- {
		if m := workHead.FindStringSubmatch(lines[i]); m != nil {
			work = m[1]
			break
		}
	}
	label := strconv.Itoa(n)
	root := "R" + label
	req := Request{Format: Format,
		Root: Node{ID: root, Kind: "round", Label: label, Title: "第 " + label + " 轮", Ordinal: n, WorkID: work,
			Evidence: &Evidence{LineNo: head + 1, Line: lines[head]}}}
	last := head
	var item *Node
	itemSteps := 0
	for i := head + 1; i < len(lines); i++ {
		line := lines[i]
		if trim(line) == "" {
			continue
		}
		if ind, _ := indent(line); ind <= h.indent {
			break
		}
		last = i
		l, err := readLine(line, h.year)
		if err != nil {
			return Request{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		switch l.kind {
		case "rest":
			continue
		case "step":
			if item == nil {
				return Request{}, fmt.Errorf("line %d: a step before any item", i+1)
			}
			itemSteps++
			stepLabel := item.Label + "-" + strconv.Itoa(itemSteps)
			req.Nodes = append(req.Nodes, Node{ID: root + ":" + stepLabel, Parent: item.ID, Kind: "step", Label: stepLabel, Title: l.title,
				Ordinal: itemSteps - 1, WorkID: work, StepKind: l.stepKind, State: l.state, Owner: l.owner, Effect: l.effect,
				Evidence: &Evidence{LineNo: i + 1, Line: line, Mark: l.mark, MarkedAt: l.markedAt, Commits: l.commits,
					Approvals: l.approvals, Reviews: l.reviews}})
		case "item":
			req.Nodes = append(req.Nodes, Node{ID: root + ":" + l.key, Parent: root, Kind: "item", Label: l.key, Title: l.title,
				Ordinal: countKind(req.Nodes, "item"), WorkID: work, Evidence: &Evidence{LineNo: i + 1, Line: line, Note: l.note}})
			item, itemSteps = &req.Nodes[len(req.Nodes)-1], 0
		}
	}
	section := strings.Join(lines[head:last+1], "\n")
	sum := sha256.Sum256([]byte(section))
	req.Source = Source{File: opt.File, Section: "第 " + label + " 轮逐项", CheckedAt: h.checked, Commit: opt.Commit,
		SHA256: hex.EncodeToString(sum[:]), FirstLine: head + 1, LastLine: last + 1}
	return req, req.Validate()
}

// line is what one line of a round says.
type line struct {
	kind                        string // item, step or rest (the 「剩余：」 line)
	key, note                   string // an item's
	title                       string
	mark, state, stepKind       string // a step's
	owner, effect               bool
	markedAt                    string
	commits, approvals, reviews []string
}

// readLine reads one non-blank line of a round. Validate reads each node's
// evidence line with it again, so a request says nothing its lines do not.
func readLine(raw string, year int) (line, error) {
	_, rest := indent(raw)
	body, ok := strings.CutPrefix(trim(rest), "- ")
	if !ok {
		return line{}, fmt.Errorf("neither an item nor a step: %.60q", raw)
	}
	if s := stepLine.FindStringSubmatch(rest); s != nil {
		return readStep(s[1], s[2], year), nil
	}
	if strings.HasPrefix(body, "剩余：") {
		return line{kind: "rest"}, nil
	}
	key, name, _ := strings.Cut(body, " ")
	l := line{kind: "item", key: key, title: name}
	if m := itemName.FindStringSubmatch(name); m != nil {
		l.title, l.note = trim(m[1]), trim(m[2])
	}
	return l, nil
}

// readStep reads a step: its title and marks before the first ｜, its
// evidence in the whole text.
func readStep(mark, text string, year int) line {
	what, when, _ := strings.Cut(text, "｜")
	what = trim(what)
	title := trim(strings.TrimLeft(what, "★"))
	owner := strings.HasPrefix(title, "（你）")
	kind := "agent"
	switch {
	case owner:
		kind = "owner"
	case strings.Contains(title, "复核"):
		kind = "review"
	}
	return line{kind: "step", title: title, mark: mark, owner: owner, effect: effect(what), stepKind: kind,
		state:    map[string]string{"x": StateDone, "~": StateActive, " ": StatePlanned}[mark],
		markedAt: dateIn(when, year), commits: commitsIn(text), approvals: approvalsIn(text), reviews: reviewsIn(text)}
}

// effect reads the run of marks a title starts with (★, 「（你）」,
// 「（补强…）」 in any order) for a ★.
func effect(what string) bool {
	s := what
	for {
		s = trim(s)
		switch {
		case strings.HasPrefix(s, "★"):
			return true
		case strings.HasPrefix(s, "（你）"):
			s = strings.TrimPrefix(s, "（你）")
		case strings.HasPrefix(s, "（补强"):
			end := strings.Index(s, "）")
			if end < 0 {
				return false
			}
			s = s[end+len("）"):]
		default:
			return false
		}
	}
}

// dateIn is the first date in the text after the title, with the
// section's year when it names none: "2026-10-14 01:09", or "2026-10-14"
// without a time. A date runs into no other digit.
func dateIn(when string, year int) string {
	for _, m := range markedAt.FindAllStringSubmatchIndex(when, -1) {
		if m[0] > 0 && isDigit(when[m[0]-1]) || m[1] < len(when) && isDigit(when[m[1]]) {
			continue
		}
		part := func(k int) string {
			if m[2*k] < 0 {
				return ""
			}
			return when[m[2*k]:m[2*k+1]]
		}
		month, _ := strconv.Atoi(part(2))
		day, _ := strconv.Atoi(part(3))
		if month < 1 || month > 12 || day < 1 || day > 31 {
			continue
		}
		y := strconv.Itoa(year)
		if part(1) != "" {
			y = part(1)
		}
		at := fmt.Sprintf("%s-%s-%s", y, part(2), part(3))
		if part(4) != "" {
			at += " " + part(4) + ":" + part(5)
		}
		return at
	}
	return ""
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// digits is the run of ASCII digits s starts with.
func digits(s string) string {
	i := 0
	for i < len(s) && isDigit(s[i]) {
		i++
	}
	return s[:i]
}

// commitsIn lists the commit-like words inside backticks: 7 to 40 lowercase
// hex characters with at least one letter (a run of digits is a number).
func commitsIn(text string) []string {
	var out []string
	for _, span := range backticks.FindAllStringSubmatch(text, -1) {
		words := strings.FieldsFunc(span[1], func(r rune) bool {
			return (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z')
		})
		for _, w := range words {
			if hexToken.MatchString(w) && strings.ContainsAny(w, "abcdef") {
				out = appendNew(out, w)
			}
		}
	}
	return out
}

// approvalsIn lists the approval numbers written "#n": a whole run of 1 to 6
// digits; a longer run is not an approval number and is left out, never cut
// to a shorter one.
func approvalsIn(text string) []string {
	var out []string
	for rest := text; ; {
		i := strings.Index(rest, "#")
		if i < 0 {
			return out
		}
		rest = rest[i+1:]
		if d := digits(rest); len(d) >= 1 && len(d) <= 6 {
			out = appendNew(out, "#"+d)
		}
	}
}

// reviewsIn lists the review numbers written after 「复核」: a number of 2 to
// 4 digits, then more joined by a list mark (、，, 和) or a range mark (–—-~～
// 到), a range counted out when it is short. A run of digits of another
// length ends the list and is left out, never cut to a shorter number.
func reviewsIn(text string) []string {
	var out []string
	for rest := text; ; {
		i := strings.Index(rest, "复核")
		if i < 0 {
			return out
		}
		rest = rest[i+len("复核"):]
		s := strings.TrimPrefix(rest, " ")
		prev, ranged := 0, false
		for {
			d := digits(s)
			if len(d) < 2 || len(d) > 4 {
				break
			}
			n, _ := strconv.Atoi(d)
			if ranged && n > prev && n-prev < maxRefs {
				for k := prev + 1; k < n; k++ {
					out = appendNew(out, strconv.Itoa(k))
				}
			}
			out = appendNew(out, d)
			prev, s = n, s[len(d):]
			sep, isRange := separator(s)
			if sep == "" {
				break
			}
			s, ranged = strings.TrimLeft(s[len(sep):], " "), isRange
		}
	}
}

// separator is the list or range mark s starts with, if any.
func separator(s string) (string, bool) {
	for _, r := range []string{"–", "—", "-", "~", "～", "到"} {
		if strings.HasPrefix(s, r) {
			return r, true
		}
	}
	for _, l := range []string{"、", "，", ",", " 和 ", "和"} {
		if strings.HasPrefix(s, l) {
			return l, false
		}
	}
	return "", false
}

func appendNew(list []string, s string) []string {
	for _, have := range list {
		if have == s {
			return list
		}
	}
	if len(list) >= maxRefs {
		return list
	}
	return append(list, s)
}

func countKind(nodes []Node, kind string) int {
	k := 0
	for _, n := range nodes {
		if n.Kind == kind {
			k++
		}
	}
	return k
}
