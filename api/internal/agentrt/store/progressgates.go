package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/weitingzhao/bifrost-platform/api/internal/agentrt/canon"
)

// What became of a gate that has not passed on what its environment runs,
// for the architecture table's reason (TD-397, CUT §5): the latest task of
// its key's batch (gates.go) and what that task left: failed, with the
// runner's verdict line its result's note quotes (or only that it failed:
// no test name is made up); could not run, with why it was cancelled;
// running; not run yet; or not dispatched yet. "" when what the
// environment runs is not recorded (the reason says so already).
func gateStatus(ctx context.Context, tx pgx.Tx, env string, d recordedDeploy, test, spec string) (string, error) {
	if !d.ok {
		return "", nil
	}
	rows, err := tx.Query(ctx, `SELECT (scope_json->'gate_run')::text FROM agentrt.mission WHERE scope_json ? 'gate_run'
	  AND created_by_actor_id = 'controller' AND scope_json->'gate_run'->>'env' = $1 AND scope_json->'gate_run'->>'commit' = $2
	  ORDER BY created_at DESC, mission_id DESC`, env, d.commit)
	if err != nil {
		return "", err
	}
	docs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", err
	}
	task := ""
	for _, doc := range docs {
		v, perr := canon.Parse([]byte(doc))
		if perr != nil {
			continue
		}
		g, ok := parseGateRun(v)
		if !ok || g.Env != env || !sameRef(g.Ref, d.ref) {
			continue
		}
		for _, e := range g.Gates {
			if e.Test == test && e.Spec == spec {
				task = e.Task
			}
		}
		if task != "" {
			break
		}
	}
	if task == "" {
		return "not dispatched yet", nil
	}
	var state, cancelled, note string
	err = tx.QueryRow(ctx, `SELECT t.state,
	    COALESCE((SELECT e.body_json->>'reason' FROM agentrt.event e WHERE e.task_id = t.task_id AND e.event_type = 'task_state_changed'
	      AND e.body_json->>'to' = 'cancelled' ORDER BY e.event_id DESC LIMIT 1), ''),
	    COALESCE((SELECT n.body_json->>'text' FROM agentrt.event n WHERE n.task_id = t.task_id AND n.event_type = 'note'
	      AND starts_with(n.body_json->>'text', $2) ORDER BY n.event_id DESC LIMIT 1), '')
	  FROM agentrt.task t WHERE t.task_id = $1`, task, gateFailPrefix).Scan(&state, &cancelled, &note)
	if errors.Is(err, pgx.ErrNoRows) {
		return "not dispatched yet", nil
	}
	if err != nil {
		return "", err
	}
	switch {
	case state == "cancelled":
		return "could not run: " + strings.TrimPrefix(cancelled, "the gate could not run: "), nil
	case state == "accepted" && note != "":
		// "gate failed: T40 on prod at <commit>: <why>" (GateFailNote).
		_, why, _ := strings.Cut(strings.TrimPrefix(note, gateFailPrefix), ": ")
		return "failed: " + why, nil
	case state == "accepted":
		return "failed, see its evidence", nil
	case state == "ready" || state == "proposed":
		return "not run yet", nil
	}
	return "running", nil
}
