package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/plan"
)

// The Owner signs a step off (W-31 2.5, the Owner 2026-10-21 「加一个只给我
// 用的「签收这一步」路由和 Console 按钮」): POST /plan/{id}/signoff records the
// owner_signoff event an owner_signoff atom of the step's done rule waits for
// (SignOff). Only the Owner signs: the route takes the admin role and no other,
// and no MCP tool writes it. The step moves on the controller's next rules
// pass, as for any fact.

// OwnerActor is the actor the Owner writes as (an admin token; writer).
const OwnerActor = "owner"

// ErrNotOwner refuses a write only the Owner makes to any other writer.
var ErrNotOwner = errors.New("store: only the Owner may do this")

// SignoffRefs are the references of a rule's owner_signoff atoms, sorted.
func SignoffRefs(r plan.Rule) []string {
	var out []string
	for _, ref := range r.Refs() {
		if ref.Kind == plan.RefSignoff && !slices.Contains(out, ref.ID) {
			out = append(out, ref.ID)
		}
	}
	return out
}

// SignOffStep signs ref of a step off for the Owner and answers the sign-off:
// plan_node_id, ref, by, at, event_id, and already_signed when the step held
// it before (nothing is written then: a second press answers the first). A
// node there is not is pgx.ErrNoRows. Refused: another kind of node, a step
// whose done rule has no owner_signoff atom, a ref the rule does not name, no
// ref when it names more than one (ErrInvalid); a step that is done, stopped
// or dropped (ErrState); a writer who is not the Owner (ErrNotOwner).
func SignOffStep(ctx context.Context, db DB, actor, stepID, ref string) (map[string]any, error) {
	if actor != OwnerActor {
		return nil, fmt.Errorf("%w: %s may not sign a step off", ErrNotOwner, actor)
	}
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		// One sign-off of a step at a time, so a second press reads the first
		// (read committed: each statement sees what committed before it).
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('agentrt.owner_signoff'), hashtext($1))`, stepID); err != nil {
			return err
		}
		var kind, state, doc string
		err := tx.QueryRow(ctx, `SELECT node_kind, COALESCE(state, ''), done_rule_json::text FROM agentrt.plan_node WHERE plan_node_id = $1`,
			stepID).Scan(&kind, &state, &doc)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("store: no plan node %s: %w", stepID, err)
		case err != nil:
			return err
		case kind != "step":
			return invalid("%s is a %s: only a step is signed off", stepID, kind)
		}
		rule, err := plan.Parse([]byte(doc))
		if err != nil {
			return fmt.Errorf("store: node %s: %w", stepID, err)
		}
		refs := SignoffRefs(rule)
		switch {
		case len(refs) == 0:
			return invalid("the done rule of %s waits on no owner_signoff: nothing on it is signed off", stepID)
		case ref == "" && len(refs) > 1:
			return invalid("the done rule of %s waits on %d sign-offs (%s): name the ref", stepID, len(refs), strings.Join(refs, ", "))
		case ref == "":
			ref = refs[0]
		case !slices.Contains(refs, ref):
			return invalid("the done rule of %s waits on no sign-off %q; it waits on %s", stepID, ref, strings.Join(refs, ", "))
		}
		var id int64
		var by string
		var at time.Time
		err = tx.QueryRow(ctx, `SELECT event_id, actor_id, at FROM agentrt.event WHERE event_type = 'owner_signoff' AND plan_node_id = $1
		  AND body_json @> jsonb_build_object('ref', $2::text) ORDER BY event_id LIMIT 1`, stepID, ref).Scan(&id, &by, &at)
		switch {
		case err == nil:
			out = signoffAnswer(stepID, ref, by, at, id, true)
			return nil
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		hook("signoff: unsigned", stepID)
		if state == "done" || state == "stopped" || state == "dropped" {
			return fmt.Errorf("%w: %s is %s; a sign-off would move nothing", ErrState, stepID, state)
		}
		ev, err := signOff(ctx, tx, actor, stepID, ref)
		if err != nil {
			return err
		}
		out = signoffAnswer(stepID, ref, actor, ev.At, ev.EventID, false)
		return nil
	})
	return out, err
}

func signoffAnswer(stepID, ref, by string, at time.Time, id int64, already bool) map[string]any {
	return map[string]any{"plan_node_id": stepID, "ref": ref, "by": by, "at": at.UTC().Format(time.RFC3339Nano),
		"event_id": id, "already_signed": already}
}
