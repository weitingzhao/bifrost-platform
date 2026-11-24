package planstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// A change of law in the plan (closed loop, Owner 2026-11-23: "only a change
// of law opens a ticket"; the plan domain owns the loop). The Owner approves a
// law_amend card (the approvals domain's): that card is the version of the
// law, and its id is the version of the law's standards. The plan records it
// as one item of the newest round, with one step per standard, so the work
// toward the law is planned, read and judged in the plan like any other.
//
// The Owner approved the card, so its item needs no second decision card
// (one thing is confirmed in one place, CLAUDE.md §5): the plan_changed
// events name the card's approval.card.executed event as their decision and
// cause. A step starts with no done rule: its standard is unmeasured until a
// reading is named for it.

// LawStandard is one measurable standard of the law, as the card states it.
type LawStandard struct {
	ID, Standard, Measure string
}

// LawCard is an executed law_amend card, as the approval store holds it.
type LawCard struct {
	ApprovalID string
	Number     int
	Op         string // add, amend or repeal
	Law        string // the law's number; empty for a new law
	After      string // the law's new text; empty for a repeal
	Before     string
	Standards  []LawStandard
	DecidedOn  string // the day the Owner approved it, YYYY-MM-DD
}

// lawNote marks the plan_changed event that recorded a card's law: a card is
// recorded once, whichever pass finds it first.
func lawNote(approvalID string) string { return "law_amend " + approvalID }

// LawTitle is the item's title: the law, the change and the version.
func LawTitle(c LawCard) string {
	law := "新法"
	if c.Law != "" {
		law = "法 " + c.Law
	}
	op := map[string]string{"add": "新增", "amend": "修改", "repeal": "废止"}[c.Op]
	text := c.After
	if text == "" {
		text = c.Before
	}
	return fmt.Sprintf("%s %s（标准版本 %s #%d）：%s", law, op, c.ApprovalID, c.Number, clip(firstLine(text), 120))
}

// LawStepTitle is a standard's step title: its id, the standard and how it
// is measured.
func LawStepTitle(s LawStandard) string {
	return fmt.Sprintf("%s：%s｜量法：%s", s.ID, clip(s.Standard, 200), clip(s.Measure, 200))
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// RecordLaws records each card not yet recorded and returns how many it
// recorded. A card whose executed event the ledger does not hold yet is left
// for a later pass.
func RecordLaws(ctx context.Context, db eventlog.DB, cards []LawCard) (int, error) {
	n := 0
	var errs []error
	for _, c := range cards {
		// The register first (lawregister.go): an added law's number goes
		// into its item's title.
		if err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
			if num, _, err := registerCard(ctx, tx, c); err != nil {
				return err
			} else if num != "" {
				c.Law = num
			}
			return nil
		}); err != nil {
			errs = append(errs, fmt.Errorf("law_amend %s: register: %w", c.ApprovalID, err))
		}
		if c.Op == "add" && c.Law == "" {
			if law, ok, err := lawByApproval(ctx, db, c.ApprovalID); err == nil && ok {
				c.Law = law
			}
		}
		ok, err := recordLaw(ctx, db, c)
		if err != nil {
			errs = append(errs, fmt.Errorf("law_amend %s: %w", c.ApprovalID, err))
			continue
		}
		if ok {
			n++
		}
	}
	return n, errors.Join(errs...)
}

func recordLaw(ctx context.Context, db eventlog.DB, c LawCard) (bool, error) {
	if c.ApprovalID == "" {
		return false, eventlog.Invalid("a law card names its approval")
	}
	recorded := false
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		var root string
		err := tx.QueryRow(ctx, `SELECT plan_node_id FROM agentrt.plan_node WHERE parent_plan_node_id IS NULL AND node_kind = 'round'
		  ORDER BY ordinal DESC LIMIT 1`).Scan(&root)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := LockTree(ctx, tx, root); err != nil {
			return err
		}
		if done, err := eventlog.HasBody(ctx, tx, "plan_changed", "", map[string]any{"note": lawNote(c.ApprovalID)}); err != nil || done {
			return err
		}
		executed, ok, err := eventlog.LatestWithField(ctx, tx, "approval.card.executed", "id", c.ApprovalID, time.Time{})
		if err != nil || !ok {
			return err
		}
		decided := executed.ID
		item, err := applyLawChanges(ctx, tx, root, decided, c.ApprovalID, []PlanChange{{Op: "add_node", Parent: root, Kind: "item", Title: LawTitle(c)}})
		if err != nil {
			return err
		}
		if len(c.Standards) > 0 {
			steps := make([]PlanChange, 0, len(c.Standards))
			for _, s := range c.Standards {
				steps = append(steps, PlanChange{Op: "add_node", Parent: item[0], Kind: "step", StepKind: "manual", Title: LawStepTitle(s)})
			}
			if _, err := applyLawChanges(ctx, tx, root, decided, c.ApprovalID, steps); err != nil {
				return err
			}
		}
		recorded = true
		return nil
	})
	return recorded, err
}

// applyLawChanges applies the changes under a plan_changed event noted as
// the card's.
func applyLawChanges(ctx context.Context, tx pgx.Tx, root string, decided int64, approvalID string, changes []PlanChange) ([]string, error) {
	if err := checkChanges(root, lawNote(approvalID), changes); err != nil {
		return nil, err
	}
	return applyChangesNoted(ctx, tx, "controller", root, decided, lawNote(approvalID), changes)
}

// lawByApproval is the number the register gave a card's law, if any.
func lawByApproval(ctx context.Context, db eventlog.DB, approvalID string) (string, bool, error) {
	laws, err := CurrentLaws(ctx, db)
	if err != nil {
		return "", false, err
	}
	for _, l := range laws {
		if l.ApprovalID == approvalID {
			return l.Law, true, nil
		}
	}
	return "", false, nil
}
