package planstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// The law_amend card's content rules (legislation, Owner 2026-11-23: law
// tickets apart from the tickets administration executes). The approvals
// domain keeps the gate the card goes through (its catalog entry, the
// Owner's approval, expiry, audit, the law track and the executed event) and
// calls these through the hooks the assembly installs (actions.SetLawCard):
// what a card's params are, whether a card may be created against the
// register, and the card's line. Moved from actions/law_amend.go on
// 2026-11-24, unchanged in behaviour.
//
// A law_amend card adds, amends or repeals one law and names the standards it
// is measured by. The approved card is that version of the law; its id is
// the standards' version (law.go, lawregister.go).

// The bounds of one card's text.
const (
	lawCardMaxText      = 8 << 10
	lawCardMaxStandards = 20
)

// LawAmend is a law_amend card's params, read.
type LawAmend struct {
	Op        string        `json:"op"`
	Law       string        `json:"law,omitempty"`
	Before    string        `json:"before,omitempty"`
	After     string        `json:"after,omitempty"`
	Standards []LawStandard `json:"standards,omitempty"`
	Repeals   []LawRepeal   `json:"repeals,omitempty"`
	Frame     *LawFrame     `json:"frame,omitempty"`
	Revert    string        `json:"revert"`
	// Adds are the laws an add_many card adds, each read as an add card.
	Adds []LawAmend `json:"adds,omitempty"`
}

// AddCards are an add_many card's laws as the plan's cards, numbered in
// order when the register takes them.
func (la LawAmend) AddCards() []LawCard {
	out := make([]LawCard, 0, len(la.Adds))
	for _, a := range la.Adds {
		out = append(out, LawCard{Op: "add", After: a.After, Standards: a.Standards, Frame: a.Frame})
	}
	return out
}

// LawRepeal is one law a repeal_many card repeals (2026-11-24: the Owner
// asked to clear the register's duplicates, superseded laws and entries that
// are not laws; one card for a batch, each law with its text as it stands
// and why it goes).
type LawRepeal struct {
	Law    string `json:"law"`
	Before string `json:"before"`
	Reason string `json:"reason"`
}

const lawCardMaxRepeals = 30

// The bounds of a batch (2026-11-24, Owner: the laws distilled from the
// material go into the framework as one batch, and the material is archived
// once the laws cover it). The approvals store is one ConfigMap (about 900
// KiB), so an archive card names numbers and reasons only: whether each is
// covered is the register's to say, not a quoted text's.
const (
	lawCardMaxAdds    = 15
	lawCardMaxArchive = 100
)

