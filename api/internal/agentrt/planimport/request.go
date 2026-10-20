// Package planimport turns a round of the work ledger (WORK.md, "第 N 轮逐项")
// into a plan import request and compares plans in the shape the board shows
// (W-31 round 2, CUT-plan-import-2026-10-19.md). It reads and writes nothing
// but its arguments: the request is written to a file by the command, and
// imported by the store (store.ImportPlan) through an entry not built yet.
package planimport

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Format names the request's shape.
const Format = "agentrt.plan_import/v1"

// Bounds of a request.
const (
	MaxNodes    = 500
	maxTitle    = 1000  // runes
	maxLine     = 65536 // bytes of a ledger line kept as evidence
	maxRefs     = 20    // commits, approvals or reviews kept per line
	maxNodeText = 200   // bytes of an id, label or work id
)

// Request is one plan tree to import: its root and its nodes, parents before
// children, each kind in ledger order.
type Request struct {
	Format string `json:"format"`
	Source Source `json:"source"`
	Root   Node   `json:"root"`
	Nodes  []Node `json:"nodes"`
}

// Source says where the request was read from.
type Source struct {
	File      string `json:"file"`
	Section   string `json:"section"`
	CheckedAt string `json:"checked_at"` // the section's 「核对于」, as written
	Commit    string `json:"commit,omitempty"`
	SHA256    string `json:"sha256"` // of the section's lines, header to last
	FirstLine int    `json:"first_line"`
	LastLine  int    `json:"last_line"`
}

// Node is a plan node. A step has a step kind and a state (planned, active
// or done); items and the root have neither. An architecture row may have a
// done rule (arch.go); no other node does. Evidence is the ledger line the
// node was read from.
type Node struct {
	ID       string `json:"id"`
	Parent   string `json:"parent,omitempty"`
	Kind     string `json:"kind"`
	Label    string `json:"label"`
	Title    string `json:"title"`
	Ordinal  int    `json:"ordinal"`
	WorkID   string `json:"work_id,omitempty"`
	StepKind string `json:"step_kind,omitempty"`
	State    string `json:"state,omitempty"`
	Owner    bool   `json:"owner_step,omitempty"`
	Effect   bool   `json:"effect_step,omitempty"`
	// Rule is an architecture row's done rule, gates_passed only; none
	// until the row's gate tests are settled.
	Rule     json.RawMessage `json:"rule,omitempty"`
	Evidence *Evidence       `json:"evidence,omitempty"`
}

// Evidence is what the ledger line says, kept as it was written: the line,
// where it was, and what can be read out of it. Nothing here is checked
// against the systems it names.
type Evidence struct {
	LineNo    int      `json:"line_no"`
	Line      string   `json:"line"`
	Mark      string   `json:"mark,omitempty"`      // a step's x, ~ or space
	Note      string   `json:"note,omitempty"`      // an item's parenthetical
	MarkedAt  string   `json:"marked_at,omitempty"` // the first date (and time) after the title: "2026-10-14 01:09"
	Commits   []string `json:"commits,omitempty"`
	Approvals []string `json:"approvals,omitempty"` // "#41"
	Reviews   []string `json:"reviews,omitempty"`   // "复核 145" as "145"
	// Cut names the reference lists that are not complete: more than
	// maxRefs, or a range too long to count out (only its ends are kept).
	Cut []string `json:"cut,omitempty"`
}

// SourcePrefix starts the legacy_imported source of an import: the source is
// SourcePrefix and the request's artifact id.
const SourcePrefix = "plan-import "

// Source of an import whose request is stored as artifactID.
func ImportSource(artifactID string) string { return SourcePrefix + artifactID }

// RequestOf is the request artifact a legacy_imported source names.
func RequestOf(source string) (string, bool) {
	id, ok := strings.CutPrefix(source, SourcePrefix)
	return id, ok && sha256ID.MatchString(id)
}

// ManualAtom is the manual atom an imported done step's rule names, and its
// note records.
func ManualAtom(stepID string) string { return "imported from the work ledger: " + stepID }

// Step states as the runtime stores them.
const (
	StatePlanned = "planned"
	StateActive  = "active"
	StateDone    = "done"
)

// StepKinds are the runtime's step kinds (v5 §5.1.2).
var StepKinds = []string{"agent", "review", "owner", "approval", "release", "ci", "gate", "manual"}

