package planimport

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Reading the ledger. The rules are the board's (~/agent-work/status/roadmap.py
// round_of and finish), so a plan imported from the ledger shows on the board
// exactly as the ledger did: an item is a line "- <key> <name>（<note>）", a
// step a line "- [x|~| ] <title>｜<when>" under it, numbered "<key>-<n>" by its
// place in the item; the title is what comes before the first ｜ with a
// leading ★ taken off. Two things are read more carefully than the board
// does, and refused rather than dropped: a line that is neither an item nor
// a step, and a step before any item. The step marks are read from the whole
// run of marks a title starts with, so 「（补强）★ …」 is a ★ step, which the
// board misses.

var (
	roundHead = regexp.MustCompile(`^(\s*)- \*\*第 ([0-9]+) 轮逐项（核对于 ([0-9]{4})-([0-9]{2})-([0-9]{2}) ([0-9]{2}):([0-9]{2})）\*\*`)
	stepLine  = regexp.MustCompile(`^\s*- \[([x~ ])\] (.+)$`)
	itemName  = regexp.MustCompile(`^(.*?)（([^（）]*)）\s*$`)
	workHead  = regexp.MustCompile(`^### (W-[0-9]+)\s*$`)
	markedAt  = regexp.MustCompile(`(?:([0-9]{4})-)?([0-9]{2})-([0-9]{2})(?: ([0-9]{2}):([0-9]{2}))?`)
	backticks = regexp.MustCompile("`([^`]+)`")
	hexToken  = regexp.MustCompile(`^[0-9a-f]{7,40}$`)
	approval  = regexp.MustCompile(`#([0-9]{1,6})`)
	review    = regexp.MustCompile(`复核 ?([0-9]{2,4})`)
)

// LedgerOptions say what the ledger itself does not: the path to name it by
// and the commit it was read at.
type LedgerOptions struct {
	File   string // as it is named in the request, e.g. bifrost-trade-infra/agent-config/WORK.md
	Commit string // the commit of the ledger's repository, when known
}

// ParseLedger reads round n of the ledger into an import request.
func ParseLedger(text string, n int, opt LedgerOptions) (Request, error) {
	lines := strings.Split(text, "\n")
	head, base, year := -1, 0, 0
	var checked string
	for i, line := range lines {
		m := roundHead.FindStringSubmatch(line)
		if m == nil || m[2] != strconv.Itoa(n) {
			continue
		}
		if head >= 0 {
			return Request{}, fmt.Errorf("line %d: a second 「第 %d 轮逐项」 header (the first is on line %d)", i+1, n, head+1)
		}
		head, base = i, len(m[1])
		year, _ = strconv.Atoi(m[3])
		checked = fmt.Sprintf("%s-%s-%s %s:%s", m[3], m[4], m[5], m[6], m[7])
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
		if strings.TrimSpace(line) == "" {
			continue
		}
		if len(line)-len(strings.TrimLeft(line, " \t")) <= base {
			break
		}
		last = i
		body, ok := strings.CutPrefix(strings.TrimSpace(line), "- ")
		if !ok {
			return Request{}, fmt.Errorf("line %d: neither an item nor a step: %.60q", i+1, line)
		}
		if s := stepLine.FindStringSubmatch(line); s != nil {
			if item == nil {
				return Request{}, fmt.Errorf("line %d: a step before any item", i+1)
			}
			itemSteps++
			req.Nodes = append(req.Nodes, step(root, item, itemSteps, s[1], s[2], i+1, line, year, work))
			continue
		}
		if strings.HasPrefix(body, "剩余：") {
			continue
		}
		key, rest, _ := strings.Cut(body, " ")
		name, note := rest, ""
		if m := itemName.FindStringSubmatch(rest); m != nil {
			name, note = strings.TrimSpace(m[1]), strings.TrimSpace(m[2])
		}
		req.Nodes = append(req.Nodes, Node{ID: root + ":" + key, Parent: root, Kind: "item", Label: key, Title: name,
			Ordinal: countKind(req.Nodes, "item"), WorkID: work, Evidence: &Evidence{LineNo: i + 1, Line: line, Note: note}})
		item, itemSteps = &req.Nodes[len(req.Nodes)-1], 0
	}
	section := strings.Join(lines[head:last+1], "\n")
	sum := sha256.Sum256([]byte(section))
	req.Source = Source{File: opt.File, Section: "第 " + label + " 轮逐项", CheckedAt: checked, Commit: opt.Commit,
		SHA256: hex.EncodeToString(sum[:]), FirstLine: head + 1, LastLine: last + 1}
	return req, req.Validate()
}

// step reads one step line: its title and marks before the first ｜, its
// evidence after it.
func step(root string, item *Node, n int, mark, text string, lineNo int, line string, year int, work string) Node {
	what, when, _ := strings.Cut(text, "｜")
	what = strings.TrimSpace(what)
	title := strings.TrimSpace(strings.TrimLeft(what, "★"))
	owner, effect := marks(what)
	state := map[string]string{"x": StateDone, "~": StateActive, " ": StatePlanned}[mark]
	kind := "agent"
	switch {
	case owner:
		kind = "owner"
	case strings.Contains(title, "复核"):
		kind = "review"
	}
	label := item.Label + "-" + strconv.Itoa(n)
	return Node{ID: root + ":" + label, Parent: item.ID, Kind: "step", Label: label, Title: title, Ordinal: n - 1,
		WorkID: work, StepKind: kind, State: state, Owner: owner, Effect: effect,
		Evidence: &Evidence{LineNo: lineNo, Line: line, Mark: mark, MarkedAt: dateIn(when, year),
			Commits: commitsIn(text), Approvals: all(approval, text, "#"), Reviews: all(review, text, "")}}
}

// marks reads the run of marks a title starts with: 「（你）」 is the
// Owner's step, 「★」 the step that puts the item into effect, and any
// 「（补强…）」 is passed over.
func marks(what string) (owner, effect bool) {
	s := what
	for {
		s = strings.TrimSpace(s)
		switch {
		case strings.HasPrefix(s, "★"):
			effect, s = true, strings.TrimPrefix(s, "★")
		case strings.HasPrefix(s, "（你）"):
			owner, s = true, strings.TrimPrefix(s, "（你）")
		case strings.HasPrefix(s, "（补强"):
			end := strings.Index(s, "）")
			if end < 0 {
				return
			}
			s = s[end+len("）"):]
		default:
			return
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

func all(re *regexp.Regexp, text, prefix string) []string {
	var out []string
	for _, m := range re.FindAllStringSubmatch(text, -1) {
		out = appendNew(out, prefix+m[1])
	}
	return out
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