// ReadLawAmend reads and checks normalized params; the error says what is
// wrong, before a card is written.
func ReadLawAmend(params map[string]any) (LawAmend, error) {
	s := func(k string) string { v, _ := params[k].(string); return strings.TrimSpace(v) }
	la := LawAmend{Op: s("op"), Law: s("law"), Before: s("before"), After: s("after"), Revert: s("revert")}
	if la.Op == "add" || la.Op == "amend" {
		f, err := readLawFrame(params["frame"])
		if err != nil {
			return la, err
		}
		la.Frame = f
	}
	switch la.Op {
	case "add_many":
		return readAddMany(params, la)
	case "archive":
		return readArchive(params, la)
	case "add":
		if la.Before != "" {
			return la, errors.New("law_amend add: before must be empty")
		}
		if la.After == "" {
			return la, errors.New("law_amend add: after (the new law's text) is required")
		}
	case "amend":
		if la.Law == "" || la.Before == "" || la.After == "" {
			return la, errors.New("law_amend amend: law, before and after are required")
		}
	case "repeal":
		if la.Law == "" || la.Before == "" {
			return la, errors.New("law_amend repeal: law and before are required")
		}
		if la.After != "" {
			return la, errors.New("law_amend repeal: after must be empty")
		}
	case "repeal_many", "purge":
		if la.Law != "" || la.Before != "" || la.After != "" || params["standards"] != nil {
			return la, fmt.Errorf("law_amend %s: law, before, after and standards stay empty; name each law in repeals", la.Op)
		}
		raw, _ := params["repeals"].([]any)
		if len(raw) == 0 || len(raw) > lawCardMaxRepeals {
			return la, fmt.Errorf("law_amend %s: repeals names 1 to %d laws", la.Op, lawCardMaxRepeals)
		}
		seen := map[string]bool{}
		for i, r := range raw {
			m, ok := r.(map[string]any)
			if !ok {
				return la, fmt.Errorf("law_amend %s: repeals[%d] must be an object", la.Op, i)
			}
			f := func(k string) string { v, _ := m[k].(string); return strings.TrimSpace(v) }
			rp := LawRepeal{Law: f("law"), Before: f("before"), Reason: f("reason")}
			if rp.Law == "" || rp.Before == "" || rp.Reason == "" {
				return la, fmt.Errorf("law_amend %s: repeals[%d] needs law, before and reason", la.Op, i)
			}
			if len(rp.Before) > lawCardMaxText || len(rp.Reason) > lawCardMaxText {
				return la, fmt.Errorf("law_amend %s: repeals[%d] is too long", la.Op, i)
			}
			if seen[rp.Law] {
				return la, fmt.Errorf("law_amend %s: law %s appears twice", la.Op, rp.Law)
			}
			seen[rp.Law] = true
			la.Repeals = append(la.Repeals, rp)
		}
		if la.Revert == "" {
			return la, errors.New("law_amend: revert (how to undo it) is required")
		}
		return la, nil
	default:
		return la, fmt.Errorf("law_amend: op must be add, amend, repeal, repeal_many, purge, add_many or archive, not %q", la.Op)
	}
	if la.Revert == "" {
		return la, errors.New("law_amend: revert (how to undo it) is required")
	}
	for _, k := range []string{"before", "after", "revert"} {
		if len(s(k)) > lawCardMaxText {
			return la, fmt.Errorf("law_amend: %s is at most %d bytes", k, lawCardMaxText)
		}
	}
	raw, _ := params["standards"].([]any)
	if la.Op != "repeal" && len(raw) == 0 {
		return la, errors.New("law_amend: add and amend name at least one standard")
	}
	if len(raw) > lawCardMaxStandards {
		return la, fmt.Errorf("law_amend: at most %d standards", lawCardMaxStandards)
	}
	seen := map[string]bool{}
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return la, fmt.Errorf("law_amend: standards[%d] must be an object", i)
		}
		f := func(k string) string { v, _ := m[k].(string); return strings.TrimSpace(v) }
		st := LawStandard{ID: f("id"), Standard: f("standard"), Measure: f("measure")}
		if st.ID == "" || st.Standard == "" || st.Measure == "" {
			return la, fmt.Errorf("law_amend: standards[%d] needs id, standard and measure", i)
		}
		if seen[st.ID] {
			return la, fmt.Errorf("law_amend: standard id %q appears twice", st.ID)
		}
		seen[st.ID] = true
		la.Standards = append(la.Standards, st)
	}
	return la, nil
}

// readAddMany reads an add_many card: law, before, after, standards and frame
// stay empty; adds lists 1 to lawCardMaxAdds laws, each read as an add card
// (after, standards, frame). A batch only enacts: none of its laws is
// enforced yet (framework: enforcement turns on law by law).
func readAddMany(params map[string]any, la LawAmend) (LawAmend, error) {
	if la.Law != "" || la.Before != "" || la.After != "" || params["standards"] != nil || params["frame"] != nil {
		return la, errors.New("law_amend add_many: law, before, after, standards and frame stay empty; put each law in adds")
	}
	if la.Revert == "" {
		return la, errors.New("law_amend: revert (how to undo it) is required")
	}
	raw, _ := params["adds"].([]any)
	if len(raw) == 0 || len(raw) > lawCardMaxAdds {
		return la, fmt.Errorf("law_amend add_many: adds names 1 to %d laws", lawCardMaxAdds)
	}
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return la, fmt.Errorf("law_amend add_many: adds[%d] must be an object", i)
		}
		one, err := ReadLawAmend(map[string]any{"op": "add", "after": m["after"], "standards": m["standards"], "frame": m["frame"], "revert": la.Revert})
		if err != nil {
			return la, fmt.Errorf("law_amend add_many: adds[%d]: %w", i, err)
		}
		if one.Frame != nil && one.Frame.Enforce == EnforceOn {
			return la, fmt.Errorf("law_amend add_many: adds[%d] is enforced; a batch only enacts, enforcement turns on law by law", i)
		}
		la.Adds = append(la.Adds, one)
	}
	return la, nil
}

