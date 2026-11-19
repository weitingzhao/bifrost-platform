package planstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/ownercard"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/planreadings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Ask creates the Owner's decision card for a plan change (ownercard.Ask:
// the contract with the Owner's questions, 2026-11-19).
type Ask = ownercard.Ask

// ErrAsk: the Owner could not be asked (no question store, or it refused
// the card: full, or over its hourly bound); nothing was written.
var ErrAsk = errors.New("store: the Owner cannot be asked now")

// Card is a decision card for the Owner (ownercard.Card).
type Card = ownercard.Card

// Options of a plan change's card and their keys (ownercard).
const (
	CardApply     = ownercard.CardApply
	CardRefuse    = ownercard.CardRefuse
	CardApplyKey  = ownercard.CardApplyKey
	CardRefuseKey = ownercard.CardRefuseKey
)

// ProposePlanChange records a proposal and, within the standing
// authorization, its decision and the change; otherwise it tries the change
// (and rolls it back), asks the Owner and records which question it is. The
// idempotency key is the writer's.
func ProposePlanChange(ctx context.Context, db eventlog.DB, w actor.Writer, key, root, reason string, changes []PlanChange, ask Ask) (map[string]any, error) {
	if key == "" {
		return nil, eventlog.Invalid("an idempotency key is required")
	}
	if err := checkChanges(root, reason, changes); err != nil {
		return nil, err
	}
	doc, err := ChangesDoc(changes)
	if err != nil {
		return nil, err
	}
	req := map[string]any{"root": root, "reason": reason, "changes": doc}
	for try := 1; ; try++ {
		out, err := proposePlanChange(ctx, db, w, key, root, reason, changes, doc, req, ask)
		if !retryable(err) {
			return out, err
		}
		if try == busyTries {
			return nil, fmt.Errorf("%w: plan %s: %v", ErrBusy, root, err)
		}
		time.Sleep(time.Duration(try*try) * 20 * time.Millisecond)
	}
}

func proposePlanChange(ctx context.Context, db eventlog.DB, w actor.Writer, key, root, reason string, changes []PlanChange, doc any,
	req map[string]any, ask Ask) (map[string]any, error) {
	return actor.IdempotentAs(ctx, db, w, "plan/proposals", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := actor.Ensure(ctx, tx, w, true); err != nil {
			return nil, err
		}
		if err := lockPlanViewChanges(ctx, tx, root, changes); err != nil {
			return nil, err
		}
		if err := lockChanges(ctx, tx, root, changes); err != nil {
			return nil, err
		}
		var rootKind string
		if err := tx.QueryRow(ctx, `SELECT node_kind FROM agentrt.plan_node WHERE plan_node_id = $1 AND parent_plan_node_id IS NULL`, root).Scan(&rootKind); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, eventlog.Invalid("%s is not the root of a plan", root)
			}
			return nil, err
		}
		// The stream's lock before the rule is read, so a standing decision
		// is made under a rule still in force when it is written.
		if err := eventlog.LockStream(ctx, tx, PlanStream(root)); err != nil {
			return nil, err
		}
		rule, err := planRuleInForce(ctx, tx)
		if err != nil {
			return nil, err
		}
		within, byRule := true, false
		for _, c := range changes {
			ok, serr := standing(ctx, tx, c, rule)
			if serr != nil {
				return nil, serr
			}
			within = within && ok
			byRule = byRule || c.Op == "set_done_rule"
		}
		by := "standing authorization"
		if byRule && rule != nil {
			by = fmt.Sprintf("standing authorization (%s v%d)", rule.ID, rule.Version)
		}
		body := map[string]any{"changes": doc, "reason": reason, "standing": within, "token": w.Token}
		if within {
			proposed, err := appendPlan(ctx, tx, eventlog.Event{Stream: PlanStream(root), Type: "plan_change_proposed", Actor: w.Actor, Body: body})
			if err != nil {
				return nil, err
			}
			decided, err := appendPlan(ctx, tx, eventlog.Event{Stream: PlanStream(root), Type: "plan_change_decided", Actor: "controller",
				Body: map[string]any{"proposal_event_id": proposed.EventID, "decision": "applied", "by": by}})
			if err != nil {
				return nil, err
			}
			ids, err := applyChanges(ctx, tx, "controller", root, decided.EventID, changes)
			if err != nil {
				return nil, err
			}
			return map[string]any{"proposal_event_id": proposed.EventID, "decision": "applied", "nodes": nonNil(ids)}, nil
		}
		// Outside it: tried first, with the database's deferred checks run
		// now, and rolled back; a change the database refuses is refused here.
		if err := tryChanges(ctx, tx, root, changes); err != nil {
			return nil, err
		}
		// The question store cuts the title and checks the body by its own
		// rules (review 182 B2); a card that does not fit comes back invalid.
		card := Card{Key: CanonKey(w.Actor, key), Title: "Plan change to " + root + ": " + firstLine(reason),
			Body: cardBody(root, reason, changes)}
		if card.ByRule, err = RuleAnswers(ctx, tx, CardPlanChange, ""); err != nil {
			return nil, err
		}
		// What finishes a plan step is the Owner's to adopt: a view that binds
		// completion atoms is never answered by a rule (PV2 review 2026-10-26;
		// cardFor refuses it too).
		if card.ByRule {
			binds, e := adoptsBindings(ctx, tx, changes)
			if e != nil {
				return nil, e
			}
			card.ByRule = !binds
		}
		if ask == nil {
			return nil, fmt.Errorf("%w: no question store is configured", ErrAsk)
		}
		q, err := ask(ctx, card)
		if errors.Is(err, eventlog.ErrInvalid) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAsk, err)
		}
		body["question_id"] = q
		proposed, err := appendPlan(ctx, tx, eventlog.Event{Stream: PlanStream(root), Type: "plan_change_proposed", Actor: w.Actor, Body: body})
		if err != nil {
			return nil, err
		}
		return map[string]any{"proposal_event_id": proposed.EventID, "decision": "", "question_id": q,
			"waiting": "the Owner decides plan changes outside the standing authorization"}, nil
	})
}

// tryChanges applies the changes in a savepoint with every deferred check
// made at once, then rolls them back. Rolling back to the savepoint also
// undoes SET CONSTRAINTS (measured on PostgreSQL 17), so the rest of the
// transaction checks as before.
func tryChanges(ctx context.Context, tx pgx.Tx, root string, changes []PlanChange) error {
	if _, err := tx.Exec(ctx, `SAVEPOINT try_plan_change`); err != nil {
		return err
	}
	_, err := applyChanges(ctx, tx, "controller", root, 0, changes)
	if err == nil {
		_, err = tx.Exec(ctx, `SET CONSTRAINTS ALL IMMEDIATE`)
	}
	if _, rerr := tx.Exec(ctx, `ROLLBACK TO SAVEPOINT try_plan_change`); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

func CanonKey(actor, key string) string {
	sum, _ := canon.Sum(map[string]any{"actor": actor, "key": key, "route": "plan/proposals"})
	return strings.TrimPrefix(sum, "sha256:")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func cardBody(root, reason string, changes []PlanChange) string {
	items := make([]any, 0, len(changes))
	for _, c := range changes {
		items = append(items, c)
	}
	return planreadings.ChangeCardBody(root, reason, items)
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
