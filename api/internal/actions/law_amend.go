// Ownership (2026-11-23, ISSUE-12: legislation apart from administration;
// agreed by legislation and the approvals line): what a law_amend card says
// (its params, its check against the register of laws, its line) is
// legislation's, in agentrt/planstore (lawcard.go), moved there on
// 2026-11-24. This file keeps the gate the card borrows from the approvals
// line: the catalog id, and the hooks the assembly installs (SetLawCard).
// Without the hooks a card is refused: no law ticket stands alone.
package actions

import (
	"context"
	"errors"
	"strings"
)

// law_amend is the one kind of card the Owner approves for a change of law
// (LAW-6). The platform
// runs nothing: the approved card is that version of the law, kept in the
// approval store and never edited (a new version is a new card).
// approval.card.executed announces it with action law_amend; the plan
// domain reads the card and records the law and its standards.

// LawAmendAction is the catalog id.
const LawAmendAction = "law_amend"

// LawCard is what legislation lends the gate: Check refuses a card that may
// not be created; Summary is its line; Read is its params as read, recorded
// when it is approved.
type LawCard struct {
	Check   func(ctx context.Context, params map[string]any) error
	Summary func(ctx context.Context, params map[string]any) string
	Read    func(params map[string]any) (any, error)
}

var lawCard *LawCard

// SetLawCard installs legislation's rules; the assembly wires planstore.
func SetLawCard(c *LawCard) {
	actMu.Lock()
	lawCard = c
	actMu.Unlock()
}

func currentLawCard() *LawCard {
	actMu.RLock()
	defer actMu.RUnlock()
	return lawCard
}

// CheckLawAmend is the create check: legislation's, refused when it is not
// installed.
func CheckLawAmend(ctx context.Context, params map[string]any) error {
	c := currentLawCard()
	if c == nil || c.Check == nil {
		return errors.New("law_amend: legislation's card rules are not wired here; no card without them")
	}
	return c.Check(ctx, params)
}

// lawAmendSummary is the card's line.
func lawAmendSummary(ctx context.Context, params map[string]any) string {
	if c := currentLawCard(); c != nil && c.Summary != nil {
		return c.Summary(ctx, params)
	}
	op, _ := params["op"].(string)
	law, _ := params["law"].(string)
	return strings.TrimSpace("Law " + op + " " + law)
}

// ExecuteLawAmend records the approval; nothing else happens.
func ExecuteLawAmend(_ context.Context, params map[string]any) (any, error) {
	var read any = params
	if c := currentLawCard(); c != nil && c.Read != nil {
		var err error
		if read, err = c.Read(params); err != nil {
			return nil, err
		}
	}
	return map[string]any{"recorded": true, "executed_by_platform": false, "law_amend": read}, nil
}
