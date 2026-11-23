package actions

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// law_amend is the one kind of card the Owner approves for a change of law
// (Owner 2026-11-23: "only a change of law opens a ticket"). It adds, amends
// or repeals one law and names the standards by which the law is measured.
// The platform runs nothing: the approved card is that version of the law,
// kept in the approval store and never edited (a new version is a new card).
// approval.card.executed announces it with action law_amend; the plan
// domain's subscriber reads the card at its ref and records the law and its
// standards, with the approval id as the standards' version.

// LawAmendAction is the catalog id.
const LawAmendAction = "law_amend"

// The bounds of one card's text.
const (
	lawAmendMaxText      = 8 << 10
	lawAmendMaxStandards = 20
)

// LawStandard is one measurable standard of a law; Audit judges each by id.
type LawStandard struct {
	ID       string `json:"id"`
	Standard string `json:"standard"`
	Measure  string `json:"measure"`
}

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
		if len(s(k)) > lawAmendMaxText {
			return la, fmt.Errorf("law_amend: %s is at most %d bytes", k, lawAmendMaxText)
		}
	}
	raw, _ := params["standards"].([]any)
	if la.Op != "repeal" && len(raw) == 0 {
		return la, errors.New("law_amend: add and amend name at least one standard")
	}
	if len(raw) > lawAmendMaxStandards {
		return la, fmt.Errorf("law_amend: at most %d standards", lawAmendMaxStandards)
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

// CheckLawAmend is the create check: a card that does not read is refused.
func CheckLawAmend(_ context.Context, params map[string]any) error {
	_, err := ReadLawAmend(params)
	return err
}

// ExecuteLawAmend records the approval; nothing else happens.
func ExecuteLawAmend(_ context.Context, params map[string]any) (any, error) {
	la, err := ReadLawAmend(params)
	if err != nil {
		return nil, err
	}
	return map[string]any{"recorded": true, "executed_by_platform": false, "law_amend": la}, nil
}
