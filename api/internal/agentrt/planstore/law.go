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
	ID       string `json:"id"`
	Standard string `json:"standard"`
	Measure  string `json:"measure"`
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
	Repeals    []LawRepeal // op repeal_many: the laws it repeals; op purge: the repealed laws it deletes
	Frame      *LawFrame   // the framework's fields of the new version
	Adds       []LawCard   // op add_many: the laws it adds, in order
	DecidedOn  string      // the day the Owner approved it, YYYY-MM-DD
}

// lawNote marks the plan_changed event that recorded a card's law: a card is
// recorded once, whichever pass finds it first.
func lawNote(approvalID string) string { return "law_amend " + approvalID }

// LawTitle is the item's title: the law, the change and the version.
func LawTitle(c LawCard) string {
	if c.Op == "add_many" {
		return fmt.Sprintf("新增 %d 条法（标准版本 %s #%d）", len(c.Adds), c.ApprovalID, c.Number)
	}
	if c.Op == "repeal_many" || c.Op == "purge" || c.Op == "archive" {
		ns := make([]string, 0, len(c.Repeals))
		for _, rp := range c.Repeals {
			ns = append(ns, rp.Law)
		}
		return fmt.Sprintf("废止 %d 条法（标准版本 %s #%d）：%s", len(ns), c.ApprovalID, c.Number, strings.Join(ns, "、"))
	}
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
		// A repealed law leaves the plan too: the steps of its items that are
		// not done are dropped, so it is no longer measured or enforced.
		if c.Op == "repeal" || c.Op == "repeal_many" || c.Op == "archive" {
			if err := dropRepealedSteps(ctx, db, c); err != nil {
				errs = append(errs, fmt.Errorf("law_amend %s: plan: %w", c.ApprovalID, err))
			}
		}
		// Only an enforced law goes into the plan to be measured: a framed
		// law enacted but not enforced, and a batch repeal, do not. A card
		// from before the framework (no frame) keeps its item.
		if c.Op == "repeal_many" || c.Op == "purge" || c.Op == "add_many" || c.Op == "archive" || (c.Frame != nil && c.Frame.Enforce != EnforceOn) {
			continue
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

// refuseLawStandardEdits refuses a plan change that retitles, drops or
// re-marks a standard of an approved law (closed loop, standard 3: the line
// that does the work cannot change an approved standard). A law's item is
// the one the laws pass made, titled with its version (「标准版本 appr_…」);
// its steps are its standards, and they change only by a new law_amend card
// (a new version, a new item). A done rule may still be set on such a step:
// that names the reading the standard is measured by, not the standard.
func refuseLawStandardEdits(ctx context.Context, tx pgx.Tx, changes []PlanChange) error {
	for _, c := range changes {
		if c.Op != "retitle" && c.Op != "drop_step" && c.Op != "set_marks" && c.Op != "rebind_step" {
			continue
		}
		var lawItem bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM agentrt.plan_node s JOIN agentrt.plan_node i ON i.plan_node_id = s.parent_plan_node_id
		  WHERE s.plan_node_id = $1 AND i.node_kind = 'item' AND strpos(i.title, '（标准版本 appr_') > 0)`, c.Node).Scan(&lawItem); err != nil {
			return err
		}
		if lawItem {
			return eventlog.Invalid("%s is a standard of an approved law: it changes only by a law_amend card (%s refused)", c.Node, c.Op)
		}
	}
	return nil
}

// dropRepealedSteps drops the steps not done of the plan items of the laws a
// card repeals (Owner 2026-11-24: a repealed law must not linger as poison).
// A law's items are the ones the laws pass made for its versions, found by
// the approval ids recorded on its stream; the drop is noted as the card's,
// so it passes the lock on a law's standards (refuseLawStandardEdits).
func dropRepealedSteps(ctx context.Context, db eventlog.DB, c LawCard) error {
	var laws []string
	if c.Op == "repeal" {
		laws = []string{c.Law}
	}
	for _, rp := range c.Repeals {
		laws = append(laws, rp.Law)
	}
	return eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
		executed, ok, err := eventlog.LatestWithField(ctx, tx, "approval.card.executed", "id", c.ApprovalID, time.Time{})
		if err != nil || !ok {
			return err
		}
		versions, err := eventlog.LatestByKey(ctx, tx, "law_recorded", "approval_id", time.Time{})
		if err != nil {
			return err
		}
		var ids []string
		for _, v := range versions {
			for _, n := range laws {
				if v.Stream == LawStream(n) {
					if a, _ := v.Body["approval_id"].(string); a != "" && a != c.ApprovalID {
						ids = append(ids, a)
					}
				}
			}
		}
		for _, id := range ids {
			rows, err := tx.Query(ctx, `SELECT s.plan_node_id, i.parent_plan_node_id FROM agentrt.plan_node s
			  JOIN agentrt.plan_node i ON i.plan_node_id = s.parent_plan_node_id
			  WHERE i.node_kind = 'item' AND strpos(i.title, $1) > 0 AND s.node_kind = 'step' AND s.state IN ('planned', 'active')`, id)
			if err != nil {
				return err
			}
			byRoot := map[string][]PlanChange{}
			for rows.Next() {
				var step, root string
				if err := rows.Scan(&step, &root); err != nil {
					rows.Close()
					return err
				}
				byRoot[root] = append(byRoot[root], PlanChange{Op: "drop_step", Node: step})
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			for root, changes := range byRoot {
				if err := LockTree(ctx, tx, root); err != nil {
					return err
				}
				if _, err := applyChangesNoted(ctx, tx, "controller", root, executed.ID, lawNote(c.ApprovalID)+" repeal", changes); err != nil {
					return err
				}
			}
		}
		return nil
	})
}