// readArchive reads an archive card: repeals lists the material it retires,
// each {law, reason}; law, before, after and standards stay empty.
func readArchive(params map[string]any, la LawAmend) (LawAmend, error) {
	if la.Law != "" || la.Before != "" || la.After != "" || params["standards"] != nil {
		return la, errors.New("law_amend archive: law, before, after and standards stay empty; name each law in repeals")
	}
	if la.Revert == "" {
		return la, errors.New("law_amend: revert (how to undo it) is required")
	}
	raw, _ := params["repeals"].([]any)
	if len(raw) == 0 || len(raw) > lawCardMaxArchive {
		return la, fmt.Errorf("law_amend archive: repeals names 1 to %d laws", lawCardMaxArchive)
	}
	seen := map[string]bool{}
	for i, r := range raw {
		m, ok := r.(map[string]any)
		if !ok {
			return la, fmt.Errorf("law_amend archive: repeals[%d] must be an object", i)
		}
		f := func(k string) string { v, _ := m[k].(string); return strings.TrimSpace(v) }
		rp := LawRepeal{Law: f("law"), Reason: f("reason")}
		if rp.Law == "" || rp.Reason == "" || len(rp.Reason) > lawCardMaxText {
			return la, fmt.Errorf("law_amend archive: repeals[%d] needs law and a reason", i)
		}
		if seen[rp.Law] {
			return la, fmt.Errorf("law_amend archive: law %s appears twice", rp.Law)
		}
		seen[rp.Law] = true
		la.Repeals = append(la.Repeals, rp)
	}
	return la, nil
}

// LawLookup reads the register for a card: Current is law n as it stands;
// Next the number an added law takes ("" while the register is empty).
type LawLookup struct {
	Current func(ctx context.Context, n string) (Law, bool, error)
	Next    func(ctx context.Context) (string, error)
	All     func(ctx context.Context) ([]Law, error)
}

// DBLawLookup reads the register in db (lawregister.go).
func DBLawLookup(db func() (eventlog.DB, error)) LawLookup {
	return LawLookup{
		Current: func(ctx context.Context, n string) (Law, bool, error) {
			d, err := db()
			if err != nil {
				return Law{}, false, err
			}
			return CurrentLaw(ctx, d, n)
		},
		Next: func(ctx context.Context) (string, error) {
			d, err := db()
			if err != nil {
				return "", err
			}
			return NextLawNumber(ctx, d)
		},
		All: func(ctx context.Context) ([]Law, error) {
			d, err := db()
			if err != nil {
				return nil, err
			}
			return CurrentLaws(ctx, d)
		},
	}
}

