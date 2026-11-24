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
	Revert    string        `json:"revert"`
}

// ReadLawAmend reads and checks normalized params; the error says what is
// wrong, before a card is written.
func ReadLawAmend(params map[string]any) (LawAmend, error) {
	s := func(k string) string { v, _ := params[k].(string); return strings.TrimSpace(v) }
	la := LawAmend{Op: s("op"), Law: s("law"), Before: s("before"), After: s("after"), Revert: s("revert")}
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
	default:
		return la, fmt.Errorf("law_amend: op must be add, amend or repeal, not %q", la.Op)
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
	law, ok, err := reg.Current(ctx, la.Law)
	if err != nil {
		return fmt.Errorf("law_amend: reading the register: %w", err)
	}
	if !ok {
		return fmt.Errorf("law_amend %s: law %s is not in the register", la.Op, la.Law)
	}
	if law.Status == RepealedStatus {
		return fmt.Errorf("law_amend %s: law %s is already repealed", la.Op, la.Law)
	}
	if strings.TrimSpace(law.Text) != la.Before {
		return fmt.Errorf("law_amend %s: before is not law %s as it stands; it reads: %s", la.Op, la.Law, strings.TrimSpace(law.Text))
	}
	return nil
}

// LawCardSummary is the card's line; an add names the number it will take.
func LawCardSummary(ctx context.Context, reg LawLookup, params map[string]any) string {
	op, _ := params["op"].(string)
	law, _ := params["law"].(string)
	if op == "add" {
		if reg.Next != nil {
			if next, err := reg.Next(ctx); err == nil && next != "" {
				return "Law add: will become law " + next
			}
		}
		return "Law add"
	}
	return strings.TrimSpace("Law " + op + " " + law)
}