var (
	roundLabel = regexp.MustCompile(`^[1-9][0-9]{0,3}$`)
	itemTail   = regexp.MustCompile(`^[0-9]{1,3}[a-z]?$`)
	workID     = regexp.MustCompile(`^W-[0-9]{1,5}$`)
	sha64      = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sha40      = regexp.MustCompile(`^[0-9a-f]{40}$`)
	sha256ID   = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// Validate checks a request by itself: the shape the store writes and the
// numbering of design v5 §5.1.2 (an item is <round>.<n>, a step <item>-<n>
// with n its place from 1; ids are <root>:<label>), or, for the architecture
// table, its rows (arch.go). The database checks the rest when the request
// is imported.
func (r Request) Validate() error {
	if r.Format != Format {
		return fmt.Errorf("format is %q, not %q", r.Format, Format)
	}
	if err := r.validateSource(); err != nil {
		return err
	}
	if r.Root.Kind == "arch_root" {
		return r.validateArch()
	}
	return r.validateRound()
}

// validateSource checks where the request says it was read from.
func (r Request) validateSource() error {
	s := r.Source
	switch {
	case s.File == "" || s.Section == "":
		return fmt.Errorf("source names its file and section")
	case !sha64.MatchString(s.SHA256):
		return fmt.Errorf("source.sha256 is 64 lowercase hex characters")
	case s.Commit != "" && !sha40.MatchString(s.Commit):
		return fmt.Errorf("source.commit is a 40-character commit")
	case s.FirstLine < 1 || s.LastLine < s.FirstLine:
		return fmt.Errorf("source lines run from first_line to last_line")
	}
	for _, f := range []string{s.File, s.Section, s.CheckedAt} {
		if err := plain("source", f, maxTitle, true); err != nil {
			return err
		}
	}
	return nil
}

// validateRound checks a round's request.
func (r Request) validateRound() error {
	s := r.Source
	root := r.Root
	switch {
	case root.Kind != "round":
		return fmt.Errorf("root is a round, not %q", root.Kind)
	case !roundLabel.MatchString(root.Label):
		return fmt.Errorf("root label is the round number, not %q", root.Label)
	case root.ID != "R"+root.Label:
		return fmt.Errorf("root id is R%s, not %q", root.Label, root.ID)
	case strconv.Itoa(root.Ordinal) != root.Label:
		return fmt.Errorf("root ordinal is the round number %s, not %d", root.Label, root.Ordinal)
	case root.Parent != "" || root.StepKind != "" || root.State != "" || root.Owner || root.Effect:
		return fmt.Errorf("root has no parent, step kind, state or step marks")
	case len(root.Rule) > 0:
		return fmt.Errorf("only an architecture row carries a rule")
	}
	if err := r.checkCommon(root); err != nil {
		return err
	}
	if root.Evidence == nil {
		return fmt.Errorf("root carries its header line")
	}
	h, ok := readHeader(root.Evidence.Line)
	switch {
	case !ok || strconv.Itoa(h.round) != root.Label:
		return fmt.Errorf("root's line is not the 「第 %s 轮逐项」 header", root.Label)
	case h.checked != s.CheckedAt || root.Evidence.LineNo != s.FirstLine:
		return fmt.Errorf("root's line is the header the source names, on its first line, checked at %s", s.CheckedAt)
	case root.Title != "第 "+root.Label+" 轮":
		return fmt.Errorf("root title is 第 %s 轮", root.Label)
	}
	if len(r.Nodes) == 0 || len(r.Nodes) > MaxNodes {
		return fmt.Errorf("a request holds 1 to %d nodes, not %d", MaxNodes, len(r.Nodes))
	}
	items := map[string]*Node{}
	steps := map[string]int{} // steps seen per item
	var last *Node            // the item the lines so far are under
	ids := map[string]bool{root.ID: true}
	lineNo := root.Evidence.LineNo
	for i := range r.Nodes {
		n := &r.Nodes[i]
		at := fmt.Sprintf("nodes[%d] (%s)", i, n.ID)
		if ids[n.ID] {
			return fmt.Errorf("%s: id used twice", at)
		}
		ids[n.ID] = true
		if n.ID != root.ID+":"+n.Label {
			return fmt.Errorf("%s: id is %s:%s", at, root.ID, n.Label)
		}
		if err := r.checkCommon(*n); err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
		switch {
		case n.WorkID != root.WorkID:
			return fmt.Errorf("%s: every node has the root's work id %q", at, root.WorkID)
		case n.Evidence == nil:
			return fmt.Errorf("%s: a node carries the line it was read from", at)
		case len(n.Rule) > 0:
			return fmt.Errorf("%s: only an architecture row carries a rule; a round's steps get theirs from the ledger", at)
		}
		switch n.Kind {
		case "item":
			tail, ok := strings.CutPrefix(n.Label, root.Label+".")
			switch {
			case n.Parent != root.ID:
				return fmt.Errorf("%s: an item sits under the root", at)
			case !ok || !itemTail.MatchString(tail):
				return fmt.Errorf("%s: an item's label is %s.<n>, not %q", at, root.Label, n.Label)
			case n.Ordinal != len(items):
				return fmt.Errorf("%s: items are numbered 0, 1, … in order; this is %d, not %d", at, len(items), n.Ordinal)
			case n.StepKind != "" || n.State != "" || n.Owner || n.Effect:
				return fmt.Errorf("%s: an item has no step kind, state or step marks", at)
			}
			items[n.ID], last = n, n
		case "step":
			parent, ok := items[n.Parent]
			switch {
			case !ok:
				return fmt.Errorf("%s: a step sits under an item listed before it", at)
			case parent != last:
				return fmt.Errorf("%s: a step sits under the item its line comes after, %s", at, last.ID)
			case n.Ordinal != steps[n.Parent]:
				return fmt.Errorf("%s: steps are numbered 0, 1, … in order; this is %d, not %d", at, steps[n.Parent], n.Ordinal)
			case n.Label != parent.Label+"-"+strconv.Itoa(n.Ordinal+1):
				return fmt.Errorf("%s: a step's label is %s-%d", at, parent.Label, n.Ordinal+1)
			case !slices.Contains(StepKinds, n.StepKind):
				return fmt.Errorf("%s: step kind is one of %v", at, StepKinds)
			case n.State != StatePlanned && n.State != StateActive && n.State != StateDone:
				return fmt.Errorf("%s: an imported step is planned, active or done", at)
			}
			steps[n.Parent]++
		default:
			return fmt.Errorf("%s: a node under the root is an item or a step", at)
		}
		if n.Evidence.LineNo <= lineNo {
			return fmt.Errorf("%s: nodes come in the order of their lines", at)
		}
		if ind, _ := indent(n.Evidence.Line); ind <= h.indent {
			return fmt.Errorf("%s: its line is not inside the round's section", at)
		}
		lineNo = n.Evidence.LineNo
		if err := derived(*n, h.year); err != nil {
			return fmt.Errorf("%s: %w", at, err)
		}
	}
	return nil
}

// derived checks that a node says what its line says: read again by the
// ledger's rules, the line gives the node's title, marks, state and kind and
// the evidence's mark, note, date and references. A request is then
// consistent in itself; that its lines are the ledger's is what its source
// digest records, not something checked here.
func derived(n Node, year int) error {
	l, err := readLine(n.Evidence.Line, year)
	if err != nil {
		return err
	}
	e := n.Evidence
	type fields struct {
		title, stepKind, state, mark, note, markedAt string
		owner, effect                                bool
	}
	want := fields{l.title, l.stepKind, l.state, l.mark, l.note, l.markedAt, l.owner, l.effect}
	switch n.Kind {
	case "item":
		if l.kind != "item" || l.key != n.Label {
			return fmt.Errorf("its line is not the item %s", n.Label)
		}
	case "step":
		if l.kind != "step" {
			return fmt.Errorf("its line is not a step")
		}
	}
	got := fields{n.Title, n.StepKind, n.State, e.Mark, e.Note, e.MarkedAt, n.Owner, n.Effect}
	if got != want || !slices.Equal(e.Commits, l.commits) || !slices.Equal(e.Approvals, l.approvals) ||
		!slices.Equal(e.Reviews, l.reviews) || !slices.Equal(e.Cut, l.cut) {
		return fmt.Errorf("it does not say what its line says (title %q, state %q, marks %v/%v, kind %q, mark %q)",
			l.title, l.state, l.owner, l.effect, l.stepKind, l.mark)
	}
	return nil
}

// checkCommon checks what every node has: its title, work id and evidence.
func (r Request) checkCommon(n Node) error {
	if len(n.ID) > maxNodeText || len(n.Label) > maxNodeText {
		return fmt.Errorf("id and label are at most %d bytes", maxNodeText)
	}
	if n.WorkID != "" && !workID.MatchString(n.WorkID) {
		return fmt.Errorf("work id is W-<n>, not %q", n.WorkID)
	}
	if err := plain("title", n.Title, maxTitle, false); err != nil {
		return err
	}
	if e := n.Evidence; e != nil {
		switch {
		case e.LineNo < r.Source.FirstLine || e.LineNo > r.Source.LastLine:
			return fmt.Errorf("evidence line %d is outside the source's lines", e.LineNo)
		case e.Line == "" || len(e.Line) > maxLine || !utf8.ValidString(e.Line):
			return fmt.Errorf("evidence line is 1 to %d bytes of UTF-8", maxLine)
		case len(e.Commits) > maxRefs || len(e.Approvals) > maxRefs || len(e.Reviews) > maxRefs:
			return fmt.Errorf("evidence keeps at most %d of each reference", maxRefs)
		}
	}
	return nil
}

// plain checks one string: present, at most max runes, no control character
// (a tab allowed where lines are).
func plain(at, s string, max int, tabs bool) error {
	switch {
	case strings.TrimSpace(s) == "":
		return fmt.Errorf("%s is empty", at)
	case !utf8.ValidString(s):
		return fmt.Errorf("%s is not UTF-8", at)
	case utf8.RuneCountInString(s) > max:
		return fmt.Errorf("%s is longer than %d characters", at, max)
	}
	for _, c := range s {
		if unicode.IsControl(c) && (!tabs || c != '\t') {
			return fmt.Errorf("%s has a control character %U", at, c)
		}
	}
	return nil
}
