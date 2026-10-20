package planimport

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// The architecture table (W-31 2.5 G2 A, CUT-round2-2.5-interface-2026-10-19.md
// §2): the ledger's 「架构对照」 section, imported as the runtime's ARCH root
// and its arch_row rows (design v5 §5.1.2). A row's line is
// "- [x|~| ] <what the design has>｜<what it actually is now>": the row is
// titled with the first part, and the second becomes the row's first note
// (store.importTree), the words the page shows beside the tick. What the
// line does not say is given beside it (ArchRow): the label the row's id is
// made of, and its done rule, gates_passed only; a row whose gate tests are
// not settled yet has none and shows unknown. The hand-written mark is kept
// as evidence only: the tick is the rule's, evaluated against what is
// deployed now.

// The architecture root and its section.
const (
	ArchRootID  = "ARCH"
	ArchSection = "架构对照"
)

const maxArchLabel = 40 // bytes of a row's label

var (
	archHead  = regexp.MustCompile(`^- \*\*架构对照（[^（）]*核对于 ([0-9]{4})-([0-9]{2})-([0-9]{2}) ([0-9]{2}):([0-9]{2})）\*\*`)
	archLabel = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
)

// ArchRow is what a row of the table has that its line does not say: the
// label its id is made of (lower-case words joined by -, at most 40 bytes:
// ARCH:state-db) and its done rule, if its gate tests are settled.
type ArchRow struct {
	Label string          `json:"label"`
	Rule  json.RawMessage `json:"rule,omitempty"`
}

// readArchHeader reads the table's header line: its indentation and the
// 「核对于」 time as written.
func readArchHeader(line string) (header, bool) {
	ind, rest := indent(line)
	m := archHead.FindStringSubmatch(rest)
	if m == nil {
		return header{}, false
	}
	year, _ := strconv.Atoi(m[1])
	return header{indent: ind, year: year, checked: fmt.Sprintf("%s-%s-%s %s:%s", m[1], m[2], m[3], m[4], m[5])}, true
}

// archLine is what a row's line says.
type archLine struct {
	mark, title, now string
}

func readArchLine(raw string) (archLine, error) {
	_, rest := indent(raw)
	s := stepLine.FindStringSubmatch(rest)
	if s == nil {
		return archLine{}, fmt.Errorf("not a row of the table: %.60q", raw)
	}
	what, now, _ := strings.Cut(s[2], "｜")
	return archLine{mark: s[1], title: trim(what), now: trim(now)}, nil
}

// ArchNow is what a row's line says the row actually is now: the text after
// its first ｜, or "" when there is none.
func ArchNow(line string) string {
	l, err := readArchLine(line)
	if err != nil {
		return ""
	}
	return l.now
}

// ParseArch reads the ledger's architecture table into an import request;
// rows[i] gives the label and the rule of the table's i-th row.
func ParseArch(text string, rows []ArchRow, opt LedgerOptions) (Request, error) {
	lines := strings.Split(text, "\n")
	head := -1
	var h header
	for i, line := range lines {
		got, ok := readArchHeader(line)
		if !ok {
			continue
		}
		if head >= 0 {
			return Request{}, fmt.Errorf("line %d: a second 「架构对照」 header (the first is on line %d)", i+1, head+1)
		}
		head, h = i, got
	}
	if head < 0 {
		return Request{}, fmt.Errorf("the ledger has no 「架构对照（…核对于 …）」 header")
	}
	work := ""
	for i := head; i >= 0; i-- {
		if m := workHead.FindStringSubmatch(lines[i]); m != nil {
			work = m[1]
			break
		}
	}
	req := Request{Format: Format, Root: Node{ID: ArchRootID, Kind: "arch_root", Label: ArchRootID, Title: ArchSection, WorkID: work,
		Evidence: &Evidence{LineNo: head + 1, Line: lines[head]}}}
	last := head
	for i := head + 1; i < len(lines); i++ {
		line := lines[i]
		if trim(line) == "" {
			continue
		}
		if ind, _ := indent(line); ind <= h.indent {
			break
		}
		last = i
		l, err := readArchLine(line)
		if err != nil {
			return Request{}, fmt.Errorf("line %d: %w", i+1, err)
		}
		k := len(req.Nodes)
		if k >= len(rows) {
			return Request{}, fmt.Errorf("line %d: the table has more rows than the %d given", i+1, len(rows))
		}
		req.Nodes = append(req.Nodes, Node{ID: ArchRootID + ":" + rows[k].Label, Parent: ArchRootID, Kind: "arch_row", Label: rows[k].Label,
			Title: l.title, Ordinal: k, WorkID: work, Rule: rows[k].Rule, Evidence: &Evidence{LineNo: i + 1, Line: line, Mark: l.mark}})
	}
	if len(req.Nodes) != len(rows) {
		return Request{}, fmt.Errorf("the table has %d rows, %d given", len(req.Nodes), len(rows))
	}
	section := strings.Join(lines[head:last+1], "\n")
	sum := sha256.Sum256([]byte(section))
	req.Source = Source{File: opt.File, Section: ArchSection, CheckedAt: h.checked, Commit: opt.Commit,
		SHA256: hex.EncodeToString(sum[:]), FirstLine: head + 1, LastLine: last + 1}
	return req, req.Validate()
}