// CheckLawCard is the create check. Owner 2026-11-23: a ticket changes a real
// record of the law, never stands alone. So a card is refused unless it
// reads, the register answers, an amend or repeal names a law in force there
// and quotes its text as it stands, and an add has a number to take. When
// the register cannot be read the card is refused, not let through.
func CheckLawCard(ctx context.Context, reg LawLookup, params map[string]any) error {
	la, err := ReadLawAmend(params)
	if err != nil {
		return err
	}
	if reg.Current == nil || reg.Next == nil {
		return errors.New("law_amend: the register of laws is not available here; no card without it")
	}
	// The framework (lawframework.go): its findings are notes on the card
	// (LawCardSummary); only a malformed frame or a conflict with a
	// fundamental law refuses it.
	if la.Op == "add" || la.Op == "amend" {
		self := ""
		if la.Op == "amend" {
			self = la.Law
		}
		if _, err := FrameNotes(ctx, reg, self, la.Frame, len(la.Standards)); err != nil {
			return err
		}
	}
	if la.Op == "add_many" {
		for i, a := range la.Adds {
			if _, err := FrameNotes(ctx, reg, "", a.Frame, len(a.Standards)); err != nil {
				return fmt.Errorf("adds[%d]: %w", i, err)
			}
		}
		next, err := reg.Next(ctx)
		if err != nil {
			return fmt.Errorf("law_amend: reading the register: %w", err)
		}
		if next == "" {
			return errors.New("law_amend add_many: the register is empty (not transcribed yet), so the new laws have no numbers")
		}
		return nil
	}
	if la.Op == "archive" {
		return checkArchive(ctx, reg, la.Repeals)
	}
	if la.Op == "add" {
		next, err := reg.Next(ctx)
		if err != nil {
			return fmt.Errorf("law_amend: reading the register: %w", err)
		}
		if next == "" {
			return errors.New("law_amend add: the register is empty (not transcribed yet), so the new law has no number")
		}
		if la.Law != "" && la.Law != next {
			return fmt.Errorf("law_amend add: the new law will be number %s, not %s", next, la.Law)
		}
		return nil
	}
	if la.Op == "repeal_many" {
		for _, rp := range la.Repeals {
			if err := checkInForce(ctx, reg, la.Op, rp.Law, rp.Before); err != nil {
				return err
			}
		}
		return nil
	}
	if la.Op == "purge" {
		for _, rp := range la.Repeals {
			if err := checkRepealed(ctx, reg, rp.Law, rp.Before); err != nil {
				return err
			}
		}
		return nil
	}
	return checkInForce(ctx, reg, la.Op, la.Law, la.Before)
}

// checkInForce says whether law n is in the register, not repealed, and
// reads as before.
func checkInForce(ctx context.Context, reg LawLookup, op, n, before string) error {
	law, ok, err := reg.Current(ctx, n)
	if err != nil {
		return fmt.Errorf("law_amend: reading the register: %w", err)
	}
	if !ok {
		return fmt.Errorf("law_amend %s: law %s is not in the register", op, n)
	}
	if law.Status == RepealedStatus {
		return fmt.Errorf("law_amend %s: law %s is already repealed", op, n)
	}
	if strings.TrimSpace(law.Text) != before {
		return fmt.Errorf("law_amend %s: before is not law %s as it stands; it reads: %s", op, n, strings.TrimSpace(law.Text))
	}
	return nil
}

// checkArchive says whether each law is material that a law in force
// absorbs: only covered material is archived.
func checkArchive(ctx context.Context, reg LawLookup, repeals []LawRepeal) error {
	if reg.All == nil {
		return errors.New("law_amend archive: the register cannot be listed here, so coverage cannot be checked")
	}
	laws, err := reg.All(ctx)
	if err != nil {
		return fmt.Errorf("law_amend: reading the register: %w", err)
	}
	now := time.Now()
	by := map[string]Law{}
	for _, l := range laws {
		by[l.Law] = l
	}
	absorbed := AbsorbedBy(laws, now)
	for _, rp := range repeals {
		l, ok := by[rp.Law]
		if !ok {
			return fmt.Errorf("law_amend archive: law %s is not in the register", rp.Law)
		}
		if LawState(l, now) != StateMaterial {
			return fmt.Errorf("law_amend archive: law %s is not material; archive retires material only", rp.Law)
		}
		if len(absorbed[rp.Law]) == 0 {
			return fmt.Errorf("law_amend archive: no law in force absorbs material %s yet", rp.Law)
		}
	}
	return nil
}

