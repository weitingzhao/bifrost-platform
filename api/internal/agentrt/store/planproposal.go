package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/rules"
)

// Ask creates the Owner's decision card for a plan change and returns its
// question id. key makes a retry find the card it created.
type Ask func(ctx context.Context, card Card) (string, error)

// ErrAsk: the Owner could not be asked (no question store, or it refused
// the card: full, or over its hourly bound); nothing was written.
var ErrAsk = errors.New("store: the Owner cannot be asked now")

// Card is a decision card for the Owner. A plan change's names its key,
// title and body only, and gets the two options below; a planner's names its
// own options, the one it goes with when nobody answers (1-based), what
// happens meanwhile, which of the Owner's duties it is and the task that asks
// (planresult.go); a mission's (missiondecide.go) names its options, what
// happens meanwhile, the duty and how soon (Urgency), with no default; a
// blocked task's (blockedcard.go) names its options, the one its rules
// recommend (1-based, nothing done on it unanswered) and why (the
// recommended option's detail in the question store), what happens
// meanwhile, the duty, the task and how soon. Its body may change between a
// try that made it and a retry (it says how long ago its mission started):
// SameByKey lets the store's card under its key stand for it when that card
// names the same task and options (review 439 A3: no other card's
// retry may take a card made for other content).
type Card struct {
	Key, Title, Body             string
	Options                      []string
	Recommended                  int
	RecommendedWhy               string
	Meanwhile, OwnerDuty, TaskID string
	Urgency                      string
	SameByKey                    bool
	// ByRule: a card-auto-answer rule in force answers a card of its kind
	// (ruleAnswers, TD-393), so the question store may take it past its
	// quotas: the rule, not the Owner, answers it within seconds.
	ByRule bool
	// WorkID is a business issue's number (BI-<n>); Link, LinkText its page (issuecard.go).
	WorkID, Link, LinkText string
}

// Options of a plan change's card, as the Owner reads them, and the keys
// the question store gives them in order, which an answer carries (review
// 180 A1): the controller applies the change on the first.
const (
	CardApply     = "Apply the change"
	CardRefuse    = "Do not apply it"
	CardApplyKey  = "a"
	CardRefuseKey = "b"
)

// ProposePlanChange records a proposal and, within the standing
// authorization, its decision and the change; otherwise it tries the change
// (and rolls it back), asks the Owner and records which question it is. The
// idempotency key is the writer's.
func ProposePlanChange(ctx context.Context, db DB, w Writer, key, root, reason string, changes []PlanChange, ask Ask) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	if err := checkChanges(root, reason, changes); err != nil {
		return nil, err
	}
	doc, err := changesDoc(changes)
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

func proposePlanChange(ctx context.Context, db DB, w Writer, key, root, reason string, changes []PlanChange, doc any,
	req map[string]any, ask Ask) (map[string]any, error) {
	return idempotentAs(ctx, db, w, "plan/proposals", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, true); err != nil {
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
				return nil, invalid("%s is not the root of a plan", root)
			}
			return nil, err
		}
		// The stream's lock before the rule is read, so a standing decision
		// is made under a rule still in force when it is written.
		if err := lockStream(ctx, tx, PlanStream(root)); err != nil {
			return nil, err
		}
		rule, err := inForce(ctx, tx, rules.KindPlan)
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
			proposed, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_change_proposed", Actor: w.Actor, Body: body})
			if err != nil {
				return nil, err
			}
			decided, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_change_decided", Actor: "controller",
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
		card := Card{Key: canonKey(w.Actor, key), Title: "Plan change to " + root + ": " + firstLine(reason),
			Body: cardBody(root, reason, changes)}
		if card.ByRule, err = ruleAnswers(ctx, tx, CardPlanChange, ""); err != nil {
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
		if card.Recommended, card.RecommendedWhy, err = gatesRecommend(ctx, tx, root, changes); err != nil {
			return nil, err
		}
		if ask == nil {
			return nil, fmt.Errorf("%w: no question store is configured", ErrAsk)
		}
		q, err := ask(ctx, card)
		if errors.Is(err, ErrInvalid) {
			return nil, err
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrAsk, err)
		}
		body["question_id"] = q
		proposed, err := Append(ctx, tx, Event{Stream: PlanStream(root), Type: "plan_change_proposed", Actor: w.Actor, Body: body})
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

func canonKey(actor, key string) string {
	sum, _ := canon.Sum(map[string]any{"actor": actor, "key": key, "route": "plan/proposals"})
	return strings.TrimPrefix(sum, "sha256:")
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func cardBody(root, reason string, changes []PlanChange) string {
	var b strings.Builder
	fmt.Fprintf(&b, "A thread proposes to change plan %s.\n\nWhy: %s\n\nChanges:\n", root, reason)
	for _, c := range changes {
		raw, _ := json.Marshal(c)
		fmt.Fprintf(&b, "- %s\n", raw)
	}
	b.WriteString("\nApplying it writes the change under your decision; not applying it leaves the plan as it is.")
	return b.String()
}
