package coord

import (
	"context"
	"errors"
	"slices"

	"github.com/jackc/pgx/v5"

	writer "github.com/weitingzhao/bifrost-platform/api/internal/agentrt/actor"
	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/eventlog"
)

// Controls through the routes (v5 §5.2: POST /controls, DELETE /controls/{id},
// POST /hosts/{id}/drain): anyone authenticated may stop, pause or drain;
// only the Owner lifts.

// controlKinds are the kinds each scope takes: drain is for hosts (a host
// takes no new work); stop and pause for any scope.
var controlKinds = map[string][]string{
	"global":  {"stop", "pause"},
	"mission": {"stop", "pause"},
	"task":    {"stop", "pause"}, // kept for the events already written; no new one is set (RequestControl)
	"host":    {"stop", "pause", "drain"}, // kept for the events already written; no new one is set (RequestControl)
}

// scopeRows names the row a scope's ref must be.
var scopeRows = map[string]string{
	"mission": `SELECT 1 FROM agentrt.mission WHERE mission_id = $1`,
}

// RequestControl sets a control (SetControl) with a new id, after checking
// its scope, kind and reference. The idempotency key is the writer's: a
// retry gets the same control.
func RequestControl(ctx context.Context, db eventlog.DB, w writer.Writer, key, scope, ref, kind, reason string) (map[string]any, error) {
	if key == "" {
		return nil, eventlog.Invalid("an idempotency key is required")
	}
	kinds, ok := controlKinds[scope]
	switch {
	case !ok:
		return nil, eventlog.Invalid("scope %q is not global, mission, task or host", scope)
	case scope == "task":
		// Nothing runs a task any more (store cut 4, 2026-11-14).
		return nil, eventlog.Invalid("a control of a task is retired: stop its mission")
	case scope == "host":
		// No host takes work any more (the dispatch lane, retired 2026-11-13;
		// measured 2026-11-15: no host control in force on PROD).
		return nil, eventlog.Invalid("a control of a host is retired: no host takes work")
	case !slices.Contains(kinds, kind):
		return nil, eventlog.Invalid("a %s control is one of %v, not %q", scope, kinds, kind)
	case reason == "":
		return nil, eventlog.Invalid("a control says why")
	case (scope == "global") != (ref == ""):
		return nil, eventlog.Invalid("a global control has no scope_ref; any other names its %s", scope)
	}
	req := map[string]any{"scope": scope, "scope_ref": ref, "kind": kind, "reason": reason}
	return writer.IdempotentAs(ctx, db, w, "controls", key, req, nil, func(tx pgx.Tx) (map[string]any, error) {
		if err := writer.Ensure(ctx, tx, w, false); err != nil {
			return nil, err
		}
		writer.Hook("control writer", w.Actor)
		if q := scopeRows[scope]; q != "" {
			var one int
			if err := tx.QueryRow(ctx, q, ref).Scan(&one); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					return nil, eventlog.Invalid("there is no %s %s", scope, ref)
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
func LiftControl(ctx context.Context, db eventlog.DB, actor, id string) (map[string]any, error) {
	var out map[string]any
	err := eventlog.InTx(ctx, db, func(tx pgx.Tx) error {
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

// SetControl records a stop, pause or drain with no token.
func SetControl(ctx context.Context, tx pgx.Tx, actor, controlID, scope, scopeRef, kind, reason string) error {
	return setControl(ctx, tx, actor, "", controlID, scope, scopeRef, kind, reason)
}

// setControl is SetControl recording the token that asked for it (the
// routes; review 176 B2).
func setControl(ctx context.Context, tx pgx.Tx, actor, token, controlID, scope, scopeRef, kind, reason string) error {
	lock := map[string]string{
		"global":  `SELECT pg_advisory_xact_lock(742105) WHERE $1::text IS NOT NULL`,
		"mission": `SELECT 1 FROM agentrt.mission WHERE mission_id = $1 FOR NO KEY UPDATE`,
	}[scope]
	if lock != "" {
		if _, err := tx.Exec(ctx, lock, scopeRef); err != nil {
			return err
		}
	}
	body := map[string]any{"control_id": controlID, "scope": scope, "kind": kind, "reason": reason}
	if scopeRef != "" {
		body["scope_ref"] = scopeRef
	}
	if token != "" {
		body["token"] = token
	}
	ev, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "control", Type: "control_set", Actor: actor, Body: body})
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO agentrt.control (control_id, scope, scope_ref, control_kind, reason, set_by_actor_id)
	  VALUES ($1,$2,$3,$4,$5,$6)`, controlID, scope, eventlog.Nullable(scopeRef), kind, reason, actor); err != nil {
		return err
	}
	return eventlog.Enqueue(ctx, tx, ev.EventID, eventlog.TopicStop, map[string]any{
		"control_id": controlID, "scope": scope, "scope_ref": eventlog.Nullable(scopeRef), "control_kind": kind})
}

// ClearControl lifts a control. Who may clear is the kernel's rule (2.2);
// the store only records it.
func ClearControl(ctx context.Context, tx pgx.Tx, actor, controlID string) error {
	if _, err := eventlog.Append(ctx, tx, eventlog.Event{Stream: "control", Type: "control_cleared", Actor: actor,
		Body: map[string]any{"control_id": controlID}}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE agentrt.control SET cleared_at = clock_timestamp(), cleared_by_actor_id = $2
	  WHERE control_id = $1 AND cleared_at IS NULL`, controlID, actor)
	return err
}
