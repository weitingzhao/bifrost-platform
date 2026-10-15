package store

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"
)

// Controls through the routes (v5 §5.2: POST /controls, DELETE /controls/{id},
// POST /hosts/{id}/drain): anyone authenticated may stop, pause or drain;
// only the Owner lifts.

// controlKinds are the kinds each scope takes: drain is for hosts (a host
// takes no new work); stop and pause for any scope.
var controlKinds = map[string][]string{
	"global":  {"stop", "pause"},
	"mission": {"stop", "pause"},
	"task":    {"stop", "pause"},
	"host":    {"stop", "pause", "drain"},
}

// scopeRows names the row a scope's ref must be.
var scopeRows = map[string]string{
	"mission": `SELECT 1 FROM agentrt.mission WHERE mission_id = $1`,
	"task":    `SELECT 1 FROM agentrt.task WHERE task_id = $1`,
	"host":    `SELECT 1 FROM agentrt.host WHERE host_id = $1`,
}

// RequestControl sets a control (SetControl) with a new id, after checking
// its scope, kind and reference. The idempotency key is the writer's: a
// retry gets the same control.
func RequestControl(ctx context.Context, db DB, w Writer, key, scope, ref, kind, reason string) (map[string]any, error) {
	if key == "" {
		return nil, invalid("an idempotency key is required")
	}
	kinds, ok := controlKinds[scope]
	switch {
	case !ok:
		return nil, invalid("scope %q is not global, mission, task or host", scope)
	case !slices.Contains(kinds, kind):
		return nil, invalid("a %s control is one of %v, not %q", scope, kinds, kind)
	case reason == "":
		return nil, invalid("a control says why")
	case (scope == "global") != (ref == ""):
		return nil, invalid("a global control has no scope_ref; any other names its %s", scope)
	}
	req := map[string]any{"scope": scope, "scope_ref": ref, "kind": kind, "reason": reason}
	return idempotent(ctx, db, w.Actor, "controls", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := ensureWriter(ctx, tx, w, false); err != nil {
			return nil, err
		}
		if q := scopeRows[scope]; q != "" {
			var one int
			if err := tx.QueryRow(ctx, q, ref).Scan(&one); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, invalid("there is no %s %s", scope, ref)
				}
				return nil, err
			}
		}
		id := NewControlID()
		if err := setControl(ctx, tx, w.Actor, w.Token, id, scope, ref, kind, reason); err != nil {
			return nil, err
		}
		return map[string]any{"control_id": id, "scope": scope, "scope_ref": ref, "kind": kind}, nil
	})
}

// LiftControl clears a control once. A control already cleared is answered
// as it is, with nothing written; a missing one is pgx.ErrNoRows.
func LiftControl(ctx context.Context, db DB, actor, id string) (map[string]any, error) {
	var out map[string]any
	err := InTx(ctx, db, func(tx pgx.Tx) error {
		var cleared *string
		if err := tx.QueryRow(ctx, `SELECT cleared_by_actor_id FROM agentrt.control WHERE control_id = $1 FOR UPDATE`, id).Scan(&cleared); err != nil {
			return err
		}
		if cleared != nil {
			out = map[string]any{"control_id": id, "cleared": false, "cleared_by": *cleared}
			return nil
		}
		if err := ClearControl(ctx, tx, actor, id); err != nil {
			return err
		}
		out = map[string]any{"control_id": id, "cleared": true, "cleared_by": actor}
		return nil
	})
	return out, err
}
