package planstore

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	case "repeal_many":
		if la.Law != "" || la.Before != "" || la.After != "" || params["standards"] != nil {
			return la, errors.New("law_amend repeal_many: law, before, after and standards stay empty; name each law in repeals")
		}
		raw, _ := params["repeals"].([]any)
		if len(raw) == 0 || len(raw) > lawCardMaxRepeals {
			return la, fmt.Errorf("law_amend repeal_many: repeals names 1 to %d laws", lawCardMaxRepeals)
		}
		seen := map[string]bool{}
		for i, r := range raw {
			m, ok := r.(map[string]any)
			if !ok {
				return la, fmt.Errorf("law_amend repeal_many: repeals[%d] must be an object", i)
			}
			f := func(k string) string { v, _ := m[k].(string); return strings.TrimSpace(v) }
			rp := LawRepeal{Law: f("law"), Before: f("before"), Reason: f("reason")}
			if rp.Law == "" || rp.Before == "" || rp.Reason == "" {
				return la, fmt.Errorf("law_amend repeal_many: repeals[%d] needs law, before and reason", i)
			}
			if len(rp.Before) > lawCardMaxText || len(rp.Reason) > lawCardMaxText {
				return la, fmt.Errorf("law_amend repeal_many: repeals[%d] is too long", i)
			}
			if seen[rp.Law] {
				return la, fmt.Errorf("law_amend repeal_many: law %s appears twice", rp.Law)
			}
			seen[rp.Law] = true
			la.Repeals = append(la.Repeals, rp)
		}
		if la.Revert == "" {
			return la, errors.New("law_amend: revert (how to undo it) is required")
		}
		return la, nil
	default:
		return la, fmt.Errorf("law_amend: op must be add, amend, repeal or repeal_many, not %q", la.Op)
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
	if op == "repeal_many" {
		if la, err := ReadLawAmend(params); err == nil {
			ns := make([]string, 0, len(la.Repeals))
			for _, rp := range la.Repeals {
				ns = append(ns, rp.Law)
			}
			return fmt.Sprintf("Law repeal ×%d: %s", len(ns), strings.Join(ns, ", "))
		}
		return "Law repeal (several)"
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