// validateArch checks the architecture table's request: the ARCH root on
// the table's header line, then its rows in the order of their lines, each
// saying what its line says and with a rule of gates only, or none.
func (r Request) validateArch() error {
	s, root := r.Source, r.Root
	switch {
	case s.Section != ArchSection:
		return fmt.Errorf("the architecture table's section is %s, not %q", ArchSection, s.Section)
	case root.ID != ArchRootID || root.Label != ArchRootID || root.Title != ArchSection || root.Ordinal != 0:
		return fmt.Errorf("the architecture root is %s, labelled %s, titled %s, at ordinal 0", ArchRootID, ArchRootID, ArchSection)
	case root.Parent != "" || root.StepKind != "" || root.State != "" || root.Owner || root.Effect || len(root.Rule) > 0:
		return fmt.Errorf("root has no parent, step kind, state, step marks or rule")
	}
	if err := r.checkCommon(root); err != nil {
		return err
	}
	if root.Evidence == nil {
		return fmt.Errorf("root carries its header line")
	}
	h, ok := readArchHeader(root.Evidence.Line)
	switch {
	case !ok:
		return fmt.Errorf("root's line is not the 「架构对照（…核对于 …）」 header")
	case h.checked != s.CheckedAt || root.Evidence.LineNo != s.FirstLine:
		return fmt.Errorf("root's line is the header the source names, on its first line, checked at %s", s.CheckedAt)
	}
	if len(r.Nodes) == 0 || len(r.Nodes) > MaxNodes {
		return fmt.Errorf("a request holds 1 to %d nodes, not %d", MaxNodes, len(r.Nodes))
	}
	ids := map[string]bool{root.ID: true}
	lineNo := root.Evidence.LineNo
	for i, n := range r.Nodes {
		at := fmt.Sprintf("nodes[%d] (%s)", i, n.ID)
		if ids[n.ID] {
			return fmt.Errorf("%s: id used twice", at)
		}
		ids[n.ID] = true
		switch {
		case n.Kind != "arch_row" || n.Parent != ArchRootID:
			return fmt.Errorf("%s: a node under %s is an architecture row", at, ArchRootID)
		case len(n.Label) > maxArchLabel || !archLabel.MatchString(n.Label):
			return fmt.Errorf("%s: a row's label is lower-case words joined by -, at most %d bytes, not %q", at, maxArchLabel, n.Label)
		case n.ID != ArchRootID+":"+n.Label:
			return fmt.Errorf("%s: id is %s:%s", at, ArchRootID, n.Label)
		case n.Ordinal != i:
			return fmt.Errorf("%s: rows are numbered 0, 1, … in order; this is %d, not %d", at, i, n.Ordinal)
		case n.StepKind != "" || n.State != "" || n.Owner || n.Effect:
			return fmt.Errorf("%s: a row has no step kind, state or step marks", at)
		case n.WorkID != root.WorkID:
			return fmt.Errorf("%s: every node has the root's work id %q", at, root.WorkID)
		case n.Evidence == nil:
			return fmt.Errorf("%s: a node carries the line it was read from", at)
		}
		if err := r.checkCommon(n); err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		if err := archRule(n.Rule); err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		e := n.Evidence
		if e.LineNo <= lineNo {
			return fmt.Errorf("%s: nodes come in the order of their lines", at)
		}
		if ind, _ := indent(e.Line); ind <= h.indent {
			return fmt.Errorf("%s: its line is not inside the table's section", at)
		}
		lineNo = e.LineNo
		l, err := readArchLine(e.Line)
		if err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		if l.title != n.Title || l.mark != e.Mark || e.Note != "" || e.MarkedAt != "" || len(e.Commits) > 0 || len(e.Approvals) > 0 ||
			len(e.Reviews) > 0 || len(e.Cut) > 0 {
			return fmt.Errorf("%s: it does not say what its line says (title %q, mark %q)", at, l.title, l.mark)
		}
	}
	return nil
}

// archRule checks a row's rule: none, or one that parses and has only
// standing conditions (gates_passed), so its tick is what is deployed now.
func archRule(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	rule, err := plan.Parse(raw)
	if err != nil {
		return err
	}
	if !rule.OnlyStanding() {
		return fmt.Errorf("a row's rule is gates_passed only (under any all and any); a row whose gate tests are not settled has none")
	}
	return nil
}