// checkRepealed says whether law n is in the register, repealed, and reads
// as before: only a repealed law is deleted (op purge, lawpurge.go).
func checkRepealed(ctx context.Context, reg LawLookup, n, before string) error {
	law, ok, err := reg.Current(ctx, n)
	if err != nil {
		return fmt.Errorf("law_amend: reading the register: %w", err)
	}
	if !ok {
		return fmt.Errorf("law_amend purge: law %s is not in the register", n)
	}
	if law.Status != RepealedStatus {
		return fmt.Errorf("law_amend purge: law %s is not repealed; repeal it first", n)
	}
	if strings.TrimSpace(law.Text) != before {
		return fmt.Errorf("law_amend purge: before is not law %s as it stands; it reads: %s", n, strings.TrimSpace(law.Text))
	}
	return nil
}

// LawCardSummary is the card's line; an add names the number it will take.
func LawCardSummary(ctx context.Context, reg LawLookup, params map[string]any) string {
	op, _ := params["op"].(string)
	law, _ := params["law"].(string)
	if op == "add" || op == "amend" {
		line := strings.TrimSpace("Law " + op + " " + law)
		if op == "add" {
			line = "Law add"
			if reg.Next != nil {
				if next, err := reg.Next(ctx); err == nil && next != "" {
					line = "Law add: will become law " + next
				}
			}
		}
		return line + lawCardNoteLine(ctx, reg, params)
	}
	if op == "add_many" {
		la, err := ReadLawAmend(params)
		if err != nil {
			return "Law add (several)"
		}
		line := fmt.Sprintf("Law add ×%d", len(la.Adds))
		if reg.Next != nil {
			if next, err := reg.Next(ctx); err == nil && next != "" {
				first := lawNumber(next)
				line += fmt.Sprintf(": will become laws %d–%d", first, first+len(la.Adds)-1)
			}
		}
		var notes []string
		for i, a := range la.Adds {
			ns, _ := FrameNotes(ctx, reg, "", a.Frame, len(a.Standards))
			if len(ns) > 0 {
				notes = append(notes, fmt.Sprintf("第 %d 条：%s", i+1, strings.Join(ns, "，")))
			}
		}
		notes = append(notes, "批了只立法，未执行")
		return line + clip(" ｜框架："+strings.Join(notes, "；"), 600)
	}
	if op == "repeal_many" || op == "purge" || op == "archive" {
		verb := map[string]string{"repeal_many": "repeal", "purge": "purge (delete for good)", "archive": "archive (material a law absorbs)"}[op]
		if la, err := ReadLawAmend(params); err == nil {
			ns := make([]string, 0, len(la.Repeals))
			for _, rp := range la.Repeals {
				ns = append(ns, rp.Law)
			}
			return fmt.Sprintf("Law %s ×%d: %s", verb, len(ns), strings.Join(ns, ", "))
		}
		return "Law " + verb + " (several)"
	}
	return strings.TrimSpace("Law " + op + " " + law)
}

// LawCardNotes is what the framework finds about a card (FrameNotes), for
// the card's line; nil for a card that is not an add or an amend.
func LawCardNotes(ctx context.Context, reg LawLookup, params map[string]any) []string {
	la, err := ReadLawAmend(params)
	if err != nil || (la.Op != "add" && la.Op != "amend") {
		return nil
	}
	self := ""
	if la.Op == "amend" {
		self = la.Law
	}
	notes, _ := FrameNotes(ctx, reg, self, la.Frame, len(la.Standards))
	if la.Frame != nil && la.Frame.Enforce == EnforceOn {
		notes = append(notes, "批了即开始执法：进计划、审计量、接检查")
	} else if la.Frame != nil {
		notes = append(notes, "批了只立法，未执行")
	}
	return notes
}

func lawCardNoteLine(ctx context.Context, reg LawLookup, params map[string]any) string {
	notes := LawCardNotes(ctx, reg, params)
	if len(notes) == 0 {
		return ""
	}
	return clip(" ｜框架："+strings.Join(notes, "；"), 400)
}
